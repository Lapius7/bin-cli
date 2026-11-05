package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// ---- 共通 ----

// pickGist は位置引数の先頭を ID として Gist を取得する(ログイン済みなら自分の非公開も読める)
func pickGist(p parsedArgs, usage string) (Config, *Session, *Gist) {
	if len(p.positional) < 1 {
		usageExit(usage)
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
	return cfg, session, g
}

// ownGist は自分の Gist だけを返す(書き込み系用)
func ownGist(p parsedArgs, usage string) (Config, *Session, *Gist) {
	if len(p.positional) < 1 {
		usageExit(usage)
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
	return cfg, session, g
}

func findFile(g *Gist, name string) *GistFile {
	for i := range g.Files {
		if g.Files[i].Filename == name {
			return &g.Files[i]
		}
	}
	return nil
}

// pickFileOf は -f 指定か、ファイルが1つならそれを返す
func pickFileOf(g *Gist, name string) *GistFile {
	if name != "" {
		if f := findFile(g, name); f != nil {
			return f
		}
		fail(fmt.Errorf(T("%s というファイルはありません"), name))
	}
	if len(g.Files) == 1 {
		return &g.Files[0]
	}
	names := make([]string, len(g.Files))
	for i, f := range g.Files {
		names[i] = f.Filename
	}
	fail(fmt.Errorf(T("ファイルが複数あります。-f <file> で指定してください(%s)"), strings.Join(names, ", ")))
	return nil
}

func cloneFiles(fs []GistFile) []GistFile { return append([]GistFile(nil), fs...) }

func commit(cfg Config, session *Session, g *Gist, files []GistFile, title, description, visibility, message string) {
	if _, err := saveGist(cfg, session, &g.ID, title, description, visibility, files, message); err != nil {
		fail(err)
	}
}

func rawURL(cfg Config, id, file string) string {
	return cfg.SiteURL + "/raw/" + id + "/" + file
}

// ---- bin diff ----

// bin diff [<id>] [<file|dir>...] [--rev N]
// 既定ではローカルのファイルと Gist を比べる(push したら何が変わるか)。--rev N は Gist の過去リビジョンと現在を比べる
func cmdDiff(args []string) {
	p := parseArgs(args, map[string]string{"--rev": "rev"}, nil)
	cfg, session, g := pickGist(p, "bin diff [<id>] [<file|dir>...] [--rev <N>]")
	_ = cfg
	maxDiffLines = 1 << 30
	if rv, ok := p.value("rev"); ok {
		revs, err := getRevisions(cfg, session, g.ID)
		if err != nil {
			fail(err)
		}
		r := findRevision(g.ID, revs, rv)
		fmt.Fprintf(os.Stderr, "%s\n", dim(fmt.Sprintf(T("リビジョン %s → 現在"), revLabel(g.ID, *r))))
		showChanges(r.Files, g.Files)
		reportChanges()
		return
	}
	var local []GistFile
	paths := p.positional[1:]
	switch {
	case len(paths) > 0:
		var skips []skipped
		for _, x := range paths {
			collectPath(x, &local, &skips)
		}
	default:
		if dir := linkedGistID(); dir == g.ID {
			d, _ := os.Getwd()
			root := d
			for {
				if _, err := os.Stat(filepath.Join(root, syncMetaFile)); err == nil {
					break
				}
				if parent := filepath.Dir(root); parent != root {
					root = parent
				} else {
					break
				}
			}
			m, _ := localFiles(root)
			for n, c := range m {
				local = append(local, GistFile{Filename: n, Content: c})
			}
		} else {
			for _, f := range g.Files {
				if data, err := os.ReadFile(filepath.FromSlash(f.Filename)); err == nil {
					local = append(local, GistFile{Filename: f.Filename, Content: string(data)})
				}
			}
		}
	}
	if len(local) == 0 {
		fail(fmt.Errorf(T("比べるローカルファイルがありません(ファイルを指定するか、Gist と同じ名前のファイルがあるフォルダで実行してください)")))
	}
	// 比較は「指定したファイルだけ」。指定外のGistファイルは削除扱いにしない
	names := map[string]bool{}
	for _, f := range local {
		names[f.Filename] = true
	}
	var remote []GistFile
	for _, f := range g.Files {
		if names[f.Filename] || len(paths) == 0 && linkedGistID() == g.ID {
			remote = append(remote, f)
		}
	}
	fmt.Fprintf(os.Stderr, "%s\n", dim(T("Gist → ローカル(push すると変わる内容)")))
	showChanges(remote, local)
	reportChanges()
}

func reportChanges() {
	if len(pendingChanges) == 0 {
		fmt.Fprintln(os.Stderr, green("✔")+" "+T("差分はありません"))
		return
	}
	printPendingChanges()
}

func revLabel(id string, r Revision) string {
	return fmt.Sprintf("%d (%s)", r.Revision, revisionHash(id, r.Revision))
}

// findRevision は「3」「a1b2c3d(ハッシュ)」「~1(1つ前)」のいずれかでリビジョンを選ぶ
func findRevision(id string, revs []Revision, spec string) *Revision {
	sort.Slice(revs, func(i, j int) bool { return revs[i].Revision < revs[j].Revision })
	if len(revs) == 0 {
		fail(fmt.Errorf(T("リビジョンがありません")))
	}
	if strings.HasPrefix(spec, "~") {
		n, err := strconv.Atoi(spec[1:])
		if err != nil || n < 1 || n >= len(revs) {
			fail(fmt.Errorf(T("~N は 1〜%d で指定してください(1つ前=~1)"), len(revs)-1))
		}
		return &revs[len(revs)-1-n]
	}
	if n, err := strconv.Atoi(spec); err == nil {
		for i := range revs {
			if revs[i].Revision == n {
				return &revs[i]
			}
		}
	}
	for i := range revs {
		if strings.HasPrefix(revisionHash(id, revs[i].Revision), spec) && len(spec) >= 4 {
			return &revs[i]
		}
	}
	fail(fmt.Errorf(T("リビジョン %s が見つかりません(bin log <id> で一覧)"), spec))
	return nil
}

// ---- bin restore / undo ----

func cmdRestore(args []string, undo bool) {
	p := parseArgs(args, map[string]string{"-m": "message", "--message": "message"}, map[string]string{"--dry-run": "dry"})
	usage := "bin restore <id> <revision|hash|~N> [--dry-run]"
	spec := ""
	if undo {
		usage, spec = "bin undo [<id>] [--dry-run]", "~1"
	} else {
		if len(p.positional) != 2 {
			usageExit(usage)
		}
		spec = p.positional[1]
	}
	cfg, session, g := ownGist(p, usage)
	revs, err := getRevisions(cfg, session, g.ID)
	if err != nil {
		fail(err)
	}
	r := findRevision(g.ID, revs, spec)
	title, desc, vis := g.Title, g.Description, g.Visibility
	if r.Title != nil {
		title = *r.Title
	}
	if r.Description != nil {
		desc = *r.Description
	}
	if r.Visibility != nil {
		vis = *r.Visibility
	}
	if len(r.Files) == 0 {
		fail(fmt.Errorf(T("このリビジョンにはファイルの記録がありません")))
	}
	showChanges(g.Files, r.Files)
	if len(pendingChanges) == 0 && title == g.Title && desc == g.Description && vis == g.Visibility {
		fail(fmt.Errorf(T("現在の内容と同じです")))
	}
	if p.bools["dry"] {
		fmt.Fprintf(os.Stderr, "%s %s\n", yellow("◌"), bold(fmt.Sprintf(T("リビジョン %s に戻します(プレビュー・保存しません)"), revLabel(g.ID, *r))))
		printPendingChanges()
		return
	}
	msg, _ := p.value("message")
	if msg == "" {
		msg = fmt.Sprintf("restore: revision %d", r.Revision)
	}
	commit(cfg, session, g, r.Files, title, desc, vis, msg)
	doneCard(T("元に戻しました"), []cardRow{{T("リビジョン"), revLabel(g.ID, *r)}}, gistURL(cfg, g.ID))
}

// ---- bin mv / bin cp ----

func renameInto(g *Gist, from, to string) ([]GistFile, int) {
	var out []GistFile
	n := 0
	for _, f := range g.Files {
		switch {
		case f.Filename == from:
			f.Filename = to
			n++
		case strings.HasPrefix(f.Filename, strings.TrimSuffix(from, "/")+"/") && strings.HasSuffix(from, "/"):
			f.Filename = to + f.Filename[len(from):]
			n++
		}
		out = append(out, f)
	}
	return out, n
}

func cmdMv(args []string) {
	p := parseArgs(args, map[string]string{"-m": "message", "--message": "message"}, nil)
	if len(p.positional) != 3 {
		usageExit("bin mv <id> <old> <new>")
	}
	cfg, session, g := ownGist(p, "bin mv <id> <old> <new>")
	from, to := p.positional[1], p.positional[2]
	if err := validateGistPath(to); err != nil {
		fail(err)
	}
	if findFile(g, from) == nil {
		// フォルダ名の変更(末尾 / を補って前方一致)
		from += "/"
		to = strings.TrimSuffix(to, "/") + "/"
	}
	prev := cloneFiles(g.Files)
	next, n := renameInto(g, from, to)
	if n == 0 {
		fail(fmt.Errorf(T("%s というファイル・フォルダはありません"), strings.TrimSuffix(from, "/")))
	}
	seen := map[string]bool{}
	for _, f := range next {
		if seen[f.Filename] {
			fail(fmt.Errorf(T("%s は既に存在します"), f.Filename))
		}
		seen[f.Filename] = true
	}
	showChanges(prev, next)
	msg, _ := p.value("message")
	if msg == "" {
		msg = "mv " + strings.TrimSuffix(from, "/") + " " + strings.TrimSuffix(to, "/")
	}
	commit(cfg, session, g, next, g.Title, g.Description, g.Visibility, msg)
	doneCard(T("名前を変更しました"), []cardRow{{T("変更"), strings.TrimSuffix(from, "/") + " → " + strings.TrimSuffix(to, "/")}}, gistURL(cfg, g.ID))
}

// bin cp <id> <file> [<新しい名前>]            同じ Gist 内にコピー
// bin cp <id> <file> --to <id2> [--as <名前>]  別の Gist にコピー
// bin cp <id> <file> --new [--as <名前>]       新しい Gist として作る
func cmdCp(args []string) {
	p := parseArgs(args, map[string]string{"--to": "to", "--as": "as", "-m": "message", "--message": "message"}, mergeBool(visibilityFlags, map[string]string{"--new": "new"}))
	usage := "bin cp <id> <file> [<new name> | --to <id> [--as <name>] | --new [--as <name>]]"
	if len(p.positional) < 2 {
		usageExit(usage)
	}
	cfg, session, g := optGistRead(p)
	src := p.positional[1]
	var items []GistFile
	for _, f := range g.Files {
		if f.Filename == src || strings.HasPrefix(f.Filename, strings.TrimSuffix(src, "/")+"/") {
			items = append(items, f)
		}
	}
	if len(items) == 0 {
		fail(fmt.Errorf(T("%s というファイル・フォルダはありません"), src))
	}
	name, _ := p.value("as")
	if len(p.positional) > 2 {
		name = p.positional[2]
	}
	rename := func(f GistFile) GistFile {
		if name == "" {
			return f
		}
		if len(items) == 1 && f.Filename == src {
			f.Filename = name
		} else {
			f.Filename = strings.TrimSuffix(name, "/") + strings.TrimPrefix(f.Filename, strings.TrimSuffix(src, "/"))
		}
		return f
	}
	var copied []GistFile
	for _, f := range items {
		f = rename(f)
		if err := validateGistPath(f.Filename); err != nil {
			fail(err)
		}
		copied = append(copied, f)
	}
	switch {
	case p.bools["new"]:
		vis, ok := pickVisibility(p)
		if !ok {
			vis = "unlisted"
		}
		cfg, session = requireSession()
		id, err := saveGist(cfg, session, nil, "", "", vis, copied, "copy from "+g.ID)
		if err != nil {
			fail(err)
		}
		doneCard(T("コピーして作成しました"), []cardRow{{T("ファイル"), fileList(namesOf(copied))}}, gistURL(cfg, id))
	case p.values["to"] != nil:
		dst, err := parseGistID(p.values["to"][0])
		if err != nil {
			fail(err)
		}
		cfg, session = requireSession()
		d, err := getGist(cfg, session, dst)
		if err != nil {
			fail(err)
		}
		if d.Owner.ID != userIDFromToken(session.AccessToken) {
			fail(fmt.Errorf(T("コピー先は自分のGistである必要があります")))
		}
		prev := cloneFiles(d.Files)
		next := cloneFiles(d.Files)
		for _, c := range copied {
			replaced := false
			for i := range next {
				if next[i].Filename == c.Filename {
					next[i].Content = c.Content
					replaced = true
				}
			}
			if !replaced {
				next = append(next, c)
			}
		}
		showChanges(prev, next)
		commit(cfg, session, d, next, d.Title, d.Description, d.Visibility, "copy from "+g.ID)
		doneCard(T("コピーしました"), []cardRow{{T("コピー先"), d.ID}}, gistURL(cfg, d.ID))
	default:
		if name == "" {
			fail(fmt.Errorf(T("新しい名前を指定してください(bin cp <id> <file> <新しい名前>)")))
		}
		cfg, session = requireSession()
		if g.Owner.ID != userIDFromToken(session.AccessToken) {
			fail(fmt.Errorf(T("自分のGistではないため編集できません(別の Gist へは --to / --new)")))
		}
		prev := cloneFiles(g.Files)
		next := cloneFiles(g.Files)
		for _, c := range copied {
			if findFile(g, c.Filename) != nil {
				fail(fmt.Errorf(T("%s は既に存在します"), c.Filename))
			}
			next = append(next, c)
		}
		showChanges(prev, next)
		commit(cfg, session, g, next, g.Title, g.Description, g.Visibility, "cp "+src+" "+name)
		doneCard(T("コピーしました"), []cardRow{{T("ファイル"), fileList(namesOf(copied))}}, gistURL(cfg, g.ID))
	}
}

func optGistRead(p parsedArgs) (Config, *Session, *Gist) {
	return pickGist(p, "bin cp <id> <file> ...")
}

func mergeBool(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func namesOf(fs []GistFile) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Filename
	}
	return out
}

// ---- bin open / copy / share ----

func cmdOpen(args []string) {
	p := parseArgs(args, map[string]string{"-f": "filename", "--filename": "filename"}, map[string]string{"--raw": "raw"})
	cfg, _, g := pickGist(p, "bin open [<id>] [-f <file>] [--raw]")
	url := gistURL(cfg, g.ID)
	if name, ok := p.value("filename"); ok || p.bools["raw"] {
		f := pickFileOf(g, name)
		url = rawURL(cfg, g.ID, f.Filename)
	}
	if err := openBrowser(url); err != nil {
		fmt.Println(url)
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", green("✔"), fmt.Sprintf(T("ブラウザで開きました: %s"), url))
}

func cmdCopy(args []string) {
	p := parseArgs(args, map[string]string{"-f": "filename", "--filename": "filename"}, map[string]string{"--url": "url"})
	cfg, _, g := pickGist(p, "bin copy [<id>] [-f <file>] [--url]")
	text := ""
	label := ""
	if p.bools["url"] {
		text, label = gistURL(cfg, g.ID), T("URL")
	} else {
		name, _ := p.value("filename")
		f := pickFileOf(g, name)
		text, label = f.Content, f.Filename
	}
	if err := clipboardWrite(text); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", green("✔"), fmt.Sprintf(T("クリップボードにコピーしました: %s"), label))
}

func cmdShare(args []string) {
	p := parseArgs(args, map[string]string{"-f": "filename", "--filename": "filename"}, map[string]string{"--no-qr": "noqr"})
	cfg, _, g := pickGist(p, "bin share [<id>] [-f <file>] [--no-qr]")
	url := gistURL(cfg, g.ID)
	if !stdoutIsTerminal() {
		fmt.Println(url)
		return
	}
	row := func(label, v string) { fmt.Printf("  %s  %s\n", dim(padLabel(label, 10)), v) }
	fmt.Printf("%s %s  %s\n\n", green("✔"), bold(gistTitle(g.Title, namesOf(g.Files))), visibilityLabel(g.Visibility))
	row("URL", hyperlink(url, cyan(url)))
	if name, ok := p.value("filename"); ok || len(g.Files) == 1 {
		f := pickFileOf(g, name)
		row("Raw", rawURL(cfg, g.ID, f.Filename))
	}
	row(T("埋め込み"), fmt.Sprintf(`<script src="%s/%s.js"></script>`, cfg.SiteURL, g.ID))
	row("git", fmt.Sprintf("git clone %s/%s.git", cfg.SiteURL, g.ID))
	row("bin", fmt.Sprintf("bin clone %s", g.ID))
	if g.Visibility == "private" {
		fmt.Printf("\n  %s %s\n", yellow("!"), T("非公開のため、他の人は開けません(--public か --unlisted に変更してください)"))
	}
	if !p.bools["noqr"] {
		if qr, err := qrcode.New(url, qrcode.Low); err == nil {
			fmt.Println()
			for _, l := range strings.Split(strings.TrimRight(qr.ToSmallString(false), "\n"), "\n") {
				fmt.Println("  " + l)
			}
		}
	}
}

// ---- bin search ----

func cmdSearch(args []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		usageExit("bin search <text> [-l <lang>] [--since <period>] [--tag <tag>] [-u <handle>] [-n <N>]")
	}
	cmdList(append([]string{"-q", args[0]}, args[1:]...))
}

// ---- bin edit --editor ----

// editInEditor は Gist のファイルを $EDITOR で開き、変更があれば保存する
func editInEditor(cfg Config, session *Session, g *Gist, name, message string) {
	f := pickFileOf(g, name)
	ed := editorCommand()
	if ed == nil {
		fail(fmt.Errorf(T("エディタが見つかりません($EDITOR を設定してください)")))
	}
	dir, err := os.MkdirTemp("", "bin-edit-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, filepath.Base(f.Filename))
	if err := os.WriteFile(tmp, []byte(f.Content), 0o600); err != nil {
		fail(err)
	}
	cmd := exec.Command(ed[0], append(ed[1:], tmp)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail(fmt.Errorf(T("エディタが異常終了しました: %w"), err))
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		fail(err)
	}
	if string(data) == f.Content {
		fmt.Fprintln(os.Stderr, dim(T("変更はありませんでした")))
		return
	}
	prev := cloneFiles(g.Files)
	next := cloneFiles(g.Files)
	for i := range next {
		if next[i].Filename == f.Filename {
			next[i].Content = string(data)
		}
	}
	showChanges(prev, next)
	if message == "" {
		message = "edit: " + f.Filename
	}
	commit(cfg, session, g, next, g.Title, g.Description, g.Visibility, message)
	doneCard(T("更新しました"), []cardRow{{T("ファイル"), f.Filename}}, gistURL(cfg, g.ID))
}

// ---- bin bulk / visibility / 複数 delete ----

var bulkValueFlags = mergeBool(listValueFlags, map[string]string{"--add": "add", "--remove": "remove", "--set": "set"})

func splitCSV(vs []string) []string {
	var out []string
	for _, v := range vs {
		for _, x := range strings.Split(v, ",") {
			if x = strings.TrimSpace(x); x != "" {
				out = append(out, x)
			}
		}
	}
	return out
}

func isBulkArgs(args []string) bool {
	pos := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-q", "--query", "-l", "--lang", "--since", "--tag":
			return true
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if _, ok := bulkValueFlags[a]; ok {
				i++
			}
			continue
		}
		pos++
	}
	return pos > 1
}

