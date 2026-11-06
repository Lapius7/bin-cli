package main

// try.go: ログイン不要の匿名ペースト(/try)。ログイン済みのGistとは別のAPI・別のID空間。
// 編集・削除用トークンは作成時に一度だけ返るので、この端末の try.json(0600)に保存する。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var tryIDPattern = regexp.MustCompile(`^[0-9a-f]{20}$`)

const (
	tryMaxFiles     = 100
	tryMaxFileBytes = 30 * 1024 * 1024
	tryMaxBytes     = 30 * 1024 * 1024
)

type tryPaste struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	CreatedAt string     `json:"created_at"`
	ExpiresAt *string    `json:"expires_at"`
	Files     []GistFile `json:"files"`
}

type tryLocal struct {
	Token     string `json:"token"`
	Title     string `json:"title,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func tryStatePath() string { return filepath.Join(configDir(), "try.json") }

func loadTryState() map[string]tryLocal {
	m := map[string]tryLocal{}
	if data, err := os.ReadFile(tryStatePath()); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func saveTryState(m map[string]tryLocal) {
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(configDir(), 0o700); err == nil {
		_ = os.WriteFile(tryStatePath(), data, 0o600)
	}
}

func parseTryID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/try/"); i >= 0 {
		s = s[i+5:]
		if j := strings.IndexAny(s, "/#?"); j >= 0 {
			s = s[:j]
		}
	}
	s = strings.ToLower(s)
	if !tryIDPattern.MatchString(s) {
		return "", fmt.Errorf(T("匿名ペーストのIDは20桁の英数字(16進)です: %s"), s)
	}
	return s, nil
}

func tryURL(cfg Config, id string) string {
	return strings.TrimRight(cfg.SiteURL, "/") + "/try/" + id
}

func tryCall(cfg Config, method, path string, body any, token string) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(cfg.SiteURL, "/")+"/api/try"+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Try-Token", token)
	}
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return nil, tryError(res.StatusCode, e.Error)
	}
	return data, nil
}

func tryError(status int, code string) error {
	switch {
	case status == 429:
		return fmt.Errorf("%s", T("短時間にリクエストしすぎです。しばらくしてからもう一度お試しください"))
	case status == 403:
		return fmt.Errorf("%s", T("編集用トークンが違います(--token で指定、または作成した端末で実行してください)"))
	case status == 404:
		return fmt.Errorf("%s", T("匿名ペーストが見つかりません(削除されたか、期限切れの可能性があります)"))
	case status == 413:
		return fmt.Errorf("%s", T("内容が大きすぎます(合計30MB・100ファイルまで)"))
	}
	switch code {
	case "file_too_large":
		return fmt.Errorf("%s", T("1ファイル30MBまでです"))
	case "too_many_files":
		return fmt.Errorf("%s", T("ファイルは100個までです"))
	case "paste_too_large":
		return fmt.Errorf("%s", T("合計30MBまでです"))
	case "invalid_ttl":
		return fmt.Errorf("%s", T("保存期間は1〜100日で指定してください"))
	}
	if code == "" {
		code = strconv.Itoa(status)
	}
	return fmt.Errorf(T("匿名ペーストの操作に失敗しました: %s"), code)
}

func checkTryFiles(files []GistFile) {
	if len(files) > tryMaxFiles {
		fail(fmt.Errorf("%s", T("ファイルは100個までです")))
	}
	total := 0
	for _, f := range files {
		n := len(f.Content)
		if n > tryMaxFileBytes {
			fail(fmt.Errorf(T("%s は30MBを超えています"), f.Filename))
		}
		total += n
	}
	if total > tryMaxBytes {
		fail(fmt.Errorf("%s", T("合計30MBまでです")))
	}
}

func tryTokenFor(p parsedArgs, id string) string {
	if t, ok := p.value("token"); ok {
		return t
	}
	if l, ok := loadTryState()[id]; ok {
		return l.Token
	}
	fail(fmt.Errorf("%s", T("この端末に編集用トークンがありません(--token <トークン> で指定)")))
	return ""
}

func getTryPaste(cfg Config, id string) (tryPaste, error) {
	var g tryPaste
	data, err := tryCall(cfg, http.MethodGet, "/"+id, nil, "")
	if err != nil {
		return g, err
	}
	err = json.Unmarshal(data, &g)
	return g, err
}

var tryValueFlags = map[string]string{
	"-t": "title", "--title": "title",
	"-f": "filename", "--filename": "filename", "--as": "filename",
	"--days": "days", "--token": "token", "--remove": "remove",
	"--include": "include", "--exclude": "exclude",
}

func cmdTry(args []string) {
	if len(args) == 0 {
		showHelp([]string{"try"})
		os.Exit(2)
	}
	rest := args[1:]
	switch args[0] {
	case "create", "new", "add":
		cmdTryCreate(rest)
	case "view", "cat", "show":
		cmdTryView(rest)
	case "edit":
		cmdTryEdit(rest)
	case "delete", "rm":
		cmdTryDelete(rest)
	case "list", "ls":
		cmdTryList(rest)
	case "token":
		cmdTryToken(rest)
	default:
		usageExit("bin try <create|view|edit|delete|list|token> ...")
	}
}

func cmdTryCreate(args []string) {
	maxFileBytes = tryMaxFileBytes
	maxGistFiles = tryMaxFiles
	p := parseArgs(foldFileOpts(args, 0), tryValueFlags, nil)
	includeGlobs, excludeGlobs = p.values["include"], p.values["exclude"]
	days := 7
	if v, ok := p.value("days"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			fail(fmt.Errorf("%s", T("保存期間は1〜100日で指定してください")))
		}
		days = n
	}
	stdinName, _ := p.value("filename")
	files := readInputFiles(applyFileOpts(p, p.positional), stdinName, false, true)
	checkTryFiles(files)
	title, _ := p.value("title")
	cfg := loadConfig()
	data, err := tryCall(cfg, http.MethodPost, "", map[string]any{"title": title, "files": files, "ttl_seconds": days * 86400}, "")
	if err != nil {
		fail(err)
	}
	var res struct {
		ID        string  `json:"id"`
		Token     string  `json:"token"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &res); err != nil || res.ID == "" {
		fail(fmt.Errorf("%s", T("サーバーの応答を読めませんでした")))
	}
	st := loadTryState()
	l := tryLocal{Token: res.Token, Title: title}
	if res.ExpiresAt != nil {
		l.ExpiresAt = *res.ExpiresAt
	}
	st[res.ID] = l
	saveTryState(st)
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Filename
	}
	doneCard(T("作成しました(ログイン不要・匿名)"), []cardRow{
		{T("ファイル"), fileList(names)},
		{T("保存期間"), fmt.Sprintf(T("%d日(期限が来ると自動削除)"), days)},
		{T("編集用トークン"), res.Token},
	}, tryURL(cfg, res.ID))
	fmt.Fprintln(os.Stderr, dim(T("トークンはこの端末に保存しました。bin try token <id> で再表示できます")))
}

