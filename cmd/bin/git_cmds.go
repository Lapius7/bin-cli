package main

// bin g の各サブコマンド(git.go のリポジトリモデルの上で、git と同じ引数・出力にする)

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var gitCommands map[string]func([]string)

func init() {
	gitCommands = map[string]func([]string){
		"init": gitInit, "clone": gitClone, "status": gitStatus, "add": gitAdd, "rm": gitRm, "mv": gitMv,
		"restore": gitRestore, "checkout": gitCheckout, "switch": gitSwitch, "reset": gitReset,
		"commit": gitCommitCmd, "log": gitLog, "show": gitShow, "diff": gitDiff, "fetch": gitFetch,
		"pull": gitPull, "push": gitPush, "remote": gitRemote, "branch": gitBranch, "stash": gitStashCmd,
		"rev-parse": gitRevParse, "ls-files": gitLsFiles, "clean": gitClean, "help": func([]string) { gitUsage() },
	}
}

func cmdGit(args []string) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch a := args[0]; {
		case a == "-C" && len(args) > 1:
			if err := os.Chdir(args[1]); err != nil {
				gitFatal(T("'%s' に移動できません: %v"), args[1], err)
			}
			args = args[1:]
		case a == "-c" && len(args) > 1:
			args = args[1:] // 設定の上書きは無視する
		case a == "--no-pager" || a == "-P":
			gitNoPager = true
		case a == "-p" || a == "--paginate":
		case a == "--version":
			fmt.Printf("bin g version %s\n", version)
			return
		case a == "-h" || a == "--help":
			gitUsage()
			return
		default:
			gitUsageError(T("不明なオプションです: %s"), a)
		}
		args = args[1:]
	}
	if len(args) == 0 {
		gitUsage()
		os.Exit(1)
	}
	name, rest := args[0], args[1:]
	for depth := 0; depth < 5; depth++ {
		if _, ok := gitCommands[name]; ok {
			break
		}
		exp := gitAlias(name)
		if exp == nil {
			break
		}
		name, rest = exp[0], append(exp[1:], rest...)
	}
	if len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help") {
		gitUsage()
		return
	}
	if f, ok := gitCommands[name]; ok {
		f(rest)
		return
	}
	switch name {
	case "tag":
		gitFatal(T("bin g ではタグは使えません(Gistのタグは bin tag <id> <tag>... で設定できます)"))
	case "merge", "rebase", "cherry-pick", "revert", "blame", "bisect", "worktree", "submodule", "config", "am", "format-patch", "reflog", "grep":
		gitFatal(T("'%s' は bin g では使えません(bin g help で対応コマンドを表示)"), name)
	}
	fmt.Fprintf(os.Stderr, T("bin g: '%s' は bin g のコマンドではありません。'bin g help' を参照してください。")+"\n", name)
	os.Exit(1)
}

func gitUsage() {
	fmt.Println(bold("bin g") + dim(" — "+T("git と同じ感覚でGistを操作する(origin=Gist、ブランチは main だけ)")))
	fmt.Println()
	printUsageSection(T("始める"), [][2]string{
		{"clone <id|url> [<dir>]", T("Gistを取得する")},
		{"init [<dir>]", T("空のリポジトリを作る(最初の push で新しいGistを作成)")},
	})
	printUsageSection(T("変更を記録する"), [][2]string{
		{"status [-s] [-b]", T("作業ツリーの状態")},
		{"add [-A|-u] <pathspec>...", T("ステージする")},
		{"rm [--cached] [-r] / mv", T("削除・移動")},
		{"restore [--staged] [-s <ref>]", T("変更を戻す(checkout -- <file> も可)")},
		{"reset [--soft|--mixed|--hard] [<ref>]", T("HEAD・インデックスを戻す")},
		{"commit [-a] [-m <msg>] [--amend]", T("手元にコミットする(push するまでGistには反映されない)")},
		{"stash [push|pop|apply|list|drop|show]", T("変更を一時退避する")},
		{"clean -n|-f [-d]", T("追跡していないファイルを消す")},
	})
	printUsageSection(T("履歴を見る"), [][2]string{
		{"log [--oneline] [-p] [--stat] [-n N]", T("コミット履歴(Gistのリビジョン+未pushのコミット)")},
		{"show [<ref>] [<ref>:<path>]", T("コミットやファイルの内容")},
		{"diff [--cached] [<ref>[..<ref>]]", T("差分(--stat / --name-only / --name-status)")},
		{"branch [-a|-v] / rev-parse / ls-files", T("情報表示")},
	})
	printUsageSection(T("Gistと同期する"), [][2]string{
		{"fetch / pull", T("Gist側の変更を取り込む(未pushのコミットはその上に積み直す)")},
		{"push [-f] [--public|--private]", T("未pushのコミットを1つずつリビジョンとして保存する")},
		{"remote [-v] / remote add origin <id>", T("origin(Gist)の表示・設定")},
	})
	fmt.Println(dim(T("git config の alias も使えます(例: alias.st=status なら bin g st)。-C <dir> と --no-pager にも対応。")))
}

// ---- init / clone ----

func gitInit(args []string) {
	a := parseG(args, map[string]string{"-q": "q", "--quiet": "q", "-b": "b", "--initial-branch": "b"}, "b")
	if b := a.val("b"); b != "" && b != "main" {
		gitFatal(T("bin ではブランチは main だけです"))
	}
	dir := "."
	if len(a.pos) > 0 {
		dir = a.pos[0]
	}
	abs, _ := filepath.Abs(dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		gitFatal("%v", err)
	}
	msg := T("空のリポジトリを %s に作成しました")
	if _, err := os.Stat(filepath.Join(abs, syncMetaFile)); err == nil {
		msg = T("既存のリポジトリ %s を初期化し直しました")
	} else if err := writeSyncMeta(abs, syncMeta{Files: map[string]string{}}); err != nil {
		gitFatal("%v", err)
	}
	_ = os.MkdirAll(filepath.Join(abs, gitStateDir), 0o755)
	if !a.has("q") {
		fmt.Printf(msg+"\n", filepath.Join(abs, gitStateDir)+string(filepath.Separator))
	}
}

