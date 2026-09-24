package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restRequest はPostgREST(/rest/v1/...)へのリクエストを共通化するヘルパー。
// schemaを指定すると Accept-Profile/Content-Profile ヘッダーでスキーマを切り替える
// (supabase-jsの`.schema("bin")`と同じ役割)。
func restRequest(cfg Config, session *Session, method, path, schema string, body interface{}) ([]byte, error) {
	respBody, status, err := doRestRequest(cfg, session, method, path, schema, body)
	if err != nil {
		return nil, err
	}
	if status == 401 {
		if session != nil && session.APIToken {
			return nil, fmt.Errorf("%s", T("APIトークンが無効か、期限切れ・失効済みです(BIN_TOKEN を確認してください)"))
		}
		// アクセストークンが期限切れ(GoTrueのJWT_EXPIRY、現在1時間)なだけの可能性が高いので、
		// 保存済みのRefreshTokenでの更新を1回だけ試してから同じリクエストをやり直す。
		// 以前はここで即座に「`bin login`でやり直せ」と案内していたため、1時間おきに
		// 強制再ログインになっていた(実際に指摘されたバグ)。
		if refreshErr := refreshAccessToken(cfg, session); refreshErr != nil {
			return nil, refreshErr
		}
		respBody, status, err = doRestRequest(cfg, session, method, path, schema, body)
		if err != nil {
			return nil, err
		}
	}
	if status == 401 {
		return nil, sessionExpiredError(respBody)
	}
	if status >= 300 {
		return nil, apiError(status, respBody)
	}
	return respBody, nil
}

// anonRequest は未ログインのままPostgRESTを呼ぶ(公開・限定公開Gistの閲覧等)。
func anonRequest(cfg Config, method, path, schema string, body interface{}) ([]byte, error) {
	respBody, status, err := doRestRequest(cfg, nil, method, path, schema, body)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, apiError(status, respBody)
	}
	return respBody, nil
}

func doRestRequest(cfg Config, session *Session, method, path, schema string, body interface{}) ([]byte, int, error) {
	base := strings.TrimRight(cfg.SupabaseURL, "/")

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("apikey", cfg.AnonKey)
	if session != nil {
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	} else if cfg.AnonKey != "" {
		// 未ログイン。既定のbin-server経由ならプロキシがANON_KEYを補うので何も送らない
		req.Header.Set("Authorization", "Bearer "+cfg.AnonKey)
	}
	req.Header.Set("Content-Type", "application/json")
	if schema != "" {
		req.Header.Set("Accept-Profile", schema)
		req.Header.Set("Content-Profile", schema)
	}
	if method == http.MethodPost || method == http.MethodPatch || method == http.MethodDelete {
		req.Header.Set("Prefer", "return=representation")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, wrapNetworkError(err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	return respBody, res.StatusCode, nil
}