func cmdTryView(args []string) {
	p := parseArgs(args, map[string]string{"-f": "filename", "--filename": "filename"}, map[string]string{"--raw": "raw"})
	if len(p.positional) != 1 {
		usageExit("bin try view <id|url> [-f <file>] [--raw]")
	}
	id, err := parseTryID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg := loadConfig()
	g, err := getTryPaste(cfg, id)
	if err != nil {
		fail(err)
	}
	if name, ok := p.value("filename"); ok {
		for _, f := range g.Files {
			if f.Filename == name {
				fmt.Print(f.Content)
				return
			}
		}
		fail(fmt.Errorf(T("%s というファイルはありません"), name))
	}
	if p.bools["raw"] || (!stdoutIsTerminal() && len(g.Files) == 1) {
		for _, f := range g.Files {
			fmt.Print(f.Content)
		}
		return
	}
	names := make([]string, len(g.Files))
	for i, f := range g.Files {
		names[i] = f.Filename
	}
	exp := ""
	if g.ExpiresAt != nil {
		exp = "  " + dim(fmt.Sprintf(T("%s に削除"), *g.ExpiresAt))
	}
	fmt.Printf("%s%s\n", bold(gistTitle(g.Title, names)), exp)
	fmt.Println(dim(tryURL(cfg, id)))
	for _, f := range g.Files {
		fmt.Printf("\n%s\n%s", bold("── "+f.Filename), f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			fmt.Println()
		}
	}
}

