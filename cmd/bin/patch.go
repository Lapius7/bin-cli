package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

var patchValueFlags = map[string]string{
	"--insert": "insert", "--after": "after", "--before": "before", "--replace": "replace", "--delete": "delete",
	"--text": "text", "--from": "from",
	"-m": "message", "--message": "message",
}

var patchBoolFlags = map[string]string{
	"--append": "append", "--dry-run": "dry",
}

const openEnd = int(^uint(0) >> 1)

// parseLineRange は "5"・"3-7"・"70-"(末尾まで)・"-20"(先頭から)を (開始, 終了) にする(1始まり、両端を含む)。
// 末尾までの指定は終了が openEnd になる
func parseLineRange(s string) (int, int, error) {
	bad := fmt.Errorf(T("行の指定が正しくありません: %s(例: 5・3-7・70-・-20)"), s)
	a, b, hasEnd := strings.Cut(s, "-")
	start := 1
	if strings.TrimSpace(a) != "" || !hasEnd {
		n, err := strconv.Atoi(strings.TrimSpace(a))
		if err != nil || n < 1 {
			return 0, 0, bad
		}
		start = n
	}
	end := start
	if hasEnd {
		if strings.TrimSpace(b) == "" {
			if strings.TrimSpace(a) == "" {
				return 0, 0, bad
			}
			end = openEnd
		} else {
			n, err := strconv.Atoi(strings.TrimSpace(b))
			if err != nil || n < start {
				return 0, 0, bad
			}
			end = n
		}
	}
	return start, end, nil
}

func splitPatchLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// patchText は追加・置換する本文の行を、--text(複数可)・--from <ファイル>[:行範囲]・標準入力(-)から集める
func patchText(p parsedArgs) []string {
	var out []string
	for _, t := range p.values["text"] {
		out = append(out, splitPatchLines(t)...)
	}
	for _, f := range p.values["from"] {
		path, rng, hasRange := f, "", false
		if i := strings.LastIndex(f, ":"); i > 0 {
			if isRangeSpec(f[i+1:]) {
				path, rng, hasRange = f[:i], f[i+1:], true
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fail(err)
		}
		lines := splitPatchLines(string(data))
		if hasRange {
			a, b, err := resolveRangeSpec(rng, lines)
			if err != nil {
				fail(fmt.Errorf("%s: %w", path, err))
			}
			lines = lines[a-1 : b]
		}
		out = append(out, lines...)
	}
	if len(p.positional) > 1 && p.positional[len(p.positional)-1] == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fail(err)
		}
		out = append(out, splitPatchLines(string(data))...)
	}
	return out
}

