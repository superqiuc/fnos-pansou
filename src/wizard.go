package main

import (
	"os"
	"strings"
)

// applyWizard 把 fnOS 安装/设置向导里的字段写入配置。
func applyWizard(dataDir string) error {
	store, err := NewSettingsStore(dataDir + "/config.json")
	if err != nil {
		return err
	}
	s := store.Get()
	if v := os.Getenv("wizard_quark_cookie"); v != "" {
		s.QuarkCookie = strings.TrimSpace(v)
	}
	if v := os.Getenv("wizard_proxy_url"); v != "" {
		s.ProxyURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("wizard_channels"); strings.TrimSpace(v) != "" {
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == ' ' })
		s.Channels = parts
	}
	return store.Save(s)
}
