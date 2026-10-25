package main

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

//go:embed webui/index.html webui/app.css webui/app.js
var uiFS embed.FS

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/api/status", a.apiStatus)
	mux.HandleFunc("/api/search", a.apiSearch)
	mux.HandleFunc("/api/save", a.apiSave)
	mux.HandleFunc("/api/resolve", a.apiResolve)
	mux.HandleFunc("/api/settings", a.apiSettings)
	mux.HandleFunc("/api/quark/test", a.apiQuarkTest)
	mux.HandleFunc("/api/quark/folders", a.apiFolders)
	mux.HandleFunc("/api/logs", a.apiLogs)
	mux.HandleFunc("/api/proxy/test", a.apiProxyTest)
	mux.HandleFunc("/api/check", a.apiCheck)
	mux.HandleFunc("/api/image", a.apiImage)
	mux.HandleFunc("/", a.serveUI)
	return mux
}

func (a *App) serveUI(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if name != "index.html" && name != "app.js" && name != "app.css" {
		name = "index.html"
	}
	data, err := uiFS.ReadFile("webui/" + name)
	if name == "index.html" {
		data = []byte(strings.ReplaceAll(string(data), "{{V}}", appVersion))
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype := "text/html; charset=utf-8"
	switch {
	case strings.HasSuffix(name, ".js"):
		ctype = "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		ctype = "text/css; charset=utf-8"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (a *App) apiStatus(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"version": appVersion, "listen": a.listen,
		"cookie_ok": cfg.QuarkCookie != "", "channels": len(cfg.Channels),
		"proxy": cfg.ProxyURL, "save_dir_fid": cfg.SaveDirFID,
	})
}

func (a *App) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		apiErr(w, http.StatusBadRequest, "请输入至少 2 个字的搜索词")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	start := time.Now()
	results, failed, err := a.search(ctx, q)
	if err != nil {
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	a.logf("搜索「%s」：%d 条结果，%d 个频道失败，耗时 %dms", q, len(results), len(failed), time.Since(start).Milliseconds())
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results, "failed": failed, "took_ms": time.Since(start).Milliseconds(),
	})
}

type saveReq struct {
	URL      string `json:"url"`
	Passcode string `json:"passcode"`
	ToFID    string `json:"to_fid"`
}

// apiCheck 检查分享链接是否有效（匿名获取 stoken 即可判断）。
func (a *App) apiCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var req saveReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	pwdID, pass, err := parseShareURL(req.URL)
	if err != nil {
		apiErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Passcode != "" {
		pass = strings.TrimSpace(req.Passcode)
	}
	cfg := a.store.Get()
	q := newQuarkClient(cfg.QuarkCookie, cfg.ProxyURL)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	token, title, err := q.ShareToken(ctx, pwdID, pass)
	if err != nil {
		reason := err.Error()
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "reason": reason, "share_id": pwdID})
		return
	}
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "reason": "无法获取分享信息", "share_id": pwdID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "title": title, "share_id": pwdID})
}

