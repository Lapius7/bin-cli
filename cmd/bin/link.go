package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ---- bin link: Discord の Lapbot から、このプロジェクトのファイルを読めるようにする ----
//
// `bin link [dir]` は bin-server へ接続して待機する(手元のポートは開けない)。
// Discord で /bin sync を実行すると、Lapbot が bin-server 経由で下の固定の操作を依頼してくる。
// 操作は info / ls / tree / cat / grep / diff / push だけ。push は --allow-push を付けたときだけ。
// 読めるのは dir の下だけ(シンボリックリンクで外へ出る経路も塞ぐ)で、.env・秘密鍵などは常に除外し、
// 返す内容の中の Token らしき文字列は伏せる。

const (
	linkMaxCatBytes  = 1 << 20
	linkMaxCatLines  = 400
	linkMaxCatChars  = 30000
	linkMaxTree      = 400
	linkMaxLs        = 200
	linkMaxGrep      = 60
	linkMaxGrepFiles = 4000
	linkMaxPatch     = 40000
	linkMaxPushBytes = 20 << 20
	linkWalkLimit    = 20000
)

// 常に読ませないファイル名(基本名のパターン)
var linkDeniedNames = []string{
	".env", ".env.*", "*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks", "*.kdbx",
	"id_rsa*", "id_ed25519*", "id_ecdsa*", "id_dsa*", ".npmrc", ".netrc", ".pgpass", ".git-credentials",
	"credentials", "credentials.*", "secrets.json", "secrets.yml", "secrets.yaml", "service-account*.json",
}

// 名前の見た目が .env でも中身が空の雛形なので読んでよいもの
var linkAllowedNames = map[string]bool{".env.example": true, ".env.sample": true, ".env.template": true, ".env.dist": true}

var linkDeniedDirs = map[string]bool{".git": true, ".ssh": true, ".aws": true, ".gnupg": true, ".kube": true, ".docker": true}

func linkIsDenied(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		if linkDeniedDirs[part] {
			return true
		}
		l := strings.ToLower(part)
		if linkAllowedNames[l] {
			continue
		}
		for _, pat := range linkDeniedNames {
			if ok, _ := filepath.Match(pat, l); ok {
				return true
			}
		}
	}
	return false
}

type linkRoot struct {
	dir       string // EvalSymlinks 済みの絶対パス
	allowPush bool
	ignore    ignoreSet
	cfg       Config
	session   *Session
}

// resolve はルート相対のパス(空ならルート)を検証し、実体の絶対パスとルート相対のパスを返す
func (r *linkRoot) resolve(p string) (string, string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "." || p == "/" {
		return r.dir, ".", nil
	}
	if strings.ContainsRune(p, 0) || filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.Contains(p, ":") && filepath.VolumeName(p) != "" {
		return "", "", errors.New("パスはプロジェクトの中の相対パスで指定してください")
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", errors.New("プロジェクトの外は読めません")
	}
	real, err := filepath.EvalSymlinks(filepath.Join(r.dir, clean))
	if err != nil {
		return "", "", errors.New("見つかりません: " + filepath.ToSlash(clean))
	}
	rel, err := filepath.Rel(r.dir, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("プロジェクトの外は読めません")
	}
	rel = filepath.ToSlash(rel)
	if linkIsDenied(rel) {
		return "", "", errors.New("このファイルは秘密情報の可能性があるため読めません")
	}
	return real, rel, nil
}

type linkFile struct {
	Path  string
	Size  int64
	MTime time.Time
	Abs   string
}