func gitClone(args []string) {
	a := parseG(args, map[string]string{"-q": "q", "--quiet": "q", "--depth": "depth", "-b": "b", "--branch": "b", "-f": "force", "--force": "force"}, "depth", "b")
	if len(a.pos) == 0 {
		gitFatal(T("clone するGistを指定してください"))
	}
	id, err := parseGistID(a.pos[0])
	if err != nil {
		gitFatal("%v", err)
	}
	dir := id
	if len(a.pos) > 1 {
		dir = a.pos[1]
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !a.has("force") {
		gitFatal(T("移動先のパス '%s' は既に存在し、空のディレクトリではありません。"), dir)
	}
	fmt.Fprintf(os.Stderr, T("'%s' にクローンしています...")+"\n", dir)
	pass := []string{id, dir, "--force"}
	cmdClone(pass)
}

// ---- status ----

type statusInfo struct {
	staged, unstaged []treeChange
	untracked        []string
}

func (r *gitRepo) status(ms []pathMatcher, all bool) statusInfo {
	head, index := r.head(), r.index()
	work := r.workTree()
	var s statusInfo
	for _, c := range diffTrees(head, index) {
		if matchAny(ms, c.path) {
			s.staged = append(s.staged, c)
		}
	}
	for _, p := range sortedKeys(index) {
		if !matchAny(ms, p) {
			continue
		}
		if h, ok := work[p]; !ok {
			s.unstaged = append(s.unstaged, treeChange{p, 'D', index[p], ""})
		} else if h != index[p] {
			s.unstaged = append(s.unstaged, treeChange{p, 'M', index[p], h})
		}
	}
	seen := map[string]bool{}
	for _, p := range sortedKeys(work) {
		if _, ok := index[p]; ok || !matchAny(ms, p) || r.ignored(p) {
			continue
		}
		shown := p
		if !all {
			parts := strings.Split(p, "/")
			for i := 1; i < len(parts); i++ {
				d := strings.Join(parts[:i], "/") + "/"
				tracked := false
				for q := range index {
					if strings.HasPrefix(q, d) {
						tracked = true
						break
					}
				}
				if !tracked {
					shown = d
					break
				}
			}
		}
		if !seen[shown] {
			seen[shown] = true
			s.untracked = append(s.untracked, shown)
		}
	}
	return s
}

func (r *gitRepo) aheadBehind() (int, int) {
	behind := 0
	if r.st.Fetched > r.meta.Revision {
		behind = r.st.Fetched - r.meta.Revision
	}
	return len(r.st.Commits), behind
}

func (r *gitRepo) trackingLine() string {
	if r.meta.ID == "" {
		return T("まだ origin がありません(bin g push で新しいGistを作成します)")
	}
	ahead, behind := r.aheadBehind()
	switch {
	case ahead > 0 && behind > 0:
		return fmt.Sprintf(T("ブランチと 'origin/main' が分岐しています(それぞれ %d・%d 個の異なるコミットがあります)。\n  (取り込むには \"bin g pull\")"), ahead, behind)
	case ahead > 0:
		return fmt.Sprintf(Tn("ブランチは 'origin/main' より %d コミット進んでいます。\n  (公開するには \"bin g push\")", ahead), ahead)
	case behind > 0:
		return fmt.Sprintf(Tn("ブランチは 'origin/main' より %d コミット遅れています。\n  (取り込むには \"bin g pull\")", behind), behind)
	}
	return T("ブランチは 'origin/main' と同じ状態です。")
}

func statusLabel(kind byte) string {
	return changeLabel(kind)
}

func gitStatus(args []string) {
	a := parseG(args, map[string]string{"-s": "short", "--short": "short", "--porcelain": "porcelain", "-b": "branch", "--branch": "branch",
		"-u": "all", "--untracked-files": "all", "-uall": "all", "--long": "long"})
	r := openRepo()
	s := r.status(r.matchers(append(a.pos, a.paths...)), a.has("all"))
	if a.has("short") || a.has("porcelain") {
		plain := a.has("porcelain")
		col := func(f func(string) string, x string) string {
			if plain {
				return x
			}
			return f(x)
		}
		if a.has("branch") {
			line := "## main"
			if r.meta.ID != "" {
				line += "...origin/main"
				ahead, behind := r.aheadBehind()
				var parts []string
				if ahead > 0 {
					parts = append(parts, fmt.Sprintf("ahead %d", ahead))
				}
				if behind > 0 {
					parts = append(parts, fmt.Sprintf("behind %d", behind))
				}
				if len(parts) > 0 {
					line += " [" + strings.Join(parts, ", ") + "]"
				}
			}
			fmt.Println(line)
		}
		type row struct{ x, y byte }
		rows := map[string]*row{}
		for _, c := range s.staged {
			rows[c.path] = &row{c.kind, ' '}
		}
		for _, c := range s.unstaged {
			if rows[c.path] == nil {
				rows[c.path] = &row{' ', c.kind}
			} else {
				rows[c.path].y = c.kind
			}
		}
		for _, p := range sortedKeysRows(rows) {
			rw := rows[p]
			fmt.Printf("%s%s %s\n", col(green, string(rw.x)), col(red, string(rw.y)), r.display(p))
		}
		for _, p := range s.untracked {
			fmt.Printf("%s %s\n", col(red, "??"), r.display(p))
		}
		return
	}
	fmt.Println(T("ブランチ main"))
	fmt.Println(r.trackingLine())
	if len(r.st.Commits) == 0 && len(r.head()) == 0 {
		fmt.Println()
		fmt.Println(T("まだコミットがありません"))
	}
	if len(s.staged) > 0 {
		fmt.Println()
		fmt.Println(T("コミット予定の変更:"))
		fmt.Println(T("  (ステージを取り消すには \"bin g restore --staged <file>...\")"))
		for _, c := range s.staged {
			fmt.Println(green("\t" + statusLabel(c.kind) + r.display(c.path)))
		}
	}
	if len(s.unstaged) > 0 {
		fmt.Println()
		fmt.Println(T("ステージされていない変更:"))
		fmt.Println(T("  (コミットに含めるには \"bin g add <file>...\")"))
		fmt.Println(T("  (作業ツリーの変更を取り消すには \"bin g restore <file>...\")"))
		for _, c := range s.unstaged {
			fmt.Println(red("\t" + statusLabel(c.kind) + r.display(c.path)))
		}
	}
	if len(s.untracked) > 0 {
		fmt.Println()
		fmt.Println(T("追跡されていないファイル:"))
		fmt.Println(T("  (コミットに含めるには \"bin g add <file>...\")"))
		for _, p := range s.untracked {
			fmt.Println(red("\t" + r.display(p)))
		}
	}
	for _, sk := range r.skips {
		if !r.ignored(sk.path) {
			warnErr(T("%s は対象外です(%s)"), sk.path, sk.reason)
		}
	}
	fmt.Println()
	switch {
	case len(s.staged) > 0:
	case len(s.unstaged) > 0:
		fmt.Println(T("コミット予定の変更はありません(\"bin g add\" か \"bin g commit -a\" を使ってください)"))
	case len(s.untracked) > 0:
		fmt.Println(T("コミット予定の変更はありませんが、追跡されていないファイルがあります(追跡するには \"bin g add\")"))
	default:
		fmt.Println(T("コミットするものはありません。作業ツリーはきれいです"))
	}
}

func sortedKeysRows[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- add / rm / mv ----

func gitAdd(args []string) {
	a := parseG(args, map[string]string{"-A": "all", "--all": "all", "-u": "update", "--update": "update", "-n": "dry", "--dry-run": "dry",
		"-f": "force", "--force": "force", "-v": "verbose", "--verbose": "verbose", "--no-all": "noall", "--ignore-removal": "noall"})
	specs := append(a.pos, a.paths...)
	if len(specs) == 0 && !a.has("all") && !a.has("update") {
		fmt.Println(T("何も指定されていないので、何も追加しませんでした。"))
		gitHint(T("'bin g add .' のつもりでしたか?"))
		return
	}
	r := openRepo()
	ms := r.matchers(specs)
	index := r.index()
	work := r.worktree()
	matched := map[int]bool{}
	var ignoredPaths []string
	var out []string
	mark := func(p string) {
		for i, m := range ms {
			if m.match(p) {
				matched[i] = true
			}
		}
	}
	for _, p := range sortedKeys(work) {
		if !matchAny(ms, p) {
			continue
		}
		_, tracked := index[p]
		if a.has("update") && !tracked {
			continue
		}
		if !tracked && r.ignored(p) && !a.has("force") {
			for _, m := range ms {
				if m.rel == p {
					ignoredPaths = append(ignoredPaths, p)
				}
			}
			continue
		}
		mark(p)
		h := contentHash(work[p])
		if index[p] == h {
			continue
		}
		out = append(out, "add '"+r.display(p)+"'")
		if !a.has("dry") {
			index[p] = r.put(work[p])
		}
	}
	if !a.has("noall") {
		for _, p := range sortedKeys(index) {
			if _, ok := work[p]; ok || !matchAny(ms, p) {
				continue
			}
			mark(p)
			out = append(out, "remove '"+r.display(p)+"'")
			if !a.has("dry") {
				delete(index, p)
			}
		}
	}
	for _, sk := range r.skips {
		if matchAny(ms, sk.path) && !r.ignored(sk.path) {
			mark(sk.path)
			warnErr(T("%s は対象外です(%s)"), sk.path, sk.reason)
		}
	}
	if len(ignoredPaths) > 0 {
		fmt.Fprintln(os.Stderr, T("次のパスは .gitignore で無視されています:"))
		for _, p := range ignoredPaths {
			fmt.Fprintln(os.Stderr, r.display(p))
		}
		gitHint(T("本当に追加するなら -f を付けてください。"))
		os.Exit(1)
	}
	for i, m := range ms {
		if !matched[i] && !m.all && !a.has("update") {
			gitFatal(T("パス指定 '%s' に一致するファイルがありません"), m.spec)
		}
	}
	if a.has("verbose") || a.has("dry") {
		for _, l := range out {
			fmt.Println(l)
		}
	}
	if !a.has("dry") {
		r.save()
	}
}

func gitRm(args []string) {
	a := parseG(args, map[string]string{"--cached": "cached", "-r": "r", "-f": "force", "--force": "force", "-q": "q", "--quiet": "q", "-n": "dry", "--dry-run": "dry"})
	specs := append(a.pos, a.paths...)
	if len(specs) == 0 {
		gitFatal(T("パスを指定してください。何も削除しませんでした。"))
	}
	r := openRepo()
	index := r.index()
	work := r.workTree()
	head := r.head()
	var targets []string
	for _, m := range r.matchers(specs) {
		n := 0
		for _, p := range sortedKeys(index) {
			if !m.match(p) {
				continue
			}
			if p != m.rel && !m.glob && !a.has("r") {
				gitFatal(T("-r が無いので '%s' を再帰的には削除しません"), m.spec)
			}
			targets = append(targets, p)
			n++
		}
		if n == 0 {
			gitFatal(T("パス指定 '%s' に一致するファイルがありません"), m.spec)
		}
	}
	if !a.has("force") && !a.has("cached") {
		var bad []string
		for _, p := range targets {
			if w, ok := work[p]; ok && (w != index[p] || index[p] != head[p]) {
				bad = append(bad, p)
			}
		}
		if len(bad) > 0 {
			fmt.Fprintln(os.Stderr, "error: "+T("次のファイルはステージ済みの内容かローカルの変更があります:"))
			for _, p := range bad {
				fmt.Fprintln(os.Stderr, "    "+r.display(p))
			}
			fmt.Fprintln(os.Stderr, T("(--cached で手元に残す、-f で強制的に削除)"))
			os.Exit(1)
		}
	}
	for _, p := range targets {
		if !a.has("q") {
			fmt.Printf("rm '%s'\n", r.display(p))
		}
		if a.has("dry") {
			continue
		}
		delete(index, p)
		if !a.has("cached") {
			if full, err := safeJoin(r.dir, p); err == nil {
				_ = os.Remove(full)
				r.removeEmptyParents(full)
			}
		}
	}
	if !a.has("dry") {
		r.save()
	}
}

func gitMv(args []string) {
	a := parseG(args, map[string]string{"-f": "force", "--force": "force", "-n": "dry", "--dry-run": "dry", "-v": "v", "--verbose": "v", "-k": "k"})
	if len(a.pos) < 2 {
		gitUsageError("usage: bin g mv [<options>] <source>... <destination>")
	}
	r := openRepo()
	index := r.index()
	ms := r.matchers(a.pos)
	dst := ms[len(ms)-1]
	dstFull := filepath.Join(r.dir, filepath.FromSlash(dst.rel))
	dstIsDir := false
	if fi, err := os.Stat(dstFull); err == nil && fi.IsDir() {
		dstIsDir = true
	} else if len(ms) > 2 {
		gitFatal(T("移動先 '%s' はディレクトリではありません"), dst.spec)
	}
	for _, src := range ms[:len(ms)-1] {
		target := dst.rel
		if dstIsDir {
			target = strings.TrimPrefix(dst.rel+"/"+filepath.Base(src.rel), "./")
		}
		var moved []string
		for _, p := range sortedKeys(index) {
			if src.match(p) {
				moved = append(moved, p)
			}
		}
		if len(moved) == 0 {
			gitFatal(T("管理下にないファイルです, source=%s, destination=%s"), src.spec, dst.spec)
		}
		if !a.has("force") {
			if _, err := os.Stat(filepath.Join(r.dir, filepath.FromSlash(target))); err == nil && !dstIsDir {
				gitFatal(T("移動先が既に存在します, source=%s, destination=%s"), src.spec, dst.spec)
			}
		}
		if a.has("v") || a.has("dry") {
			fmt.Printf(T("%s を %s に名前変更")+"\n", r.display(src.rel), r.display(target))
		}
		if a.has("dry") {
			continue
		}
		from := filepath.Join(r.dir, filepath.FromSlash(src.rel))
		to := filepath.Join(r.dir, filepath.FromSlash(target))
		_ = os.MkdirAll(filepath.Dir(to), 0o755)
		if err := os.Rename(from, to); err != nil {
			gitFatal(T("名前を変更できません: %v"), err)
		}
		for _, p := range moved {
			np := target + strings.TrimPrefix(p, src.rel)
			index[np] = index[p]
			delete(index, p)
		}
	}
	if !a.has("dry") {
		r.save()
	}
}

// ---- restore / checkout / switch / reset ----

func (r *gitRepo) restorePaths(ms []pathMatcher, source map[string]string, staged, worktree bool) {
	index := r.index()
	work := r.workTree()
	for _, m := range ms {
		found := false
		for _, t := range []map[string]string{source, index} {
			for p := range t {
				if m.match(p) {
					found = true
				}
			}
		}
		if !found {
			gitError(T("パス指定 '%s' に一致する管理下のファイルがありません"), m.spec)
		}
	}
	paths := map[string]bool{}
	for _, t := range []map[string]string{source, index} {
		for p := range t {
			if matchAny(ms, p) {
				paths[p] = true
			}
		}
	}
	for p := range paths {
		h, ok := source[p]
		if staged {
			if ok {
				index[p] = h
			} else {
				delete(index, p)
			}
		}
		if worktree {
			if !ok {
				if full, err := safeJoin(r.dir, p); err == nil {
					_ = os.Remove(full)
					r.removeEmptyParents(full)
				}
			} else if work[p] != h {
				r.writeFile(p, r.blob(h))
			}
		}
	}
	r.work = nil
	r.save()
}

func gitRestore(args []string) {
	a := parseG(args, map[string]string{"-S": "staged", "--staged": "staged", "-W": "worktree", "--worktree": "worktree",
		"-s": "source", "--source": "source", "-q": "q", "--quiet": "q"}, "source")
	specs := append(a.pos, a.paths...)
	if len(specs) == 0 {
		gitFatal(T("戻すパスを指定してください"))
	}
	r := openRepo()
	staged, worktree := a.has("staged"), a.has("worktree") || !a.has("staged")
	var source map[string]string
	switch {
	case a.has("source"):
		source = r.resolve(a.val("source"))[0].tree
	case staged:
		source = r.head()
	default:
		source = copyTree(r.index())
	}
	r.restorePaths(r.matchers(specs), source, staged, worktree)
}

func gitCheckout(args []string) {
	a := parseG(args, map[string]string{"-b": "b", "-B": "b", "-f": "force", "--force": "force", "-q": "q", "--quiet": "q", "--orphan": "b"}, "b")
	if a.has("b") {
		gitFatal(T("bin ではブランチは main だけです"))
	}
	r := openRepo()
	pos := a.pos
	if len(pos) == 1 && len(a.paths) == 0 && (pos[0] == "main" || pos[0] == "-" || pos[0] == "HEAD") {
		fmt.Fprintln(os.Stderr, T("既に 'main' にいます"))
		fmt.Println(r.trackingLine())
		return
	}
	if len(pos) > 0 && len(a.paths) > 0 {
		// checkout <ref> -- <paths>
		src := r.resolve(pos[0])[0].tree
		r.restorePaths(r.matchers(a.paths), src, true, true)
		return
	}
	specs := append(pos, a.paths...)
	if len(specs) == 0 {
		fmt.Println(r.trackingLine())
		return
	}
	if len(pos) == 1 && len(a.paths) == 0 && looksLikeRef(pos[0]) {
		if _, err := os.Stat(pos[0]); err != nil {
			gitFatal(T("bin では main 以外に切り替えられません('%s' の内容は bin g restore -s %s <file> で取り出せます)"), pos[0], pos[0])
		}
	}
	r.restorePaths(r.matchers(specs), copyTree(r.index()), false, true)
}

func gitSwitch(args []string) {
	a := parseG(args, map[string]string{"-c": "c", "-C": "c", "--create": "c", "-q": "q", "--detach": "c", "-d": "c"}, "c")
	if a.has("c") || len(a.pos) != 1 || (a.pos[0] != "main" && a.pos[0] != "-") {
		gitFatal(T("bin ではブランチは main だけです"))
	}
	fmt.Fprintln(os.Stderr, T("既に 'main' にいます"))
}

func gitReset(args []string) {
	a := parseG(args, map[string]string{"--soft": "soft", "--mixed": "mixed", "--hard": "hard", "-q": "q", "--quiet": "q", "--keep": "hard", "--merge": "hard"})
	r := openRepo()
	ref := "HEAD"
	specs := a.paths
	for i, p := range a.pos {
		if i == 0 && looksLikeRef(p) && len(a.paths) == 0 {
			if _, err := os.Stat(p); err != nil || len(a.pos) == 1 {
				ref = p
				continue
			}
		}
		if i == 0 && looksLikeRef(p) && len(a.paths) > 0 {
			ref = p
			continue
		}
		specs = append(specs, p)
	}
	if len(specs) > 0 {
		if a.has("soft") || a.has("hard") {
			gitFatal(T("パスを指定した reset では --soft / --hard は使えません"))
		}
		src := r.resolve(ref)[0].tree
		r.restorePaths(r.matchers(specs), src, true, false)
		r.printUnstaged(a.has("q"))
		return
	}
	target := r.resolve(ref)[0]
	oldHead, oldIndex := copyTree(r.head()), copyTree(r.index())
	if target.local {
		for i, c := range r.st.Commits {
			if c.Hash == target.hash {
				r.st.Commits = r.st.Commits[:i+1]
				break
			}
		}
	} else {
		// Gistのリビジョンまで戻す(push 済みの位置より前なら、次の push は -f が必要になる)
		r.st.Commits = nil
		if target.rev != r.meta.Revision {
			if target.rev > r.st.Fetched {
				r.st.Fetched = max(r.st.Fetched, r.meta.Revision)
			}
			r.meta.Revision = target.rev
			r.meta.Files = copyTree(target.tree)
			r.saveMeta()
		}
	}
	switch {
	case a.has("soft"):
		r.st.Index = oldIndex
	case a.has("hard"):
		r.st.Index = nil
		tracked := copyTree(oldIndex)
		for p, h := range oldHead {
			tracked[p] = h
		}
		r.checkoutTree(r.head(), tracked)
		if !a.has("q") {
			fmt.Printf(T("HEAD の位置を %s %s に変更しました")+"\n", target.hash, firstLine(target.message))
		}
	default:
		r.st.Index = nil
	}
	r.save()
	if !a.has("soft") && !a.has("hard") {
		r.printUnstaged(a.has("q"))
	}
}

func (r *gitRepo) printUnstaged(quiet bool) {
	if quiet {
		return
	}
	s := r.status(nil, false)
	if len(s.unstaged) == 0 {
		return
	}
	fmt.Println(T("reset 後のステージされていない変更:"))
	for _, c := range s.unstaged {
		fmt.Printf("%c\t%s\n", c.kind, r.display(c.path))
	}
}

// ---- commit ----

func commitHash(parent, message, t string, tree map[string]string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", parent, message, t)
	for _, p := range sortedKeys(tree) {
		fmt.Fprintf(h, "%s %s\n", tree[p], p)
	}
	return hex.EncodeToString(h.Sum(nil))[:7]
}

func (r *gitRepo) lineStat(c treeChange) (int, int) {
	var from, to string
	if c.from != "" {
		from = r.blob(c.from)
	}
	if c.to != "" {
		to = r.blob(c.to)
	}
	add, del := 0, 0
	for _, op := range diffLines(splitLines(from), splitLines(to)) {
		switch op.kind {
		case '+':
			add++
		case '-':
			del++
		}
	}
	return add, del
}

func (r *gitRepo) summaryLine(changes []treeChange) string {
	add, del := 0, 0
	for _, c := range changes {
		x, y := r.lineStat(c)
		add += x
		del += y
	}
	return statSummary(len(changes), add, del)
}

func statSummary(files, add, del int) string {
	s := fmt.Sprintf(" %d file%s changed", files, plural(files))
	if add > 0 || del == 0 {
		s += fmt.Sprintf(", %d insertion%s(+)", add, plural(add))
	}
	if del > 0 || add == 0 {
		s += fmt.Sprintf(", %d deletion%s(-)", del, plural(del))
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func gitCommitCmd(args []string) {
	a := parseG(args, map[string]string{"-m": "m", "--message": "m", "-a": "all", "--all": "all", "--amend": "amend", "--allow-empty": "empty",
		"-q": "q", "--quiet": "q", "-F": "file", "--file": "file", "--no-edit": "noedit", "-v": "v", "--verbose": "v",
		"--allow-empty-message": "emptymsg", "-n": "noverify", "--no-verify": "noverify", "-s": "signoff", "--signoff": "signoff"}, "m", "file")
	r := openRepo()
	index := r.index()
	if a.has("all") || len(a.pos)+len(a.paths) > 0 {
		work := r.worktree()
		ms := r.matchers(append(a.pos, a.paths...))
		for _, p := range sortedKeys(index) {
			if !matchAny(ms, p) {
				continue
			}
			if c, ok := work[p]; ok {
				index[p] = r.put(c)
			} else {
				delete(index, p)
			}
		}
		if len(ms) > 0 {
			for p, c := range work {
				if _, ok := index[p]; !ok && matchAny(ms, p) && !r.ignored(p) {
					for _, m := range ms {
						if m.rel == p {
							index[p] = r.put(c)
						}
					}
				}
			}
		}
	}
	amend := a.has("amend")
	if amend && len(r.st.Commits) == 0 {
		gitFatal(T("最後のコミットは push 済みなので --amend できません(Gistのリビジョンは書き換えられません)"))
	}
	commits := r.st.Commits
	if amend {
		commits = commits[:len(commits)-1]
	}
	parentTree := r.meta.Files
	parentHash := ""
	if n := len(commits); n > 0 {
		parentTree, parentHash = commits[n-1].Tree, commits[n-1].Hash
	} else if r.meta.ID != "" && r.meta.Revision > 0 {
		parentHash = revisionHash(r.meta.ID, r.meta.Revision)
	}
	changes := diffTrees(parentTree, index)
	if len(changes) == 0 && !a.has("empty") && !amend {
		s := r.status(nil, false)
		fmt.Println(T("ブランチ main"))
		fmt.Println(r.trackingLine())
		fmt.Println()
		switch {
		case len(s.unstaged) > 0:
			fmt.Println(T("コミット予定の変更はありません(\"bin g add\" か \"bin g commit -a\" を使ってください)"))
		case len(s.untracked) > 0:
			fmt.Println(T("コミット予定の変更はありませんが、追跡されていないファイルがあります(追跡するには \"bin g add\")"))
		default:
			fmt.Println(T("コミットするものはありません。作業ツリーはきれいです"))
		}
		os.Exit(1)
	}
	if len(index) == 0 {
		gitFatal(T("Gistには1ファイル以上必要なので、空のツリーはコミットできません"))
	}
	var msg string
	switch {
	case len(a.vals["m"]) > 0:
		msg = strings.Join(a.vals["m"], "\n\n")
	case a.has("file"):
		var data []byte
		var err error
		if a.val("file") == "-" {
			data, err = readAllStdin()
		} else {
			data, err = os.ReadFile(a.val("file"))
		}
		if err != nil {
			gitFatal(T("'%s' を読み込めません: %v"), a.val("file"), err)
		}
		msg = strings.TrimSpace(string(data))
	case amend && a.has("noedit"):
		msg = r.st.Commits[len(r.st.Commits)-1].Message
	default:
		initial := ""
		if amend {
			initial = r.st.Commits[len(r.st.Commits)-1].Message
		}
		msg = r.editMessage(initial, changes)
	}
	msg = strings.TrimSpace(msg)
	if msg == "" && !a.has("emptymsg") {
		fmt.Fprintln(os.Stderr, T("コミットメッセージが空なので、コミットを中止しました。"))
		os.Exit(1)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c := gitCommit{Hash: commitHash(parentHash, msg, now, index), Message: msg, Time: now, Tree: copyTree(index)}
	r.st.Commits = append(commits, c)
	r.st.Index = nil
	r.save()
	if a.has("q") {
		return
	}
	root := ""
	if parentHash == "" {
		root = " (root-commit)"
	}
	fmt.Printf("[main%s %s] %s\n", root, c.Hash, firstLine(msg))
	fmt.Println(r.summaryLine(changes))
	for _, ch := range changes {
		switch ch.kind {
		case 'A':
			fmt.Printf(" create mode 100644 %s\n", ch.path)
		case 'D':
			fmt.Printf(" delete mode 100644 %s\n", ch.path)
		}
	}
}

func readAllStdin() ([]byte, error) {
	var b []byte
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return b, nil
			}
			return b, err
		}
	}
}

// ---- diff ----

type diffOpts struct {
	stat, nameOnly, nameStatus, numstat, quiet bool
	context                                    int
}

func (r *gitRepo) printDiff(changes []treeChange, o diffOpts) {
	switch {
	case o.nameOnly:
		for _, c := range changes {
			fmt.Println(r.display(c.path))
		}
		return
	case o.nameStatus:
		for _, c := range changes {
			fmt.Printf("%c\t%s\n", c.kind, r.display(c.path))
		}
		return
	case o.numstat:
		for _, c := range changes {
			add, del := r.lineStat(c)
			fmt.Printf("%d\t%d\t%s\n", add, del, r.display(c.path))
		}
		return
	case o.stat:
		r.printStat(changes)
		return
	}
	for _, c := range changes {
		r.printFilePatch(c, o.context)
	}
}

func (r *gitRepo) printStat(changes []treeChange) {
	if len(changes) == 0 {
		return
	}
	type row struct {
		name     string
		add, del int
	}
	var rows []row
	width, most := 0, 0
	tAdd, tDel := 0, 0
	for _, c := range changes {
		add, del := r.lineStat(c)
		name := r.display(c.path)
		rows = append(rows, row{name, add, del})
		width = max(width, displayWidth(name))
		most = max(most, add+del)
		tAdd += add
		tDel += del
	}
	numW := len(strconv.Itoa(most))
	barMax := max(10, 60-width-numW)
	for _, rw := range rows {
		add, del := rw.add, rw.del
		if most > barMax {
			add = (add*barMax + most - 1) / most
			del = (del*barMax + most - 1) / most
		}
		fmt.Printf(" %s%s | %*d %s%s\n", rw.name, strings.Repeat(" ", width-displayWidth(rw.name)), numW, rw.add+rw.del,
			green(strings.Repeat("+", add)), red(strings.Repeat("-", del)))
	}
	fmt.Println(statSummary(len(rows), tAdd, tDel))
}

func (r *gitRepo) printFilePatch(c treeChange, ctx int) {
	short := func(h string) string {
		if h == "" {
			return "0000000"
		}
		return h[:7]
	}
	fmt.Println(bold(fmt.Sprintf("diff --git a/%s b/%s", c.path, c.path)))
	var from, to string
	switch c.kind {
	case 'A':
		fmt.Println(bold("new file mode 100644"))
		fmt.Println(bold(fmt.Sprintf("index 0000000..%s", short(c.to))))
	case 'D':
		fmt.Println(bold("deleted file mode 100644"))
		fmt.Println(bold(fmt.Sprintf("index %s..0000000", short(c.from))))
	default:
		fmt.Println(bold(fmt.Sprintf("index %s..%s 100644", short(c.from), short(c.to))))
	}
	if c.from != "" {
		from = r.blob(c.from)
	}
	if c.to != "" {
		to = r.blob(c.to)
	}
	a, b := "a/"+c.path, "b/"+c.path
	if c.kind == 'A' {
		a = "/dev/null"
	}
	if c.kind == 'D' {
		b = "/dev/null"
	}
	fmt.Println(bold("--- " + a))
	fmt.Println(bold("+++ " + b))
	printHunks(diffLines(splitLines(from), splitLines(to)), ctx)
}

// printHunks は git diff と同じ @@ -a,b +c,d @@ 形式で、変更箇所の前後 ctx 行を表示する
func printHunks(ops []lineOp, ctx int) {
	keep := make([]bool, len(ops))
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		for j := max(0, i-ctx); j <= i+ctx && j < len(ops); j++ {
			keep[j] = true
		}
	}
	oldLine, newLine := make([]int, len(ops)+1), make([]int, len(ops)+1)
	o, n := 1, 1
	for i, op := range ops {
		oldLine[i], newLine[i] = o, n
		if op.kind != '+' {
			o++
		}
		if op.kind != '-' {
			n++
		}
	}
	for i := 0; i < len(ops); {
		if !keep[i] {
			i++
			continue
		}
		j := i
		for j < len(ops) && keep[j] {
			j++
		}
		oc, nc := 0, 0
		for _, op := range ops[i:j] {
			if op.kind != '+' {
				oc++
			}
			if op.kind != '-' {
				nc++
			}
		}
		os, ns := oldLine[i], newLine[i]
		if oc == 0 {
			os--
		}
		if nc == 0 {
			ns--
		}
		fmt.Println(cyan(fmt.Sprintf("@@ -%s +%s @@", hunkRange(os, oc), hunkRange(ns, nc))))
		for _, op := range ops[i:j] {
			switch op.kind {
			case '+':
				fmt.Println(green("+" + op.text))
			case '-':
				fmt.Println(red("-" + op.text))
			default:
				fmt.Println(" " + op.text)
			}
		}
		i = j
	}
}

func hunkRange(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

var diffOptMap = map[string]string{"--stat": "stat", "--name-only": "nameonly", "--name-status": "namestatus", "--numstat": "numstat",
	"-U": "U", "--unified": "U", "--color": "color", "--no-color": "color", "-p": "patch", "-u": "patch", "--patch": "patch", "--no-patch": "nopatch", "-s": "nopatch"}

func diffOptsFrom(a gArgs) diffOpts {
	o := diffOpts{stat: a.has("stat"), nameOnly: a.has("nameonly"), nameStatus: a.has("namestatus"), numstat: a.has("numstat"), context: 3}
	if a.has("U") {
		if n, err := strconv.Atoi(a.val("U")); err == nil {
			o.context = n
		}
	}
	return o
}

func gitDiff(args []string) {
	opts := map[string]string{"--cached": "cached", "--staged": "cached", "-q": "quiet", "--quiet": "quiet", "--exit-code": "exit"}
	for k, v := range diffOptMap {
		opts[k] = v
	}
	a := parseG(args, opts, "U")
	r := openRepo()
	var refs, specs []string
	for i, p := range a.pos {
		if len(specs) == 0 && (strings.Contains(p, "..") || looksLikeRef(p)) && (a.dash || i < 2) {
			if _, err := os.Stat(p); err != nil || strings.Contains(p, "..") {
				refs = append(refs, p)
				continue
			}
		}
		specs = append(specs, p)
	}
	specs = append(specs, a.paths...)
	ms := r.matchers(specs)
	var from, to map[string]string
	trackedWork := func(base map[string]string) map[string]string {
		work := r.workTree()
		out := map[string]string{}
		for _, t := range []map[string]string{base, r.index()} {
			for p := range t {
				if h, ok := work[p]; ok {
					out[p] = h
				}
			}
		}
		return out
	}
	switch {
	case len(refs) == 1 && strings.Contains(refs[0], ".."):
		x, y, _ := strings.Cut(strings.Replace(refs[0], "...", "..", 1), "..")
		if x == "" {
			x = "HEAD"
		}
		if y == "" {
			y = "HEAD"
		}
		from, to = r.resolve(x)[0].tree, r.resolve(y)[0].tree
	case len(refs) == 2:
		from, to = r.resolve(refs[0])[0].tree, r.resolve(refs[1])[0].tree
	case a.has("cached"):
		from = r.head()
		if len(refs) == 1 {
			from = r.resolve(refs[0])[0].tree
		}
		to = r.index()
	case len(refs) == 1:
		from = r.resolve(refs[0])[0].tree
		to = trackedWork(from)
	default:
		from = copyTree(r.index())
		to = trackedWork(nil)
	}
	var changes []treeChange
	for _, c := range diffTrees(from, to) {
		if matchAny(ms, c.path) {
			changes = append(changes, c)
		}
	}
	if a.has("quiet") {
		if len(changes) > 0 {
			os.Exit(1)
		}
		return
	}
	stop := startPager()
	r.printDiff(changes, diffOptsFrom(a))
	stop()
	if a.has("exit") && len(changes) > 0 {
		os.Exit(1)
	}
}

// ---- log / show ----

func parentTreeOf(chain []gitEntry, i int, r *gitRepo) map[string]string {
	if i+1 < len(chain) {
		return chain[i+1].tree
	}
	if chain[i].local {
		return r.meta.Files
	}
	return nil
}

func (r *gitRepo) decorations(e gitEntry, headHash string) string {
	var d []string
	if e.hash == headHash {
		d = append(d, cyan(bold("HEAD -> "))+green(bold("main")))
	}
	if !e.local && r.meta.ID != "" && e.rev == r.originRevision() {
		d = append(d, red(bold("origin/main")))
	}
	if len(d) == 0 {
		return ""
	}
	return strings.Join(d, yellow(", "))
}

const gitDateFormat = "Mon Jan 2 15:04:05 2006 -0700"

var formatVar = regexp.MustCompile(`%(C\([^)]*\)|C(?:red|green|blue|reset)|a[nearD]|c[nearD]|[hHsbBdDnptT%]|[aA][dD])`)

func (r *gitRepo) formatEntry(format string, e gitEntry, decor string) string {
	subject, body, _ := strings.Cut(e.message, "\n")
	body = strings.TrimSpace(body)
	return formatVar.ReplaceAllStringFunc(format, func(v string) string {
		switch v[1:] {
		case "h", "H", "t", "T":
			return e.hash
		case "p", "P":
			return ""
		case "s":
			return subject
		case "b":
			if body == "" {
				return ""
			}
			return body + "\n"
		case "B":
			return e.message + "\n"
		case "an", "aN", "cn", "cN", "ae", "aE", "ce", "cE":
			return r.authorName()
		case "ad", "cd", "aD", "cD":
			return e.time.Format(gitDateFormat)
		case "ar", "cr":
			return relativeTime(e.time)
		case "d":
			if decor == "" {
				return ""
			}
			return " (" + decor + ")"
		case "D":
			return decor
		case "n":
			return "\n"
		case "%":
			return "%"
		case "Cred":
			return colorCode("31")
		case "Cgreen":
			return colorCode("32")
		case "Cblue":
			return colorCode("34")
		case "Creset":
			return colorCode("0")
		}
		if strings.HasPrefix(v, "%C(") {
			return ""
		}
		return v
	})
}

func colorCode(c string) string {
	if !colorEnabled {
		return ""
	}
	return "\x1b[" + c + "m"
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 730*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%d years ago", int(d.Hours()/24/365))
}

var logOptMap = map[string]string{"--oneline": "oneline", "-n": "n", "--max-count": "n", "--reverse": "reverse", "--format": "format",
	"--pretty": "format", "--graph": "graph", "--decorate": "decorate", "--no-decorate": "nodecorate", "--all": "all", "--abbrev-commit": "x",
	"--no-merges": "x", "--first-parent": "x", "--date": "date", "--skip": "skip"}

func gitLog(args []string) { runLog(args, false) }

func gitShow(args []string) {
	for _, s := range args {
		if !strings.HasPrefix(s, "-") && strings.Contains(s, ":") {
			ref, p, _ := strings.Cut(s, ":")
			r := openRepo()
			if ref == "" {
				ref = "HEAD"
			}
			tree := r.resolve(ref)[0].tree
			p = strings.TrimPrefix(p, "./")
			h, ok := tree[p]
			if !ok {
				gitFatal(T("パス '%s' は '%s' に存在しません"), p, ref)
			}
			fmt.Print(r.blob(h))
			return
		}
	}
	runLog(args, true)
}

func runLog(args []string, show bool) {
	var rest []string
	maxCount := -1
	for _, s := range args {
		if len(s) > 1 && s[0] == '-' && s[1] >= '0' && s[1] <= '9' {
			if n, err := strconv.Atoi(s[1:]); err == nil {
				maxCount = n
				continue
			}
		}
		rest = append(rest, s)
	}
	opts := map[string]string{}
	for k, v := range diffOptMap {
		opts[k] = v
	}
	for k, v := range logOptMap {
		opts[k] = v
	}
	a := parseG(rest, opts, "n", "format", "U", "date", "skip")
	if a.has("n") {
		maxCount, _ = strconv.Atoi(a.val("n"))
	}
	r := openRepo()
	var refs, specs []string
	for _, p := range a.pos {
		if len(specs) == 0 && (strings.Contains(p, "..") || looksLikeRef(p)) {
			if _, err := os.Stat(p); err != nil {
				refs = append(refs, p)
				continue
			}
		}
		specs = append(specs, p)
	}
	specs = append(specs, a.paths...)
	ms := r.matchers(specs)
	if show && maxCount < 0 {
		maxCount = 1
	}

	var chain []gitEntry
	exclude := map[string]bool{}
	switch {
	case len(refs) == 1 && strings.Contains(refs[0], ".."):
		x, y, _ := strings.Cut(refs[0], "..")
		if x == "" {
			x = "HEAD"
		}
		if y == "" {
			y = "HEAD"
		}
		for _, e := range r.resolve(x) {
			exclude[e.hash] = true
		}
		chain = r.resolve(y)
	case len(refs) >= 1:
		chain = r.resolve(refs[0])
	case a.has("all"):
		chain = r.headChain()
		if r.st.Fetched > r.meta.Revision {
			chain = append(r.remoteEntries(r.st.Fetched)[:r.st.Fetched-r.meta.Revision], chain...)
			sort.SliceStable(chain, func(i, j int) bool { return chain[i].time.After(chain[j].time) })
		}
	default:
		chain = r.headChain()
		if len(chain) == 0 {
			gitFatal(T("ブランチ 'main' にはまだコミットがありません"))
		}
	}
	headHash := r.headHash()
	if a.has("nodecorate") {
		headHash = "\x00"
	}
	format := a.val("format")
	switch format {
	case "oneline":
		format, a.vals["oneline"] = "", []string{""}
	case "short":
		format = "%C(yellow)commit %h%d%nAuthor: %an%n%n    %s%n"
	case "", "medium", "full", "fuller":
		format = ""
	}
	format = strings.TrimPrefix(strings.TrimPrefix(format, "format:"), "tformat:")
	patch := a.has("patch") || (show && !a.has("stat") && !a.has("nameonly") && !a.has("namestatus") && !a.has("numstat") && !a.has("nopatch"))
	o := diffOptsFrom(a)

	type item struct {
		e       gitEntry
		changes []treeChange
	}
	var items []item
	skip, _ := strconv.Atoi(a.val("skip"))
	for i, e := range chain {
		if exclude[e.hash] {
			continue
		}
		var changes []treeChange
		for _, c := range diffTrees(parentTreeOf(chain, i, r), e.tree) {
			if matchAny(ms, c.path) {
				changes = append(changes, c)
			}
		}
		if len(ms) > 0 && len(changes) == 0 {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		if maxCount >= 0 && len(items) >= maxCount {
			break
		}
		items = append(items, item{e, changes})
	}
	if a.has("reverse") {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	stop := startPager()
	defer stop()
	graph := ""
	if a.has("graph") {
		graph = "* "
	}
	for n, it := range items {
		e := it.e
		decor := r.decorations(e, headHash)
		switch {
		case a.has("oneline"):
			d := ""
			if decor != "" {
				d = " " + yellow("(") + decor + yellow(")")
			}
			fmt.Printf("%s%s%s %s\n", graph, yellow(e.hash), d, firstLine(e.message))
		case format != "":
			fmt.Println(graph + r.formatEntry(format, e, decor))
		default:
			if n > 0 {
				fmt.Println()
			}
			d := ""
			if decor != "" {
				d = " " + yellow("(") + decor + yellow(")")
			}
			fmt.Println(graph + yellow("commit "+e.hash) + d)
			fmt.Printf("Author: %s\n", r.authorName())
			fmt.Printf("Date:   %s\n\n", e.time.Format(gitDateFormat))
			for _, l := range strings.Split(e.message, "\n") {
				fmt.Println("    " + l)
			}
		}
		if patch || o.stat || o.nameOnly || o.nameStatus || o.numstat {
			if !a.has("oneline") || patch || o.stat {
				fmt.Println()
			}
			if patch && o.stat {
				r.printStat(it.changes)
				fmt.Println()
			}
			if patch {
				for _, c := range it.changes {
					r.printFilePatch(c, o.context)
				}
			} else {
				r.printDiff(it.changes, o)
			}
		}
	}
}

// ---- fetch / pull / push ----

func (r *gitRepo) needOrigin() {
	if r.meta.ID == "" {
		gitFatal(T("'origin' はGitリポジトリではありません(まだGistと紐付いていません。bin g push で作成するか、bin g remote add origin <id>)"))
	}
}

func (r *gitRepo) fetchGist() *Gist {
	r.needOrigin()
	cfg, session := optionalSession()
	g, err := getGist(cfg, session, r.meta.ID)
	if err != nil {
		gitFatal("%v", err)
	}
	for _, f := range g.Files {
		r.put(f.Content)
	}
	return g
}

func (r *gitRepo) printFetched(oldRev, newRev int) {
	if newRev <= oldRev {
		return
	}
	fmt.Fprintf(os.Stderr, "From %s\n", strings.TrimSuffix(r.remoteURL(), ".git"))
	old := "0000000"
	if oldRev > 0 {
		old = revisionHash(r.meta.ID, oldRev)
	}
	fmt.Fprintf(os.Stderr, "   %s..%s  main       -> origin/main\n", old, revisionHash(r.meta.ID, newRev))
}

func gitFetch(args []string) {
	a := parseG(args, map[string]string{"-q": "q", "--quiet": "q", "--all": "x", "-p": "x", "--prune": "x", "--tags": "x", "-v": "x"})
	r := openRepo()
	g := r.fetchGist()
	if !a.has("q") {
		r.printFetched(r.originRevision(), g.RevisionCount)
	}
	r.st.Fetched = g.RevisionCount
	r.save()
}

func gitPull(args []string) {
	a := parseG(args, map[string]string{"-q": "q", "--quiet": "q", "--rebase": "x", "-r": "x", "--no-rebase": "x", "--ff-only": "x", "--ff": "x",
		"--no-ff": "x", "-v": "x", "--autostash": "autostash"})
	r := openRepo()
	g := r.fetchGist()
	oldOrigin := r.originRevision()
	if !a.has("q") {
		r.printFetched(oldOrigin, g.RevisionCount)
	}
	r.st.Fetched = g.RevisionCount
	if g.RevisionCount == r.meta.Revision {
		r.save()
		fmt.Println(T("既に最新です。"))
		return
	}
	base := r.meta.Files
	remote := metaFromGist(g).Files
	head, index := r.head(), r.index()
	work := r.workTree()

	var conflicts, dirty []string
	for _, c := range diffTrees(base, remote) {
		p := c.path
		for _, cm := range r.st.Commits {
			if cm.Tree[p] != base[p] && cm.Tree[p] != remote[p] {
				conflicts = append(conflicts, p)
				break
			}
		}
		if head[p] == base[p] && (index[p] != head[p] || work[p] != head[p]) {
			dirty = append(dirty, p)
		}
	}
	if len(conflicts) > 0 {
		fmt.Fprintln(os.Stderr, "error: "+T("手元のコミットとGist側の両方で変更されたファイルがあるため、pull を中止しました:"))
		for _, p := range dedup(conflicts) {
			fmt.Fprintln(os.Stderr, "\t"+r.display(p))
		}
		gitHint(T("手元のコミットを bin g reset --soft origin/main でまとめてステージに戻し、bin g stash → bin g pull → bin g stash pop で手で直してください。"))
		os.Exit(1)
	}
	if len(dirty) > 0 && !a.has("autostash") {
		fmt.Fprintln(os.Stderr, "error: "+T("次のファイルの手元の変更は、pull で上書きされてしまいます:"))
		for _, p := range dirty {
			fmt.Fprintln(os.Stderr, "\t"+r.display(p))
		}
		fmt.Fprintln(os.Stderr, T("pull する前に、変更をコミットするか stash してください。"))
		fmt.Fprintln(os.Stderr, T("中止しました"))
		os.Exit(1)
	}
	oldHeadHash := r.headHash()
	remoteChanges := diffTrees(base, remote)
	// 未pushのコミットをGist側の変更の上に積み直す(git pull --rebase 相当)
	parent := revisionHash(r.meta.ID, g.RevisionCount)
	for i := range r.st.Commits {
		c := &r.st.Commits[i]
		for _, ch := range remoteChanges {
			if c.Tree[ch.path] == base[ch.path] {
				if ch.to == "" {
					delete(c.Tree, ch.path)
				} else {
					c.Tree[ch.path] = ch.to
				}
			}
		}
		c.Hash = commitHash(parent, c.Message, c.Time, c.Tree)
		parent = c.Hash
	}
	if r.st.Index != nil {
		for _, ch := range remoteChanges {
			if head[ch.path] == base[ch.path] {
				if ch.to == "" {
					delete(r.st.Index, ch.path)
				} else {
					r.st.Index[ch.path] = ch.to
				}
			}
		}
	}
	for _, ch := range remoteChanges {
		if head[ch.path] != base[ch.path] || len(dirty) > 0 && contains(dirty, ch.path) {
			continue
		}
		if ch.to == "" {
			if full, err := safeJoin(r.dir, ch.path); err == nil {
				_ = os.Remove(full)
				r.removeEmptyParents(full)
			}
		} else {
			r.writeFile(ch.path, r.blob(ch.to))
		}
	}
	r.work = nil
	r.meta = metaFromGist(g)
	r.saveMeta()
	r.save()
	if a.has("q") {
		return
	}
	if len(r.st.Commits) > 0 {
		fmt.Println(T("リベースして refs/heads/main を更新しました。"))
		return
	}
	fmt.Printf("Updating %s..%s\nFast-forward\n", oldHeadHash, r.headHash())
	r.printStat(remoteChanges)
	for _, ch := range remoteChanges {
		switch ch.kind {
		case 'A':
			fmt.Printf(" create mode 100644 %s\n", ch.path)
		case 'D':
			fmt.Printf(" delete mode 100644 %s\n", ch.path)
		}
	}
}

func dedup(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func treeFiles(r *gitRepo, tree map[string]string) []GistFile {
	files := make([]GistFile, 0, len(tree))
	for _, p := range sortedKeys(tree) {
		files = append(files, GistFile{Filename: p, Content: r.blob(tree[p])})
	}
	return files
}

func gitPush(args []string) {
	a := parseG(args, map[string]string{"-f": "force", "--force": "force", "--force-with-lease": "force", "-u": "x", "--set-upstream": "x",
		"-q": "q", "--quiet": "q", "--public": "public", "--unlisted": "unlisted", "--private": "private", "-n": "dry", "--dry-run": "dry",
		"--tags": "x", "-v": "x"})
	for i, p := range a.pos {
		switch {
		case i == 0 && p == "origin":
		case i == 1 && (p == "main" || p == "HEAD" || p == "main:main" || p == "HEAD:main" || p == "+main"):
			if strings.HasPrefix(p, "+") {
				a.vals["force"] = []string{""}
			}
		default:
			gitError(T("bin では origin の main にだけ push できます('%s')"), p)
		}
	}
	vis := ""
	for _, v := range []string{"public", "unlisted", "private"} {
		if a.has(v) {
			vis = v
		}
	}
	r := openRepo()
	cfg, session := requireSession()
	if r.meta.ID == "" {
		if len(r.st.Commits) == 0 {
			fmt.Fprintln(os.Stderr, "error: src refspec main does not match any")
			fmt.Fprintln(os.Stderr, "error: failed to push some refs to 'origin'")
			os.Exit(1)
		}
		if a.has("dry") {
			fmt.Fprintln(os.Stderr, " * [new branch]      main -> main")
			return
		}
		if vis == "" {
			vis = "unlisted"
		}
		title := filepath.Base(r.dir)
		first := r.st.Commits[0]
		id, err := saveGist(cfg, session, nil, title, "", vis, treeFiles(r, first.Tree), first.Message)
		if err != nil {
			gitFatal("%v", err)
		}
		r.meta = syncMeta{ID: id, Files: map[string]string{}}
		r.st.Commits = r.st.Commits[1:]
		r.pushCommits(cfg, session, 0, vis, a.has("q"), true)
		success(T("Gistを作成しました(%s)"), T(map[string]string{"public": "公開", "unlisted": "限定公開", "private": "非公開"}[vis]))
		fmt.Println(gistURL(cfg, id))
		return
	}
	g, err := getGist(cfg, session, r.meta.ID)
	if err != nil {
		gitFatal("%v", err)
	}
	if g.Owner.ID != userIDFromToken(session.AccessToken) {
		gitFatal(T("自分のGistではないため push できません(bin fork で自分のGistにしてから clone してください)"))
	}
	r.st.Fetched = g.RevisionCount
	if len(r.st.Commits) == 0 && vis == "" && g.RevisionCount == r.meta.Revision {
		r.save()
		fmt.Fprintln(os.Stderr, "Everything up-to-date")
		return
	}
	if g.RevisionCount != r.meta.Revision && !a.has("force") {
		r.save()
		fmt.Fprintf(os.Stderr, "To %s\n", r.remoteURL())
		fmt.Fprintln(os.Stderr, red(" ! [rejected]")+"        main -> main (fetch first)")
		fmt.Fprintf(os.Stderr, "error: failed to push some refs to '%s'\n", r.remoteURL())
		gitHint(T("Gist側に手元に無い変更があるため拒否されました。先に 'bin g pull' で取り込んでから、もう一度 push してください。"))
		os.Exit(1)
	}
	if a.has("dry") {
		fmt.Fprintf(os.Stderr, "To %s\n   %s..%s  main -> main\n", r.remoteURL(), revisionHash(r.meta.ID, g.RevisionCount), r.headHash())
		return
	}
	if g.RevisionCount != r.meta.Revision && len(r.st.Commits) == 0 {
		// reset で戻した状態を -f で押し付ける
		r.st.Commits = []gitCommit{{Message: T("以前の状態に戻す"), Tree: copyTree(r.head())}}
	}
	if vis == "" {
		vis = g.Visibility
	} else if len(r.st.Commits) == 0 {
		if _, err := saveGist(cfg, session, &g.ID, g.Title, g.Description, vis, treeFiles(r, r.head()), ""); err != nil {
			gitFatal("%v", err)
		}
	}
	r.meta.Revision = g.RevisionCount
	r.pushCommits(cfg, session, g.RevisionCount, vis, a.has("q"), false)
}

// pushCommits は未pushのコミットを古い順に1つずつGistのリビジョンとして保存する
func (r *gitRepo) pushCommits(cfg Config, session *Session, oldRev int, vis string, quiet, created bool) {
	var g *Gist
	var err error
	for len(r.st.Commits) > 0 {
		if g == nil {
			if g, err = getGist(cfg, session, r.meta.ID); err != nil {
				gitFatal("%v", err)
			}
		}
		c := r.st.Commits[0]
		if len(c.Tree) == 0 {
			gitFatal(T("Gistには1ファイル以上必要なので、空のツリーはコミットできません"))
		}
		if _, err := saveGist(cfg, session, &g.ID, g.Title, g.Description, vis, treeFiles(r, c.Tree), c.Message); err != nil {
			r.save()
			gitFatal("%v", err)
		}
		r.st.Commits = r.st.Commits[1:]
	}
	updated, err := getGist(cfg, session, r.meta.ID)
	if err != nil {
		gitFatal("%v", err)
	}
	r.meta = metaFromGist(updated)
	r.st.Fetched = updated.RevisionCount
	r.st.Index = nil
	r.saveMeta()
	r.save()
	if quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "To %s\n", r.remoteURL())
	if created {
		fmt.Fprintln(os.Stderr, " * [new branch]      main -> main")
		return
	}
	old := "0000000"
	if oldRev > 0 {
		old = revisionHash(r.meta.ID, oldRev)
	}
	fmt.Fprintf(os.Stderr, "   %s..%s  main -> main\n", old, revisionHash(r.meta.ID, updated.RevisionCount))
}

// ---- remote / branch ----

func gitRemote(args []string) {
	a := parseG(args, map[string]string{"-v": "v", "--verbose": "v"})
	r := openRepo()
	if len(a.pos) == 0 {
		if r.meta.ID == "" {
			return
		}
		if a.has("v") {
			fmt.Printf("origin\t%s (fetch)\norigin\t%s (push)\n", r.remoteURL(), r.remoteURL())
		} else {
			fmt.Println("origin")
		}
		return
	}
	sub, rest := a.pos[0], a.pos[1:]
	if len(rest) > 0 && rest[0] != "origin" {
		gitError(T("bin で使えるリモートは origin だけです"))
	}
	switch sub {
	case "add":
		if len(rest) != 2 {
			gitUsageError("usage: bin g remote add origin <id|url>")
		}
		if r.meta.ID != "" {
			fmt.Fprintln(os.Stderr, "error: remote origin already exists.")
			os.Exit(3)
		}
		r.setOrigin(rest[1])
	case "set-url":
		if len(rest) != 2 {
			gitUsageError("usage: bin g remote set-url origin <id|url>")
		}
		r.needOrigin()
		r.setOrigin(rest[1])
	case "get-url":
		r.needOrigin()
		fmt.Println(r.remoteURL())
	case "remove", "rm":
		r.needOrigin()
		// 紐付けを外しても手元の履歴は残す(今の origin/main の中身を1つのコミットにする)
		if len(r.meta.Files) > 0 {
			now := time.Now().UTC().Format(time.RFC3339)
			msg := fmt.Sprintf(T("%s から取り込み"), r.meta.ID)
			base := gitCommit{Hash: commitHash("", msg, now, r.meta.Files), Message: msg, Time: now, Tree: copyTree(r.meta.Files)}
			r.st.Commits = append([]gitCommit{base}, r.st.Commits...)
		}
		r.meta = syncMeta{Files: map[string]string{}}
		r.st.Fetched = 0
		r.saveMeta()
		r.save()
	case "show":
		r.needOrigin()
		fmt.Printf("* remote origin\n  Fetch URL: %s\n  Push  URL: %s\n  HEAD branch: main\n", r.remoteURL(), r.remoteURL())
	default:
		gitUsageError(T("'%s' は bin g remote では使えません(add / set-url / get-url / remove / show / -v)"), sub)
	}
}

func (r *gitRepo) setOrigin(s string) {
	id, err := parseGistID(s)
	if err != nil {
		gitFatal("%v", err)
	}
	if id != r.meta.ID {
		// 別のGistに付け替えたら、共通の元は無い状態から(次の pull で取り込み、push は pull 後に)
		if len(r.st.Commits) == 0 && len(r.meta.Files) > 0 {
			now := time.Now().UTC().Format(time.RFC3339)
			msg := fmt.Sprintf(T("%s から取り込み"), r.meta.ID)
			r.st.Commits = []gitCommit{{Hash: commitHash("", msg, now, r.meta.Files), Message: msg, Time: now, Tree: copyTree(r.meta.Files)}}
		}
		r.meta = syncMeta{ID: id, Files: map[string]string{}}
		r.st.Fetched = 0
	}
	r.saveMeta()
	r.save()
}

func gitBranch(args []string) {
	a := parseG(args, map[string]string{"-a": "all", "--all": "all", "-r": "remote", "--remotes": "remote", "-v": "v", "-vv": "v", "--verbose": "v",
		"--show-current": "current", "-l": "list", "--list": "list", "--no-color": "x", "--color": "x"})
	if len(a.pos) > 0 || len(a.paths) > 0 {
		gitFatal(T("bin ではブランチは main だけです"))
	}
	r := openRepo()
	if a.has("current") {
		fmt.Println("main")
		return
	}
	verbose := func(name string, e *gitEntry, track string) string {
		if !a.has("v") || e == nil {
			return name
		}
		return fmt.Sprintf("%s %s %s%s", name, yellow(e.hash), track, firstLine(e.message))
	}
	var headEntry, originEntry *gitEntry
	if c := r.headChain(); len(c) > 0 {
		headEntry = &c[0]
	}
	if r.meta.ID != "" && r.originRevision() > 0 {
		if c := r.remoteEntries(r.originRevision()); len(c) > 0 {
			originEntry = &c[0]
		}
	}
	if !a.has("remote") {
		track := ""
		if r.meta.ID != "" && a.has("v") {
			ahead, behind := r.aheadBehind()
			var parts []string
			if ahead > 0 {
				parts = append(parts, fmt.Sprintf("ahead %d", ahead))
			}
			if behind > 0 {
				parts = append(parts, fmt.Sprintf("behind %d", behind))
			}
			track = "[" + blue("origin/main")
			if len(parts) > 0 {
				track += ": " + strings.Join(parts, ", ")
			}
			track += "] "
		}
		fmt.Println(verbose("* "+green("main"), headEntry, track))
	}
	if (a.has("all") || a.has("remote")) && r.meta.ID != "" {
		name := "origin/main"
		if a.has("all") {
			name = "remotes/origin/main"
		}
		fmt.Println(verbose("  "+red(name), originEntry, ""))
	}
}

// ---- stash ----

func gitStashCmd(args []string) {
	sub := "push"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	r := openRepo()
	switch sub {
	case "push", "save":
		a := parseG(args, map[string]string{"-m": "m", "--message": "m", "-u": "u", "--include-untracked": "u", "-q": "q", "--quiet": "q", "-k": "x", "--keep-index": "x"}, "m")
		msg := a.val("m")
		if sub == "save" && len(a.pos) > 0 {
			msg = strings.Join(a.pos, " ")
		}
		r.stashPush(msg, a.has("u"), a.has("q"))
	case "list":
		for i, s := range r.st.Stash {
			fmt.Printf("stash@{%d}: %s\n", i, s.Message)
		}
	case "pop", "apply":
		a := parseG(args, map[string]string{"-q": "q", "--quiet": "q", "--index": "index"})
		i := stashIndex(r, a.pos)
		r.stashApply(i, a.has("index"))
		if sub == "pop" {
			r.st.Stash = append(r.st.Stash[:i], r.st.Stash[i+1:]...)
			r.save()
			fmt.Printf(T("stash@{%d} を削除しました")+"\n", i)
		}
	case "drop":
		i := stashIndex(r, args)
		r.st.Stash = append(r.st.Stash[:i], r.st.Stash[i+1:]...)
		r.save()
		fmt.Printf(T("stash@{%d} を削除しました")+"\n", i)
	case "clear":
		r.st.Stash = nil
		r.save()
	case "show":
		opts := map[string]string{}
		for k, v := range diffOptMap {
			opts[k] = v
		}
		a := parseG(args, opts, "U")
		s := r.st.Stash[stashIndex(r, a.pos)]
		changes := diffTrees(s.Base, s.Work)
		o := diffOptsFrom(a)
		if !a.has("patch") && !o.nameOnly && !o.nameStatus && !o.numstat {
			o.stat = true
		}
		stop := startPager()
		r.printDiff(changes, o)
		stop()
	default:
		gitUsageError(T("'%s' は bin g stash では使えません(push / list / pop / apply / drop / clear / show)"), sub)
	}
}

func stashIndex(r *gitRepo, pos []string) int {
	i := 0
	if len(pos) > 0 {
		s := strings.TrimSuffix(strings.TrimPrefix(pos[0], "stash@{"), "}")
		n, err := strconv.Atoi(s)
		if err != nil {
			gitFatal(T("'%s' は stash の指定として正しくありません"), pos[0])
		}
		i = n
	}
	if len(r.st.Stash) == 0 {
		gitError(T("stash がありません。"))
	}
	if i < 0 || i >= len(r.st.Stash) {
		gitError(T("stash@{%d} は存在しません"), i)
	}
	return i
}

func (r *gitRepo) stashPush(msg string, untracked, quiet bool) {
	head, index := r.head(), r.index()
	work := r.worktree()
	tracked := copyTree(index)
	for p, h := range head {
		tracked[p] = h
	}
	w := map[string]string{}
	for p := range tracked {
		if c, ok := work[p]; ok {
			w[p] = r.put(c)
		}
	}
	if untracked {
		for p, c := range work {
			if _, ok := tracked[p]; !ok && !r.ignored(p) {
				w[p] = r.put(c)
				tracked[p] = w[p]
			}
		}
	}
	if sameTree(head, index) && sameTree(index, w) {
		fmt.Println(T("保存するローカルの変更はありません"))
		return
	}
	if msg == "" {
		desc := "(no commit)"
		if c := r.headChain(); len(c) > 0 {
			desc = c[0].hash + " " + firstLine(c[0].message)
		}
		msg = "WIP on main: " + desc
	} else {
		msg = "On main: " + msg
	}
	s := gitStash{Message: msg, Time: time.Now().UTC().Format(time.RFC3339), Base: copyTree(head), Index: copyTree(index), Work: w}
	r.st.Stash = append([]gitStash{s}, r.st.Stash...)
	r.st.Index = nil
	r.checkoutTree(head, tracked)
	r.save()
	if !quiet {
		fmt.Printf(T("作業ディレクトリとインデックスの状態を保存しました: %s")+"\n", msg)
	}
}

func (r *gitRepo) stashApply(i int, withIndex bool) {
	s := r.st.Stash[i]
	head, index := r.head(), r.index()
	work := r.workTree()
	changes := diffTrees(s.Base, s.Work)
	var bad []string
	for _, c := range changes {
		cur, ok := work[c.path]
		if !ok && index[c.path] == "" && head[c.path] == "" {
			continue // 消えている/無いファイルはそのまま戻せる
		}
		if cur != index[c.path] || index[c.path] != head[c.path] || head[c.path] != s.Base[c.path] {
			if cur != c.to {
				bad = append(bad, c.path)
			}
		}
	}
	if len(bad) > 0 {
		fmt.Fprintln(os.Stderr, "error: "+T("次のファイルの手元の変更は、stash の適用で上書きされてしまいます:"))
		for _, p := range bad {
			fmt.Fprintln(os.Stderr, "\t"+r.display(p))
		}
		fmt.Fprintln(os.Stderr, T("適用する前に、変更をコミットするか stash してください。"))
		os.Exit(1)
	}
	for _, c := range changes {
		if c.to == "" {
			if full, err := safeJoin(r.dir, c.path); err == nil {
				_ = os.Remove(full)
				r.removeEmptyParents(full)
			}
		} else {
			r.writeFile(c.path, r.blob(c.to))
		}
	}
	for _, c := range diffTrees(s.Base, s.Index) {
		if withIndex || c.kind == 'A' {
			if c.to == "" {
				delete(index, c.path)
			} else {
				index[c.path] = c.to
			}
		}
	}
	r.work = nil
	r.save()
	gitStatus(nil)
}

// ---- rev-parse / ls-files / clean ----

func gitRevParse(args []string) {
	r := openRepo()
	short := false
	abbrev := false
	for _, s := range args {
		switch s {
		case "--show-toplevel":
			fmt.Println(r.dir)
		case "--git-dir", "--absolute-git-dir":
			fmt.Println(filepath.Join(r.dir, gitStateDir))
		case "--is-inside-work-tree":
			fmt.Println("true")
		case "--is-bare-repository", "--is-inside-git-dir":
			fmt.Println("false")
		case "--show-prefix":
			cwd, _ := os.Getwd()
			rel, _ := filepath.Rel(r.dir, cwd)
			if rel == "." {
				fmt.Println()
			} else {
				fmt.Println(filepath.ToSlash(rel) + "/")
			}
		case "--short":
			short = true
		case "--abbrev-ref":
			abbrev = true
		case "--verify", "-q", "--quiet":
		default:
			if strings.HasPrefix(s, "--short=") {
				short = true
				continue
			}
			if abbrev {
				switch s {
				case "HEAD", "main", "@":
					fmt.Println("main")
				case "@{u}", "@{upstream}", "HEAD@{upstream}", "main@{u}":
					r.needOrigin()
					fmt.Println("origin/main")
				default:
					fmt.Println(s)
				}
				continue
			}
			_ = short
			fmt.Println(r.resolve(s)[0].hash)
		}
	}
}

func gitLsFiles(args []string) {
	a := parseG(args, map[string]string{"-c": "c", "--cached": "c", "-o": "o", "--others": "o", "-m": "m", "--modified": "m", "-d": "d",
		"--deleted": "d", "--exclude-standard": "x", "-s": "x", "-z": "z"})
	r := openRepo()
	ms := r.matchers(append(a.pos, a.paths...))
	end := "\n"
	if a.has("z") {
		end = "\x00"
	}
	s := r.status(ms, true)
	switch {
	case a.has("o"):
		for _, p := range s.untracked {
			fmt.Print(r.display(p) + end)
		}
	case a.has("m") || a.has("d"):
		for _, c := range s.unstaged {
			if a.has("m") || c.kind == 'D' {
				fmt.Print(r.display(c.path) + end)
			}
		}
	default:
		for _, p := range sortedKeys(r.index()) {
			if matchAny(ms, p) {
				fmt.Print(r.display(p) + end)
			}
		}
	}
}

func gitClean(args []string) {
	a := parseG(args, map[string]string{"-n": "dry", "--dry-run": "dry", "-f": "force", "--force": "force", "-d": "d", "-x": "x", "-q": "q", "--quiet": "q", "-i": "i"})
	if !a.has("dry") && !a.has("force") {
		gitFatal(T("-f(削除)か -n(確認のみ)を指定してください。何も削除しませんでした"))
	}
	r := openRepo()
	s := r.status(r.matchers(append(a.pos, a.paths...)), false)
	var targets []string
	for _, p := range s.untracked {
		if !strings.HasSuffix(p, "/") {
			targets = append(targets, p)
			continue
		}
		if a.has("d") {
			for _, q := range sortedKeys(r.workTree()) {
				if strings.HasPrefix(q, p) && !r.ignored(q) {
					targets = append(targets, q)
				}
			}
		}
	}
	if a.has("x") {
		for p := range r.workTree() {
			if _, ok := r.index()[p]; !ok && r.ignored(p) && (a.has("d") || !strings.Contains(p, "/")) {
				targets = append(targets, p)
			}
		}
		sort.Strings(targets)
	}
	for _, p := range targets {
		if a.has("dry") {
			fmt.Printf(T("%s を削除します")+"\n", r.display(p))
			continue
		}
		full, err := safeJoin(r.dir, p)
		if err != nil {
			continue
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			warnErr("%v", err)
			continue
		}
		r.removeEmptyParents(full)
		if !a.has("q") {
			fmt.Printf(T("%s を削除しました")+"\n", r.display(p))
		}
	}
}
