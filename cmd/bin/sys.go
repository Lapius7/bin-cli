package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type cmdSpec struct {
	name string
	args []string
}

func firstAvailable(cands []cmdSpec) (cmdSpec, bool) {
	for _, c := range cands {
		if _, err := exec.LookPath(c.name); err == nil {
			return c, true
		}
	}
	return cmdSpec{}, false
}

func clipboardWrite(text string) error {
	c, ok := firstAvailable([]cmdSpec{
		{"wl-copy", nil}, {"xclip", []string{"-selection", "clipboard"}}, {"xsel", []string{"-ib"}},
		{"pbcopy", nil}, {"clip.exe", nil}, {"powershell.exe", []string{"-NoProfile", "-Command", "Set-Clipboard -Value ([Console]::In.ReadToEnd())"}},
	})
	if !ok {
		return errors.New(T("クリップボードのコマンドが見つかりません(wl-copy / xclip / xsel / pbcopy / clip.exe)"))
	}
	cmd := exec.Command(c.name, c.args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func clipboardRead() (string, error) {
	c, ok := firstAvailable([]cmdSpec{
		{"wl-paste", []string{"-n"}}, {"xclip", []string{"-selection", "clipboard", "-o"}}, {"xsel", []string{"-ob"}},
		{"pbpaste", nil}, {"powershell.exe", []string{"-NoProfile", "-Command", "Get-Clipboard -Raw"}},
	})
	if !ok {
		return "", errors.New(T("クリップボードのコマンドが見つかりません(wl-paste / xclip / xsel / pbpaste / powershell.exe)"))
	}
	out, err := exec.Command(c.name, c.args...).Output()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n"), nil
}

func openBrowser(url string) error {
	cands := []cmdSpec{{"wslview", nil}, {"xdg-open", nil}, {"open", nil}, {"cmd.exe", []string{"/c", "start", ""}}}
	if runtime.GOOS == "windows" {
		cands = []cmdSpec{{"rundll32", []string{"url.dll,FileProtocolHandler"}}}
	}
	c, ok := firstAvailable(cands)
	if !ok {
		return errors.New(T("ブラウザを開くコマンドが見つかりません(URLを手動で開いてください)"))
	}
	return exec.Command(c.name, append(c.args, url)...).Start()
}

// editorCommand は $VISUAL → $EDITOR → 一般的なエディタの順で選ぶ
func editorCommand() []string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := strings.Fields(os.Getenv(k)); len(v) > 0 {
			return v
		}
	}
	for _, n := range []string{"nano", "vim", "vi", "notepad"} {
		if _, err := exec.LookPath(n); err == nil {
			return []string{n}
		}
	}
	return nil
}
