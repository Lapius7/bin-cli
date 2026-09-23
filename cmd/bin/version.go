package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// cmdVersion はバージョンを表示し、配布中の最新版(<サイト>/cli/VERSION)と比べる。
// 標準出力は従来どおり「bin <バージョン>」の1行だけにして、比較結果は標準エラーに出す
// (スクリプトから `bin version` の出力を読んでいても壊れないように)。オフライン等で確認できない時は何も言わない
func cmdVersion() {
	fmt.Println("bin " + version)
	latest := fetchLatestVersion(loadConfig().SiteURL)
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
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim(T("更新:")), cyan("curl -fsSL https://bin.lapius7.com/install.sh | bash"))
	}
}

func fetchLatestVersion(siteURL string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(siteURL + "/cli/VERSION")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 200))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}
