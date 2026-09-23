package main

import (
	"os"
	"strings"
)

// CLIの表示言語(ja / en / ko)。gettextと同じく、日本語の文字列そのものをキーにして
// T("…") で包み、英語・韓国語は messages.go の対応表から引く(無ければ日本語のまま)。
// 語順が変わる訳文では %[2]s のような位置指定で引数を並べ替える。

var lang = detectLang()

// detectLang は BIN_LANG → LC_ALL → LC_MESSAGES → LANG の順に見る。
// 未設定や C / POSIX(サーバーやWSLの既定でよくある)は日本語、それ以外の未対応言語は英語にする
func detectLang() string {
	for _, key := range []string{"BIN_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if v == "" {
			continue
		}
		base := strings.FieldsFunc(v, func(r rune) bool { return r == '_' || r == '-' || r == '.' || r == '@' })
		if len(base) == 0 {
			continue
		}
		switch base[0] {
		case "ja", "en", "ko":
			return base[0]
		case "c", "posix":
			continue
		default:
			return "en"
		}
	}
	return "ja"
}

// T は日本語の文言(書式文字列)を現在の言語に置き換える
func T(ja string) string {
	if lang == "ja" {
		return ja
	}
	if m, ok := messages[ja]; ok {
		if lang == "en" && m[0] != "" {
			return m[0]
		}
		if lang == "ko" && m[1] != "" {
			return m[1]
		}
	}
	return ja
}

// Tn は件数付きの文言用。英語で件数が1の時だけ単数形(messagesOne)を使う
func Tn(ja string, n int) string {
	if lang == "en" && n == 1 {
		if s, ok := messagesOne[ja]; ok {
			return s
		}
	}
	return T(ja)
}

// 英語の単数形(件数が1の時)
var messagesOne = map[string]string{
	"%dファイル":                        "%d file",
	"%dファイル · %s":                   "%d file · %s",
	"%s 作成しました(%s・%dファイル)\n":        "%s Created (%s, %d file)\n",
	"%s 「%s」をフォークしました(%s・%dファイル)\n": "%s Forked \"%s\" (%s, %d file)\n",
	"%dファイルを %s にダウンロードしました":        "Downloaded %d file to %s",
}
