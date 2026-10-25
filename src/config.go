package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Settings struct {
	Listen      string   `json:"listen"`
	ProxyURL    string   `json:"proxy_url"`
	QuarkCookie string   `json:"quark_cookie"`
	SaveDirFID  string   `json:"save_dir_fid"`
	Channels    []string `json:"channels"`
	MaxResults  int      `json:"max_results"`
	SearchLimit int      `json:"search_limit"`
	OnlyQuark   bool     `json:"only_quark"`
	StrictMatch bool     `json:"strict_match"`
}

var defaultChannels = []string{
	"Quark_Movies", "quark_share", "yunpan_share", "QuarkFree",
	"SharePanFilms", "leoziyuan", "baicaoZY", "kuakedongman",
	"Q_dianying", "Q_jilupian", "dianying4k", "Netdisk_Movies",
}

func defaultSettings() Settings {
	return Settings{
		Listen:      "8909",
		Channels:    append([]string{}, defaultChannels...),
		MaxResults:  60,
		SearchLimit: 12,
		OnlyQuark:   true,
		StrictMatch: true,
	}
}

type SettingsStore struct {
	mu   sync.RWMutex
	path string
	data Settings
}

func NewSettingsStore(path string) (*SettingsStore, error) {
	st := &SettingsStore{path: path}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &st.data)
		// 旧配置没有 strict_match 字段时默认开启严格匹配
		var probe map[string]json.RawMessage
		if json.Unmarshal(b, &probe) == nil {
			if _, ok := probe["strict_match"]; !ok {
				st.data.StrictMatch = true
			}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		st.data = defaultSettings()
	} else {
		return nil, err
	}
	st.normalize()
	return st, nil
}

func (s *SettingsStore) normalize() {
	d := defaultSettings()
	if s.data.Listen == "" {
		s.data.Listen = d.Listen
	}
	if s.data.MaxResults <= 0 || s.data.MaxResults > 300 {
		s.data.MaxResults = d.MaxResults
	}
	if s.data.SearchLimit <= 0 || s.data.SearchLimit > 50 {
		s.data.SearchLimit = d.SearchLimit
	}
	clean := []string{}
	seen := map[string]bool{}
	for _, c := range s.data.Channels {
		c = strings.TrimSpace(strings.TrimPrefix(c, "@"))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		clean = append(clean, c)
		if len(clean) >= 60 {
			break
		}
	}
	if len(clean) == 0 {
		clean = append([]string{}, d.Channels...)
	}
	s.data.Channels = clean
	s.data.QuarkCookie = strings.TrimSpace(s.data.QuarkCookie)
	s.data.SaveDirFID = strings.TrimSpace(s.data.SaveDirFID)
	if s.data.SaveDirFID == "" {
		s.data.SaveDirFID = "0"
	}
}

func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.data
	out.Channels = append([]string{}, s.data.Channels...)
	return out
}

func (s *SettingsStore) Save(in Settings) error {
	s.mu.Lock()
	s.data = in
	s.normalize()
	data := s.data
	s.mu.Unlock()
	b, _ := json.MarshalIndent(data, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func maskCookie(c string) string {
	if c == "" {
		return ""
	}
	if len(c) <= 12 {
		return "***"
	}
	return c[:6] + "***" + c[len(c)-4:]
}
