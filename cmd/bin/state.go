package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// binState は ~/.config/bin/state.json。直前に触った Gist・見かけた ID(短縮 ID の補完用)・エイリアスを持つ
type binState struct {
	Last    string            `json:"last"`
	Seen    []string          `json:"seen"`
	Aliases map[string]string `json:"aliases"`
}

func statePath() string { return filepath.Join(configDir(), "state.json") }

func loadState() binState {
	var s binState
	if data, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.Aliases == nil {
		s.Aliases = map[string]string{}
	}
	return s
}

func saveState(s binState) {
	_ = os.MkdirAll(configDir(), 0o700)
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(statePath(), data, 0o600)
}

const maxSeen = 300

// rememberGist は直前の Gist と、短縮 ID 用の履歴を更新する(失敗しても無視)
func rememberGist(id string) {
	if !gistIDPattern.MatchString(id) {
		return
	}
	s := loadState()
	if s.Last == id && len(s.Seen) > 0 && s.Seen[0] == id {
		return
	}
	s.Last = id
	seen := []string{id}
	for _, x := range s.Seen {
		if x != id {
			seen = append(seen, x)
		}
	}
	if len(seen) > maxSeen {
		seen = seen[:maxSeen]
	}
	s.Seen = seen
	saveState(s)
}

var hexPrefixPattern = regexp.MustCompile(`^[0-9a-f]{4,15}$`)

// resolveIDShortcut はエイリアスと短縮 ID(4桁以上の前方一致)を完全な ID に直す。該当しなければ ok=false
func resolveIDShortcut(s string) (string, bool, error) {
	st := loadState()
	if id, ok := st.Aliases[s]; ok {
		return id, true, nil
	}
	if hexPrefixPattern.MatchString(s) {
		var hits []string
		for _, id := range st.Seen {
			if strings.HasPrefix(id, s) {
				hits = append(hits, id)
			}
		}
		if len(hits) == 1 {
			return hits[0], true, nil
		}
		if len(hits) > 1 {
			return "", false, fmt.Errorf(T("短縮 ID %s は複数の Gist に一致します。桁数を増やしてください"), s)
		}
	}
	return "", false, nil
}

// looksLikeGistRef は引数が「Gist を指すもの」(ID・URL・エイリアス・短縮 ID)かどうか
func looksLikeGistRef(s string) bool {
	if strings.Contains(s, "://") || gistIDPattern.MatchString(s) {
		return true
	}
	id, ok, _ := resolveIDShortcut(s)
	return ok && id != ""
}

// linkedGistID はカレントから親へたどって .lapbin.json の Gist ID を探す(無ければ空)
func linkedGistID() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, syncMetaFile)); err == nil {
			var m syncMeta
			if json.Unmarshal(data, &m) == nil && gistIDPattern.MatchString(m.ID) {
				return m.ID
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

var idFlagValues = map[string]bool{}

func init() {
	for _, m := range []map[string]string{gistValueFlags, patchValueFlags, listValueFlags} {
		for k := range m {
			idFlagValues[k] = true
		}
	}
	for _, k := range []string{"--rev", "--to", "--as", "--lines", "--include", "--exclude"} {
		idFlagValues[k] = true
	}
}

// withDefaultID は先頭の位置引数が Gist を指していないとき、ID を補って返す。
// 補う先は「同期フォルダに紐付いた Gist」→「直前に触った Gist」の順
func withDefaultID(args []string) []string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if idFlagValues[a] {
				i++
			}
			continue
		}
		if looksLikeGistRef(a) {
			return args
		}
		break
	}
	id, from := linkedGistID(), T("このフォルダに紐付いた Gist")
	if id == "" {
		id, from = loadState().Last, T("直前に使った Gist")
	}
	if id == "" {
		return args
	}
	fmt.Fprintf(os.Stderr, "%s\n", dim(fmt.Sprintf(T("対象: %s (%s)"), id, from)))
	return append([]string{id}, args...)
}

// bin alias set <名前> <id> / rm <名前> / list
func cmdAlias(args []string) {
	if len(args) == 0 || args[0] == "list" || args[0] == "ls" {
		st := loadState()
		if len(st.Aliases) == 0 {
			fmt.Println(dim(T("エイリアスはありません(bin alias set <名前> <id> で登録)")))
			return
		}
		names := make([]string, 0, len(st.Aliases))
		for n := range st.Aliases {
			names = append(names, n)
		}
		sort.Strings(names)
		nw := 0
		for _, n := range names {
			if w := displayWidth(n); w > nw {
				nw = w
			}
		}
		fmt.Println(bold(T("エイリアス")) + "  " + dim(fmt.Sprintf(T("%d件"), len(names))))
		fmt.Println()
		for _, n := range names {
			fmt.Printf("  %s  %s  %s\n", cyan(padRight(n, nw)), dim("→"), st.Aliases[n])
		}
		return
	}
	switch args[0] {
	case "set", "add":
		if len(args) != 3 {
			usageExit("bin alias set <name> <id>")
		}
		if gistIDPattern.MatchString(args[1]) || strings.ContainsAny(args[1], "/: ") {
			fail(fmt.Errorf(T("エイリアス名に ID・スペース・/ : は使えません: %s"), args[1]))
		}
		id, err := parseGistID(args[2])
		if err != nil {
			fail(err)
		}
		st := loadState()
		st.Aliases[args[1]] = id
		saveState(st)
		doneCard(T("登録しました"), []cardRow{{T("名前"), args[1]}, {"ID", id}}, "")
	case "rm", "remove", "delete":
		if len(args) != 2 {
			usageExit("bin alias rm <name>")
		}
		st := loadState()
		if _, ok := st.Aliases[args[1]]; !ok {
			fail(fmt.Errorf(T("エイリアス %s はありません"), args[1]))
		}
		delete(st.Aliases, args[1])
		saveState(st)
		fmt.Fprintln(os.Stderr, green("✔")+" "+T("削除しました"))
	default:
		usageExit("bin alias [list | set <name> <id> | rm <name>]")
	}
}