func cmdTryEdit(args []string) {
	maxFileBytes = tryMaxFileBytes
	maxGistFiles = tryMaxFiles
	p := parseArgs(foldFileOpts(args, 0), tryValueFlags, nil)
	includeGlobs, excludeGlobs = p.values["include"], p.values["exclude"]
	if len(p.positional) < 1 {
		usageExit("bin try edit <id|url> [<file|dir>...] [-t <title>] [--remove <path>] [--token <token>]")
	}
	id, err := parseTryID(p.positional[0])
	if err != nil {
		fail(err)
	}
	token := tryTokenFor(p, id)
	cfg := loadConfig()
	g, err := getTryPaste(cfg, id)
	if err != nil {
		fail(err)
	}
	stdinName, _ := p.value("filename")
	var added []GistFile
	if paths := applyFileOpts(p, p.positional[1:]); len(paths) > 0 {
		added = readInputFiles(paths, stdinName, false, false)
	}
	files := g.Files
	for _, r := range p.values["remove"] {
		out := files[:0:0]
		found := false
		for _, f := range files {
			if f.Filename == r {
				found = true
				continue
			}
			out = append(out, f)
		}
		if !found {
			fail(fmt.Errorf(T("%s というファイルはありません"), r))
		}
		files = out
	}
	for _, a := range added {
		replaced := false
		for i := range files {
			if files[i].Filename == a.Filename {
				files[i] = a
				replaced = true
			}
		}
		if !replaced {
			files = append(files, a)
		}
	}
	title := g.Title
	if t, ok := p.value("title"); ok {
		title = t
	}
	if len(files) == 0 {
		fail(fmt.Errorf("%s", T("ファイルが1つも残りません")))
	}
	checkTryFiles(files)
	if _, err := tryCall(cfg, http.MethodPut, "/"+id, map[string]any{"title": title, "files": files}, token); err != nil {
		fail(err)
	}
	st := loadTryState()
	if l, ok := st[id]; ok {
		l.Title = title
		st[id] = l
		saveTryState(st)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Filename
	}
	doneCard(T("更新しました"), []cardRow{{T("ファイル"), fileList(names)}}, tryURL(cfg, id))
}

func cmdTryDelete(args []string) {
	p := parseArgs(args, map[string]string{"--token": "token"}, map[string]string{"-y": "yes", "--yes": "yes"})
	if len(p.positional) != 1 {
		usageExit("bin try delete <id|url> [-y] [--token <token>]")
	}
	id, err := parseTryID(p.positional[0])
	if err != nil {
		fail(err)
	}
	token := tryTokenFor(p, id)
	if !p.bools["yes"] && !confirm(T("匿名ペースト %s を削除しますか?元に戻せません。"), id) {
		fmt.Println(T("キャンセルしました。"))
		return
	}
	if _, err := tryCall(loadConfig(), http.MethodDelete, "/"+id, nil, token); err != nil {
		fail(err)
	}
	st := loadTryState()
	delete(st, id)
	saveTryState(st)
	success(T("削除しました"))
}

func cmdTryList(args []string) {
	_ = parseArgs(args, nil, nil)
	st := loadTryState()
	if len(st) == 0 {
		fmt.Println(T("この端末で作成した匿名ペーストはありません"))
		return
	}
	cfg := loadConfig()
	ids := make([]string, 0, len(st))
	for id := range st {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l := st[id]
		title := l.Title
		if title == "" {
			title = T("(無題)")
		}
		exp := ""
		if t, err := time.Parse(time.RFC3339, l.ExpiresAt); err == nil {
			if time.Now().After(t) {
				exp = dim(T("期限切れ"))
			} else {
				exp = dim(t.Local().Format("2006-01-02") + " " + T("まで"))
			}
		}
		fmt.Printf("%s  %s  %s\n", cyan(tryURL(cfg, id)), title, exp)
	}
}

func cmdTryToken(args []string) {
	p := parseArgs(args, nil, nil)
	if len(p.positional) == 0 {
		st := loadTryState()
		if len(st) == 0 {
			fmt.Println(T("この端末で作成した匿名ペーストはありません"))
			return
		}
		ids := make([]string, 0, len(st))
		for id := range st {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("%s  %s\n", id, st[id].Token)
		}
		return
	}
	if len(p.positional) != 1 {
		usageExit("bin try token [<id|url>]")
	}
	id, err := parseTryID(p.positional[0])
	if err != nil {
		fail(err)
	}
	l, ok := loadTryState()[id]
	if !ok {
		fail(fmt.Errorf("%s", T("この端末に編集用トークンがありません(--token <トークン> で指定)")))
	}
	fmt.Println(l.Token)
}
