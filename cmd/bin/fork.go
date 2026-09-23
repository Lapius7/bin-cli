package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func forkGist(cfg Config, session *Session, id string) (string, error) {
	body, err := rpc(cfg, session, "fork_gist", map[string]any{"p_id": id})
	if err != nil {
		return "", describeDBError(err)
	}
	var newID string
	if err := json.Unmarshal(body, &newID); err != nil || newID == "" {
		return "", fmt.Errorf(T("レスポンスの解析に失敗しました: %s"), string(body))
	}
	return newID, nil
}

// cmdFork はGistをコピーして自分の新しいGistを作る。自分のGistもフォークでき、元を残したまま派生版を作れる。
// 公開範囲は元のまま(DB側のfork_gistが引き継ぐ)。-t/-d/公開範囲を指定した時は、フォーク直後に1回だけ更新する
// (変更履歴には「フォーク」と「その変更」の2件が残る)。
// createと同じく、標準出力には新しいGistのURLだけを出す
func cmdFork(args []string) {
	p := parseArgs(args, map[string]string{
		"-t": "title", "--title": "title",
		"-d": "description", "--description": "description",
		"-m": "message", "--message": "message",
	}, visibilityFlags)
	if len(p.positional) != 1 {
		usageExit("bin fork <id> [-t <title>] [-d <description>] [-m <message>] [--public|--unlisted|--private]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := requireSession()
	src, err := getGist(cfg, session, id)
	if err != nil {
		fail(err)
	}
	newID, err := forkGist(cfg, session, src.ID)
	if err != nil {
		fail(err)
	}

	title, description, visibility := src.Title, src.Description, src.Visibility
	if v, ok := p.value("title"); ok {
		title = v
	}
	if v, ok := p.value("description"); ok {
		description = v
	}
	if v, ok := pickVisibility(p); ok {
		visibility = v
	}
	if title != src.Title || description != src.Description || visibility != src.Visibility {
		message, _ := p.value("message")
		if _, err := saveGist(cfg, session, &newID, title, description, visibility, src.Files, message); err != nil {
			// フォーク自体はできているので、URLは出してから失敗を伝える
			fmt.Println(gistURL(cfg, newID))
			fail(fmt.Errorf(T("フォークはできましたが、タイトル等の変更に失敗しました: %v"), err))
		}
	}

	names := make([]string, len(src.Files))
	for i, f := range src.Files {
		names[i] = f.Filename
	}
	fmt.Fprintf(os.Stderr, Tn("%s 「%s」をフォークしました(%s・%dファイル)\n", len(src.Files)), green("✓"), gistTitle(src.Title, names), visibilityLabel(visibility), len(src.Files))
	if src.Owner.ID == userIDFromToken(session.AccessToken) {
		fmt.Fprintln(os.Stderr, dim("  "+T("元のGistはそのまま残ります。")))
	}
	fmt.Fprintf(os.Stderr, "  %s %s\n", dim(T("手元で編集:")), cyan("bin clone "+newID))
	fmt.Println(gistURL(cfg, newID))
}
