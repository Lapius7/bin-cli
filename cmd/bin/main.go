package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"
)

// version はビルド時に -ldflags "-X main.version=..." で埋め込む
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	args := os.Args[2:]

	switch os.Args[1] {
	case "login":
		cmdLogin(args)
	case "logout":
		doLogout()
		success("ログアウトしました。")
	case "whoami":
		cmdWhoami()
	case "create", "new":
		cmdCreate(args)
	case "list", "ls":
		cmdList(args)
	case "view", "cat", "show":
		cmdView(args)
	case "edit":
		cmdEdit(args)
	case "delete", "rm":
		cmdDelete(args)
	case "clone", "download":
		cmdClone(args)
	case "version", "--version", "-v":
		fmt.Println("bin " + version)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Printf("%s %s\n\n", bold("bin"), dim("bin.lapius7.com CLIクライアント "+version))

	printUsageSection("認証", [][2]string{
		{"bin login", "ブラウザでLapount(account.lapius7.com)にログインする"},
		{"bin login --device", "デバイスコード方式でログインする(SSH越し等、手元でブラウザが開けない場合)"},
		{"bin logout", "ローカルのセッションを破棄する"},
		{"bin whoami", "ログイン中のユーザーを表示する"},
	})
	printUsageSection("Gist", [][2]string{
		{"bin create <file>...", "ファイルからGistを作成する(標準入力からも可: cat x | bin create -f x.txt)"},
		{"bin list [-u <handle>]", "自分(または指定ユーザー)のGist一覧"},
		{"bin view <id> [-f <file>]", "内容を表示する(-fで1ファイルだけ生出力)"},
		{"bin edit <id> [<file>...]", "ファイルを追加・上書きする(--remove <file>で削除)"},
		{"bin clone <id> [<dir>]", "Gistのファイルをディレクトリにダウンロードする"},
		{"bin delete <id>", "削除する(確認あり、-yで省略)"},
	})
	printUsageSection("オプション(create/edit)", [][2]string{
		{"-t, --title <text>", "タイトル"},
		{"-d, --description <text>", "説明"},
		{"-f, --filename <name>", "標準入力から読み込む時のファイル名"},
		{"--public / --unlisted / --private", "公開範囲(作成時の既定は --unlisted)"},
	})
	fmt.Println(dim("<id> にはGistのURL(https://bin.lapius7.com/<id>)もそのまま指定できます。"))
}

func printUsageSection(title string, rows [][2]string) {
	fmt.Println(bold(title))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintf(w, "  %s\t%s\n", cyan(r[0]), r[1])
	}
	w.Flush()
	fmt.Println()
}

// ---- 引数パーサー ----
// 標準のflagパッケージは最初の位置引数でフラグ解析を止めてしまい、
// `bin create main.go --public` のような自然な書き方ができないため、自前で解析する。

type parsedArgs struct {
	positional []string
	values     map[string][]string
	bools      map[string]bool
}

func parseArgs(args []string, valueFlags map[string]string, boolFlags map[string]string) parsedArgs {
	p := parsedArgs{values: map[string][]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			p.positional = append(p.positional, args[i+1:]...)
			break
		}
		name, inline, hasInline := strings.Cut(a, "=")
		if key, ok := valueFlags[name]; ok {
			if hasInline {
				p.values[key] = append(p.values[key], inline)
			} else if i+1 < len(args) {
				p.values[key] = append(p.values[key], args[i+1])
				i++
			} else {
				fail(fmt.Errorf("%s には値が必要です", name))
			}
			continue
		}
		if key, ok := boolFlags[a]; ok {
			p.bools[key] = true
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			fail(fmt.Errorf("不明なオプションです: %s(`bin help` で使い方を表示)", a))
		}
		p.positional = append(p.positional, a)
	}
	return p
}

