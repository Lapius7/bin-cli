package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// defaultSupabaseURL はbin.lapius7.comのCLI専用リバースプロキシ(実体は
// web/bin.lapius7.com/server/main.goの/api/)。このプロキシがsupabase.lapius7.comへの
// 全リクエストにANON_KEYを付与してから中継するため、CLI自体はANON_KEYを一切持たない
// (sca-cliと同じ構成)。
const defaultSupabaseURL = "https://bin.lapius7.com/api"

// defaultSiteURL はGistのURL表示・/raw取得に使うWebサイトのURL。
const defaultSiteURL = "https://bin.lapius7.com"

type Config struct {
	SupabaseURL string
	AnonKey     string
	SiteURL     string
}

func configDir() string {
	if v := os.Getenv("BIN_CONFIG_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bin")
}

func configFilePath() string  { return filepath.Join(configDir(), "config.env") }
func sessionFilePath() string { return filepath.Join(configDir(), "session.json") }

// config.envは通常不要(bin loginだけで動く)。自前のSupabaseインスタンスや開発中の
// サーバーに向けたい場合だけ ~/.config/bin/config.env か同名の環境変数
// (BIN_SUPABASE_URL / BIN_ANON_KEY / BIN_SITE_URL)で上書きする。
func loadConfig() Config {
	cfg := Config{SupabaseURL: defaultSupabaseURL, SiteURL: defaultSiteURL}
	values := map[string]string{}

	if data, err := os.ReadFile(configFilePath()); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(v) != "" {
				values[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	for _, k := range []string{"BIN_SUPABASE_URL", "BIN_ANON_KEY", "BIN_SITE_URL"} {
		if v := os.Getenv(k); v != "" {
			values[k] = v
		}
	}

	if v, ok := values["BIN_SUPABASE_URL"]; ok {
		cfg.SupabaseURL = v
	}
	if v, ok := values["BIN_ANON_KEY"]; ok {
		cfg.AnonKey = v
	}
	if v, ok := values["BIN_SITE_URL"]; ok {
		cfg.SiteURL = strings.TrimRight(v, "/")
	}
	return cfg
}
