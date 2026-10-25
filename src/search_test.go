package main

import "testing"

func TestParseChannelPage(t *testing.T) {
	html := `<div class="tgme_widget_message_wrap"><div class="tgme_widget_message" data-post="Quark_Movies/1">
<div class="tgme_widget_message_text js-message_text" dir="auto">名称：庆余年 4K<br>链接：<a href="https://pan.quark.cn/s/abc123">点击</a> 提取码：8x2k</div>
<time datetime="2026-10-01T10:00:00+00:00"></time></div></div>`
	items := parseChannelPage("Quark_Movies", html, "庆余年", true)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0]
	if it.ShareID != "abc123" || it.Passcode != "8x2k" || it.Title == "" {
		t.Fatalf("bad parse: %+v", it)
	}
	if _, _, err := parseShareURL("https://pan.quark.cn/s/abc123 提取码：8x2k"); err != nil {
		t.Fatalf("parseShareURL: %v", err)
	}
	// 不相关内容在严格模式下应被过滤
	other := `<div class="tgme_widget_message_wrap"><div class="tgme_widget_message_text js-message_text">最新短剧合集 <a href="https://pan.quark.cn/s/zzz999">链接</a></div></div>`
	if got := parseChannelPage("Quark_Movies", other, "黑猫警长", true); len(got) != 0 {
		t.Fatalf("strict filter failed, got %d", len(got))
	}
	if got := parseChannelPage("Quark_Movies", other, "黑猫警长", false); len(got) != 1 {
		t.Fatalf("non-strict should keep 1, got %d", len(got))
	}
}
