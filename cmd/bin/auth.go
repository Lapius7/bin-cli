package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Session は~/.config/bin/session.jsonの内容。
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Email        string `json:"email"`
}

func saveSession(s Session) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFilePath(), data, 0600)
}

func loadSession() (*Session, error) {
	data, err := os.ReadFile(sessionFilePath())
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func doLogout() {
	_ = os.Remove(sessionFilePath())
}

// refreshAccessToken はRefreshTokenを使ってアクセストークンを更新し、session(呼び出し元が
// 持っているポインタ)とsession.jsonの両方を新しい値で上書きする。
// アクセストークンはGoTrueのJWT_EXPIRY(現在1時間)で失効するが、これまでこのCLIは
// 401を受け取ると即座に「`bin login`でやり直せ」と案内するだけで、せっかく保存してある
// RefreshTokenを一切使っていなかった(実際に指摘されたバグ)。restRequest/whoamiUserは
// 401を見たらまずこれを試してから、ダメだった場合だけセッション切れ扱いにする。
func refreshAccessToken(cfg Config, session *Session) error {
	base := strings.TrimRight(cfg.SupabaseURL, "/")
	reqBody, _ := json.Marshal(map[string]string{"refresh_token": session.RefreshToken})

	req, err := http.NewRequest(http.MethodPost, base+"/auth/v1/token?grant_type=refresh_token", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", cfg.AnonKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return wrapNetworkError(err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		// RefreshToken自体も失効/失われている場合はここに来る。この場合だけは
		// 本当に再ログインするしかない
		return sessionExpiredError(respBody)
	}

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil || out.AccessToken == "" || out.RefreshToken == "" {
		return fmt.Errorf("トークン更新レスポンスの解析に失敗しました: %s", string(respBody))
	}

	session.AccessToken = out.AccessToken
	session.RefreshToken = out.RefreshToken
	return saveSession(*session)
}

// fetchEmail は取得したアクセストークンでGoTrueに問い合わせ、ユーザーのメールアドレスを取る
// (session.jsonの表示用フィールドを埋めるためだけに使う。ログイン処理自体には不要)。
func fetchEmail(cfg Config, accessToken string) string {
	base := strings.TrimRight(cfg.SupabaseURL, "/")
	req, err := http.NewRequest(http.MethodGet, base+"/auth/v1/user", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("apikey", cfg.AnonKey)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return ""
	}
	var u struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return ""
	}
	return u.Email
}
