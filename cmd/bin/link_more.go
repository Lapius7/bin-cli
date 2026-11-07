package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// bin link の追加操作(すべて読み取り専用): find / recent / big / todo / stat / log / show / deps

var linkHashRe = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)
var linkTodoRe = regexp.MustCompile(`\b(TODO|FIXME|HACK|XXX|BUG)\b[:\s(]?(.*)`)

func (r *linkRoot) opFind(a linkArgs) (any, error) {
	q := strings.ToLower(strings.TrimSpace(a.Pattern))
	if q == "" || len(q) > 100 {
		return nil, errors.New("検索語は 1〜100 文字にしてください")
	}
	type hit struct {
		Path  string `json:"path"`
		Size  int64  `json:"size"`
		MTime int64  `json:"mtime"`
	}
	var hits []hit
	total := 0
	r.walk(r.dir, time.Now().Add(8*time.Second), func(f linkFile) bool {
		if strings.Contains(strings.ToLower(f.Path), q) {
			total++
			if len(hits) < 200 {
				hits = append(hits, hit{f.Path, f.Size, f.MTime.Unix()})
			}
		}
		return true
	})
	return map[string]any{"query": a.Pattern, "hits": hits, "total": total}, nil
}

func (r *linkRoot) opRecent(a linkArgs) (any, error) {
	type it struct {
		Path  string `json:"path"`
		Size  int64  `json:"size"`
		MTime int64  `json:"mtime"`
	}
	var all []it
	r.walk(r.dir, time.Now().Add(8*time.Second), func(f linkFile) bool {
		all = append(all, it{f.Path, f.Size, f.MTime.Unix()})
		return true
	})
	by := a.Pattern
	if by == "big" {
		sort.Slice(all, func(i, j int) bool { return all[i].Size > all[j].Size })
	} else {
		by = "recent"
		sort.Slice(all, func(i, j int) bool { return all[i].MTime > all[j].MTime })
	}
	total := len(all)
	if len(all) > 100 {
		all = all[:100]
	}
	return map[string]any{"by": by, "items": all, "total": total}, nil
}

func (r *linkRoot) opTodo(a linkArgs) (any, error) {
	type hit struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	var hits []hit
	total := 0
	r.walk(r.dir, time.Now().Add(8*time.Second), func(f linkFile) bool {
		if f.Size > 512<<10 {
			return true
		}
		txt, err := linkReadText(f.Abs, 512<<10)
		if err != nil {
			return true
		}
		sc := bufio.NewScanner(strings.NewReader(txt))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		n := 0
		for sc.Scan() {
			n++
			m := linkTodoRe.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			total++
			if len(hits) < 200 {
				t, _ := linkRedact(strings.TrimSpace(m[2]))
				if len(t) > 140 {
					t = t[:140]
				}
				hits = append(hits, hit{f.Path, n, m[1], t})
			}
		}
		return true
	})
	return map[string]any{"hits": hits, "total": total}, nil
}

