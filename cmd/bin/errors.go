package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// apiErrorMessage はSupabase/PostgREST/Edge Functionが返すJSONエラーボディから、
// 人間が読める短い一文だけを取り出す(生のJSONをそのまま出すと読みづらいため)。
// 既知のフィールド名がどれも無ければ、本文をそのまま(長すぎれば切り詰めて)使う。
func apiErrorMessage(body []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		for _, key := range []string{"message", "msg", "error_description", "error", "hint"} {
			if v, ok := parsed[key].(string); ok && v != "" {
				return v
			}
		}
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		return "(空のレスポンス)"
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// apiError はHTTPステータスコードとレスポンスボディから、整形済みのエラーを作る。
func apiError(statusCode int, body []byte) error {
	return fmt.Errorf("APIエラー(HTTP %d): %s", statusCode, apiErrorMessage(body))
}

// sessionExpiredError はセッション切れ(401)専用の、次に何をすればいいか分かるエラー。
func sessionExpiredError(body []byte) error {
	return fmt.Errorf("セッションが切れています。`bin login` でログインし直してください(%s)", apiErrorMessage(body))
}

// wrapNetworkError はDNS解決失敗・接続拒否など、リクエスト自体が届かなかった場合の
// Goの生エラー("dial tcp: lookup ...: no such host"等)を分かりやすく言い換える。
func wrapNetworkError(err error) error {
	return fmt.Errorf("サーバーに接続できませんでした。ネットワーク接続を確認してください(%w)", err)
}
