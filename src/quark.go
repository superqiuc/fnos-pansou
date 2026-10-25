package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type QuarkClient struct {
	cookie string
	http   *http.Client
}

type QuarkFile struct {
	FID      string `json:"fid"`
	Name     string `json:"file_name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"dir"`
	ShareFID string `json:"share_fid_token"`
}

type QuarkFolder struct {
	FID   string `json:"fid"`
	Name  string `json:"file_name"`
	IsDir bool   `json:"dir"`
}

func newQuarkClient(cookie, proxy string) *QuarkClient {
	tr := &http.Transport{Proxy: nil}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &QuarkClient{cookie: cookie, http: &http.Client{Timeout: 30 * time.Second, Transport: tr}}
}

type quarkResp struct {
	Status  int             `json:"status"`
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (q *QuarkClient) do(ctx context.Context, method, apiURL string, body any) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://pan.quark.cn/")
	req.Header.Set("Content-Type", "application/json")
	if q.cookie != "" {
		req.Header.Set("Cookie", q.cookie)
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求夸克失败：%v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var qr quarkResp
	if err := json.Unmarshal(raw, &qr); err != nil {
		return nil, fmt.Errorf("夸克返回异常（HTTP %d）", resp.StatusCode)
	}
	if qr.Code != 0 {
		msg := qr.Message
		if msg == "" {
			msg = fmt.Sprintf("code=%d", qr.Code)
		}
		if qr.Code == 31001 || qr.Code == 31002 || strings.Contains(msg, "登录") {
			return nil, fmt.Errorf("夸克未登录或 Cookie 失效：%s", msg)
		}
		return nil, fmt.Errorf("夸克接口错误：%s", msg)
	}
	return qr.Data, nil
}

// ShareToken 获取分享的 stoken 与标题（无需登录）。
func (q *QuarkClient) ShareToken(ctx context.Context, pwdID, passcode string) (string, string, error) {
	api := "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc&uc_param_str="
	data, err := q.do(ctx, http.MethodPost, api, map[string]string{"pwd_id": pwdID, "passcode": passcode})
	if err != nil {
		return "", "", err
	}
	var out struct {
		Stoken string `json:"stoken"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Stoken == "" {
		return "", "", fmt.Errorf("无法获取分享信息（可能链接失效）")
	}
	return out.Stoken, out.Title, nil
}

func (q *QuarkClient) ShareDetail(ctx context.Context, pwdID, stoken, pdirFID string) ([]QuarkFile, error) {
	if pdirFID == "" {
		pdirFID = "0"
	}
	api := fmt.Sprintf("https://drive-pc.quark.cn/1/clouddrive/share/sharepage/detail?pr=ucpro&fr=pc&pwd_id=%s&stoken=%s&pdir_fid=%s&force=0&_page=1&_size=200&_fetch_banner=1&_fetch_share=1&_fetch_total=1&_sort=file_type:asc,updated_at:desc",
		url.QueryEscape(pwdID), url.QueryEscape(stoken), url.QueryEscape(pdirFID))
	data, err := q.do(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		List []QuarkFile `json:"list"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out.List, nil
}

// Save 转存分享到指定目录，返回任务 ID。
func (q *QuarkClient) Save(ctx context.Context, pwdID, stoken string, files []QuarkFile, toFID string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("没有可转存的文件")
	}
	if toFID == "" {
		toFID = "0"
	}
	fids := make([]string, 0, len(files))
	tokens := make([]string, 0, len(files))
	for _, f := range files {
		fids = append(fids, f.FID)
		tokens = append(tokens, f.ShareFID)
	}
	payload := map[string]any{
		"fid_list":        fids,
		"fid_token_list":  tokens,
		"to_pdir_fid":     toFID,
		"pwd_id":          pwdID,
		"stoken":          stoken,
		"pdir_fid":        "0",
		"scene":           "link",
		"create_fid_list": []string{},
	}
	api := "https://drive-pc.quark.cn/1/clouddrive/share/sharepage/save?pr=ucpro&fr=pc&uc_param_str="
	data, err := q.do(ctx, http.MethodPost, api, payload)
	if err != nil {
		return "", err
	}
	var out struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(data, &out)
	if out.TaskID == "" {
		return "", fmt.Errorf("转存请求已提交但未返回任务 ID")
	}
	return out.TaskID, nil
}

func (q *QuarkClient) WaitTask(ctx context.Context, taskID string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		api := "https://drive-pc.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&task_id=" + url.QueryEscape(taskID) + "&retry_index=0"
		data, err := q.do(ctx, http.MethodGet, api, nil)
		if err == nil {
			var out struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(data, &out)
			if out.Status == 2 {
				return nil
			}
			if out.Status == 3 {
				if out.Message == "" {
					out.Message = "任务失败"
				}
				return fmt.Errorf("转存任务失败：%s", out.Message)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("转存任务超时")
}

func (q *QuarkClient) ListFolders(ctx context.Context, pdirFID string) ([]QuarkFolder, error) {
	if pdirFID == "" {
		pdirFID = "0"
	}
	api := fmt.Sprintf("https://drive-pc.quark.cn/1/clouddrive/file/sort?pr=ucpro&fr=pc&pdir_fid=%s&_page=1&_size=200&_fetch_total=1&_sort=file_type:asc,file_name:asc", url.QueryEscape(pdirFID))
	data, err := q.do(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		List []QuarkFolder `json:"list"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	folders := []QuarkFolder{}
	for _, f := range out.List {
		if f.Name != "" && f.IsDir {
			folders = append(folders, f)
		}
	}
	return folders, nil
}

// CheckCookie 验证 Cookie 是否有效。
func (q *QuarkClient) CheckCookie(ctx context.Context) (string, error) {
	api := "https://drive-pc.quark.cn/1/clouddrive/member?pr=ucpro&fr=pc&uc_param_str="
	data, err := q.do(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Nickname string `json:"nickname"`
	}
	_ = json.Unmarshal(data, &out)
	if out.Nickname == "" {
		out.Nickname = "已登录"
	}
	return out.Nickname, nil
}
