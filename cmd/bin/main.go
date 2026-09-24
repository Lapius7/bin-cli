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
		cmdLogout()
	case "whoami":
		cmdWhoami()
	case "user":
		cmdUser(args)
	case "create", "new":
		cmdCreate(args)
	case "list", "ls":
		cmdList(args)
	case "view", "cat", "show":
		cmdView(args)
	case "edit":
		cmdEdit(args)
	case "fork":
		cmdFork(args)
	case "delete", "rm":
		cmdDelete(args)
	case "clone", "download":
		cmdClone(args)
	case "timeline", "tl":
		cmdTimeline(args)
	case "log", "history":
		cmdLog(args)
	case "status", "st":
		cmdStatus(args)
	case "pull":
		cmdPull(args)
	case "push":
		cmdPush(args)
	case "sync":
		cmdSync(args)
	case "run":
		cmdRun(args)
	case "star":
		cmdStar(args, true)
	case "unstar":
		cmdStar(args, false)
	case "starred", "stars":
		cmdStarred(args)
	case "tag", "tags":
		cmdTag(args)
	case "token", "tokens":
		cmdToken(args)
	case "version", "--version", "-v":
		cmdVersion()
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Printf("%s %s\n\n", bold("bin"), dim(T("bin.lapius7.com CLIクライアント ")+version))

	printUsageSection(T("認証"), [][2]string{
		{"bin login", T("ブラウザでLapount(account.lapius7.com)にログインする")},
		{"bin login --device", T("デバイスコード方式でログインする(SSH越し等、手元でブラウザが開けない場合)")},
		{"bin logout", T("ログアウトする(このCLIのセッションをサーバー側でも失効させる)")},
		{"bin whoami", T("自分のアカウント情報(連携・2段階認証・Gistの集計など)を表示する")},
		{"bin user -u <handle>", T("ユーザーの公開プロフィールと公開Gistの集計を表示する")},
	})
	printUsageSection("Gist", [][2]string{
		{"bin create <file|dir>...", T("ファイル・フォルダからGistを作成する(標準入力からも可: cat x | bin create -f x.txt)")},
		{"bin list [-u <handle>]", T("自分(または指定ユーザー)のGist一覧")},
		{"bin timeline", T("みんなの公開Gistを新しい順に表示する")},
		{"bin view <id> [-f <file>]", T("内容を表示する(-fで1ファイルだけ生出力)")},
		{"bin edit <id> [<file|dir>...]", T("ファイルを追加・上書きする(--remove <path>で削除)")},
		{"bin clone <id> [<dir>]", T("Gistのファイルをフォルダ構成ごとダウンロードする")},
		{"bin fork <id>", T("コピーして自分の新しいGistを作る(自分のGistも可。公開範囲は元のまま)")},
		{"bin log <id> [-p]", T("変更履歴を git log 風に表示する(-pで差分も表示)")},
		{"bin delete <id>", T("削除する(確認あり、-yで省略)")},
	})
	printUsageSection(T("スター・タグ"), [][2]string{
		{"bin star <id> / bin unstar <id>", T("スターを付ける・外す")},
		{"bin starred [-u <handle>]", T("スターしたGistの一覧")},
		{"bin tag <id> [<tag>...]", T("タグを設定する(指定しなければ表示、--clearで外す)")},
	})
	printUsageSection(T("同期・実行(clone したフォルダで)"), [][2]string{
		{"bin status", T("手元の変更とGist側の新しい変更を表示する")},
		{"bin pull", T("Gist側の変更を取り込む(両方で変えたファイルは止まる。--forceでGist側を優先)")},
		{"bin push [-m <message>]", T("手元のフォルダの内容でGistを更新する")},
		{"bin sync [-m <message>]", T("pull してから、手元の変更があれば push する")},
		{"bin run <id> [-f <file>] [-- <args>]", T("Gistのスクリプトを実行する(実行前に内容を表示して確認。-yで省略)")},
		{"git clone " + defaultSiteURL + "/<id>.git", T("git でも取得できる(読み取り専用。非公開はパスワードにAPIトークン)")},
	})
	printUsageSection(T("APIトークン(CI・スクリプト用)"), [][2]string{
		{"bin token create <name> [--read]", T("APIトークンを発行する(--expires <日数> で期限付き)")},
		{"bin token list / bin token revoke <id>", T("一覧・失効")},
		{"BIN_TOKEN=<token> bin …", T("bin login の代わりにAPIトークンで操作する")},
	})
	printUsageSection(T("絞り込み(list/timeline)"), [][2]string{
		{"-q, --query <text>", T("タイトル・説明・ファイルパスで検索")},
		{T("-l, --lang <言語>"), T("言語で絞り込み(python,go のようにカンマ区切り、拡張子でも可)")},
		{T("--since <期間>"), T("24h・7d・2w・3m・1y、または 2026-09-01 以降")},
		{T("--sort <順>"), T("updated(既定) / created / oldest")},
		{"-n, --limit <N>", T("表示件数(既定30)")},
	})
	printUsageSection(T("オプション(create/edit/fork)"), [][2]string{
		{"-t, --title <text>", T("タイトル")},
		{"-d, --description <text>", T("説明")},
		{"-f, --filename <name>", T("標準入力から読み込む時のファイル名")},
		{"-m, --message <text>", T("変更履歴に残すメモ(コミットメッセージ)")},
		{"--public / --unlisted / --private", T("公開範囲(作成時の既定は --unlisted)")},
	})
	fmt.Println(dim(T("<id> にはGistのURL(https://bin.lapius7.com/<id>)もそのまま指定できます。")))
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
				fail(fmt.Errorf(T("%s には値が必要です"), name))
			}
			continue
		}
		if key, ok := boolFlags[a]; ok {
			p.bools[key] = true
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			fail(fmt.Errorf(T("不明なオプションです: %s(`bin help` で使い方を表示)"), a))
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
	"-m":       "message", "--message": "message",
}