func (p parsedArgs) value(key string) (string, bool) {
	v := p.values[key]
	if len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

var gistValueFlags = map[string]string{
	"-t": "title", "--title": "title",
	"-d": "description", "--description": "description",
	"-f": "filename", "--filename": "filename",
	"--remove": "remove",
}

var visibilityFlags = map[string]string{
	"--public": "public", "--unlisted": "unlisted", "--private": "private",
}

func pickVisibility(p parsedArgs) (string, bool) {
	found := ""
	for _, v := range []string{"public", "unlisted", "private"} {
		if p.bools[v] {
			if found != "" {
				fail(fmt.Errorf("公開範囲の指定は1つだけにしてください"))
			}
			found = v
		}
	}
	return found, found != ""
}

// ---- 共通ヘルパー ----

func requireSession() (Config, *Session) {
	cfg := loadConfig()
	session, err := loadSession()
	if err != nil {
		fail(fmt.Errorf("ログインしていません。先に `bin login` を実行してください"))
	}
	return cfg, session
}

// optionalSession はログインしていれば使う(自分の非公開Gistも読めるようにするため)。
func optionalSession() (Config, *Session) {
	cfg := loadConfig()
	session, err := loadSession()
	if err != nil {
		return cfg, nil
	}
	return cfg, session
}

// userIDFromToken はアクセストークン(JWT)のsubを取り出す(署名検証はサーバー側で行われる)。
func userIDFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(payload, &claims)
	return claims.Sub
}

func stdinIsPiped() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice == 0
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// readInputFiles は位置引数のファイルと(パイプされていれば)標準入力を読み込む。
func readInputFiles(paths []string, stdinName string, allowEmpty bool) []GistFile {
	var files []GistFile
	for _, path := range paths {
		if path == "-" {
			files = append(files, readStdinFile(stdinName))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fail(fmt.Errorf("ファイルを読み込めません: %w", err))
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			fail(fmt.Errorf("%s はテキストファイルではないため追加できません", path))
		}
		files = append(files, GistFile{Filename: filepath.Base(path), Content: string(data)})
	}
	if len(paths) == 0 && stdinIsPiped() {
		files = append(files, readStdinFile(stdinName))
	}
	if len(files) == 0 && !allowEmpty {
		fail(fmt.Errorf("ファイルを指定するか、標準入力から渡してください(例: cat main.go | bin create -f main.go)"))
	}
	return files
}

func readStdinFile(name string) GistFile {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1024*1024+1))
	if err != nil {
		fail(fmt.Errorf("標準入力を読み込めません: %w", err))
	}
	if len(data) > 1024*1024 {
		fail(fmt.Errorf("1ファイルあたり1MBまでです"))
	}
	if !utf8.Valid(data) {
		fail(fmt.Errorf("標準入力の内容がテキスト(UTF-8)ではありません"))
	}
	if name == "" {
		name = "stdin.txt"
	}
	return GistFile{Filename: name, Content: string(data)}
}

func gistURL(cfg Config, id string) string {
	return cfg.SiteURL + "/" + id
}

func formatDate(iso string) string {
	if t, err := time.Parse(time.RFC3339Nano, iso); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return iso
}

func usageExit(usage string) {
	fmt.Fprintln(os.Stderr, "usage: "+usage)
	os.Exit(2)
}

// ---- コマンド ----

func cmdLogin(args []string) {
	cfg := loadConfig()
	p := parseArgs(args, nil, map[string]string{"--device": "device"})

	var err error
	if p.bools["device"] {
		_, err = loginViaDeviceCode(cfg)
	} else {
		_, err = loginViaBrowser(cfg)
	}
	if err != nil {
		fail(err)
	}
	success("ログインしました")
	cmdWhoami()
}

type myProfile struct {
	Handle      *string `json:"handle"`
	DisplayName *string `json:"display_name"`
}

func fetchMyProfile(cfg Config, session *Session) (string, *myProfile, error) {
	userID := userIDFromToken(session.AccessToken)
	body, err := restRequest(cfg, session, http.MethodGet, "/rest/v1/user_profiles?select=handle,display_name&user_id=eq."+url.QueryEscape(userID), "", nil)
	if err != nil {
		return userID, nil, err
	}
	var rows []myProfile
	_ = json.Unmarshal(body, &rows)
	if len(rows) == 0 {
		return userID, nil, nil
	}
	return userID, &rows[0], nil
}

