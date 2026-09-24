package main

// 手元のフォルダとGistの同期(bin status / pull / push / sync)。
//
// bin clone したフォルダには .lapbin.json(GistのIDと、取得した時点のリビジョン番号・各ファイルのSHA-256)を置き、
// それを「共通の元」として、手元の変更とGist側の変更を見分ける(gitの3-way比較を単純化したもの)。
//   pull: Gist側で変わったファイルを取り込む。手元でも変えていたファイルは衝突として止める(--forceでGist側を優先)
//   push: 手元のフォルダの中身でGistを更新する。Gist側が先に更新されていたら止める(--forceで上書き)
//   sync: pull してから、手元に変更があれば push

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const syncMetaFile = ".lapbin.json"

type syncMeta struct {
	ID       string            `json:"id"`
	Revision int               `json:"revision"`
	Files    map[string]string `json:"files"`
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func metaFromGist(g *Gist) syncMeta {
	m := syncMeta{ID: g.ID, Revision: g.RevisionCount, Files: map[string]string{}}
	for _, f := range g.Files {
		m.Files[f.Filename] = contentHash(f.Content)
	}
	return m
}

func writeSyncMeta(dir string, m syncMeta) error {
	data, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(filepath.Join(dir, syncMetaFile), append(data, '\n'), 0o644)
}

// findSyncDir は指定のフォルダ(省略時はカレント)から親をたどって .lapbin.json を探す
func findSyncDir(start string) (string, syncMeta) {
	dir, err := filepath.Abs(start)
	if err != nil {
		fail(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, syncMetaFile))
		if err == nil {
			var m syncMeta
			if err := json.Unmarshal(data, &m); err != nil || !gistIDPattern.MatchString(m.ID) {
				fail(fmt.Errorf(T("%s を読み込めません"), filepath.Join(dir, syncMetaFile)))
			}
			if m.Files == nil {
				m.Files = map[string]string{}
			}
			return dir, m
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fail(errors.New(T("Gistと同期しているフォルダではありません(bin clone で取得したフォルダの中で実行してください)")))
		}
		dir = parent
	}
}

// localFiles はフォルダの中身(.lapbin.json と除外フォルダ以外)を集める
func localFiles(dir string) (map[string]string, []skipped) {
	files := map[string]string{}
	var skips []skipped
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		gp := filepath.ToSlash(rel)
		if gp == syncMetaFile {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		switch {
		case len(data) > maxFileBytes:
			skips = append(skips, skipped{gp, T("1MB超")})
		case !utf8.Valid(data) || strings.ContainsRune(string(data), 0):
			skips = append(skips, skipped{gp, T("バイナリ")})
		default:
			files[gp] = string(data)
		}
		return nil
	})
	return files, skips
}

type localChange struct {
	path string
	kind string // A(追加) M(変更) D(削除)
}

