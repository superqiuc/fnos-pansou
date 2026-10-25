package main

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type proxyCheck struct {
	Target  string `json:"target"`
	OK      bool   `json:"ok"`
	Status  int    `json:"status"`
	Latency int64  `json:"latency_ms"`
	Error   string `json:"error,omitempty"`
}

// testProxy 通过指定代理访问两个目标：Telegram（搜索必需）与百度（基础连通性）。
func (a *App) testProxy(ctx context.Context, proxyURL string) []proxyCheck {
	targets := []struct {
		name string
		url  string
	}{
		{"Telegram", "https://t.me/s/Quark_Movies"},
		{"百度", "https://www.baidu.com"},
	}
	out := make([]proxyCheck, 0, len(targets))
	tr := &http.Transport{Proxy: nil}
	if proxyURL != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: tr}
	for _, t := range targets {
		c := proxyCheck{Target: t.name}
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		if err != nil {
			c.Error = err.Error()
			out = append(out, c)
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.0.0")
		resp, err := client.Do(req)
		c.Latency = time.Since(start).Milliseconds()
		if err != nil {
			c.Error = err.Error()
			out = append(out, c)
			continue
		}
		resp.Body.Close()
		c.Status = resp.StatusCode
		c.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
		out = append(out, c)
	}
	return out
}