func cmdWhoami() {
	cfg, session := requireSession()
	_, profile, err := fetchMyProfile(cfg, session)
	if err != nil {
		fail(err)
	}
	name, handle := "", ""
	if profile != nil {
		if profile.DisplayName != nil {
			name = *profile.DisplayName
		}
		if profile.Handle != nil {
			handle = *profile.Handle
		}
	}
	if name == "" {
		name = session.Email
	}
	if handle != "" {
		fmt.Printf("%s %s\n", bold(name), dim("@"+handle))
	} else {
		fmt.Printf("%s %s\n", bold(name), dim("(ハンドル名未設定)"))
	}
	if session.Email != "" {
		fmt.Println(dim(session.Email))
	}
	if handle != "" {
		fmt.Println(dim(cfg.SiteURL + "/u/" + handle))
	}
}

func cmdCreate(args []string) {
	p := parseArgs(args, gistValueFlags, visibilityFlags)
	cfg, session := requireSession()
	stdinName, _ := p.value("filename")
	files := readInputFiles(p.positional, stdinName, false)

	visibility, ok := pickVisibility(p)
	if !ok {
		visibility = "unlisted"
	}
	title, _ := p.value("title")
	description, _ := p.value("description")

	id, err := saveGist(cfg, session, nil, title, description, visibility, files)
	if err != nil {
		fail(err)
	}
	// URLだけを標準出力に出す(`url=$(bin create x.go)` のように使えるように)。装飾はstderrへ
	fmt.Fprintf(os.Stderr, "%s 作成しました(%s・%dファイル)\n", green("✓"), visibilityLabel(visibility), len(files))
	fmt.Println(gistURL(cfg, id))
}

func cmdList(args []string) {
	p := parseArgs(args, map[string]string{"-u": "user", "--user": "user", "-n": "limit", "--limit": "limit"}, nil)
	limit := 30
	if v, ok := p.value("limit"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(fmt.Errorf("--limit には1以上の数を指定してください"))
		}
		limit = n
	}

	var cfg Config
	var session *Session
	var owner, label string
	if handle, ok := p.value("user"); ok {
		cfg, session = optionalSession()
		handle = strings.TrimPrefix(handle, "@")
		id, err := resolveHandle(cfg, handle)
		if err != nil {
			fail(err)
		}
		owner, label = id, "@"+handle+" のGist"
	} else {
		cfg, session = requireSession()
		owner, label = userIDFromToken(session.AccessToken), "自分のGist"
	}

	items, total, err := listGists(cfg, session, owner, limit, 0)
	if err != nil {
		fail(err)
	}
	if len(items) == 0 {
		fmt.Println(dim("Gistがありません。") + cyan(" `bin create <file>`") + dim(" で作成できます。"))
		return
	}
	fmt.Println(bold(fmt.Sprintf("%s (%d)", label, total)))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, g := range items {
		names := make([]string, len(g.Files))
		for i, f := range g.Files {
			names[i] = f.Filename
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", cyan(g.ID), bold(gistTitle(g.Title, names)), visibilityLabel(g.Visibility), dim(formatDate(g.UpdatedAt)))
	}
	w.Flush()
	if total > len(items) {
		fmt.Println(dim(fmt.Sprintf("  ...他%d件(--limit で件数を指定)", total-len(items))))
	}
}

