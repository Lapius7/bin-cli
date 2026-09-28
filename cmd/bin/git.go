package main

// bin g: git と同じ操作感で使うための互換レイヤー(bin g <gitのサブコマンド> ...)。
//
// clone したフォルダをリポジトリ、Gistを origin、リビジョンをコミットとみなす(ブランチは main だけ)。
// Gist側はファイルの最新状態しか受け取れないので、push していないコミットは手元の .lapbin/ に置き、
// push の時に「1コミット=1リビジョン」として古い順に保存する。
//   .lapbin.json       … bin status/pull/push と共通の基準(origin/main の時点)
//   .lapbin/state.json … インデックス(ステージ)・未pushのコミット・stash・fetch で知った最新リビジョン
//   .lapbin/objects/   … ファイルの内容(SHA-256 ごと)

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const gitStateDir = ".lapbin"

type gitCommit struct {
	Hash    string            `json:"hash"`
	Message string            `json:"message"`
	Time    string            `json:"time"`
	Tree    map[string]string `json:"tree"`
}

type gitStash struct {
	Message string            `json:"message"`
	Time    string            `json:"time"`
	Base    map[string]string `json:"base"`
	Index   map[string]string `json:"index"`
	Work    map[string]string `json:"work"`
}

type gitState struct {
	// nil なら HEAD と同じ(何もステージしていない)
	Index   map[string]string `json:"index,omitempty"`
	Commits []gitCommit       `json:"commits,omitempty"` // 未pushのコミット(古い順)
	Stash   []gitStash        `json:"stash,omitempty"`   // 新しい順
	Fetched int               `json:"fetched_revision,omitempty"`
}

type gitRepo struct {
	dir   string
	meta  syncMeta
	st    gitState
	blobs map[string]string // SHA-256 → 内容

	work   map[string]string // 作業ツリー(パス → 内容)
	skips  []skipped
	ignore []ignoreRule

	revs       []Revision
	revsLoaded bool
	author     string
}

// gitEntry はログ上の1コミット(未pushのローカルコミット、またはGistのリビジョン)
type gitEntry struct {
	hash    string
	message string
	time    time.Time
	tree    map[string]string
	local   bool
	rev     int
}

var gitNoPager bool

func gitFatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fatal: %s\n", fmt.Sprintf(format, args...))
	os.Exit(128)
}

func gitError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func gitHint(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, dim("hint: "+fmt.Sprintf(format, args...)))
}

// openRepo はカレントから親をたどって .lapbin.json を探す(bin g init 直後の、まだGistと紐付いていないフォルダも可)
func openRepo() *gitRepo {
	dir, err := os.Getwd()
	if err != nil {
		gitFatal("%v", err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, syncMetaFile))
		if err == nil {
			r := &gitRepo{dir: dir, blobs: map[string]string{}}
			if err := json.Unmarshal(data, &r.meta); err != nil || (r.meta.ID != "" && !gistIDPattern.MatchString(r.meta.ID)) {
				gitFatal(T("%s を読み込めません"), filepath.Join(dir, syncMetaFile))
			}
			if r.meta.Files == nil {
				r.meta.Files = map[string]string{}
			}
			if data, err := os.ReadFile(r.statePath()); err == nil {
				if err := json.Unmarshal(data, &r.st); err != nil {
					gitFatal(T("%s を読み込めません"), r.statePath())
				}
			}
			r.loadIgnore()
			return r
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			gitFatal(T("Gistと同期しているフォルダではありません(bin g clone <id> か bin g init で作成してください)"))
		}
		dir = parent
	}
}

func (r *gitRepo) statePath() string { return filepath.Join(r.dir, gitStateDir, "state.json") }

func (r *gitRepo) save() {
	if r.st.Index != nil && sameTree(r.st.Index, r.head()) {
		r.st.Index = nil
	}
	if err := os.MkdirAll(filepath.Join(r.dir, gitStateDir), 0o755); err != nil {
		gitFatal("%v", err)
	}
	data, _ := json.MarshalIndent(r.st, "", "  ")
	if err := os.WriteFile(r.statePath(), append(data, '\n'), 0o644); err != nil {
		gitFatal("%v", err)
	}
}

func (r *gitRepo) saveMeta() {
	if err := writeSyncMeta(r.dir, r.meta); err != nil {
		gitFatal("%v", err)
	}
}

// ---- ファイルの内容 ----

func (r *gitRepo) objectPath(h string) string {
	return filepath.Join(r.dir, gitStateDir, "objects", h[:2], h[2:])
}