func diffLocal(base map[string]string, local map[string]string) []localChange {
	var out []localChange
	for p, c := range local {
		if h, ok := base[p]; !ok {
			out = append(out, localChange{p, "A"})
		} else if h != contentHash(c) {
			out = append(out, localChange{p, "M"})
		}
	}
	for p := range base {
		if _, ok := local[p]; !ok {
			out = append(out, localChange{p, "D"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func changeColor(kind string) string {
	switch kind {
	case "A":
		return green("A")
	case "D":
		return red("D")
	}
	return yellow("M")
}

func dirArg(p parsedArgs, usage string) string {
	if len(p.positional) > 1 {
		usageExit(usage)
	}
	if len(p.positional) == 1 {
		return p.positional[0]
	}
	return "."
}

func cmdStatus(args []string) {
	p := parseArgs(args, nil, nil)
	dir, meta := findSyncDir(dirArg(p, "bin status [<dir>]"))
	cfg, session := optionalSession()
	local, skips := localFiles(dir)
	changes := diffLocal(meta.Files, local)

	fmt.Printf("%s %s  %s\n", bold("Gist"), cyan(meta.ID), dim(gistURL(cfg, meta.ID)))
	fmt.Println(dim(fmt.Sprintf(T("基準: リビジョン %d · %s"), meta.Revision, dir)))
	if g, err := getGist(cfg, session, meta.ID); err == nil {
		if g.RevisionCount > meta.Revision {
			fmt.Println(yellow("↓ ") + fmt.Sprintf(T("Gist側に新しい変更があります(リビジョン %d → %d)。bin pull で取り込めます"), meta.Revision, g.RevisionCount))
		} else {
			fmt.Println(dim(T("Gist側に新しい変更はありません")))
		}
	} else {
		warnErr(T("Gistの状態を確認できませんでした: %v"), err)
	}
	fmt.Println()
	if len(changes) == 0 {
		fmt.Println(T("手元の変更はありません"))
	} else {
		fmt.Println(bold(fmt.Sprintf(Tn("手元の変更 %d件", len(changes)), len(changes))) + dim("  ("+T("bin push で反映")+")"))
		for _, c := range changes {
			fmt.Printf("  %s %s\n", changeColor(c.kind), c.path)
		}
	}
	for _, s := range skips {
		fmt.Println(dim(fmt.Sprintf("  - %s (%s)", s.path, s.reason)))
	}
}

// pullInto はGist側の変更を取り込む。衝突があり force でなければ何も書かずにエラーを返す
func pullInto(cfg Config, session *Session, dir string, meta syncMeta, force bool) (syncMeta, int, error) {
	g, err := getGist(cfg, session, meta.ID)
	if err != nil {
		return meta, 0, err
	}
	local, _ := localFiles(dir)
	remote := map[string]string{}
	for _, f := range g.Files {
		remote[f.Filename] = f.Content
	}

	type op struct {
		path    string
		content *string // nil なら削除
	}
	var ops []op
	var conflicts []string
	localChanged := func(p string) bool {
		c, ok := local[p]
		base, inBase := meta.Files[p]
		if !ok {
			return inBase // 手元で消した
		}
		return !inBase || contentHash(c) != base
	}
	for p, c := range remote {
		if meta.Files[p] == contentHash(c) {
			continue // Gist側は変わっていない
		}
		if lc, ok := local[p]; ok && lc == c {
			continue // 手元も同じ内容
		}
		if localChanged(p) && !force {
			conflicts = append(conflicts, p)
			continue
		}
		content := c
		ops = append(ops, op{p, &content})
	}
	for p := range meta.Files {
		if _, ok := remote[p]; ok {
			continue
		}
		// Gist側で消えたファイル
		if _, ok := local[p]; !ok {
			continue
		}
		if localChanged(p) && !force {
			conflicts = append(conflicts, p)
			continue
		}
		ops = append(ops, op{p, nil})
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return meta, 0, fmt.Errorf(T("手元とGistの両方で変更されたファイルがあります(%s)。手元の変更を退避するか、--force でGist側の内容を優先してください"), strings.Join(conflicts, ", "))
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].path < ops[j].path })
	for _, o := range ops {
		path, err := safeJoin(dir, o.path)
		if err != nil {
			return meta, 0, err
		}
		if o.content == nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return meta, 0, err
			}
			fmt.Printf("  %s %s\n", red("D"), o.path)
			continue
		}
		kind := "M"
		if _, err := os.Stat(path); os.IsNotExist(err) {
			kind = "A"
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return meta, 0, err
		}
		if err := os.WriteFile(path, []byte(*o.content), 0o644); err != nil {
			return meta, 0, err
		}
		fmt.Printf("  %s %s\n", changeColor(kind), o.path)
	}
	next := metaFromGist(g)
	return next, len(ops), writeSyncMeta(dir, next)
}

func cmdPull(args []string) {
	p := parseArgs(args, nil, map[string]string{"--force": "force", "-f": "force"})
	dir, meta := findSyncDir(dirArg(p, "bin pull [<dir>] [--force]"))
	cfg, session := optionalSession()
	next, n, err := pullInto(cfg, session, dir, meta, p.bools["force"])
	if err != nil {
		fail(err)
	}
	if n == 0 {
		success(T("最新です(リビジョン %d)"), next.Revision)
		return
	}
	success(Tn("%dファイルを取り込みました(リビジョン %d)", n), n, next.Revision)
}

// pushFrom は手元のフォルダの中身でGistを更新する。変更が無ければ false
func pushFrom(cfg Config, session *Session, dir string, meta syncMeta, message string, force bool) (syncMeta, bool, error) {
	g, err := getGist(cfg, session, meta.ID)
	if err != nil {
		return meta, false, err
	}
	if g.Owner.ID != userIDFromToken(session.AccessToken) {
		return meta, false, errors.New(T("自分のGistではないため反映できません(bin fork で自分のGistにしてから clone してください)"))
	}
	if g.RevisionCount != meta.Revision && !force {
		return meta, false, fmt.Errorf(T("Gist側が先に更新されています(リビジョン %d → %d)。先に bin pull で取り込むか、--force で上書きしてください"), meta.Revision, g.RevisionCount)
	}
	local, skips := localFiles(dir)
	for _, s := range skips {
		warnErr(T("%s は対象外です(%s)"), s.path, s.reason)
	}
	remote := metaFromGist(g)
	changes := diffLocal(remote.Files, local)
	if len(changes) == 0 {
		// Gist側と同じ内容なら基準だけ合わせる
		return remote, false, writeSyncMeta(dir, remote)
	}
	if len(local) == 0 {
		return meta, false, errors.New(T("フォルダが空です(Gistには1ファイル以上必要です)"))
	}
	files := make([]GistFile, 0, len(local))
	for p, c := range local {
		files = append(files, GistFile{Filename: p, Content: c})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Filename < files[j].Filename })
	if _, err := saveGist(cfg, session, &g.ID, g.Title, g.Description, g.Visibility, files, message); err != nil {
		return meta, false, err
	}
	for _, c := range changes {
		fmt.Printf("  %s %s\n", changeColor(c.kind), c.path)
	}
	updated, err := getGist(cfg, session, g.ID)
	if err != nil {
		return meta, true, err
	}
	next := metaFromGist(updated)
	return next, true, writeSyncMeta(dir, next)
}

