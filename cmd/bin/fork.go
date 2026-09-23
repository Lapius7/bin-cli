package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// forkGist はフォークする。overrides に p_title / p_description / p_visibility を入れるとその値で作る(無ければ元と同じ)
func forkGist(cfg Config, session *Session, id string, overrides map[string]any) (string, error) {
	args := map[string]any{"p_id": id}
	for k, v := range overrides {
		args[k] = v
	}
	body, err := rpc(cfg, session, "fork_gist", args)
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
// 公開範囲は省略すると元のまま(DB側のfork_gistが引き継ぐ)。-t/-d/公開範囲はフォークと同時に反映する
// (変更履歴は「フォーク」の1件だけ)。
// createと同じく、標準出力には新しいGistのURLだけを出す
func cmdFork(args []string) {
	p := parseArgs(args, map[string]string{
		"-t": "title", "--title": "title",
		"-d": "description", "--description": "description",
	}, visibilityFlags)
	if len(p.positional) != 1 {
		usageExit("bin fork <id> [-t <title>] [-d <description>] [--public|--unlisted|--private]")
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
	overrides := map[string]any{}
	visibility := src.Visibility
	if v, ok := p.value("title"); ok {
		overrides["p_title"] = v
	}
	if v, ok := p.value("description"); ok {
		overrides["p_description"] = v
	}
	if v, ok := pickVisibility(p); ok {
		overrides["p_visibility"] = v
		visibility = v
	}
	newID, err := forkGist(cfg, session, src.ID, overrides)
	if err != nil {
		fail(err)
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
