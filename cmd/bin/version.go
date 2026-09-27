package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// channel は配布経路。npm 版は -ldflags "-X main.channel=npm" で埋め込み、
// 最新版の確認先と更新方法の案内を npm に切り替える(サイト配布版とは版番号の体系が違うため)
var channel = ""

const npmPackage = "@lapius/bin-cli"

// cmdVersion はバージョンを表示し、配布中の最新版(<サイト>/cli/VERSION、npm 版は npm レジストリ)と比べる。
// 標準出力は従来どおり「bin <バージョン>」の1行だけにして、比較結果は標準エラーに出す
// (スクリプトから `bin version` の出力を読んでいても壊れないように)。オフライン等で確認できない時は何も言わない
func cmdVersion() {
	fmt.Println("bin " + version)
	var latest, update string
	if channel == "npm" {
		latest = fetchNpmLatest(npmPackage)
		update = "npm i -g " + npmPackage
	} else {
		latest = fetchLatestVersion(loadConfig().SiteURL)
		update = "curl -fsSL https://bin.lapius7.com/install.sh | bash"
	}
	if latest == "" {
		return
	}
	switch {
	case latest == version:
		fmt.Fprintln(os.Stderr, green("✓ ")+T("最新版です"))
	case version == "dev":
		fmt.Fprintf(os.Stderr, dim(T("開発版です(配布中の最新版: %s)"))+"\n", latest)
	default:
		fmt.Fprintf(os.Stderr, yellow("! ")+T("新しいバージョン %s が配布されています")+"\n", bold(latest))
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim(T("更新:")), cyan(update))
	}
}

func fetchLatestVersion(siteURL string) string {
	body := httpGet(siteURL+"/cli/VERSION", 200)
	return strings.TrimSpace(string(body))
}

func fetchNpmLatest(pkg string) string {
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(httpGet("https://registry.npmjs.org/"+pkg+"/latest", 64<<10), &v) != nil {
		return ""
	}
	return v.Version
}

func httpGet(url string, limit int64) []byte {
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil
	}
	return body
}