// put は内容を保存してハッシュを返す(ステージ・コミット・stash したものは後から取り出せるようにする)
func (r *gitRepo) put(content string) string {
	h := contentHash(content)
	r.blobs[h] = content
	p := r.objectPath(h)
	if _, err := os.Stat(p); err != nil {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			gitFatal("%v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			gitFatal("%v", err)
		}
	}
	return h
}

// blob はハッシュから内容を引く。手元に無ければ(clone した時点の内容など)Gistの履歴から取ってくる
func (r *gitRepo) blob(h string) string {
	if c, ok := r.blobs[h]; ok {
		return c
	}
	if data, err := os.ReadFile(r.objectPath(h)); err == nil {
		r.blobs[h] = string(data)
		return string(data)
	}
	r.worktree()
	if c, ok := r.blobs[h]; ok {
		return c
	}
	r.remoteRevisions()
	if c, ok := r.blobs[h]; ok {
		return c
	}
	gitFatal(T("ファイルの内容(%s)が見つかりません"), h[:12])
	return ""
}

func (r *gitRepo) worktree() map[string]string {
	if r.work == nil {
		r.work, r.skips = localFiles(r.dir)
		for _, c := range r.work {
			r.blobs[contentHash(c)] = c
		}
	}
	return r.work
}

func (r *gitRepo) workTree() map[string]string {
	out := map[string]string{}
	for p, c := range r.worktree() {
		out[p] = contentHash(c)
	}
	return out
}

func (r *gitRepo) remoteRevisions() []Revision {
	if r.revsLoaded || r.meta.ID == "" {
		return r.revs
	}
	r.revsLoaded = true
	cfg, session := optionalSession()
	revs, err := getRevisions(cfg, session, r.meta.ID)
	if err != nil {
		warnErr(T("Gistの履歴を取得できませんでした: %v"), err)
		return nil
	}
	for _, rv := range revs {
		for _, f := range rv.Files {
			r.blobs[contentHash(f.Content)] = f.Content
		}
	}
	r.revs = revs
	return revs
}

// ---- HEAD・インデックス ----

func (r *gitRepo) head() map[string]string {
	if n := len(r.st.Commits); n > 0 {
		return r.st.Commits[n-1].Tree
	}
	return r.meta.Files
}

func (r *gitRepo) index() map[string]string {
	if r.st.Index == nil {
		r.st.Index = copyTree(r.head())
	}
	return r.st.Index
}

func (r *gitRepo) headHash() string {
	if n := len(r.st.Commits); n > 0 {
		return r.st.Commits[n-1].Hash
	}
	if r.meta.ID != "" && r.meta.Revision > 0 {
		return revisionHash(r.meta.ID, r.meta.Revision)
	}
	return ""
}

// originRevision は origin/main が指すリビジョン(fetch で知った分も含む)
func (r *gitRepo) originRevision() int {
	if r.st.Fetched > r.meta.Revision {
		return r.st.Fetched
	}
	return r.meta.Revision
}

func (r *gitRepo) remoteURL() string {
	return gistURL(loadConfig(), r.meta.ID) + ".git"
}

func (r *gitRepo) authorName() string {
	if r.author != "" {
		return r.author
	}
	r.author = T("あなた")
	if s, err := loadSession(); err == nil && s.Email != "" {
		r.author = s.Email
	}
	if r.meta.ID != "" {
		cfg, session := optionalSession()
		if g, err := getGist(cfg, session, r.meta.ID); err == nil {
			switch {
			case g.Owner.Handle != nil && *g.Owner.Handle != "":
				r.author = "@" + *g.Owner.Handle
			case g.Owner.DisplayName != nil && *g.Owner.DisplayName != "":
				r.author = *g.Owner.DisplayName
			}
		}
	}
	return r.author
}

// ---- 履歴 ----

func (r *gitRepo) localEntries() []gitEntry {
	var out []gitEntry
	for i := len(r.st.Commits) - 1; i >= 0; i-- {
		c := r.st.Commits[i]
		t, _ := time.Parse(time.RFC3339, c.Time)
		out = append(out, gitEntry{hash: c.Hash, message: c.Message, time: t.Local(), tree: c.Tree, local: true})
	}
	return out
}

