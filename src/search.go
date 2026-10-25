package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type SearchResult struct {
	Title    string `json:"title"`
	Channel  string `json:"channel"`
	URL      string `json:"url"`
	ShareID  string `json:"share_id"`
	Passcode string `json:"passcode"`
	PubDate  string `json:"pub_date"`
	Desc     string `json:"desc"`
	Match    bool   `json:"match"`
	Image    string `json:"image"`
}

var (
	reTextDiv  = regexp.MustCompile(`(?s)<div class="tgme_widget_message_text[^"]*"[^>]*>(.*?)</div>`)
	reQuark    = regexp.MustCompile(`https?://pan\.quark\.cn/s/([A-Za-z0-9]+)`)
	rePasscode = regexp.MustCompile(`(?:提取码|密码|pwd)[：:\s]*([A-Za-z0-9]{4})`)
	reTime     = regexp.MustCompile(`<time[^>]*datetime="([^"]+)"`)
	reTags     = regexp.MustCompile(`<[^>]+>`)
	reImage    = regexp.MustCompile(`(?s)tgme_widget_message_(?:photo_wrap|video_thumb)[^>]*?url\('([^']+)'\)`)
	reSpace    = regexp.MustCompile(`\s+`)
)

func stripHTML(s string) string {
	s = reTags.ReplaceAllString(s, "\n")
	s = html.UnescapeString(s)
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

// matchesKeyword 判断文本是否与关键词相关：空格分词时要求全部命中，否则要求整串包含。
func matchesKeyword(text, keyword string) bool {
	text = strings.ToLower(text)
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return true
	}
	fields := strings.Fields(keyword)
	if len(fields) > 1 {
		for _, f := range fields {
			if !strings.Contains(text, f) {
				return false
			}
		}
		return true
	}
	return strings.Contains(text, keyword)
}

func parseChannelPage(channel, body, keyword string, strict bool) []SearchResult {
	chunks := strings.Split(body, `<div class="tgme_widget_message_wrap`)
	out := []SearchResult{}
	seen := map[string]bool{}
	for _, chunk := range chunks[1:] {
		m := reTextDiv.FindStringSubmatch(chunk)
		if len(m) < 2 {
			continue
		}
		text := m[1]
		links := reQuark.FindAllStringSubmatch(text, -1)
		if len(links) == 0 {
			continue
		}
		plain := stripHTML(text)
		if strict && !matchesKeyword(plain, keyword) {
			continue
		}
		title := plain
		if i := strings.IndexAny(plain, "\n"); i > 0 {
			title = plain[:i]
		}
		title = strings.TrimSpace(title)
		if len([]rune(title)) > 90 {
			title = string([]rune(title)[:90]) + "…"
		}
		desc := plain
		if len([]rune(desc)) > 220 {
			desc = string([]rune(desc)[:220]) + "…"
		}
		pub := ""
		if tm := reTime.FindStringSubmatch(chunk); len(tm) > 1 {
			pub = tm[1]
		}
		image := ""
		if im := reImage.FindStringSubmatch(chunk); len(im) > 1 {
			image = im[1]
		}
		pass := ""
		if pm := rePasscode.FindStringSubmatch(plain); len(pm) > 1 {
			pass = pm[1]
		}
		for _, l := range links {
			id := l[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, SearchResult{
				Title: title, Channel: channel, URL: l[0], ShareID: id,
				Passcode: pass, PubDate: pub, Desc: desc, Image: image,
				Match: strings.Contains(strings.ToLower(title), strings.ToLower(strings.TrimSpace(keyword))),
			})
		}
	}
	return out
}

func (a *App) newHTTPClient(timeout time.Duration) *http.Client {
	cfg := a.store.Get()
	tr := &http.Transport{Proxy: nil}
	if cfg.ProxyURL != "" {
		if u, err := url.Parse(cfg.ProxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

// search 并发检索配置的 Telegram 频道，返回去重后的结果与失败频道。
func (a *App) search(ctx context.Context, keyword string) ([]SearchResult, []string, error) {
	cfg := a.store.Get()
	client := a.newHTTPClient(20 * time.Second)
	var mu sync.Mutex
	results := []SearchResult{}
	failed := []string{}
	seen := map[string]bool{}
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, ch := range cfg.Channels {
		wg.Add(1)
		go func(ch string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u := "https://t.me/s/" + url.PathEscape(ch) + "?q=" + url.QueryEscape(keyword)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.0.0")
			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				failed = append(failed, ch)
				mu.Unlock()
				return
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			if resp.StatusCode != 200 {
				mu.Lock()
				failed = append(failed, ch)
				mu.Unlock()
				return
			}
			items := parseChannelPage(ch, string(body), keyword, cfg.StrictMatch)
			if len(items) == 0 && cfg.StrictMatch {
				// 频道搜索可能没有索引，回退到最近帖子并本地过滤（最多 15 条）
				if items2, ok := a.fallbackLatest(client, ch, keyword); ok {
					items = items2
				}
			}
			mu.Lock()
			for _, it := range items {
				if seen[it.ShareID] {
					continue
				}
				seen[it.ShareID] = true
				results = append(results, it)
			}
			mu.Unlock()
		}(ch)
	}
	wg.Wait()
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Match != results[j].Match {
			return results[i].Match
		}
		return results[i].PubDate > results[j].PubDate
	})
	if len(results) > cfg.MaxResults {
		results = results[:cfg.MaxResults]
	}
	if len(results) == 0 && len(failed) == len(cfg.Channels) {
		return results, failed, fmt.Errorf("所有搜索频道都不可达，请检查代理设置")
	}
	return results, failed, nil
}

// fallbackLatest 拉取频道最近帖子，按关键词本地过滤（用于频道搜索无结果的场景）。
func (a *App) fallbackLatest(client *http.Client, channel, keyword string) ([]SearchResult, bool) {
	u := "https://t.me/s/" + url.PathEscape(channel)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.0.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	items := parseChannelPage(channel, string(body), keyword, true)
	if len(items) > 15 {
		items = items[:15]
	}
	return items, true
}

func parseShareURL(raw string) (pwdID, passcode string, err error) {
	raw = strings.TrimSpace(raw)
	m := reQuark.FindStringSubmatch(raw)
	if len(m) < 2 {
		return "", "", fmt.Errorf("不是有效的夸克分享链接")
	}
	pwdID = m[1]
	if pm := rePasscode.FindStringSubmatch(raw); len(pm) > 1 {
		passcode = pm[1]
	}
	return pwdID, passcode, nil
}
