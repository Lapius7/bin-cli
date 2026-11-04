package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ---- 通信中のスピナー(stderrが端末の時だけ。速く終わる通信では何も出さない) ----

func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

var spinnerMu sync.Mutex

// withSpinner は fn の実行中、少し待っても終わらなければ stderr にスピナーを出す
func withSpinner[T any](label string, fn func() (T, error)) (T, error) {
	if !stderrIsTerminal() {
		return fn()
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-done:
			return
		case <-time.After(250 * time.Millisecond):
		}
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		fmt.Fprint(os.Stderr, "\x1b[?25l")
		for i := 0; ; i++ {
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\x1b[2K\x1b[?25h")
				return
			case <-time.After(80 * time.Millisecond):
				fmt.Fprintf(os.Stderr, "\r%s %s", cyan(frames[i%len(frames)]), dim(label))
			}
		}
	}()
	v, err := fn()
	close(done)
	<-stopped
	return v, err
}

// ---- 相対時刻 ----

func relTime(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return iso
	}
	d := time.Since(t)
	switch {
	case d < 0, d < time.Minute:
		return T("たった今")
	case d < time.Hour:
		return fmt.Sprintf(T("%d分前"), int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf(T("%d時間前"), int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf(T("%d日前"), int(d.Hours()/24))
	}
	return t.Local().Format("2006-01-02")
}

// ---- 結果カード ----

type cardRow struct{ label, value string }

// doneCard は処理結果を stderr に整えて出す(✔ タイトル、その下に項目)。URLだけは標準出力へ
func doneCard(title string, rows []cardRow, url string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", green("✔"), bold(title))
	w := 0
	for _, r := range rows {
		if n := displayWidth(r.label); n > w {
			w = n
		}
	}
	for _, r := range rows {
		fmt.Fprintf(os.Stderr, "  %s  %s\n", dim(padLabel(r.label, w)), r.value)
	}
	printPendingChanges()
	if url != "" {
		if stdoutIsTerminal() {
			fmt.Fprintf(os.Stderr, "  %s  %s\n", dim(padLabel("URL", w)), hyperlink(url, cyan(url)))
		} else {
			fmt.Println(url)
		}
	}
}

// hyperlink は対応する端末ではクリックできるリンクにする(OSC 8)
func hyperlink(url, text string) string {
	if !colorEnabled {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func filesLabel(n int) string {
	return fmt.Sprintf(Tn("%dファイル", n), n)
}

// pendingChanges は次の doneCard が結果カードの下に git 風の差分として表示する
var pendingChanges []fileChange

// showChanges は保存前後のファイル一覧の差分を、次の doneCard で表示するよう登録する
func showChanges(prev, next []GistFile) { pendingChanges = changesBetween(prev, next) }

var maxDiffLines = 16

func printPendingChanges() {
	cs := pendingChanges
	pendingChanges = nil
	if len(cs) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr)
	totalAdd, totalDel := 0, 0
	for _, c := range cs {
		totalAdd += c.add
		totalDel += c.del
		mark := yellow("M")
		switch c.status {
		case T("追加"):
			mark = green("A")
		case T("削除"):
			mark = red("D")
		}
		fmt.Fprintln(os.Stderr, rightAlign("  "+mark+" "+bold(c.name), green(fmt.Sprintf("+%d", c.add))+" "+red(fmt.Sprintf("-%d", c.del))))
		shown := 0
		skipped := 0
		for _, op := range c.ops {
			if op.kind == ' ' {
				continue
			}
			if shown >= maxDiffLines {
				skipped++
				continue
			}
			shown++
			text := op.text
			if displayWidth(text) > 100 {
				text = string([]rune(text)[:50]) + "…"
			}
			if op.kind == '+' {
				fmt.Fprintf(os.Stderr, "    %s\n", green("+ "+text))
			} else {
				fmt.Fprintf(os.Stderr, "    %s\n", red("- "+text))
			}
		}
		if skipped > 0 {
			fmt.Fprintf(os.Stderr, "    %s\n", dim(fmt.Sprintf(T("…他 %d 行"), skipped)))
		}
	}
	fmt.Fprintf(os.Stderr, "  %s\n\n", dim(fmt.Sprintf(T("%d ファイル変更、%d 行追加(+)、%d 行削除(-)"), len(cs), totalAdd, totalDel)))
}