// walk は start 以下の通常ファイルを辞書順にたどる(除外ルール・秘密ファイル・シンボリックリンクは飛ばす)
func (r *linkRoot) walk(start string, deadline time.Time, fn func(f linkFile) bool) (truncated bool) {
	count := 0
	_ = filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			truncated = true
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(r.dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[d.Name()] || linkIsDenied(rel) || r.ignore.matches(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || linkIsDenied(rel) || r.ignore.matches(rel, false) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		count++
		if count > linkWalkLimit {
			truncated = true
			return fs.SkipAll
		}
		if !fn(linkFile{Path: rel, Size: info.Size(), MTime: info.ModTime(), Abs: p}) {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	return truncated
}

// 返す文字列の中の秘密らしき部分を伏せる。変えたら changed=true
func linkRedact(s string) (string, bool) {
	changed := false
	for _, rule := range secretRules {
		s = rule.re.ReplaceAllStringFunc(s, func(m string) string {
			changed = true
			return maskSample(m)
		})
	}
	return s, changed
}

func linkReadText(abs string, limit int64) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", errors.New("読み込めません")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("通常のファイルではありません")
	}
	if st.Size() > limit {
		return "", fmt.Errorf("大きすぎます(%s、上限 %s)", linkBytes(st.Size()), linkBytes(limit))
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", errors.New("読み込めません")
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", errors.New("バイナリファイルです")
	}
	return string(data), nil
}

func linkBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func linkGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.fsmonitor=false", "-c", "core.pager=cat"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", errors.New(strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// ---- 操作 ----

type linkArgs struct {
	Path       string `json:"path"`
	From       int    `json:"from"`
	To         int    `json:"to"`
	Depth      int    `json:"depth"`
	Pattern    string `json:"pattern"`
	IgnoreCase bool   `json:"ignore_case"`
	Literal    bool   `json:"literal"`
	Staged     bool   `json:"staged"`
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
}

func (r *linkRoot) opInfo() (any, error) {
	type extStat struct {
		Ext   string `json:"ext"`
		Count int    `json:"count"`
		Bytes int64  `json:"bytes"`
	}
	exts := map[string]*extStat{}
	var total int64
	files := 0
	var newest time.Time
	trunc := r.walk(r.dir, time.Now().Add(8*time.Second), func(f linkFile) bool {
		files++
		total += f.Size
		if f.MTime.After(newest) {
			newest = f.MTime
		}
		e := strings.ToLower(filepath.Ext(f.Path))
		if e == "" {
			e = "(なし)"
		}
		s := exts[e]
		if s == nil {
			s = &extStat{Ext: e}
			exts[e] = s
		}
		s.Count++
		s.Bytes += f.Size
		return true
	})
	list := make([]extStat, 0, len(exts))
	for _, v := range exts {
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Count != list[j].Count {
			return list[i].Count > list[j].Count
		}
		return list[i].Ext < list[j].Ext
	})
	if len(list) > 8 {
		list = list[:8]
	}
	res := map[string]any{
		"name": filepath.Base(r.dir), "files": files, "bytes": total, "exts": list,
		"truncated": trunc, "allow_push": r.allowPush, "newest": newest.Unix(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if out, err := linkGit(ctx, r.dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		g := map[string]any{"branch": strings.TrimSpace(out)}
		if out, err := linkGit(ctx, r.dir, "log", "-1", "--format=%h%x09%s%x09%cr%x09%an"); err == nil {
			f := strings.SplitN(strings.TrimSpace(out), "\t", 4)
			if len(f) == 4 {
				subj, _ := linkRedact(f[1])
				g["commit"], g["subject"], g["when"], g["author"] = f[0], subj, f[2], f[3]
			}
		}
		if out, err := linkGit(ctx, r.dir, "status", "--porcelain"); err == nil {
			n := 0
			for _, l := range strings.Split(out, "\n") {
				if strings.TrimSpace(l) != "" && !linkIsDenied(strings.TrimSpace(l[min(3, len(l)):])) {
					n++
				}
			}
			g["dirty"] = n
		}
		res["git"] = g
	}
	return res, nil
}

func (r *linkRoot) opLs(a linkArgs) (any, error) {
	abs, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return nil, errors.New("フォルダではありません")
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return nil, errors.New("読み込めません")
	}
	type item struct {
		Name  string `json:"name"`
		Dir   bool   `json:"dir"`
		Size  int64  `json:"size"`
		MTime int64  `json:"mtime"`
	}
	var items []item
	for _, e := range ents {
		er := e.Name()
		if rel != "." {
			er = rel + "/" + e.Name()
		}
		if linkIsDenied(er) || r.ignore.matches(er, e.IsDir()) || (e.IsDir() && ignoredDirs[e.Name()]) {
			continue
		}
		if !e.IsDir() && !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{e.Name(), e.IsDir(), info.Size(), info.ModTime().Unix()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Dir != items[j].Dir {
			return items[i].Dir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	total := len(items)
	if total > linkMaxLs {
		items = items[:linkMaxLs]
	}
	return map[string]any{"path": rel, "items": items, "total": total}, nil
}

func (r *linkRoot) opTree(a linkArgs) (any, error) {
	abs, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	depth := a.Depth
	if depth < 1 || depth > 6 {
		depth = 3
	}
	type item struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	var items []item
	total := 0
	base := 0
	if rel != "." {
		base = strings.Count(rel, "/") + 1
	}
	r.walk(abs, time.Now().Add(8*time.Second), func(f linkFile) bool {
		total++
		if strings.Count(f.Path, "/")+1-base <= depth && len(items) < linkMaxTree {
			items = append(items, item{f.Path, f.Size})
		}
		return true
	})
	return map[string]any{"path": rel, "depth": depth, "items": items, "total": total}, nil
}

func (r *linkRoot) opCat(a linkArgs) (any, error) {
	abs, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, errors.New("ファイルを指定してください")
	}
	text, err := linkReadText(abs, linkMaxCatBytes)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	from, to := a.From, a.To
	if from < 1 {
		from = 1
	}
	if to < from || to > len(lines) {
		to = len(lines)
	}
	if from > len(lines) {
		return nil, fmt.Errorf("%d 行しかありません", len(lines))
	}
	truncated := false
	if to-from+1 > linkMaxCatLines {
		to, truncated = from+linkMaxCatLines-1, true
	}
	var b strings.Builder
	redacted := false
	for i := from; i <= to; i++ {
		l, ch := linkRedact(lines[i-1])
		redacted = redacted || ch
		if b.Len()+len(l) > linkMaxCatChars {
			to, truncated = i-1, true
			break
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return map[string]any{
		"path": rel, "from": from, "to": to, "total_lines": len(lines), "size": len(text),
		"content": b.String(), "truncated": truncated, "redacted": redacted,
	}, nil
}

func (r *linkRoot) opGrep(a linkArgs) (any, error) {
	if a.Pattern == "" || len(a.Pattern) > 200 {
		return nil, errors.New("検索語は 1〜200 文字にしてください")
	}
	expr := a.Pattern
	if a.Literal {
		expr = regexp.QuoteMeta(expr)
	}
	if a.IgnoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr) // Go の正規表現は RE2 なので、実行時間が入力に比例する
	if err != nil {
		return nil, errors.New("正規表現が正しくありません")
	}
	abs, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	type hit struct {
		File  string `json:"file"`
		Line  int    `json:"line"`
		Text  string `json:"text"`
		Start int    `json:"start"`
		End   int    `json:"end"`
	}
	var hits []hit
	filesWith := map[string]bool{}
	scanned, matchTotal := 0, 0
	deadline := time.Now().Add(10 * time.Second)
	scan := func(f linkFile) bool {
		scanned++
		if scanned > linkMaxGrepFiles {
			return false
		}
		if f.Size > linkMaxCatBytes {
			return true
		}
		text, err := linkReadText(f.Abs, linkMaxCatBytes)
		if err != nil {
			return true
		}
		for i, line := range strings.Split(text, "\n") {
			loc := re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			matchTotal++
			filesWith[f.Path] = true
			if len(hits) >= linkMaxGrep {
				continue
			}
			rs := []rune(line)
			s := utf8.RuneCountInString(line[:loc[0]])
			e := utf8.RuneCountInString(line[:loc[1]])
			off := 0
			if len(rs) > 200 {
				off = max(0, s-60)
				end := min(len(rs), off+200)
				rs = rs[off:end]
			}
			t := strings.TrimRight(string(rs), "\r")
			s, e = s-off, min(e-off, len(rs))
			if red, ch := linkRedact(t); ch {
				t, s, e = red, -1, -1
			}
			hits = append(hits, hit{f.Path, i + 1, t, s, e})
		}
		return time.Now().Before(deadline)
	}
	st, _ := os.Stat(abs)
	truncated := false
	if st != nil && !st.IsDir() {
		scan(linkFile{Path: rel, Size: st.Size(), Abs: abs})
	} else {
		truncated = r.walk(abs, deadline, scan)
	}
	return map[string]any{
		"pattern": a.Pattern, "path": rel, "hits": hits, "matches": matchTotal,
		"files": len(filesWith), "scanned": scanned, "truncated": truncated || matchTotal > len(hits),
	}, nil
}

func (r *linkRoot) opDiff(a linkArgs) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := linkGit(ctx, r.dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, errors.New("git リポジトリではありません")
	}
	_, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	spec := []string{}
	if rel != "." {
		spec = append(spec, ":(literal)"+rel)
	}
	for _, d := range []string{".env", ".env.*", "*.pem", "*.key", "*.p12", "*.pfx", "id_rsa*", "id_ed25519*", "id_ecdsa*", ".npmrc", ".netrc", ".git-credentials", "credentials", "credentials.*"} {
		spec = append(spec, ":(exclude,glob)**/"+d)
	}
	base := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv"}
	if a.Staged {
		base = append(base, "--cached")
	}
	run := func(extra ...string) (string, error) {
		args := append(append([]string{}, base...), extra...)
		args = append(args, "--")
		return linkGit(ctx, r.dir, append(args, spec...)...)
	}
	stat, err := run("--numstat")
	if err != nil {
		return nil, errors.New("差分を取得できません")
	}
	type fileStat struct {
		Path string `json:"path"`
		Add  int    `json:"add"`
		Del  int    `json:"del"`
		Bin  bool   `json:"bin"`
	}
	var stats []fileStat
	for _, l := range strings.Split(strings.TrimSpace(stat), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		fs := fileStat{Path: f[2]}
		if f[0] == "-" {
			fs.Bin = true
		} else {
			fmt.Sscanf(f[0], "%d", &fs.Add)
			fmt.Sscanf(f[1], "%d", &fs.Del)
		}
		stats = append(stats, fs)
	}
	patch, _ := run("--unified=2")
	truncated := false
	if len(patch) > linkMaxPatch {
		patch, truncated = patch[:linkMaxPatch], true
		if i := strings.LastIndex(patch, "\n"); i > 0 {
			patch = patch[:i]
		}
	}
	patch, redacted := linkRedact(patch)
	if len(stats) > 60 {
		stats = stats[:60]
		truncated = true
	}
	return map[string]any{"stats": stats, "patch": patch, "truncated": truncated, "redacted": redacted, "staged": a.Staged}, nil
}

func (r *linkRoot) opPush(a linkArgs) (any, error) {
	if !r.allowPush {
		return nil, errors.New("このセッションは push が許可されていません(bin link --allow-push で起動してください)")
	}
	_, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	abs := r.dir
	if rel != "." {
		abs, _, _ = r.resolve(rel)
	}
	vis := a.Visibility
	if vis == "" {
		vis = "unlisted"
	}
	if vis != "unlisted" && vis != "private" && vis != "public" {
		return nil, errors.New("公開範囲は public / unlisted / private のどれかです")
	}
	type skip struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	var files []GistFile
	var skips []skip
	var total int64
	r.walk(abs, time.Now().Add(15*time.Second), func(f linkFile) bool {
		if len(files) >= maxGistFiles {
			skips = append(skips, skip{f.Path, "ファイル数の上限"})
			return true
		}
		text, err := linkReadText(f.Abs, 5<<20)
		if err != nil {
			skips = append(skips, skip{f.Path, err.Error()})
			return true
		}
		if total+int64(len(text)) > linkMaxPushBytes {
			skips = append(skips, skip{f.Path, "合計サイズの上限"})
			return true
		}
		gf := GistFile{Filename: f.Path, Content: text}
		if hits := scanFiles([]GistFile{gf}); len(hits) > 0 {
			skips = append(skips, skip{f.Path, "秘密情報らしき文字列(" + hits[0].kind + ")"})
			return true
		}
		if validateGistPath(f.Path) != nil {
			skips = append(skips, skip{f.Path, "ファイル名が使えません"})
			return true
		}
		total += int64(len(text))
		files = append(files, gf)
		return true
	})
	if len(files) == 0 {
		return nil, errors.New("アップロードできるファイルがありません")
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = filepath.Base(r.dir)
	}
	if rs := []rune(title); len(rs) > 100 {
		title = string(rs[:100])
	}
	if err := freshenSession(r.cfg, r.session); err != nil {
		return nil, errors.New("ログインの更新に失敗しました。bin login をやり直してください")
	}
	id, err := saveGist(r.cfg, r.session, nil, title, "Discord から push", vis, files, "Discord から push")
	if err != nil {
		return nil, err
	}
	if len(skips) > 30 {
		skips = skips[:30]
	}
	return map[string]any{
		"id": id, "url": strings.TrimRight(r.cfg.SiteURL, "/") + "/" + id,
		"files": len(files), "bytes": total, "visibility": vis, "skipped": skips,
	}, nil
}

func (r *linkRoot) handle(op string, raw json.RawMessage) (res any, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, errors.New("内部エラーが発生しました")
		}
	}()
	var a linkArgs
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &a); e != nil {
			return nil, errors.New("引数が正しくありません")
		}
	}
	switch op {
	case "info":
		return r.opInfo()
	case "ls":
		return r.opLs(a)
	case "tree":
		return r.opTree(a)
	case "cat":
		return r.opCat(a)
	case "grep":
		return r.opGrep(a)
	case "diff":
		return r.opDiff(a)
	case "push":
		return r.opPush(a)
	case "find":
		return r.opFind(a)
	case "recent":
		return r.opRecent(a)
	case "todo":
		return r.opTodo(a)
	case "stat":
		return r.opStat(a)
	case "log":
		return r.opLog(a)
	case "show":
		return r.opShow(a)
	case "deps":
		return r.opDeps(a)
	}
	return nil, errors.New("未対応の操作です")
}