func (a *App) apiResolve(w http.ResponseWriter, r *http.Request) {
	var req saveReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	pwdID, pass, err := parseShareURL(req.URL)
	if err != nil {
		apiErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Passcode != "" {
		pass = strings.TrimSpace(req.Passcode)
	}
	cfg := a.store.Get()
	q := newQuarkClient(cfg.QuarkCookie, cfg.ProxyURL)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	token, title, err := q.ShareToken(ctx, pwdID, pass)
	if err != nil {
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	files, err := q.ShareDetail(ctx, pwdID, token, "0")
	if err != nil {
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": title, "files": files, "share_id": pwdID})
}

func (a *App) apiSave(w http.ResponseWriter, r *http.Request) {
	var req saveReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	cfg := a.store.Get()
	if cfg.QuarkCookie == "" {
		apiErr(w, http.StatusBadRequest, "请先在「设置」里填写夸克 Cookie（浏览器登录 pan.quark.cn 后复制）")
		return
	}
	pwdID, pass, err := parseShareURL(req.URL)
	if err != nil {
		apiErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Passcode != "" {
		pass = strings.TrimSpace(req.Passcode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	q := newQuarkClient(cfg.QuarkCookie, cfg.ProxyURL)
	token, title, err := q.ShareToken(ctx, pwdID, pass)
	if err != nil {
		a.logf("转存失败（获取分享）：%v", err)
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	files, err := q.ShareDetail(ctx, pwdID, token, "0")
	if err != nil {
		a.logf("转存失败（读取文件）：%v", err)
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	toFID := strings.TrimSpace(req.ToFID)
	if toFID == "" {
		toFID = cfg.SaveDirFID
	}
	if toFID == "" {
		toFID = "0"
	}
	taskID, err := q.Save(ctx, pwdID, token, files, toFID)
	if err != nil {
		a.logf("转存失败（提交）：%v", err)
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := q.WaitTask(ctx, taskID); err != nil {
		a.logf("转存失败（任务）：%v", err)
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	a.logf("转存成功：%s（%d 个文件）", title, len(files))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "title": title, "count": len(files), "task_id": taskID})
}

func (a *App) apiSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.store.Get()
		out := cfg
		out.QuarkCookie = maskCookie(cfg.QuarkCookie)
		writeJSON(w, http.StatusOK, out)
	case http.MethodPut:
		var in Settings
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			apiErr(w, http.StatusBadRequest, "参数错误")
			return
		}
		cur := a.store.Get()
		if in.QuarkCookie == "__clear__" {
			in.QuarkCookie = ""
		} else if in.QuarkCookie == "" || strings.Contains(in.QuarkCookie, "***") {
			in.QuarkCookie = cur.QuarkCookie
		}
		if err := a.store.Save(in); err != nil {
			apiErr(w, http.StatusInternalServerError, "保存失败："+err.Error())
			return
		}
		a.logf("设置已更新（频道 %d 个，Cookie %s）", len(in.Channels), maskCookie(in.QuarkCookie))
		out := a.store.Get()
		out.QuarkCookie = maskCookie(out.QuarkCookie)
		writeJSON(w, http.StatusOK, out)
	default:
		apiErr(w, http.StatusMethodNotAllowed, "方法不支持")
	}
}

func (a *App) apiQuarkTest(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Get()
	if cfg.QuarkCookie == "" {
		apiErr(w, http.StatusBadRequest, "还没有填写 Cookie")
		return
	}
	q := newQuarkClient(cfg.QuarkCookie, cfg.ProxyURL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	name, err := q.CheckCookie(ctx)
	if err != nil {
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
}

func (a *App) apiFolders(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.Get()
	if cfg.QuarkCookie == "" {
		apiErr(w, http.StatusBadRequest, "请先填写 Cookie")
		return
	}
	q := newQuarkClient(cfg.QuarkCookie, cfg.ProxyURL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	folders, err := q.ListFolders(ctx, r.URL.Query().Get("fid"))
	if err != nil {
		apiErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (a *App) apiProxyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiErr(w, http.StatusMethodNotAllowed, "方法不支持")
		return
	}
	var req struct {
		ProxyURL string `json:"proxy_url"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	proxy := strings.TrimSpace(req.ProxyURL)
	if proxy == "" {
		proxy = a.store.Get().ProxyURL
	}
	if proxy == "" {
		apiErr(w, http.StatusBadRequest, "还没有配置代理地址")
		return
	}
	if _, err := url.Parse(proxy); err != nil {
		apiErr(w, http.StatusBadRequest, "代理地址格式不正确")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	checks := a.testProxy(ctx, proxy)
	okCount := 0
	for _, c := range checks {
		if c.OK {
			okCount++
		}
	}
	a.logf("代理测试 %s：%d/%d 通过", proxy, okCount, len(checks))
	writeJSON(w, http.StatusOK, map[string]any{"checks": checks, "ok": okCount == len(checks)})
}

func (a *App) apiLogs(w http.ResponseWriter, r *http.Request) {
	n := 200
	if v := r.URL.Query().Get("lines"); v == "500" {
		n = 500
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": a.logs.Tail(n), "version": appVersion})
}

var imageHosts = []string{"telegram-cdn.org", "telesco.pe", "t.me", "telegram.org", "doubanio.com"}

func (a *App) apiImage(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	allowed := false
	for _, h := range imageHosts {
		if u.Hostname() == h || strings.HasSuffix(u.Hostname(), "."+h) {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	client := a.newHTTPClient(20 * time.Second)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, raw, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.0.0")
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 6<<20))
}

// 防止未使用导入