func resolveHandle(cfg Config, handle string) (string, error) {
	body, err := anonRequest(cfg, http.MethodGet, "/rest/v1/user_profiles?select=user_id&handle=ilike."+url.QueryEscape(escapeLike(handle)), "", nil)
	if err != nil {
		return "", err
	}
	var rows []struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(body, &rows)
	if len(rows) == 0 {
		return "", fmt.Errorf("@%s というユーザーは見つかりません", handle)
	}
	return rows[0].UserID, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func cmdView(args []string) {
	p := parseArgs(args, map[string]string{"-f": "filename", "--filename": "filename"}, map[string]string{"--raw": "raw"})
	if len(p.positional) != 1 {
		usageExit("bin view <id> [-f <file>] [--raw]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := optionalSession()
	g, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}

	// -f指定時、またはパイプ先への出力で1ファイルだけの場合は、中身だけをそのまま出す
	if name, ok := p.value("filename"); ok {
		for _, f := range g.Files {
			if f.Filename == name {
				fmt.Print(f.Content)
				return
			}
		}
		fail(fmt.Errorf("%s というファイルはありません", name))
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
	owner := ""
	if g.Owner.Handle != nil {
		owner = "@" + *g.Owner.Handle + " / "
	}
	fmt.Printf("%s%s  %s\n", dim(owner), bold(gistTitle(g.Title, names)), visibilityLabel(g.Visibility))
	if g.Description != "" {
		fmt.Println(g.Description)
	}
	fmt.Println(dim(fmt.Sprintf("%s · 更新 %s · リビジョン %d", gistURL(cfg, g.ID), formatDate(g.UpdatedAt), g.RevisionCount)))
	for _, f := range g.Files {
		fmt.Printf("\n%s %s\n", cyan("──"), bold(f.Filename))
		fmt.Print(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			fmt.Println()
		}
	}
}

func cmdEdit(args []string) {
	p := parseArgs(args, gistValueFlags, visibilityFlags)
	if len(p.positional) < 1 {
		usageExit("bin edit <id> [<file>...] [--remove <file>] [-t <title>] [-d <description>] [--public|--unlisted|--private]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := requireSession()
	g, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}
	if g.Owner.ID != userIDFromToken(session.AccessToken) {
		fail(fmt.Errorf("自分のGistではないため編集できません"))
	}

	stdinName, _ := p.value("filename")
	updates := readInputFiles(p.positional[1:], stdinName, true)
	removes := p.values["remove"]

	files := g.Files
	for _, u := range updates {
		replaced := false
		for i := range files {
			if files[i].Filename == u.Filename {
				files[i].Content = u.Content
				replaced = true
			}
		}
		if !replaced {
			files = append(files, u)
		}
	}
	for _, name := range removes {
		kept := files[:0]
		found := false
		for _, f := range files {
			if f.Filename == name {
				found = true
				continue
			}
			kept = append(kept, f)
		}
		if !found {
			fail(fmt.Errorf("%s というファイルはありません", name))
		}
		files = kept
	}

	title, description, visibility := g.Title, g.Description, g.Visibility
	if v, ok := p.value("title"); ok {
		title = v
	}
	if v, ok := p.value("description"); ok {
		description = v
	}
	if v, ok := pickVisibility(p); ok {
		visibility = v
	}
	if len(updates) == 0 && len(removes) == 0 && title == g.Title && description == g.Description && visibility == g.Visibility {
		fail(fmt.Errorf("変更内容がありません(ファイル・--remove・-t・-d・公開範囲のいずれかを指定してください)"))
	}

	if _, err := saveGist(cfg, session, &g.ID, title, description, visibility, files); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "%s 更新しました\n", green("✓"))
	fmt.Println(gistURL(cfg, g.ID))
}

func cmdDelete(args []string) {
	p := parseArgs(args, nil, map[string]string{"-y": "yes", "--yes": "yes"})
	if len(p.positional) != 1 {
		usageExit("bin delete <id> [-y]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := requireSession()
	g, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}
	names := make([]string, len(g.Files))
	for i, f := range g.Files {
		names[i] = f.Filename
	}
	if !p.bools["yes"] && !confirm("「%s」を削除しますか?変更履歴も含めて元に戻せません。", gistTitle(g.Title, names)) {
		fmt.Println("キャンセルしました。")
		return
	}
	if err := deleteGist(cfg, session, id); err != nil {
		fail(err)
	}
	success("削除しました")
}

func cmdClone(args []string) {
	p := parseArgs(args, nil, map[string]string{"--force": "force"})
	if len(p.positional) < 1 || len(p.positional) > 2 {
		usageExit("bin clone <id> [<dir>] [--force]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	dir := id
	if len(p.positional) == 2 {
		dir = p.positional[1]
	}
	cfg, session := optionalSession()
	g, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}
	for _, f := range g.Files {
		// ファイル名はDB側で/や\を禁止しているが、念のためディレクトリ外に書かないようBaseを取る
		path := filepath.Join(dir, filepath.Base(f.Filename))
		if _, err := os.Stat(path); err == nil && !p.bools["force"] {
			fail(fmt.Errorf("%s は既に存在します(上書きするには --force)", path))
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			fail(err)
		}
		fmt.Println(dim("  " + path))
	}
	success("%dファイルを %s にダウンロードしました", len(g.Files), dir)
}