// ---- bin-server との通信 ----

type linkClient struct {
	cfg     Config
	session *Session
	http    *http.Client
}

func (c *linkClient) url(path string) string {
	return strings.TrimRight(c.cfg.SupabaseURL, "/") + "/bin/" + path
}

func (c *linkClient) do(method, path string, body any, timeout time.Duration) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.url(path), rd)
	if err != nil {
		return 0, nil, err
	}
	if err := freshenSession(c.cfg, c.session); err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.session.AccessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := *c.http
	cl.Timeout = timeout
	res, err := cl.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return res.StatusCode, data, nil
}

func (c *linkClient) register(name string, allowPush bool, info any, prev, cid string) (string, error) {
	st, data, err := c.do(http.MethodPost, "link/register", map[string]any{
		"name": name, "allow_push": allowPush, "info": info, "session": prev, "cid": cid,
	}, 15*time.Second)
	if err != nil {
		return "", err
	}
	if st != http.StatusOK {
		return "", errors.New(apiErrorMessage(data))
	}
	var out struct {
		Session string `json:"session"`
	}
	if json.Unmarshal(data, &out) != nil || out.Session == "" {
		return "", errors.New("unexpected response")
	}
	return out.Session, nil
}

func cmdLink(args []string) {
	p := parseArgs(args, map[string]string{"--name": "name", "--idle": "idle"}, map[string]string{"--allow-push": "push"})
	dir := "."
	if len(p.positional) > 0 {
		dir = p.positional[0]
	}
	if len(p.positional) > 1 {
		usageExit("bin link [<dir>] [--name <name>] [--allow-push] [--idle <minutes>]")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fail(err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		fail(fmt.Errorf(T("フォルダを開けません: %s"), dir))
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		fail(fmt.Errorf(T("フォルダを指定してください: %s"), dir))
	}
	if home, _ := os.UserHomeDir(); home != "" {
		if h, err := filepath.EvalSymlinks(home); err == nil && (real == h || real == filepath.Dir(h) || real == string(filepath.Separator)) {
			fail(errors.New(T("ホームフォルダやルートは指定できません。プロジェクトのフォルダで実行してください")))
		}
	}
	name := filepath.Base(real)
	if v, ok := p.value("name"); ok && strings.TrimSpace(v) != "" {
		name = strings.TrimSpace(v)
	}
	idle := 120
	if v, ok := p.value("idle"); ok {
		if _, err := fmt.Sscanf(v, "%d", &idle); err != nil || idle < 1 || idle > 1440 {
			fail(errors.New(T("--idle は 1〜1440 の分数で指定してください")))
		}
	}

	cfg, session := requireSession()
	root := &linkRoot{dir: real, allowPush: p.bools["push"], ignore: loadIgnore(real), cfg: cfg, session: session}
	c := &linkClient{cfg: cfg, session: session, http: &http.Client{}}

	info, _ := root.opInfo()
	cid := newConnID()
	sid, err := c.register(name, root.allowPush, info, "", cid)
	if err != nil {
		fail(fmt.Errorf(T("接続できません: %w"), err))
	}

	fmt.Printf("%s %s\n", green("●"), bold(T("待機中"))+"  "+name)
	fmt.Printf("  %s %s\n", dim(T("フォルダ")), real)
	fmt.Printf("  %s %s\n", dim(T("セッション")), sid)
	fmt.Printf("  %s %s\n", dim(T("接続ID  ")), cyan(connLabel(cid)))
	fmt.Printf("  %s %s  %s\n", dim(T("指紋    ")), cyan(connFingerprint(cid)), dim(T("← Discord の表示と同じか確認できます")))
	if root.allowPush {
		fmt.Printf("  %s\n", T("push を許可しています"))
	} else {
		fmt.Printf("  %s\n", dim(T("読み取り専用(push は --allow-push で許可)")))
	}
	fmt.Println(dim(fmt.Sprintf(T("Discord で /bin sync を実行してください。何も操作がないまま %d 分たつと自動で終了します。Ctrl+C で終了"), idle)))

	closeSession := func() {
		_, _, _ = c.do(http.MethodPost, "link/close", map[string]string{"session": sid}, 5*time.Second)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		closeSession()
		fmt.Println("\n" + T("待機を終了しました"))
		os.Exit(0)
	}()

	lastActive := time.Now()
	backoff := time.Second
	for {
		if time.Since(lastActive) > time.Duration(idle)*time.Minute {
			closeSession()
			fmt.Println(T("操作がなかったため待機を終了しました"))
			return
		}
		st, data, err := c.do(http.MethodGet, "link/poll?session="+sid, nil, 40*time.Second)
		switch {
		case err != nil:
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		case st == http.StatusGone:
			// サーバーの再起動などでセッションが消えた: 登録し直す
			ns, err := c.register(name, root.allowPush, info, sid, cid)
			if err != nil {
				time.Sleep(backoff)
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}
			sid = ns
			fmt.Println(dim(fmt.Sprintf(T("再接続しました(セッション %s)"), sid)))
			backoff = time.Second
			continue
		case st == http.StatusUnauthorized:
			fail(errors.New(T("ログインの期限が切れました。bin login をやり直してください")))
		case st == http.StatusNoContent:
			backoff = time.Second
			continue
		case st != http.StatusOK:
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		var job struct {
			ID   string          `json:"id"`
			Op   string          `json:"op"`
			Args json.RawMessage `json:"args"`
		}
		if json.Unmarshal(data, &job) != nil || job.ID == "" {
			continue
		}
		lastActive = time.Now()
		started := time.Now()
		res, herr := root.handle(job.Op, job.Args)
		digest := ""
		reply := map[string]any{"session": sid, "id": job.ID, "ok": herr == nil}
		if herr != nil {
			reply["error"] = herr.Error()
		} else {
			// 結果は JSON 文字列のまま送り、その SHA-256 を添える(Discord 側が同じ値を計算して照合する)
			payload, _ := json.Marshal(res)
			sum := sha256.Sum256(payload)
			digest = hex.EncodeToString(sum[:])
			reply["payload"] = string(payload)
			reply["sha256"] = digest
		}
		_, _, _ = c.do(http.MethodPost, "link/reply", reply, 20*time.Second)
		status := green("✓")
		if herr != nil {
			status = "✗"
		}
		sum := ""
		if digest != "" {
			sum = " " + dim("sha256:"+digest[:12])
		}
		fmt.Printf("%s %s  %s %s%s\n", dim(time.Now().Format("15:04:05")), status, job.Op, dim(fmt.Sprintf("(%dms)", time.Since(started).Milliseconds())), sum)
	}
}

// newConnID は接続(bin link の1回の実行)ごとに変わる 128 ビットの乱数。Discord 側の表示と突き合わせる指紋の元になる。
func newConnID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		fail(err)
	}
	return hex.EncodeToString(b)
}

// connLabel は表示用の短い接続ID
func connLabel(cid string) string { return "lk_" + cid[:12] }

// connFingerprint は接続IDの SHA-256 の先頭 64 ビットを 4 文字ずつ区切ったもの(Lapbot も同じ式で計算する)
func connFingerprint(cid string) string {
	sum := sha256.Sum256([]byte(cid))
	h := hex.EncodeToString(sum[:8])
	return h[0:4] + " " + h[4:8] + " " + h[8:12] + " " + h[12:16]
}
