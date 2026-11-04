package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Gistのファイル名は「src/lib/util.go」のような/区切りの相対パス(サーバー側の規則と同じ:
// 255文字以内・10階層まで・空の階層/./..不可・先頭と末尾の/不可・\不可)。

const maxFileBytes = 1024 * 1024

// ディレクトリを読み込む時に丸ごと飛ばすもの(Web版と同じ一覧)
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true, "dist": true, "build": true,
	"__pycache__": true, ".venv": true, "venv": true, "target": true, ".idea": true, ".vscode": true, gitStateDir: true,
}

func validateGistPath(p string) error {
	if p == "" || len(p) > 255 {
		return fmt.Errorf(T("ファイル名は1〜255文字にしてください: %q"), p)
	}
	if strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return fmt.Errorf(T("ファイル名が不正です: %q"), p)
	}
	segs := strings.Split(p, "/")
	if len(segs) > 10 {
		return fmt.Errorf(T("フォルダは10階層までです: %q"), p)
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return fmt.Errorf(T("ファイル名が不正です(空のフォルダ名・.・..は使えません): %q"), p)
		}
	}
	return nil
}

// gistPathFor はローカルのパスからGist上のパスを決める。
// カレントディレクトリ配下の相対パス(src/main.go)はそのまま使い、絶対パスや ../ で始まる
// パスはファイル名だけにする(手元のディレクトリ構成が意図せず載らないように)。
func gistPathFor(local string) string {
	p := filepath.ToSlash(filepath.Clean(local))
	if filepath.IsAbs(local) || p == ".." || strings.HasPrefix(p, "../") {
		return path.Base(p)
	}
	return p
}

var localRangePattern = regexp.MustCompile(`^(.+):(\d+-\d*|-\d+|\d+)(?:=(.+))?$`)

type skipped struct{ path, reason string }

func readLocalFile(localPath, gistPath string, out *[]GistFile, skips *[]skipped) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		fail(fmt.Errorf(T("ファイルを読み込めません: %w"), err))
	}
	if len(data) > maxFileBytes {
		*skips = append(*skips, skipped{gistPath, T("1MB超")})
		return
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		*skips = append(*skips, skipped{gistPath, T("バイナリ")})
		return
	}
	*out = append(*out, GistFile{Filename: gistPath, Content: string(data)})
}

// collectPath はファイルならそれ1つ、ディレクトリなら中身を再帰的に集める。
// ディレクトリ「src」を渡すと src/main.go のようにディレクトリ名から始まるパスになる
// (「.」を渡した場合はカレントディレクトリ直下からの相対パス)。
func collectPath(local string, out *[]GistFile, skips *[]skipped) {
	// ローカルの行範囲指定: main.go:70-84 / main.go:70- / main.go:-20 / main.go:5 、末尾に =名前 でGist上の名前を指定
	if m := localRangePattern.FindStringSubmatch(local); m != nil {
		if _, err := os.Stat(local); err != nil {
			a, b, err := parseLineRange(m[2])
			if err != nil {
				fail(err)
			}
			data, err := os.ReadFile(m[1])
			if err != nil {
				fail(fmt.Errorf(T("ファイルを読み込めません: %w"), err))
			}
			lines := splitPatchLines(string(data))
			if b == openEnd {
				b = len(lines)
			}
			if a > len(lines) || b > len(lines) {
				fail(fmt.Errorf(T("%s は %d 行しかありません(指定: %s)"), m[1], len(lines), m[2]))
			}
			name := m[3]
			if name == "" {
				name = gistPathFor(m[1])
			}
			content := strings.Join(lines[a-1:b], "\n") + "\n"
			*out = append(*out, GistFile{Filename: name, Content: content})
			return
		}
	}
	info, err := os.Stat(local)
	if err != nil {
		fail(fmt.Errorf(T("ファイルを読み込めません: %w"), err))
	}
	if !info.IsDir() {
		readLocalFile(local, gistPathFor(local), out, skips)
		return
	}
	base := gistPathFor(local)
	if base == "." {
		base = ""
	}
	_ = filepath.WalkDir(local, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != local && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // シンボリックリンク等は辿らない
		}
		rel, _ := filepath.Rel(local, p)
		gp := filepath.ToSlash(rel)
		if base != "" {
			gp = base + "/" + gp
		}
		readLocalFile(p, gp, out, skips)
		return nil
	})
}

