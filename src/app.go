package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const appVersion = "1.0.0"

type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	file  *os.File
	path  string
}

func NewLogBuffer(path string) *LogBuffer {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	lb := &LogBuffer{file: f, path: path}
	if f != nil {
		if b, err := os.ReadFile(path); err == nil {
			lb.lines = splitLines(string(b), 300)
		}
	}
	return lb
}

func splitLines(s string, max int) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func (l *LogBuffer) Add(msg string) {
	line := time.Now().Format("2006-01-02 15:04:05") + " " + msg
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > 500 {
		l.lines = l.lines[len(l.lines)-500:]
	}
	if l.file != nil {
		fmt.Fprintln(l.file, line)
	}
	l.mu.Unlock()
}

func (l *LogBuffer) Tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.lines) {
		n = len(l.lines)
	}
	out := make([]string, n)
	copy(out, l.lines[len(l.lines)-n:])
	return out
}

type App struct {
	appDir  string
	dataDir string
	listen  string
	store   *SettingsStore
	logs    *LogBuffer
}

func NewApp(appDir, dataDir, listen string) (*App, error) {
	absApp, _ := filepath.Abs(appDir)
	absData, _ := filepath.Abs(dataDir)
	if err := os.MkdirAll(absData, 0o755); err != nil {
		return nil, err
	}
	store, err := NewSettingsStore(filepath.Join(absData, "config.json"))
	if err != nil {
		return nil, err
	}
	s := store.Get()
	if listen == "" {
		listen = s.Listen
	}
	if listen == "" {
		listen = "8909"
	}
	return &App{
		appDir: absApp, dataDir: absData, listen: listen,
		store: store, logs: NewLogBuffer(filepath.Join(absData, "pansou.log")),
	}, nil
}

func (a *App) logf(format string, args ...any) {
	a.logs.Add(fmt.Sprintf(format, args...))
}

func (a *App) Run() error {
	a.logf("网盘搜索已启动（版本 %s，端口 %s）", appVersion, a.listen)
	srv := &http.Server{Addr: ":" + a.listen, Handler: a.routes()}
	return srv.ListenAndServe()
}
