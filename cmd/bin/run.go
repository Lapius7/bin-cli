package main

// bin run <id> [-f <file>] [-y] [-- <args>...]
//
// Gistのスクリプトを取ってきて実行する。他人が書いたコードをそのまま動かすことになるので、
// 実行前に必ず中身を表示して確認を取る(-y で省略。確認できない環境では -y が必須)。
// Gistの全ファイルを一時フォルダに展開してから実行するので、同じGist内の別ファイルの読み込み
// (Pythonのimport等)も動く。作業ディレクトリは実行した場所のまま。

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// 拡張子ごとの実行方法(先頭の要素がコマンド。候補が複数あるものは見つかった方を使う)
var runners = map[string][][]string{
	".sh":    {{"bash"}, {"sh"}},
	".bash":  {{"bash"}},
	".zsh":   {{"zsh"}},
	".fish":  {{"fish"}},
	".py":    {{"python3"}, {"python"}},
	".js":    {{"node"}},
	".mjs":   {{"node"}},
	".cjs":   {{"node"}},
	".ts":    {{"deno", "run"}, {"bun", "run"}, {"npx", "--yes", "tsx"}},
	".rb":    {{"ruby"}},
	".pl":    {{"perl"}},
	".php":   {{"php"}},
	".lua":   {{"lua"}},
	".go":    {{"go", "run"}},
	".r":     {{"Rscript"}},
	".ps1":   {{"pwsh", "-File"}, {"powershell", "-File"}},
	".java":  {{"java"}},
	".dart":  {{"dart", "run"}},
	".swift": {{"swift"}},
}

func runnerFor(filename, content string) ([]string, error) {
	// shebang があればそれを優先(#!/usr/bin/env python3 等)
	if first, _, _ := strings.Cut(content, "\n"); strings.HasPrefix(first, "#!") && runtime.GOOS != "windows" {
		fields := strings.Fields(strings.TrimPrefix(first, "#!"))
		if len(fields) > 0 {
			if path.Base(fields[0]) == "env" {
				fields = fields[1:]
				if len(fields) > 0 && fields[0] == "-S" {
					fields = fields[1:]
				}
			}
			if len(fields) > 0 {
				if _, err := exec.LookPath(fields[0]); err == nil {
					return fields, nil
				}
			}
		}
	}
	ext := strings.ToLower(path.Ext(filename))
	cands, ok := runners[ext]
	if !ok {
		return nil, fmt.Errorf(T("%s の実行方法が分かりません(対応: .sh .py .js .ts .rb .pl .php .lua .go など、または先頭に #! 行)"), filename)
	}
	for _, c := range cands {
		if _, err := exec.LookPath(c[0]); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf(T("%s を実行するためのコマンド(%s)が見つかりません"), filename, cands[0][0])
}

// pickRunFile は実行するファイルを決める(-f 指定 > 1ファイルだけ > main/index/run.* > 実行方法が分かる最初のファイル)
func pickRunFile(g *Gist, name string) (*GistFile, error) {
	if name != "" {
		for i := range g.Files {
			if g.Files[i].Filename == name {
				return &g.Files[i], nil
			}
		}
		return nil, fmt.Errorf(T("%s というファイルはありません"), name)
	}
	if len(g.Files) == 1 {
		return &g.Files[0], nil
	}
	for _, stem := range []string{"main", "index", "run", "script"} {
		for i := range g.Files {
			base := path.Base(g.Files[i].Filename)
			if strings.TrimSuffix(base, path.Ext(base)) == stem {
				if _, ok := runners[strings.ToLower(path.Ext(base))]; ok {
					return &g.Files[i], nil
				}
			}
		}
	}
	for i := range g.Files {
		if _, ok := runners[strings.ToLower(path.Ext(g.Files[i].Filename))]; ok {
			return &g.Files[i], nil
		}
	}
	names := make([]string, len(g.Files))
	for i, f := range g.Files {
		names[i] = f.Filename
	}
	return nil, fmt.Errorf(T("実行するファイルを -f で指定してください(%s)"), strings.Join(names, ", "))
}

func cmdRun(args []string) {
	// -- 以降はスクリプトへの引数
	var scriptArgs []string
	for i, a := range args {
		if a == "--" {
			scriptArgs = args[i+1:]
			args = args[:i]
			break
		}
	}
	p := parseArgs(args, map[string]string{"-f": "filename", "--file": "filename"}, map[string]string{"-y": "yes", "--yes": "yes"})
	if len(p.positional) != 1 {
		usageExit("bin run <id> [-f <file>] [-y] [-- <args>...]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := optionalSession()
	g, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}
	name, _ := p.value("filename")
	file, err := pickRunFile(g, name)
	if err != nil {
		fail(err)
	}
	runner, err := runnerFor(file.Filename, file.Content)
	if err != nil {
		fail(err)
	}

	owner := "?"
	if g.Owner.Handle != nil {
		owner = "@" + *g.Owner.Handle
	}
	if !p.bools["yes"] {
		if !stdinIsTerminal() {
			fail(errors.New(T("確認を表示できないため中断しました。内容を確認済みなら -y を付けて実行してください")))
		}
		fmt.Fprintf(os.Stderr, "%s %s  %s\n", cyan("──"), bold(file.Filename), dim(owner+" · "+gistURL(cfg, g.ID)))
		for i, line := range strings.Split(strings.TrimRight(file.Content, "\n"), "\n") {
			fmt.Fprintf(os.Stderr, "%s %s\n", dim(fmt.Sprintf("%4d", i+1)), line)
		}
		fmt.Fprintf(os.Stderr, "%s %s\n\n", cyan("──"), dim(strings.Join(append(append([]string{}, runner...), file.Filename), " ")))
		if !confirm(T("このスクリプトを実行しますか?(%s のコードです。内容を確認してください)"), owner) {
			fmt.Fprintln(os.Stderr, T("キャンセルしました。"))
			return
		}
	}

	tmp, err := os.MkdirTemp("", "bin-run-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(tmp)
	for _, f := range g.Files {
		dst, err := safeJoin(tmp, f.Filename)
		if err != nil {
			fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(dst, []byte(f.Content), 0o755); err != nil {
			fail(err)
		}
	}
	script, _ := safeJoin(tmp, file.Filename)
	recordHit(cfg, session, g.ID, "download")

	cmdArgs := append(append(append([]string{}, runner[1:]...), script), scriptArgs...)
	cmd := exec.Command(runner[0], cmdArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.RemoveAll(tmp)
			os.Exit(exit.ExitCode())
		}
		fail(err)
	}
}

// recordHit は閲覧・ダウンロード数の記録(失敗しても無視する)
func recordHit(cfg Config, session *Session, id, kind string) {
	_, _ = rpc(cfg, session, "record_hit", map[string]any{"p_id": id, "p_kind": kind})
}