// readInputFiles は位置引数のファイル/ディレクトリと標準入力を読み込む。
// 標準入力は「-」を指定した時か、implicitStdin=trueで位置引数が無くパイプされている時だけ読む。
// (editでは暗黙に読まない。スクリプトやSSH経由など標準入力が端末でない環境で
// `bin edit <id> -t 新タイトル` を実行すると、空の標準入力がstdin.txtとして追加されてしまっていた)
func readInputFiles(paths []string, stdinName string, allowEmpty, implicitStdin bool) []GistFile {
	var files []GistFile
	var skips []skipped
	for _, p := range paths {
		if p == "-" {
			files = append(files, readStdinFile(stdinName))
			continue
		}
		collectPath(p, &files, &skips)
	}
	if len(paths) == 0 && implicitStdin && stdinIsPiped() {
		f := readStdinFile(stdinName)
		if f.Content == "" && !allowEmpty {
			fail(fmt.Errorf(T("標準入力が空です。ファイルを指定するか、内容をパイプで渡してください")))
		}
		if f.Content != "" {
			files = append(files, f)
		}
	}
	if len(skips) > 0 {
		for i, s := range skips {
			if i == 5 {
				warnErr(T("  …他%d件"), len(skips)-5)
				break
			}
			warnErr(T("%s を除外しました(%s)"), s.path, s.reason)
		}
	}
	for _, f := range files {
		if err := validateGistPath(f.Filename); err != nil {
			fail(err)
		}
	}
	if len(files) > 300 {
		fail(fmt.Errorf(T("ファイルは1つのGistにつき300個までです(%d個あります)"), len(files)))
	}
	if len(files) == 0 && !allowEmpty {
		fail(fmt.Errorf(T("ファイルを指定するか、標準入力から渡してください(例: cat main.go | bin create -f main.go)")))
	}
	return files
}

// safeJoin はGist上のパスを保存先ディレクトリ配下のパスに変換する。
// サーバー側でも..等は禁止しているが、ディレクトリ外に書き込まないようここでも確かめる。
func safeJoin(dir, gistPath string) (string, error) {
	if err := validateGistPath(gistPath); err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.FromSlash(gistPath))
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(T("保存先ディレクトリの外には書き込めません: %s"), gistPath)
	}
	return p, nil
}

func warnErr(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s %s\n", yellow("!"), fmt.Sprintf(format, args...))
}

// applyFileOpts は --lines <A-B> と --as <名前>(-f)を、ファイルが1つだけのときに
// 「file:A-B=名前」の形へ変換する。ファイルが複数のときは曖昧なのでエラーにする
func applyFileOpts(p parsedArgs, paths []string) []string {
	lines, _ := p.value("lines")
	name, _ := p.value("filename")
	var srcs []int
	for i, x := range paths {
		if x != "-" {
			srcs = append(srcs, i)
		}
	}
	if lines == "" && (name == "" || len(srcs) == 0) {
		return paths
	}
	if len(srcs) != 1 {
		if lines != "" {
			fail(fmt.Errorf(T("--lines はファイルが1つのときだけ使えます(複数のときは file:A-B の形で指定)")))
		}
		return paths
	}
	out := append([]string(nil), paths...)
	i := srcs[0]
	if lines != "" {
		if _, _, err := parseLineRange(lines); err != nil {
			fail(err)
		}
		out[i] += ":" + lines
	}
	if name != "" {
		if info, err := os.Stat(paths[i]); err == nil && info.IsDir() {
			return paths
		}
		if lines == "" {
			out[i] += ":1-" // 全体を名前付きで取り込む
		}
		out[i] += "=" + name
	}
	return out
}

// foldFileOpts は「ファイルの直後に置いた --lines / --as」をそのファイルだけに適用し、
// file:A-B=名前 の形に畳み込んだ引数列を返す。ファイルが複数でもファイルごとに指定できる。
// skip は先頭から数える「ファイルではない位置引数」の数(editのID=1)
func foldFileOpts(args []string, skip int) []string {
	type ent struct{ val, lines, as string }
	var out []string // 位置引数は "\x00<index>" の目印で置く
	var pos []*ent
	last := -1
	seen := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasEq := a, "", false
		if k := strings.Index(a, "="); strings.HasPrefix(a, "--") && k > 0 {
			name, val, hasEq = a[:k], a[k+1:], true
		}
		if name == "--lines" || name == "--as" {
			if !hasEq {
				if i+1 >= len(args) {
					fail(fmt.Errorf(T("%s には値が必要です"), name))
				}
				i++
				val = args[i]
			}
			if last < 0 {
				// 直前にファイルが無い場合は従来どおり全体オプションとして残す
				out = append(out, name, val)
				continue
			}
			if name == "--lines" {
				pos[last].lines = val
			} else {
				pos[last].as = val
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			out = append(out, a)
			if _, ok := gistValueFlags[a]; ok && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		seen++
		if seen <= skip || a == "-" {
			out = append(out, a)
			last = -1
			continue
		}
		pos = append(pos, &ent{val: a})
		last = len(pos) - 1
		out = append(out, fmt.Sprintf("\x00%d", last))
	}
	for j, o := range out {
		if strings.HasPrefix(o, "\x00") {
			n := 0
			fmt.Sscanf(o[1:], "%d", &n)
			e := pos[n]
			s := e.val
			if e.lines != "" {
				if _, _, err := parseLineRange(e.lines); err != nil {
					fail(err)
				}
				s += ":" + e.lines
			}
			if e.as != "" {
				if e.lines == "" {
					if info, err := os.Stat(e.val); err == nil && info.IsDir() {
						fail(fmt.Errorf(T("--as はフォルダには使えません: %s"), e.val))
					}
					s += ":1-"
				}
				s += "=" + e.as
			}
			out[j] = s
		}
	}
	return out
}