// bulkTargets は位置引数の ID、または絞り込み条件に一致する自分の Gist を返す
func bulkTargets(p parsedArgs, ids []string, cfg Config, session *Session) []GistSummary {
	var out []GistSummary
	if len(ids) > 0 {
		for _, s := range ids {
			id, err := parseGistID(s)
			if err != nil {
				fail(err)
			}
			g, err := getGist(cfg, session, id)
			if err != nil {
				fail(err)
			}
			if g.Owner.ID != userIDFromToken(session.AccessToken) {
				fail(fmt.Errorf(T("自分のGistではありません: %s"), id))
			}
			out = append(out, GistSummary{ID: g.ID, Title: g.Title, Visibility: g.Visibility, Tags: g.Tags})
		}
		return out
	}
	f, _, limit := filterFromArgs(p)
	if _, ok := p.value("limit"); !ok {
		limit = 200
	}
	f.Owner = userIDFromToken(session.AccessToken)
	items, _, err := listGists(cfg, session, f, limit, 0)
	if err != nil {
		fail(err)
	}
	return items
}

func cmdBulk(op string, args []string) {
	p := parseArgs(args, bulkValueFlags, map[string]string{"-y": "yes", "--yes": "yes", "--dry-run": "dry"})
	pos := p.positional
	arg := ""
	if op == "visibility" {
		if len(pos) < 1 {
			usageExit("bin visibility <public|unlisted|private> [<id>...] [-q <text> -l <lang> --since <period> --tag <tag>] [-y]")
		}
		arg, pos = pos[0], pos[1:]
		if arg != "public" && arg != "unlisted" && arg != "private" {
			fail(fmt.Errorf(T("公開範囲は public / unlisted / private のどれかです: %s"), arg))
		}
	}
	if op == "tag" && len(p.values["add"])+len(p.values["remove"])+len(p.values["set"]) == 0 {
		fail(fmt.Errorf(T("--add / --remove / --set のどれかでタグを指定してください")))
	}
	cfg, session := requireSession()
	if len(pos) == 0 && len(p.values) == 0 {
		fail(fmt.Errorf(T("対象を指定してください(ID を並べるか、-q / -l / --since / --tag で絞り込み)")))
	}
	items := bulkTargets(p, pos, cfg, session)
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, dim(T("対象のGistはありません")))
		return
	}
	verb := map[string]string{"delete": T("削除"), "visibility": T("公開範囲を変更"), "tag": T("タグを変更"), "star": T("スターを付ける"), "unstar": T("スターを外す")}[op]
	fmt.Fprintf(os.Stderr, "%s %s\n", bold(fmt.Sprintf(T("%d 件を対象に「%s」を行います"), len(items), verb)), dim(arg))
	for i, it := range items {
		if i == 15 {
			fmt.Fprintf(os.Stderr, "  %s\n", dim(fmt.Sprintf(T("…他 %d 件"), len(items)-15)))
			break
		}
		fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", dim(it.ID), gistTitle(it.Title, nil), visibilityLabel(it.Visibility))
	}
	if p.bools["dry"] {
		return
	}
	if !p.bools["yes"] && !confirm(T("実行しますか?")) {
		fmt.Fprintln(os.Stderr, dim(T("中止しました")))
		os.Exit(1)
	}
	okN, failed := 0, 0
	for _, it := range items {
		var err error
		switch op {
		case "delete":
			err = deleteGist(cfg, session, it.ID)
		case "star", "unstar":
			_, err = rpc(cfg, session, "star_gist", map[string]any{"p_id": it.ID, "p_star": op == "star"})
		case "visibility":
			var g *Gist
			if g, err = getGist(cfg, session, it.ID); err == nil && g.Visibility != arg {
				_, err = saveGist(cfg, session, &g.ID, g.Title, g.Description, arg, g.Files, "visibility: "+arg)
			}
		case "tag":
			tags := map[string]bool{}
			var order []string
			add := func(t string) {
				if !tags[t] {
					tags[t] = true
					order = append(order, t)
				}
			}
			if len(p.values["set"]) == 0 {
				for _, t := range it.Tags {
					add(t)
				}
			}
			for _, t := range splitCSV(p.values["set"]) {
				add(t)
			}
			for _, t := range splitCSV(p.values["add"]) {
				add(t)
			}
			for _, t := range splitCSV(p.values["remove"]) {
				delete(tags, t)
			}
			final := []string{}
			for _, t := range order {
				if tags[t] {
					final = append(final, t)
				}
			}
			_, err = rpc(cfg, session, "set_gist_tags", map[string]any{"p_id": it.ID, "p_tags": final})
		}
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "%s %s  %v\n", red("✖"), it.ID, describeDBError(err))
			continue
		}
		okN++
	}
	mark := green("✔")
	if failed > 0 {
		mark = yellow("!")
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", mark, fmt.Sprintf(T("%d 件完了、%d 件失敗"), okN, failed))
	if failed > 0 {
		os.Exit(1)
	}
}
