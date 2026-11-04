package main

import (
	"fmt"
	"regexp"
	"strings"
)

// 行指定には数値(5・3-7・70-・-20)のほか、次の書き方を使える
//
//	@名前            関数・メソッド・クラス等の定義(波括弧またはインデントで終わりを判定)
//	/開始/,/終了/    開始パターンを含む行から、その後で最初に終了パターンを含む行まで
const rangeSpecHelp = "70-84 · @init · /start/,/end/"

var markerRange = regexp.MustCompile(`^/(.+)/,/(.+)/$`)

// isRangeSpec は文字列が行指定として有効な形か(行数に依存せず)を調べる
func isRangeSpec(s string) bool {
	if strings.HasPrefix(s, "@") && len(s) > 1 {
		return true
	}
	if markerRange.MatchString(s) {
		return true
	}
	_, _, err := parseLineRange(s)
	return err == nil
}

func matchLine(pat, line string) bool {
	if re, err := regexp.Compile(pat); err == nil {
		return re.MatchString(line)
	}
	return strings.Contains(line, pat)
}

func indentOf(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

// resolveRangeSpec は行指定を (開始, 終了)(1始まり・両端を含む)にする。linesは対象ファイルの全行
func resolveRangeSpec(spec string, lines []string) (int, int, error) {
	if m := markerRange.FindStringSubmatch(spec); m != nil {
		a := -1
		for i, l := range lines {
			if matchLine(m[1], l) {
				a = i
				break
			}
		}
		if a < 0 {
			return 0, 0, fmt.Errorf(T("「%s」を含む行がありません"), m[1])
		}
		for i := a; i < len(lines); i++ {
			if matchLine(m[2], lines[i]) {
				return a + 1, i + 1, nil
			}
		}
		return 0, 0, fmt.Errorf(T("「%s」の後に「%s」を含む行がありません"), m[1], m[2])
	}
	if strings.HasPrefix(spec, "@") && len(spec) > 1 {
		return findDefinition(spec[1:], lines)
	}
	a, b, err := parseLineRange(spec)
	if err != nil {
		return 0, 0, err
	}
	if b == openEnd {
		b = len(lines)
	}
	if a > len(lines) || b > len(lines) {
		return 0, 0, fmt.Errorf(T("%d 行しかありません(指定: %s)"), len(lines), spec)
	}
	return a, b, nil
}

func findDefinition(name string, lines []string) (int, int, error) {
	q := regexp.QuoteMeta(name)
	pats := []*regexp.Regexp{
		regexp.MustCompile(`\b(function\*?|def|func|fn|class|struct|interface|type|sub|proc)\s+(\([^)]*\)\s*)?` + q + `\b`),
		regexp.MustCompile(`\b` + q + `\s*[:=]\s*(async\s*)?(function\b|\([^)]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)`),
		regexp.MustCompile(`^\s*(public|private|protected|static|async|override|\s)*` + q + `\s*\([^)]*\)\s*(:\s*[\w<>\[\], |]+)?\s*\{?\s*$`),
	}
	start := -1
	for i, l := range lines {
		for _, p := range pats {
			if p.MatchString(l) {
				start = i
				break
			}
		}
		if start >= 0 {
			break
		}
	}
	if start < 0 {
		return 0, 0, fmt.Errorf(T("「%s」の定義が見つかりません"), name)
	}
	// 波括弧の対応で終わりを探す(最初の数行に { が無ければインデント方式)
	depth, seen := 0, false
	for i := start; i < len(lines); i++ {
		for _, c := range lines[i] {
			if c == '{' {
				depth++
				seen = true
			} else if c == '}' {
				depth--
			}
		}
		if seen && depth <= 0 {
			return start + 1, i + 1, nil
		}
		if !seen && i >= start+4 {
			break
		}
	}
	base := indentOf(lines[start])
	end := start
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= base {
			break
		}
		end = i
	}
	return start + 1, end + 1, nil
}
