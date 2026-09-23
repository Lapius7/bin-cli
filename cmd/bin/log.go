package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Revision struct {
	Revision  int        `json:"revision"`
	CreatedAt string     `json:"created_at"`
	Message   string     `json:"message"`
	Files     []GistFile `json:"files"`
}

func getRevisions(cfg Config, session *Session, id string) ([]Revision, error) {
	body, err := rpc(cfg, session, "get_revisions", map[string]any{"p_id": id})
	if err != nil {
		return nil, describeDBError(err)
	}
	var revs []Revision
	if err := json.Unmarshal(body, &revs); err != nil {
		return nil, fmt.Errorf("レスポンスの解析に失敗しました: %w", err)
	}
	return revs, nil
}

// revisionHash はWeb版(lib/revisions.ts)と同じFNV-1aで、Gist IDとリビジョン番号から
// コミットハッシュ風の7桁を作る(両方で同じ値が表示される)。
func revisionHash(gistID string, revision int) string {
	h := uint32(0x811c9dc5)
	for _, c := range fmt.Sprintf("%s:%d", gistID, revision) {
		// JSのcharCodeAtと揃えるためUTF-16単位で扱う(IDとリビジョン番号はASCIIのみなので実質同じ)
		h ^= uint32(c)
		h *= 0x01000193
	}
	return fmt.Sprintf("%08x", h)[:7]
}

type lineOp struct {
	kind byte // '+', '-', ' '
	text string
}

// diffLines はMyersのアルゴリズムで行単位の差分を求める。
func diffLines(a, b []string) []lineOp {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}
	// Myersは差分が大きいとtraceのメモリがO(d×(n+m))で膨らむので、巨大な差分は
	// 「全行削除+全行追加」として扱う(変更行数の表示には十分)
	const maxEdits = 3000
	if max > 40000 {
		return replaceAll(a, b)
	}
	v := make([]int, 2*max+2)
	var trace [][]int
	for d := 0; d <= max; d++ {
		if d > maxEdits {
			return replaceAll(a, b)
		}
		vc := make([]int, len(v))
		copy(vc, v)
		trace = append(trace, vc)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[max+k-1] < v[max+k+1]) {
				x = v[max+k+1]
			} else {
				x = v[max+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[max+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b, max, d)
			}
		}
	}
	return nil
}

func replaceAll(a, b []string) []lineOp {
	ops := make([]lineOp, 0, len(a)+len(b))
	for _, s := range a {
		ops = append(ops, lineOp{'-', s})
	}
	for _, s := range b {
		ops = append(ops, lineOp{'+', s})
	}
	return ops
}

func backtrack(trace [][]int, a, b []string, max, d int) []lineOp {
	x, y := len(a), len(b)
	var ops []lineOp
	for ; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[max+k-1] < v[max+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[max+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			ops = append(ops, lineOp{' ', a[x]})
		}
		if x == prevX {
			y--
			ops = append(ops, lineOp{'+', b[y]})
		} else {
			x--
			ops = append(ops, lineOp{'-', a[x]})
		}
	}
	for x > 0 && y > 0 {
		x--
		y--
		ops = append(ops, lineOp{' ', a[x]})
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

type fileChange struct {
	name     string
	status   string // 追加/変更/削除
	ops      []lineOp
	add, del int
}

func changesBetween(prev, next []GistFile) []fileChange {
	before := map[string]string{}
	for _, f := range prev {
		before[f.Filename] = f.Content
	}
	after := map[string]bool{}
	var out []fileChange
	for _, f := range next {
		after[f.Filename] = true
		old, existed := before[f.Filename]
		if existed && old == f.Content {
			continue
		}
		status := "変更"
		if !existed {
			status = "追加"
		}
		out = append(out, makeChange(f.Filename, status, old, f.Content))
	}
	for _, f := range prev {
		if !after[f.Filename] {
			out = append(out, makeChange(f.Filename, "削除", f.Content, ""))
		}
	}
	return out
}

func makeChange(name, status, before, after string) fileChange {
	ops := diffLines(splitLines(before), splitLines(after))
	c := fileChange{name: name, status: status, ops: ops}
	for _, op := range ops {
		switch op.kind {
		case '+':
			c.add++
		case '-':
			c.del++
		}
	}
	return c
}

func cmdLog(args []string) {
	p := parseArgs(args, nil, map[string]string{"-p": "patch", "--patch": "patch"})
	if len(p.positional) != 1 {
		usageExit("bin log <id> [-p]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := optionalSession()
	revs, err := getRevisions(cfg, session, id)
	if err != nil {
		fail(err)
	}
	if len(revs) == 0 {
		fail(fmt.Errorf("Gistが見つかりません(存在しないか、非公開です)"))
	}

	lastDay := ""
	for i, r := range revs {
		var prev []GistFile
		if i+1 < len(revs) {
			prev = revs[i+1].Files
		}
		changes := changesBetween(prev, r.Files)
		add, del := 0, 0
		for _, c := range changes {
			add += c.add
			del += c.del
		}

		t, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		t = t.Local()
		if day := t.Format("2006年1月2日"); day != lastDay {
			if lastDay != "" {
				fmt.Println()
			}
			fmt.Println(dim("── " + day + "のコミット"))
			lastDay = day
		}

		msg := r.Message
		if msg == "" {
			switch {
			case i == len(revs)-1:
				msg = "Gistを作成"
			case len(changes) == 1:
				msg = changes[0].name + " を" + changes[0].status
			default:
				msg = fmt.Sprintf("%dファイルを変更", len(changes))
			}
			msg = dim(msg)
		}
		latest := ""
		if i == 0 {
			latest = " " + cyan("(最新)")
		}
		fmt.Printf("%s %s %s%s  %s %s  %s\n", yellow("●"), yellow(revisionHash(id, r.Revision)), msg, latest,
			green(fmt.Sprintf("+%d", add)), red(fmt.Sprintf("-%d", del)), dim(t.Format("15:04")))

		if p.bools["patch"] {
			for _, c := range changes {
				printPatch(c)
			}
			fmt.Println()
		} else if len(changes) > 0 && len(changes) <= 5 {
			for _, c := range changes {
				fmt.Printf("  %s %s %s\n", dim("│"), dim(c.status), c.name)
			}
		}
	}
}

// printPatch は変更箇所の前後3行だけを表示する(git diffのハンク相当)。
func printPatch(c fileChange) {
	fmt.Printf("  %s %s %s\n", bold("───"), bold(c.name), dim("("+c.status+")"))
	const ctx = 3
	keep := make([]bool, len(c.ops))
	for i, op := range c.ops {
		if op.kind == ' ' {
			continue
		}
		for j := i - ctx; j <= i+ctx; j++ {
			if j >= 0 && j < len(c.ops) {
				keep[j] = true
			}
		}
	}
	gap := false
	for i, op := range c.ops {
		if !keep[i] {
			if !gap {
				fmt.Println(dim("  ⋯"))
				gap = true
			}
			continue
		}
		gap = false
		switch op.kind {
		case '+':
			fmt.Println(green("  +" + op.text))
		case '-':
			fmt.Println(red("  -" + op.text))
		default:
			fmt.Println(dim("   " + op.text))
		}
	}
}
