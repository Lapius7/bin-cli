package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- bin watch ----

// bin watch [<id>] <file|dir>... : 手元のファイル・フォルダを監視し、保存のたびに Gist へ反映する。
// IDを省くと最初に新しいGistを作る。フォルダは中身を再帰的に追う(.gitignore風の除外・--include/--exclude も有効)。
func cmdWatch(args []string) {
	p := parseArgs(foldFileOpts(args, 0), mergeBool(gistValueFlags, map[string]string{"--interval": "interval"}), mergeBool(visibilityFlags, map[string]string{"--mirror": "mirror", "--keep-deleted": "keep"}))
	includeGlobs, excludeGlobs = p.values["include"], p.values["exclude"]
	usage := "bin watch [<id>] <file|dir>... [-t <title>] [-d <desc>] [--public|--unlisted|--private] [--mirror] [--interval <sec>]"
	pos := p.positional
	if len(pos) == 0 {
		usageExit(usage)
	}

	// 先頭が実在するパスならパス、そうでなければGistのID(URL可)として扱う
	var id string
	if _, err := os.Stat(pos[0]); err != nil {
		gid, perr := parseGistID(pos[0])
		if perr != nil {
			fail(fmt.Errorf(T("%s は存在するパスでもGistのIDでもありません"), pos[0]))
		}
		id, pos = gid, pos[1:]
	}
	if len(pos) == 0 {
		usageExit(usage)
	}
	for _, r := range pos {
		if _, err := os.Stat(r); err != nil {
			fail(fmt.Errorf(T("ファイルを読み込めません: %w"), err))
		}
	}

	asName, _ := p.value("filename")
	if asName != "" {
		if len(pos) != 1 {
			fail(fmt.Errorf(T("--as は監視対象が1ファイルのときだけ使えます")))
		}
		if st, _ := os.Stat(pos[0]); st != nil && st.IsDir() {
			fail(fmt.Errorf(T("--as は監視対象が1ファイルのときだけ使えます")))
		}
		if err := validateGistPath(asName); err != nil {
			fail(err)
		}
	}

	iv := time.Second
	if v, ok := p.value("interval"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.2 {
			iv = time.Duration(f * float64(time.Second))
		}
	}

	cfg, session := requireSession()
	title, _ := p.value("title")
	description, _ := p.value("description")
	visibility, hasVis := pickVisibility(p)
	message, _ := p.value("message")

	collect := func() []GistFile {
		var files []GistFile
		var skips []skipped
		softReadErrors = true
		for _, r := range pos {
			collectPath(r, &files, &skips)
		}
		softReadErrors = false
		if asName != "" && len(files) == 1 {
			files[0].Filename = asName
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Filename < files[j].Filename })
		return files
	}

	// 既存Gistなら持ち主か確認。--mirror は既存Gistでは明示した時だけ(手元に無いファイルを消してしまうため)
	mirror := p.bools["mirror"]
	if id != "" {
		g, err := getGist(cfg, session, id)
		if err != nil {
			fail(err)
		}
		if g.Owner.ID != userIDFromToken(session.AccessToken) {
			fail(fmt.Errorf(T("自分のGistではないため編集できません")))
		}
	} else {
		mirror = !p.bools["keep"] // 新規作成したGistは手元のフォルダと同じ内容に保つ(消したファイルも反映)
	}

	local := collect()
	if len(local) == 0 && id == "" {
		fail(fmt.Errorf(T("アップロードできるファイルがありません")))
	}

	var last map[string]string // 直近に反映した手元の内容
	var remoteCache *Gist
	push := func(cur []GistFile, first bool) (bool, error) {
		if id == "" {
			names := make([]string, len(cur))
			for i, f := range cur {
				names[i] = f.Filename
			}
			vis := visibility
			if !hasVis {
				vis = "unlisted"
			}
			nid, err := saveGist(cfg, session, nil, title, description, vis, cur, message)
			if err != nil {
				return false, err
			}
			id = nid
			fmt.Fprintf(os.Stderr, "%s %s\n", green("✔"), T("作成しました"))
			fmt.Fprintf(os.Stderr, "  %s\n", gistURL(cfg, id))
			return true, nil
		}
		remote, err := getGist(cfg, session, id)
		if err != nil {
			return false, err
		}
		remoteCache = remote
		prev := cloneFiles(remote.Files)
		byName := map[string]GistFile{}
		for _, f := range remote.Files {
			byName[f.Filename] = f
		}
		if mirror {
			byName = map[string]GistFile{}
		}
		for _, f := range cur {
			byName[f.Filename] = f
		}
		next := make([]GistFile, 0, len(byName))
		for _, f := range byName {
			next = append(next, f)
		}
		sort.Slice(next, func(i, j int) bool { return next[i].Filename < next[j].Filename })
		if len(next) == 0 {
			return false, fmt.Errorf(T("Gistを空にはできません"))
		}
		cs := changesBetween(prev, next)
		if len(cs) == 0 {
			return false, nil
		}
		add, del := 0, 0
		names := make([]string, 0, len(cs))
		for _, c := range cs {
			add += c.add
			del += c.del
			names = append(names, c.name)
		}
		t, d, v := remote.Title, remote.Description, remote.Visibility
		if first {
			if title != "" {
				t = title
			}
			if description != "" {
				d = description
			}
			if hasVis {
				v = visibility
			}
		}
		msg := message
		if msg == "" {
			msg = "watch: " + summarizeNames(names)
		}
		if _, err := saveGist(cfg, session, &id, t, d, v, next, msg); err != nil {
			return false, err
		}
		fmt.Fprintf(os.Stderr, "%s %s  %s %s %s\n", green("✔"), dim(time.Now().Format("15:04:05")), bold(summarizeNames(names)), green(fmt.Sprintf("+%d", add)), red(fmt.Sprintf("-%d", del)))
		return true, nil
	}
	_ = remoteCache

	fmt.Fprintf(os.Stderr, "%s %s %s\n", cyan("◉"), bold(strings.Join(pos, ", ")), dim(T("を監視します(Ctrl-C で終了)")))
	if id != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", dim(gistURL(cfg, id)))
	}
	if _, err := push(local, true); err != nil {
		fail(err)
	}
	last = toMap(local)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	tick := time.NewTicker(iv)
	defer tick.Stop()
	fp := fingerprint(pos)
	dirty := false
	for {
		select {
		case <-stop:
			fmt.Fprintln(os.Stderr, "\n"+dim(T("終了しました")))
			return
		case <-tick.C:
			if nf := fingerprint(pos); nf != fp {
				fp = nf
				dirty = true
				time.Sleep(300 * time.Millisecond) // 保存途中・まとめて保存の途中を避ける
				fp = fingerprint(pos)
			}
			if !dirty {
				continue
			}
			cur := collect()
			m := toMap(cur)
			if mapsEqual(m, last) {
				dirty = false
				continue
			}
			if _, err := push(cur, false); err != nil {
				fmt.Fprintf(os.Stderr, "%s %v %s\n", red("✖"), err, dim(T("(次の周期で再試行します)")))
				continue // dirty のまま残して再試行
			}
			last = m
			dirty = false
		}
	}
}

func toMap(fs []GistFile) map[string]string {
	m := make(map[string]string, len(fs))
	for _, f := range fs {
		m[f.Filename] = f.Content
	}
	return m
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func summarizeNames(names []string) string {
	if len(names) <= 2 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, %s %s", names[0], names[1], fmt.Sprintf(T("ほか%d件"), len(names)-2))
}

// fingerprint は監視対象の(更新日時,サイズ)をまとめた文字列。内容は読まないので軽い。
func fingerprint(roots []string) string {
	var sb strings.Builder
	for _, r := range roots {
		info, err := os.Stat(r)
		if err != nil {
			sb.WriteString("x|")
			continue
		}
		if !info.IsDir() {
			fmt.Fprintf(&sb, "%s:%d:%d|", r, info.ModTime().UnixNano(), info.Size())
			continue
		}
		_ = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != r && ignoredDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if st, err := d.Info(); err == nil {
				fmt.Fprintf(&sb, "%s:%d:%d|", p, st.ModTime().UnixNano(), st.Size())
			}
			return nil
		})
	}
	return sb.String()
}
