package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// 言語名 → 拡張子(Web版 app/src/lib/languages.ts と同じ対応)。一覧に無い値は拡張子そのものとして扱う
var languageExts = map[string][]string{
	"typescript": {"ts", "tsx", "mts", "cts"}, "ts": {"ts", "tsx", "mts", "cts"},
	"javascript": {"js", "jsx", "mjs", "cjs"}, "js": {"js", "jsx", "mjs", "cjs"},
	"python": {"py", "pyw", "ipynb"}, "go": {"go"}, "golang": {"go"}, "rust": {"rs"},
	"java": {"java"}, "kotlin": {"kt", "kts"}, "c": {"c", "h"}, "cpp": {"cpp", "cc", "cxx", "hpp", "hh"}, "c++": {"cpp", "cc", "cxx", "hpp", "hh"},
	"csharp": {"cs"}, "c#": {"cs"}, "php": {"php"}, "ruby": {"rb"}, "swift": {"swift"}, "lua": {"lua"},
	"shell": {"sh", "bash", "zsh", "fish"}, "bash": {"sh", "bash"}, "powershell": {"ps1", "psm1"},
	"html": {"html", "htm"}, "css": {"css", "scss", "sass", "less"}, "vue": {"vue"}, "sql": {"sql"},
	"json": {"json", "jsonc"}, "yaml": {"yml", "yaml"}, "toml": {"toml", "ini", "conf"},
	"markdown": {"md", "markdown"}, "docker": {"dockerfile"}, "dockerfile": {"dockerfile"}, "text": {"txt", "log", "csv"},
}

// parseLangs は「python,go」「py」のような指定を拡張子の一覧にする
func parseLangs(v string) []string {
	var exts []string
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		part = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(part), "."))
		if part == "" {
			continue
		}
		list, ok := languageExts[part]
		if !ok {
			list = []string{part}
		}
		for _, e := range list {
			if !seen[e] {
				seen[e] = true
				exts = append(exts, e)
			}
		}
	}
	return exts
}

// parseSince は「24h」「7d」「2w」「3m」「1y」または日付(2026-09-01)を、その時点のRFC3339にする
func parseSince(v string) (string, error) {
	v = strings.TrimSpace(v)
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t.Format(time.RFC3339), nil
	}
	if len(v) >= 2 {
		n, err := strconv.Atoi(v[:len(v)-1])
		if err == nil && n > 0 {
			now := time.Now()
			switch v[len(v)-1] {
			case 'h':
				return now.Add(-time.Duration(n) * time.Hour).Format(time.RFC3339), nil
			case 'd':
				return now.AddDate(0, 0, -n).Format(time.RFC3339), nil
			case 'w':
				return now.AddDate(0, 0, -7*n).Format(time.RFC3339), nil
			case 'm':
				return now.AddDate(0, -n, 0).Format(time.RFC3339), nil
			case 'y':
				return now.AddDate(-n, 0, 0).Format(time.RFC3339), nil
			}
		}
	}
	return "", fmt.Errorf(T("--since には 24h・7d・2w・3m・1y のような期間か、2026-09-01 のような日付を指定してください: %s"), v)
}

var listValueFlags = map[string]string{
	"-u": "user", "--user": "user",
	"-n": "limit", "--limit": "limit",
	"-q": "query", "--query": "query",
	"-l": "lang", "--lang": "lang",
	"--since": "since",
	"--sort":  "sort",
}

// filterFromArgs は -q / -l / --since / --sort を listFilter に変換し、見出し用の説明も返す
func filterFromArgs(p parsedArgs) (listFilter, []string, int) {
	var f listFilter
	var notes []string
	limit := 30
	if v, ok := p.value("limit"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(fmt.Errorf(T("--limit には1以上の数を指定してください")))
		}
		limit = n
	}
	if v, ok := p.value("query"); ok && v != "" {
		f.Query = v
		notes = append(notes, fmt.Sprintf(T("「%s」"), v))
	}
	if v, ok := p.value("lang"); ok && v != "" {
		f.Exts = parseLangs(v)
		notes = append(notes, fmt.Sprintf(T("言語: %s"), v))
	}
	if v, ok := p.value("since"); ok && v != "" {
		s, err := parseSince(v)
		if err != nil {
			fail(err)
		}
		f.Since = s
		notes = append(notes, fmt.Sprintf(T("%s以内"), v))
	}
	if v, ok := p.value("sort"); ok && v != "" {
		switch v {
		case "updated", "created", "oldest":
			f.Sort = v
		default:
			fail(fmt.Errorf(T("--sort には updated / created / oldest のどれかを指定してください")))
		}
		notes = append(notes, map[string]string{"updated": T("更新が新しい順"), "created": T("作成が新しい順"), "oldest": T("作成が古い順")}[v])
	}
	return f, notes, limit
}

func printGistTable(label string, notes []string, items []GistSummary, total int, showOwner bool) {
	if len(notes) > 0 {
		label += dim(" (" + strings.Join(notes, " · ") + ")")
	}
	if len(items) == 0 {
		fmt.Println(bold(label))
		fmt.Println(dim(T("  該当するGistはありません。")))
		return
	}
	fmt.Println(bold(fmt.Sprintf(T("%s %d件"), label, total)))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, g := range items {
		names := make([]string, len(g.Files))
		for i, f := range g.Files {
			names[i] = f.Filename
		}
		owner := ""
		if showOwner && g.Owner.Handle != nil {
			owner = dim("@"+*g.Owner.Handle) + "\t"
		} else if showOwner {
			owner = "\t"
		}
		fmt.Fprintf(w, "  %s\t%s%s\t%s\t%s\t%s\n", cyan(g.ID), owner, bold(gistTitle(g.Title, names)), visibilityLabel(g.Visibility), dim(fmt.Sprintf(Tn("%dファイル", len(g.Files)), len(g.Files))), dim(formatDate(g.UpdatedAt)))
	}
	w.Flush()
	if total > len(items) {
		fmt.Println(dim(fmt.Sprintf(T("  ...他%d件(--limit で件数を指定)"), total-len(items))))
	}
}

// cmdTimeline は全ユーザーの公開Gistを新しい順に表示する(Webの /timeline と同じ)
func cmdTimeline(args []string) {
	p := parseArgs(args, listValueFlags, nil)
	if _, ok := p.value("user"); ok {
		fail(fmt.Errorf(T("特定のユーザーの一覧は bin list -u <handle> を使ってください")))
	}
	f, notes, limit := filterFromArgs(p)
	cfg, session := optionalSession()
	items, total, err := listGists(cfg, session, f, limit, 0)
	if err != nil {
		fail(err)
	}
	printGistTable(T("タイムライン"), notes, items, total, true)
}