var visibilityFlags = map[string]string{
	"--public": "public", "--unlisted": "unlisted", "--private": "private",
}

func pickVisibility(p parsedArgs) (string, bool) {
	found := ""
	for _, v := range []string{"public", "unlisted", "private"} {
		if p.bools[v] {
			if found != "" {
				fail(fmt.Errorf(T("公開範囲の指定は1つだけにしてください")))
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
		fail(fmt.Errorf(T("ログインしていません。先に `bin login` を実行してください")))
	}
	if err := freshenSession(cfg, session); err != nil {
		fail(err)
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
	// 更新できない(refresh_tokenも失効している)時は、未ログインとして続ける(公開Gistの閲覧等はできる)
	if err := freshenSession(cfg, session); err != nil {
		warnErr("%v", err)
		return cfg, nil
	}
	return cfg, session
}

// userIDFromToken はアクセストークン(JWT)のsubを取り出す(署名検証はサーバー側で行われる)。
func userIDFromToken(token string) string {
	if strings.HasPrefix(token, "lbt_") {
		return tokenOwner(loadConfig(), token)
	}
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

func readStdinFile(name string) GistFile {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1024*1024+1))
	if err != nil {
		fail(fmt.Errorf(T("標準入力を読み込めません: %w"), err))
	}
	if len(data) > 1024*1024 {
		fail(fmt.Errorf(T("1ファイルあたり1MBまでです")))
	}
	if !utf8.Valid(data) {
		fail(fmt.Errorf(T("標準入力の内容がテキスト(UTF-8)ではありません")))
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
	success(T("ログインしました"))
	cmdWhoami()
}

func cmdCreate(args []string) {
	p := parseArgs(args, gistValueFlags, visibilityFlags)
	cfg, session := requireSession()
	stdinName, _ := p.value("filename")
	files := readInputFiles(p.positional, stdinName, false, true)

	visibility, ok := pickVisibility(p)
	if !ok {
		visibility = "unlisted"
	}
	title, _ := p.value("title")
	description, _ := p.value("description")

	message, _ := p.value("message")

	id, err := saveGist(cfg, session, nil, title, description, visibility, files, message)
	if err != nil {
		fail(err)
	}
	// URLだけを標準出力に出す(`url=$(bin create x.go)` のように使えるように)。装飾はstderrへ
	fmt.Fprintf(os.Stderr, Tn("%s 作成しました(%s・%dファイル)\n", len(files)), green("✓"), visibilityLabel(visibility), len(files))
	fmt.Println(gistURL(cfg, id))
}

func cmdList(args []string) {
	p := parseArgs(args, listValueFlags, nil)
	f, notes, limit := filterFromArgs(p)

	var cfg Config
	var session *Session
	var label string
	if handle, ok := p.value("user"); ok {
		cfg, session = optionalSession()
		handle = strings.TrimPrefix(handle, "@")
		id, err := resolveHandle(cfg, handle)
		if err != nil {
			fail(err)
		}
		f.Owner, label = id, fmt.Sprintf(T("@%s のGist"), handle)
	} else {
		cfg, session = requireSession()
		f.Owner, label = userIDFromToken(session.AccessToken), T("自分のGist")
	}

	items, total, err := listGists(cfg, session, f, limit, 0)
	if err != nil {
		fail(err)
	}
	if len(items) == 0 && len(notes) == 0 {
		fmt.Println(dim(fmt.Sprintf(T("Gistがありません。%s で作成できます。"), cyan("`bin create <file>`"))))
		return
	}
	printGistTable(label, notes, items, total, false)
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
		return "", fmt.Errorf(T("@%s というユーザーは見つかりません"), handle)
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
	owner := ""
	if g.Owner.Handle != nil {
		owner = "@" + *g.Owner.Handle + " / "
	}
	fmt.Printf("%s%s  %s\n", dim(owner), bold(gistTitle(g.Title, names)), visibilityLabel(g.Visibility))
	if g.Description != "" {
		fmt.Println(g.Description)
	}
	fmt.Println(dim(fmt.Sprintf(T("%s · 更新 %s · リビジョン %d"), gistURL(cfg, g.ID), formatDate(g.UpdatedAt), g.RevisionCount)))
	if len(g.Tags) > 0 || g.StarCount > 0 || g.CommentCount > 0 {
		line := []string{}
		if g.StarCount > 0 {
			line = append(line, yellow(fmt.Sprintf("★%d", g.StarCount)))
		}
		if g.CommentCount > 0 {
			line = append(line, dim(fmt.Sprintf(Tn("コメント %d件", g.CommentCount), g.CommentCount)))
		}
		if len(g.Tags) > 0 {
			line = append(line, formatTags(g.Tags))
		}
		fmt.Println(strings.Join(line, "  "))
	}
	if s := g.ForkSource; s != nil {
		if s.Hidden {
			fmt.Println(dim("↳ " + T("非公開または限定公開のGistからフォーク")))
		} else {
			title := s.Title
			if title == "" && s.FirstFile != nil {
				title = *s.FirstFile
			}
			src := title
			if s.Owner.Handle != nil {
				src = "@" + *s.Owner.Handle + " / " + title
			}
			fmt.Println(dim("↳ ") + fmt.Sprintf(T("%s からフォーク"), src) + " " + dim(gistURL(cfg, s.ID)))
		}
	}
	if g.ForkCount > 0 {
		fmt.Println(dim(fmt.Sprintf(Tn("フォーク %d件", g.ForkCount), g.ForkCount) + " · " + gistURL(cfg, g.ID) + "/forks"))
	}
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
		usageExit("bin edit <id> [<file|dir>...] [--remove <path>] [-m <message>] [-t <title>] [-d <description>] [--public|--unlisted|--private]")
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
		fail(fmt.Errorf(T("自分のGistではないため編集できません")))
	}

	stdinName, _ := p.value("filename")
	// editは「-」か -f を明示した時だけ標準入力を読む
	paths := p.positional[1:]
	if stdinName != "" && len(paths) == 0 {
		paths = []string{"-"}
	}
	updates := readInputFiles(paths, stdinName, true, false)
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
	// --remove はファイルのパスか、フォルダのパス(その配下をまとめて削除)を受け付ける
	for _, name := range removes {
		name = strings.TrimSuffix(name, "/")
		kept := files[:0]
		found := false
		for _, f := range files {
			if f.Filename == name || strings.HasPrefix(f.Filename, name+"/") {
				found = true
				continue
			}
			kept = append(kept, f)
		}
		if !found {
			fail(fmt.Errorf(T("%s というファイル・フォルダはありません"), name))
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
		fail(fmt.Errorf(T("変更内容がありません(ファイル・--remove・-t・-d・公開範囲のいずれかを指定してください)")))
	}

	message, _ := p.value("message")
	if _, err := saveGist(cfg, session, &g.ID, title, description, visibility, files, message); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, T("%s 更新しました\n"), green("✓"))
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
	if !p.bools["yes"] && !confirm(T("「%s」を削除しますか?変更履歴も含めて元に戻せません。"), gistTitle(g.Title, names)) {
		fmt.Println(T("キャンセルしました。"))
		return
	}
	if err := deleteGist(cfg, session, id); err != nil {
		fail(err)
	}
	success(T("削除しました"))
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
	for _, f := range g.Files {
		path, err := safeJoin(dir, f.Filename)
		if err != nil {
			fail(err)
		}
		if _, err := os.Stat(path); err == nil && !p.bools["force"] {
			fail(fmt.Errorf(T("%s は既に存在します(上書きするには --force)"), path))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			fail(err)
		}
		fmt.Println(dim("  " + path))
	}
	// 同期(bin status / pull / push / sync)の基準を残す
	if err := writeSyncMeta(dir, metaFromGist(g)); err != nil {
		warnErr(T("%s を書き込めませんでした: %v"), syncMetaFile, err)
	}
	recordHit(cfg, session, g.ID, "download")
	success(Tn("%dファイルを %s にダウンロードしました", len(g.Files)), len(g.Files), dir)
	fmt.Println(dim("  " + T("このフォルダで bin status / bin pull / bin push / bin sync が使えます")))
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