// remoteEntries はGistのリビジョンを新しい順に返す(upTo より新しいものは除く。0 なら全部)
func (r *gitRepo) remoteEntries(upTo int) []gitEntry {
	revs := r.remoteRevisions()
	var out []gitEntry
	for i, rv := range revs {
		if upTo > 0 && rv.Revision > upTo {
			continue
		}
		tree := map[string]string{}
		for _, f := range rv.Files {
			tree[f.Filename] = contentHash(f.Content)
		}
		var prev map[string]string
		if i+1 < len(revs) {
			prev = map[string]string{}
			for _, f := range revs[i+1].Files {
				prev[f.Filename] = contentHash(f.Content)
			}
		}
		msg := rv.Message
		if msg == "" {
			msg = autoMessage(prev, tree)
		}
		t, _ := time.Parse(time.RFC3339Nano, rv.CreatedAt)
		out = append(out, gitEntry{hash: revisionHash(r.meta.ID, rv.Revision), message: msg, time: t.Local(), tree: tree, rev: rv.Revision})
	}
	return out
}

func autoMessage(prev, next map[string]string) string {
	if prev == nil {
		return T("Gistを作成")
	}
	changes := diffTrees(prev, next)
	switch len(changes) {
	case 0:
		return T("タイトル等を変更")
	case 1:
		status := map[byte]string{'A': T("追加"), 'M': T("変更"), 'D': T("削除")}[changes[0].kind]
		return fmt.Sprintf(T("%s を%s"), changes[0].path, status)
	}
	return fmt.Sprintf(T("%dファイルを変更"), len(changes))
}

func (r *gitRepo) headChain() []gitEntry {
	out := r.localEntries()
	if r.meta.ID != "" && r.meta.Revision > 0 {
		out = append(out, r.remoteEntries(r.meta.Revision)...)
	}
	return out
}

var refSuffixPattern = regexp.MustCompile(`^(.*?)((?:[~^][0-9]*)*)$`)
var hexPattern = regexp.MustCompile(`^[0-9a-f]{4,}$`)

// looksLikeRef は diff などで、引数をコミット指定とパスのどちらとして扱うかの判定に使う
func looksLikeRef(s string) bool {
	m := refSuffixPattern.FindStringSubmatch(s)
	switch m[1] {
	case "HEAD", "@", "main", "origin", "origin/main", "origin/HEAD", "FETCH_HEAD", "ORIG_HEAD":
		return true
	}
	if strings.HasPrefix(m[1], "stash@{") {
		return false
	}
	return hexPattern.MatchString(m[1])
}

