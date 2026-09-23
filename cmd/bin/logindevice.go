package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// loginViaDeviceCode はaccount.lapius7.comのdevice code方式でログインする。
// loginViaBrowser(ローカルにコールバック待受サーバーを立てる方式)と違い、CLI自身は
// ポートを一切開かない。SSH越しの作業など、CLIを動かしている端末とブラウザを開く端末が
// 別々の場合(ブラウザからCLIのlocalhostに戻ってこれない場合)向け。
//
// 流れ:
//  1. oauth-device-code関数(action=create)を呼び、device_code(CLI用の秘密値)と
//     user_code(人が確認できる短いコード)を受け取る
//  2. ユーザーに `${verification_uri_complete}` を提示する(任意の端末のブラウザで開く)
//  3. ユーザーがLapountでコードを確認し承認するまで、action=pollを一定間隔でポーリングする
//  4. 承認されたら、この関数がその場でaccess_token/refresh_tokenを受け取れる
//     (承認時点でLapount側がCLI専用の独立したセッションを発行し、サーバー側で
//     一度だけ引き渡してくれるため、CLIはブラウザのリダイレクトを待つ必要が無い)
func loginViaDeviceCode(cfg Config) (*Session, error) {
	created, err := createDeviceCode(cfg)
	if err != nil {
		return nil, fmt.Errorf(T("デバイスコードの発行に失敗しました: %w"), err)
	}

	fmt.Printf(T("%s 以下のURLを(別の端末でもかまいません)ブラウザで開き、コードを確認して承認してください:\n"), cyan("→"))
	fmt.Printf("  %s\n", created.VerificationURIComplete)
	fmt.Printf("  %s\n", dim(fmt.Sprintf(T("コード: %s"), created.UserCode)))
	fmt.Printf("  %s\n", dim(fmt.Sprintf(T("(このコードは%d分で期限切れになります)"), created.ExpiresIn/60)))

	interval := time.Duration(created.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(created.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(T("タイムアウトしました(コードの有効期限内に承認が完了しませんでした。もう一度 `bin login --device` からやり直してください)"))
		}
		time.Sleep(interval)

		res, err := pollDeviceCode(cfg, created.DeviceCode)
		if err != nil {
			return nil, fmt.Errorf(T("ログイン状態の確認に失敗しました: %w"), err)
		}
		switch res.Status {
		case "pending":
			continue
		case "denied":
			return nil, fmt.Errorf(T("ログイン要求が拒否されました"))
		case "expired", "invalid":
			return nil, fmt.Errorf(T("コードが無効化されました。もう一度 `bin login --device` からやり直してください"))
		case "already_used":
			return nil, fmt.Errorf(T("このコードは既に使用済みです"))
		case "authorized":
			if res.AccessToken == "" || res.RefreshToken == "" {
				return nil, fmt.Errorf(T("ログインに失敗しました(トークンを受信できませんでした)"))
			}
			email := fetchEmail(cfg, res.AccessToken)
			session := Session{AccessToken: res.AccessToken, RefreshToken: res.RefreshToken, Email: email}
			if err := saveSession(session); err != nil {
				return nil, err
			}
			return &session, nil
		default:
			return nil, fmt.Errorf(T("予期しない状態です: %s"), res.Status)
		}
	}
}

type deviceCreateResult struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func createDeviceCode(cfg Config) (*deviceCreateResult, error) {
	body, _ := json.Marshal(map[string]string{"action": "create"})
	respBody, err := postDeviceFn(cfg, body)
	if err != nil {
		return nil, err
	}
	var out deviceCreateResult
	if err := json.Unmarshal(respBody, &out); err != nil || out.DeviceCode == "" {
		return nil, fmt.Errorf(T("レスポンスの解析に失敗しました: %s"), string(respBody))
	}
	return &out, nil
}

type devicePollResult struct {
	Status       string `json:"status"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func pollDeviceCode(cfg Config, deviceCode string) (*devicePollResult, error) {
	body, _ := json.Marshal(map[string]string{"action": "poll", "device_code": deviceCode})
	respBody, err := postDeviceFn(cfg, body)
	if err != nil {
		return nil, err
	}
	var out devicePollResult
	if err := json.Unmarshal(respBody, &out); err != nil || out.Status == "" {
		return nil, fmt.Errorf(T("レスポンスの解析に失敗しました: %s"), string(respBody))
	}
	return &out, nil
}

func postDeviceFn(cfg Config, body []byte) ([]byte, error) {
	base := strings.TrimRight(cfg.SupabaseURL, "/")
	req, err := http.NewRequest(http.MethodPost, base+"/functions/v1/oauth-device-code", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, apiError(res.StatusCode, respBody)
	}
	return respBody, nil
}