func (r *linkRoot) opStat(a linkArgs) (any, error) {
	abs, rel, err := r.resolve(a.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, errors.New("読み込めません")
	}
	res := map[string]any{"path": rel, "dir": st.IsDir(), "size": st.Size(), "mtime": st.ModTime().Unix(), "mode": st.Mode().String()}
	if !st.IsDir() {
		res["ext"] = strings.ToLower(filepath.Ext(rel))
		if st.Size() <= 20<<20 {
			if data, err := os.ReadFile(abs); err == nil {
				h := sha256.Sum256(data)
				res["sha256"] = hex.EncodeToString(h[:])
				res["lines"] = strings.Count(string(data), "\n") + 1
				res["binary"] = strings.IndexByte(string(data), 0) >= 0
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if out, err := linkGit(ctx, r.dir, "log", "-1", "--format=%h%x09%s%x09%cr%x09%an", "--", rel); err == nil && strings.TrimSpace(out) != "" {
		f := strings.SplitN(strings.TrimSpace(out), "\t", 4)
		if len(f) == 4 {
			s, _ := linkRedact(f[1])
			res["last_commit"] = map[string]string{"hash": f[0], "subject": s, "when": f[2], "author": f[3]}
		}
	}
	return res, nil
}

func (r *linkRoot) opLog(a linkArgs) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	args := []string{"log", "--format=%h%x09%an%x09%ct%x09%s", "-n", "100", "--skip", strconv.Itoa(max(a.From, 0))}
	if a.Path != "" {
		_, rel, err := r.resolve(a.Path)
		if err != nil {
			return nil, err
		}
		args = append(args, "--", rel)
	}
	out, err := linkGit(ctx, r.dir, args...)
	if err != nil {
		return nil, errors.New("git の履歴を読めません")
	}
	type c struct {
		Hash    string `json:"hash"`
		Author  string `json:"author"`
		Time    int64  `json:"time"`
		Subject string `json:"subject"`
	}
	var items []c
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(l, "\t", 4)
		if len(f) != 4 {
			continue
		}
		t, _ := strconv.ParseInt(f[2], 10, 64)
		s, _ := linkRedact(f[3])
		items = append(items, c{f[0], f[1], t, s})
	}
	return map[string]any{"items": items, "skip": max(a.From, 0), "path": a.Path}, nil
}

func (r *linkRoot) opShow(a linkArgs) (any, error) {
	if !linkHashRe.MatchString(a.Pattern) {
		return nil, errors.New("コミットのハッシュが正しくありません")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	meta, err := linkGit(ctx, r.dir, "show", "-s", "--format=%H%x09%an%x09%ct%x09%s%x09%b", a.Pattern)
	if err != nil {
		return nil, errors.New("そのコミットは見つかりません")
	}
	f := strings.SplitN(strings.TrimSpace(meta), "\t", 5)
	if len(f) < 4 {
		return nil, errors.New("コミットを読めません")
	}
	t, _ := strconv.ParseInt(f[2], 10, 64)
	ns, _ := linkGit(ctx, r.dir, "show", "--numstat", "--format=", a.Pattern)
	type fs struct {
		Path string `json:"path"`
		Add  int    `json:"add"`
		Del  int    `json:"del"`
	}
	var files []fs
	for _, l := range strings.Split(strings.TrimSpace(ns), "\n") {
		p := strings.SplitN(l, "\t", 3)
		if len(p) != 3 || linkIsDenied(p[2]) {
			continue
		}
		ad, _ := strconv.Atoi(p[0])
		de, _ := strconv.Atoi(p[1])
		files = append(files, fs{p[2], ad, de})
	}
	if len(files) > 100 {
		files = files[:100]
	}
	subj, _ := linkRedact(f[3])
	body := ""
	if len(f) == 5 {
		body, _ = linkRedact(strings.TrimSpace(f[4]))
		if len(body) > 800 {
			body = body[:800]
		}
	}
	return map[string]any{"hash": f[0], "author": f[1], "time": t, "subject": subj, "body": body, "files": files}, nil
}

func (r *linkRoot) opDeps(a linkArgs) (any, error) {
	type dep struct {
		Name string `json:"name"`
		Ver  string `json:"ver"`
		Kind string `json:"kind"`
	}
	var deps []dep
	var sources []string
	readIn := func(name string) (string, error) {
		abs, _, err := r.resolve(name)
		if err != nil {
			return "", err
		}
		return linkReadText(abs, 1<<20)
	}
	if txt, err := readIn("package.json"); err == nil {
		var pj struct {
			Deps    map[string]string `json:"dependencies"`
			Dev     map[string]string `json:"devDependencies"`
			Peer    map[string]string `json:"peerDependencies"`
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal([]byte(txt), &pj) == nil {
			sources = append(sources, "package.json")
			for k, v := range pj.Deps {
				deps = append(deps, dep{k, v, "dependencies"})
			}
			for k, v := range pj.Dev {
				deps = append(deps, dep{k, v, "dev"})
			}
			for k, v := range pj.Peer {
				deps = append(deps, dep{k, v, "peer"})
			}
		}
	}
	if txt, err := readIn("go.mod"); err == nil {
		sources = append(sources, "go.mod")
		in := false
		for _, l := range strings.Split(txt, "\n") {
			l = strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(l, "require ("):
				in = true
			case l == ")":
				in = false
			case in || strings.HasPrefix(l, "require "):
				p := strings.Fields(strings.TrimPrefix(l, "require "))
				if len(p) >= 2 {
					k := "go"
					if strings.Contains(l, "// indirect") {
						k = "go (indirect)"
					}
					deps = append(deps, dep{p[0], p[1], k})
				}
			}
		}
	}
	if txt, err := readIn("requirements.txt"); err == nil {
		sources = append(sources, "requirements.txt")
		for _, l := range strings.Split(txt, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "-") {
				continue
			}
			if i := strings.IndexAny(l, "=<>~!"); i > 0 {
				deps = append(deps, dep{l[:i], l[i:], "python"})
			} else {
				deps = append(deps, dep{l, "", "python"})
			}
		}
	}
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Kind != deps[j].Kind {
			return deps[i].Kind < deps[j].Kind
		}
		return deps[i].Name < deps[j].Name
	})
	return map[string]any{"deps": deps, "sources": sources}, nil
}