// resolve はコミットの指定(HEAD~2・origin/main・ハッシュの先頭など)を、そのコミットから始まる履歴に解決する
func (r *gitRepo) resolve(ref string) []gitEntry {
	m := refSuffixPattern.FindStringSubmatch(ref)
	base, suffix := m[1], m[2]
	var chain []gitEntry
	idx := 0
	switch base {
	case "HEAD", "@", "main", "refs/heads/main", "":
		chain = r.headChain()
	case "origin", "origin/main", "origin/HEAD", "remotes/origin/main", "refs/remotes/origin/main", "FETCH_HEAD":
		chain = r.remoteEntries(r.originRevision())
	default:
		if !hexPattern.MatchString(base) {
			gitFatal(T("'%s' はコミットとして解釈できません(ブランチは main だけです)"), ref)
		}
		found := false
		for _, c := range [][]gitEntry{r.headChain(), r.remoteEntries(0)} {
			for i, e := range c {
				if strings.HasPrefix(e.hash, base) {
					chain, idx, found = c, i, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			gitFatal(T("'%s' はコミットとして解釈できません(ブランチは main だけです)"), ref)
		}
	}
	for _, part := range regexp.MustCompile(`[~^][0-9]*`).FindAllString(suffix, -1) {
		n := 1
		if len(part) > 1 {
			n, _ = strconv.Atoi(part[1:])
			if part[0] == '^' && n > 1 {
				gitFatal(T("'%s' はコミットとして解釈できません(ブランチは main だけです)"), ref)
			}
			if part[0] == '^' {
				n = min(n, 1)
			}
		}
		idx += n
	}
	if len(chain) == 0 {
		gitFatal(T("ブランチ 'main' にはまだコミットがありません"))
	}
	if idx >= len(chain) {
		gitFatal(T("'%s' はコミットとして解釈できません(ブランチは main だけです)"), ref)
	}
	return chain[idx:]
}

// ---- ツリー ----

type treeChange struct {
	path     string
	kind     byte // A M D
	from, to string
}

func diffTrees(a, b map[string]string) []treeChange {
	var out []treeChange
	for p, h := range b {
		if old, ok := a[p]; !ok {
			out = append(out, treeChange{p, 'A', "", h})
		} else if old != h {
			out = append(out, treeChange{p, 'M', old, h})
		}
	}
	for p, h := range a {
		if _, ok := b[p]; !ok {
			out = append(out, treeChange{p, 'D', h, ""})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func copyTree(t map[string]string) map[string]string {
	out := make(map[string]string, len(t))
	for k, v := range t {
		out[k] = v
	}
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkoutTree は作業ツリーを target に合わせる(tracked のうち target に無いものは消す)
func (r *gitRepo) checkoutTree(target map[string]string, tracked map[string]string) {
	work := r.workTree()
	for p := range tracked {
		if _, ok := target[p]; ok {
			continue
		}
		if full, err := safeJoin(r.dir, p); err == nil {
			_ = os.Remove(full)
			r.removeEmptyParents(full)
		}
	}
	for p, h := range target {
		if work[p] == h {
			continue
		}
		r.writeFile(p, r.blob(h))
	}
	r.work = nil
}

func (r *gitRepo) writeFile(p, content string) {
	full, err := safeJoin(r.dir, p)
	if err != nil {
		gitFatal("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		gitFatal("%v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		gitFatal("%v", err)
	}
}

func (r *gitRepo) removeEmptyParents(full string) {
	for d := filepath.Dir(full); d != r.dir && strings.HasPrefix(d, r.dir); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			return
		}
	}
}

// ---- パス指定 ----

// pathMatcher はコマンドライン上のパス(カレントからの相対・glob可)を、リポジトリ内のパスに対する判定にする
type pathMatcher struct {
	spec, rel string
	all       bool
	glob      bool
}

func (r *gitRepo) matchers(specs []string) []pathMatcher {
	cwd, _ := os.Getwd()
	var out []pathMatcher
	for _, s := range specs {
		rel, err := filepath.Rel(r.dir, filepath.Join(cwd, s))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			gitFatal(T("%s: '%s' はリポジトリの外です"), s, r.dir)
		}
		rel = filepath.ToSlash(rel)
		out = append(out, pathMatcher{spec: s, rel: rel, all: rel == ".", glob: strings.ContainsAny(s, "*?[")})
	}
	return out
}

func (m pathMatcher) match(p string) bool {
	if m.all || p == m.rel || strings.HasPrefix(p, m.rel+"/") {
		return true
	}
	if m.glob {
		if ok, _ := path.Match(m.rel, p); ok {
			return true
		}
	}
	return false
}

func matchAny(ms []pathMatcher, p string) bool {
	if len(ms) == 0 {
		return true
	}
	for _, m := range ms {
		if m.match(p) {
			return true
		}
	}
	return false
}

// display はリポジトリ内のパスを、git と同じくカレントからの相対パスにする
func (r *gitRepo) display(p string) string {
	cwd, _ := os.Getwd()
	rel, err := filepath.Rel(cwd, filepath.Join(r.dir, p))
	if err != nil {
		return p
	}
	return filepath.ToSlash(rel)
}

// ---- .gitignore(ルート直下のみ。追跡していないファイルにだけ効く) ----

type ignoreRule struct {
	pat               string
	neg, dirOnly, anc bool
}

func (r *gitRepo) loadIgnore() {
	data, err := os.ReadFile(filepath.Join(r.dir, ".gitignore"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, " \r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.neg, line = true, line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly, line = true, strings.TrimSuffix(line, "/")
		}
		line = strings.TrimPrefix(line, "**/")
		if strings.HasPrefix(line, "/") {
			rule.anc, line = true, line[1:]
		} else if strings.Contains(line, "/") {
			rule.anc = true
		}
		rule.pat = line
		r.ignore = append(r.ignore, rule)
	}
}

func (r *gitRepo) ignored(p string) bool {
	parts := strings.Split(p, "/")
	ignored := false
	for i := 1; i <= len(parts); i++ {
		sub := strings.Join(parts[:i], "/")
		isDir := i < len(parts)
		for _, rule := range r.ignore {
			if rule.dirOnly && !isDir {
				continue
			}
			target := parts[i-1]
			if rule.anc {
				target = sub
			}
			if ok, _ := path.Match(rule.pat, target); ok {
				ignored = !rule.neg
			}
		}
		if ignored && isDir {
			return true
		}
	}
	return ignored
}

// ---- 引数 ----

type gArgs struct {
	pos   []string
	paths []string // -- より後
	dash  bool
	vals  map[string][]string
}

func (a gArgs) has(k string) bool { _, ok := a.vals[k]; return ok }

func (a gArgs) val(k string) string {
	v := a.vals[k]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// parseG は git 風の引数を解析する(-am "msg" のようなまとめ書き・--opt=value・-n5 に対応)。
// opts はオプションの綴り → キー、valued は値を取るキー
func parseG(args []string, opts map[string]string, valued ...string) gArgs {
	isVal := map[string]bool{}
	for _, v := range valued {
		isVal[v] = true
	}
	a := gArgs{vals: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--" {
			a.dash = true
			a.paths = append(a.paths, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(s, "-") || s == "-" {
			a.pos = append(a.pos, s)
			continue
		}
		if strings.HasPrefix(s, "--") {
			name, v, hasV := strings.Cut(s, "=")
			key, ok := opts[name]
			if !ok {
				gitUsageError(T("不明なオプションです: %s"), s)
			}
			if isVal[key] {
				if !hasV {
					if i+1 >= len(args) {
						gitUsageError(T("%s には値が必要です"), name)
					}
					i++
					v = args[i]
				}
				a.vals[key] = append(a.vals[key], v)
			} else {
				a.vals[key] = append(a.vals[key], "")
			}
			continue
		}
		for j := 1; j < len(s); j++ {
			name := "-" + string(s[j])
			key, ok := opts[name]
			if !ok {
				gitUsageError(T("不明なオプションです: %s"), name)
			}
			if !isVal[key] {
				a.vals[key] = append(a.vals[key], "")
				continue
			}
			v := s[j+1:]
			if v == "" {
				if i+1 >= len(args) {
					gitUsageError(T("%s には値が必要です"), name)
				}
				i++
				v = args[i]
			}
			a.vals[key] = append(a.vals[key], v)
			break
		}
	}
	return a
}

func gitUsageError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: %s\n", fmt.Sprintf(format, args...))
	os.Exit(129)
}

// ---- ページャ・エディタ・エイリアス ----

// startPager は git と同じく、端末に出す時だけ出力をページャ(GIT_PAGER → PAGER → less)に通す
func startPager() func() {
	noop := func() {}
	if gitNoPager || !stdoutIsTerminal() || runtime.GOOS == "windows" {
		return noop
	}
	pager := os.Getenv("GIT_PAGER")
	if pager == "" {
		pager = os.Getenv("PAGER")
	}
	if pager == "" {
		pager = "less"
	}
	if pager == "cat" {
		return noop
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return noop
	}
	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pr, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	if os.Getenv("LESS") == "" {
		cmd.Env = append(cmd.Env, "LESS=FRX")
	}
	if err := cmd.Start(); err != nil {
		return noop
	}
	pr.Close()
	orig := os.Stdout
	os.Stdout = pw
	return func() {
		os.Stdout = orig
		pw.Close()
		_ = cmd.Wait()
	}
}

func gitConfig(key string) string {
	out, err := exec.Command("git", "config", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitAlias は git に設定済みのエイリアス(git config alias.st status など)をそのまま使えるようにする
func gitAlias(name string) []string {
	v := gitConfig("alias." + name)
	if v == "" || strings.HasPrefix(v, "!") {
		return nil
	}
	return strings.Fields(v)
}

// editMessage はコミットメッセージをエディタで書いてもらう(-m が無い時。git と同じ順でエディタを選ぶ)
func (r *gitRepo) editMessage(initial string, changes []treeChange) string {
	editor := os.Getenv("GIT_EDITOR")
	if editor == "" {
		editor = gitConfig("core.editor")
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	var b strings.Builder
	b.WriteString(initial + "\n")
	b.WriteString("# " + T("コミットメッセージを入力してください。'#' で始まる行は無視され、空のメッセージならコミットを中止します。") + "\n#\n")
	b.WriteString("# " + T("ブランチ main") + "\n# " + T("コミット予定の変更点:") + "\n")
	for _, c := range changes {
		b.WriteString("#\t" + changeLabel(c.kind) + c.path + "\n")
	}
	file := filepath.Join(r.dir, gitStateDir, "COMMIT_EDITMSG")
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		gitFatal("%v", err)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(editor, file)
	} else {
		cmd = exec.Command("sh", "-c", editor+` "$@"`, editor, file)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		gitError(T("エディタ '%s' が失敗しました: %v"), editor, err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		gitFatal("%v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, strings.TrimRight(l, " \t\r"))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func changeLabel(kind byte) string {
	label := map[byte]string{'A': T("新規ファイル:"), 'M': T("変更:"), 'D': T("削除:")}[kind]
	if w := displayWidth(label); w < 12 {
		label += strings.Repeat(" ", 12-w)
	}
	return label
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