// cmdPatch は Gist の1ファイルに対して、行単位で追加・置換・削除する
func cmdPatch(args []string) {
	p := parseArgs(args, patchValueFlags, patchBoolFlags)
	usage := "bin patch <id> <file> (--append | --insert <行> | --after <文字列> | --before <文字列> | --replace <行[-行]> | --delete <行[-行]>) [--text <文字列>]... [--from <ローカルファイル>[:行[-行]]] [-] [-m <message>] [--dry-run]"
	if len(p.positional) < 2 {
		usageExit(usage)
	}
	ops := 0
	for _, k := range []string{"insert", "replace", "delete", "after", "before"} {
		if len(p.values[k]) > 0 {
			ops++
		}
	}
	if p.bools["append"] {
		ops++
	}
	if ops != 1 {
		fail(fmt.Errorf(T("操作は --append・--insert・--after・--before・--replace・--delete のうち1つだけ指定してください")))
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	name := p.positional[1]
	cfg, session := requireSession()
	g, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}
	if g.Owner.ID != userIDFromToken(session.AccessToken) {
		fail(fmt.Errorf(T("自分のGistではないため編集できません")))
	}
	files := g.Files
	idx := -1
	for i := range files {
		if files[i].Filename == name {
			idx = i
		}
	}
	creating := false
	if idx < 0 {
		// 無いファイルは、追加系(--append・--insert 1)のときだけ新規作成する
		if p.bools["append"] || (len(p.values["insert"]) > 0 && p.values["insert"][0] == "1") {
			creating = true
			files = append(files, GistFile{Filename: name})
			idx = len(files) - 1
			if err := validateGistPath(name); err != nil {
				fail(err)
			}
		} else {
			fail(fmt.Errorf(T("%s というファイルはありません"), name))
		}
	}
	lines := splitPatchLines(files[idx].Content)
	total := len(lines)
	text := patchText(p)

	var result []string
	var summary string
	switch {
	case p.bools["append"]:
		if len(text) == 0 {
			fail(fmt.Errorf(T("追加する内容がありません(--text・--from・標準入力 `-` のどれかを指定してください)")))
		}
		result = append(append(result, lines...), text...)
		summary = fmt.Sprintf(T("末尾に %d 行追加"), len(text))
	case len(p.values["insert"]) > 0:
		n, err := strconv.Atoi(p.values["insert"][0])
		if err != nil || n < 1 || n > total+1 {
			fail(fmt.Errorf(T("--insert は 1〜%d の行番号で指定してください(その行の手前に入ります)"), total+1))
		}
		if len(text) == 0 {
			fail(fmt.Errorf(T("追加する内容がありません(--text・--from・標準入力 `-` のどれかを指定してください)")))
		}
		result = append(result, lines[:n-1]...)
		result = append(result, text...)
		result = append(result, lines[n-1:]...)
		summary = fmt.Sprintf(T("%d 行目の手前に %d 行追加"), n, len(text))
	case len(p.values["after"]) > 0, len(p.values["before"]) > 0:
		key, after := "before", false
		if len(p.values["after"]) > 0 {
			key, after = "after", true
		}
		pat := p.values[key][0]
		at := -1
		for i, l := range lines {
			if strings.Contains(l, pat) {
				at = i
				break
			}
		}
		if at < 0 {
			fail(fmt.Errorf(T("%s に「%s」を含む行がありません"), name, pat))
		}
		if len(text) == 0 {
			fail(fmt.Errorf(T("追加する内容がありません(--text・--from・標準入力 `-` のどれかを指定してください)")))
		}
		pos := at
		if after {
			pos = at + 1
		}
		result = append(result, lines[:pos]...)
		result = append(result, text...)
		result = append(result, lines[pos:]...)
		summary = fmt.Sprintf(T("%d 行目の手前に %d 行追加"), pos+1, len(text))
	case len(p.values["replace"]) > 0, len(p.values["delete"]) > 0:
		spec, isDelete := "", len(p.values["delete"]) > 0
		if isDelete {
			spec = p.values["delete"][0]
		} else {
			spec = p.values["replace"][0]
		}
		a, b, err := resolveRangeSpec(spec, lines)
		if err != nil {
			fail(fmt.Errorf("%s: %w", name, err))
		}
		if !isDelete && len(text) == 0 {
			fail(fmt.Errorf(T("置き換える内容がありません(--text・--from・標準入力 `-` のどれかを指定してください)")))
		}
		result = append(result, lines[:a-1]...)
		if !isDelete {
			result = append(result, text...)
		}
		result = append(result, lines[b:]...)
		if isDelete {
			if a == b {
				summary = fmt.Sprintf(T("%d 行目を削除"), a)
			} else {
				summary = fmt.Sprintf(T("%d〜%d 行目を削除"), a, b)
			}
		} else {
			summary = fmt.Sprintf(T("%d〜%d 行目を %d 行に置換"), a, b, len(text))
			if a == b {
				summary = fmt.Sprintf(T("%d 行目を %d 行に置換"), a, len(text))
			}
		}
	}

	newContent := strings.Join(result, "\n")
	if len(result) > 0 {
		newContent += "\n"
	}
	if newContent == files[idx].Content {
		fail(fmt.Errorf(T("変更内容がありません")))
	}
	if creating {
		summary = T("新規作成: ") + summary
	}
	if p.bools["dry"] {
		fmt.Fprintf(os.Stderr, "%s %s\n  %s  %s\n  %s  %d → %d\n", yellow("◌"), bold(T("プレビュー(保存しません)")), dim(T("ファイル")), name, dim(T("行数")), total, len(result))
		fmt.Fprintf(os.Stderr, "  %s  %s\n", dim(T("変更")), summary)
		next := append([]GistFile(nil), files...)
		next[idx].Content = newContent
		showChanges(files, next)
		printPendingChanges()
		return
	}
	prevFiles := append([]GistFile(nil), files...)
	files[idx].Content = newContent
	message, _ := p.value("message")
	if message == "" {
		message = name + ": " + summary
	}
	if _, err := saveGist(cfg, session, &g.ID, g.Title, g.Description, g.Visibility, files, message); err != nil {
		fail(err)
	}
	showChanges(prevFiles, files)
	title := T("更新しました")
	if creating {
		title = T("作成しました")
	}
	doneCard(title, []cardRow{
		{T("ファイル"), name},
		{T("変更"), summary},
		{T("行数"), fmt.Sprintf("%d → %d", total, len(result))},
	}, gistURL(cfg, g.ID))
}