func cmdPush(args []string) {
	p := parseArgs(args, map[string]string{"-m": "message", "--message": "message"}, map[string]string{"--force": "force"})
	dir, meta := findSyncDir(dirArg(p, "bin push [<dir>] [-m <message>] [--force]"))
	cfg, session := requireSession()
	message, _ := p.value("message")
	next, changed, err := pushFrom(cfg, session, dir, meta, message, p.bools["force"])
	if err != nil {
		fail(err)
	}
	if !changed {
		success(T("反映する変更はありません"))
		return
	}
	success(T("Gistを更新しました(リビジョン %d)"), next.Revision)
	fmt.Println(gistURL(cfg, next.ID))
}

func cmdSync(args []string) {
	p := parseArgs(args, map[string]string{"-m": "message", "--message": "message"}, nil)
	dir, meta := findSyncDir(dirArg(p, "bin sync [<dir>] [-m <message>]"))
	cfg, session := requireSession()
	next, pulled, err := pullInto(cfg, session, dir, meta, false)
	if err != nil {
		fail(err)
	}
	if pulled > 0 {
		success(Tn("%dファイルを取り込みました(リビジョン %d)", pulled), pulled, next.Revision)
	}
	message, _ := p.value("message")
	final, changed, err := pushFrom(cfg, session, dir, next, message, false)
	if err != nil {
		fail(err)
	}
	if changed {
		success(T("Gistを更新しました(リビジョン %d)"), final.Revision)
	} else if pulled == 0 {
		success(T("同期済みです(リビジョン %d)"), final.Revision)
	}
}
