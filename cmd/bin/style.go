package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// colorEnabled はNO_COLOR環境変数やTTY判定に基づき、色付き出力を使うかどうかを決める
// (パイプ/リダイレクト時に生のANSIコードが混ざらないようにするための一般的な作法)。
var colorEnabled = detectColorSupport()

func detectColorSupport() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)

func style(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string    { return style(ansiBold, s) }
func dim(s string) string     { return style(ansiDim, s) }
func red(s string) string     { return style(ansiRed, s) }
func green(s string) string   { return style(ansiGreen, s) }
func yellow(s string) string  { return style(ansiYellow, s) }
func blue(s string) string    { return style(ansiBlue, s) }
func magenta(s string) string { return style(ansiMagenta, s) }
func cyan(s string) string    { return style(ansiCyan, s) }

// displayWidth は全角文字(日本語含む)を2セル、それ以外を1セルとして数える簡易実装。
// text/tabwriterはrune数=1固定で幅を数えてしまい、日本語ラベルが混ざる出力の桁揃えが
// 崩れるため、`printField`ではこちらで揃える。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && (r <= 0x115F ||
			r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE6F) ||
			(r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)):
			w += 2
		default:
			w++
		}
	}
	return w
}

func padLabel(label string, target int) string {
	w := displayWidth(label)
	if w >= target {
		return label
	}
	return label + spaces(target-w)
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

// printField はラベル(日本語混在)を固定幅で揃えて1行出力する共通ヘルパー。
// labelが空文字列の場合はラベル列ごと空けて値だけをぶら下げる(MFA factorの2件目以降など)。
func printField(label, value string) {
	if label == "" {
		fmt.Printf("  %s %s\n", spaces(labelWidth), value)
		return
	}
	fmt.Printf("  %s %s\n", dim(padLabel(label, labelWidth)), value)
}

const labelWidth = 14

// sectionTitle はアイコン付きの太字見出しを表示する。
func sectionTitle(icon, title string) {
	fmt.Printf("%s %s\n", icon, bold(title))
}

// providerMeta は連携プロバイダごとの表示名とバッジ色。
type providerMeta struct {
	label string
	color string
}

var providerMetas = map[string]providerMeta{
	"google":  {"Google", ansiBlue},
	"github":  {"GitHub", ansiDim},
	"discord": {"Discord", ansiMagenta},
	"twitch":  {"Twitch", ansiYellow},
	"twitter": {"X (Twitter)", ansiCyan},
	"spotify": {"Spotify", ansiGreen},
}

func providerLabel(provider string) string {
	if m, ok := providerMetas[provider]; ok {
		return m.label
	}
	return provider
}

// providerBadge は「●プロバイダ名」の色付きバッジ文字列を返す。
func providerBadge(provider string) string {
	color := ansiDim
	if m, ok := providerMetas[provider]; ok {
		color = m.color
	}
	return style(color, "●") + " " + providerLabel(provider)
}

func success(format string, args ...interface{}) {
	fmt.Printf("%s %s\n", green("✓"), fmt.Sprintf(format, args...))
}

func warn(format string, args ...interface{}) {
	fmt.Printf("%s %s\n", yellow("!"), fmt.Sprintf(format, args...))
}

func step(format string, args ...interface{}) {
	fmt.Printf("%s %s\n", cyan("→"), fmt.Sprintf(format, args...))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "%s %s\n", red("✗"), err)
	os.Exit(1)
}

// confirm はy/nの確認プロンプトを表示し、"y"/"yes"(大文字小文字を無視)の場合のみtrueを返す。
// 破壊的な操作(ルーム削除等)の前に必ず挟む。
func confirm(format string, args ...interface{}) bool {
	fmt.Printf("%s %s %s", yellow("?"), fmt.Sprintf(format, args...), dim("[y/N]: "))
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
