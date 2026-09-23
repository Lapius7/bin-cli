package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	accountURL = "https://account.lapius7.com"
	// oauth-connect-token関数(mint)が発行するトークンの有効期限と合わせる。
	loginTimeout = 5 * time.Minute
)

// callbackHTML はブラウザに一瞬だけ表示するページ。GoTrueはトークンをURL fragment
// (#access_token=...)で返すが、fragmentはサーバーに送られてこないため、
// このページのJSでlocation.hashを読み取り、ローカルサーバーにPOSTし直す。
const callbackHTML = `<!doctype html>
<html lang="ja"><head><meta charset="utf-8"><title>bin ログイン</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
.box{text-align:center}h1{font-size:1.25rem}</style></head>
<body><div class="box"><h1 id="msg">処理中...</h1></div>
<script>
(function () {
  var params = new URLSearchParams(location.hash.replace(/^#/, ""));
  var accessToken = params.get("access_token");
  var refreshToken = params.get("refresh_token");
  var msg = document.getElementById("msg");
  if (!accessToken || !refreshToken) {
    msg.textContent = "ログインに失敗しました。このタブを閉じてCLIを確認してください。";
    fetch("/callback/complete", {
      method: "POST", headers: {"Content-Type": "application/json"},
      body: JSON.stringify({ error: "no_token" }),
    });
    return;
  }
  fetch("/callback/complete", {
    method: "POST", headers: {"Content-Type": "application/json"},
    body: JSON.stringify({ access_token: accessToken, refresh_token: refreshToken }),
  }).then(function () {
    msg.textContent = "ログインが完了しました。このタブを閉じてください。";
  }).catch(function () {
    msg.textContent = "CLIへの通知に失敗しました。ターミナルを確認してください。";
  });
})();
</script></body></html>`

type callbackResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

// loginViaBrowser はaccount.lapius7.com(Lapount)のSSOハンドオフを使い、
// ローカルにコールバック待受サーバーを立ててブラウザ経由でログインする
// (gh/aws等のCLIと同じ方式)。SERVICE_ROLE_KEYをCLI側に置く必要がない。
func loginViaBrowser(cfg Config) (*Session, error) {
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(callbackHTML))
	})
	mux.HandleFunc("/callback/complete", func(w http.ResponseWriter, r *http.Request) {
		var res callbackResult
		_ = json.NewDecoder(r.Body).Decode(&res)
		w.WriteHeader(http.StatusOK)
		select {
		case resultCh <- res:
		default:
		}
	})

	// ポート0でOSに空いているポートを選ばせる。固定ポートだと第三者が事前に同じ
	// ポートを乗っ取ってフィッシングできてしまう(このCLIはOSSでポート番号も公開
	// されているため)ので、毎回ランダムなポートを使うことでそれを防ぐ。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("ローカルサーバーの起動に失敗しました: %w", err)
	}
	callbackPort := listener.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	redirectTo := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	// 転送先(redirect_to)をURLにそのまま出さず、事前にoauth-connect-token関数で
	// 使い捨てトークンを発行してから開く(見た目・扱いを他のSSO入口と統一するため)。
	// このmint呼び出しもbin-server(/api)経由にすることで、CLIはANON_KEYを一切持たずに済む。
	token, err := mintConnectToken(cfg, redirectTo)
	if err != nil {
		return nil, fmt.Errorf("ログインURLの発行に失敗しました: %w", err)
	}
	authURL := strings.TrimRight(accountURL, "/") + "/oauth/v2/authorize?token=" + url.QueryEscape(token)

	fmt.Printf("%s 以下のURLをクリック(または手動でコピーしてブラウザに貼り付け)して開いてください:\n", cyan("→"))
	fmt.Printf("  %s\n", authURL)
	fmt.Printf("  %s\n", dim(fmt.Sprintf("(このリンクは%d分で期限切れになります)", int(loginTimeout.Minutes()))))

	select {
	case res := <-resultCh:
		if res.Error != "" || res.AccessToken == "" || res.RefreshToken == "" {
			return nil, fmt.Errorf("ログインに失敗しました(トークンを受信できませんでした)")
		}
		email := fetchEmail(cfg, res.AccessToken)
		session := Session{AccessToken: res.AccessToken, RefreshToken: res.RefreshToken, Email: email}
		if err := saveSession(session); err != nil {
			return nil, err
		}
		return &session, nil
	case <-time.After(loginTimeout):
		return nil, fmt.Errorf("タイムアウトしました(%s以内にブラウザでのログインが完了しませんでした。ログインURLの有効期限が切れています。もう一度 `bin login` からやり直してください)", loginTimeout)
	}
}

// mintConnectToken はoauth-connect-token関数(action=mint)を呼び、redirect_toに
// 対応する使い捨てトークンを取得する。bin-server(/api)経由で呼ぶため、CLI自身はANON_KEYを
// 一切送らなくてよい(プロキシがapikeyを付与する)。
func mintConnectToken(cfg Config, redirectTo string) (string, error) {
	base := strings.TrimRight(cfg.SupabaseURL, "/")
	// client_nameはLapount側のダッシュボード(連携サービス一覧・アクティビティ履歴)で
	// 「http://127.0.0.1:xxxx への謎のログイン」ではなく「bin-cli」と分かるようにするための
	// 表示専用ラベル(認可には使われない)。
	body, _ := json.Marshal(map[string]string{"action": "mint", "redirect_to": redirectTo, "client_name": "bin-cli"})

	req, err := http.NewRequest(http.MethodPost, base+"/functions/v1/oauth-connect-token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", wrapNetworkError(err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", apiError(res.StatusCode, respBody)
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("レスポンスの解析に失敗しました: %s", string(respBody))
	}
	return out.Token, nil
}
