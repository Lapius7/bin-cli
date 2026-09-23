package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

type GistFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type GistOwner struct {
	ID          string  `json:"id"`
	Handle      *string `json:"handle"`
	DisplayName *string `json:"display_name"`
}

type Gist struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Visibility    string     `json:"visibility"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
	Owner         GistOwner  `json:"owner"`
	RevisionCount int        `json:"revision_count"`
	Files         []GistFile `json:"files"`
}

type GistSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	UpdatedAt   string    `json:"updated_at"`
	Owner       GistOwner `json:"owner"`
	Files       []struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	} `json:"files"`
}

var gistIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// parseGistID は「ID」「https://bin.lapius7.com/<id>」「.../raw/<id>/...」のどれでも受け付ける。
func parseGistID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		parts := strings.Split(s, "/")
		for _, p := range parts[1:] {
			if gistIDPattern.MatchString(p) {
				return p, nil
			}
		}
		return "", fmt.Errorf("URLからGistのIDを読み取れません: %s", s)
	}
	if !gistIDPattern.MatchString(s) {
		return "", fmt.Errorf("GistのIDは16桁の英数字です: %s", s)
	}
	return s, nil
}

// rpc はbinスキーマのDB関数を呼ぶ。未ログインで呼べる関数(get_gist等)はsessionがnilでもよい。
func rpc(cfg Config, session *Session, fn string, args map[string]any) ([]byte, error) {
	if session == nil {
		return anonRequest(cfg, http.MethodPost, "/rest/v1/rpc/"+fn, "bin", args)
	}
	return restRequest(cfg, session, http.MethodPost, "/rest/v1/rpc/"+fn, "bin", args)
}

// describeDBError はDB関数がraiseするエラーコード(save_gist等)を日本語の説明に置き換える。
func describeDBError(err error) error {
	msg := err.Error()
	table := []struct{ code, text string }{
		{"not_authenticated", "ログインが必要です。`bin login` を実行してください"},
		{"not_found", "Gistが見つかりません(存在しないか、自分のGistではありません)"},
		{"files_required", "ファイルを1つ以上指定してください"},
		{"too_many_files", "ファイルは1つのGistにつき300個までです"},
		{"invalid_filename", "ファイル名が不正です(1〜255文字・10階層まで。空のフォルダ名・.・..・\\ は使えません)"},
		{"path_conflict", "同じ名前のファイルとフォルダは同時に置けません(例: a と a/b)"},
		{"duplicate_filename", "同じファイル名が重複しています"},
		{"file_too_large", "1ファイルあたり1MBまでです"},
		{"gist_too_large", "1つのGistにつき合計5MBまでです"},
		{"gist_limit_exceeded", "作成できるGistの上限に達しました"},
	}
	for _, t := range table {
		if strings.Contains(msg, t.code) {
			return fmt.Errorf("%s", t.text)
		}
	}
	return err
}

func getGist(cfg Config, session *Session, id string) (*Gist, error) {
	body, err := rpc(cfg, session, "get_gist", map[string]any{"p_id": id})
	if err != nil {
		return nil, describeDBError(err)
	}
	var g *Gist
	if err := json.Unmarshal(body, &g); err != nil {
		return nil, fmt.Errorf("レスポンスの解析に失敗しました: %w", err)
	}
	if g == nil {
		return nil, fmt.Errorf("Gistが見つかりません(存在しないか、非公開です)")
	}
	return g, nil
}

func listGists(cfg Config, session *Session, owner string, limit, offset int, query string) ([]GistSummary, int, error) {
	var q any
	if query != "" {
		q = query
	}
	body, err := rpc(cfg, session, "list_gists", map[string]any{"p_owner": owner, "p_limit": limit, "p_offset": offset, "p_query": q})
	if err != nil {
		return nil, 0, describeDBError(err)
	}
	var out struct {
		Total int           `json:"total"`
		Items []GistSummary `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, 0, fmt.Errorf("レスポンスの解析に失敗しました: %w", err)
	}
	return out.Items, out.Total, nil
}

func saveGist(cfg Config, session *Session, id *string, title, description, visibility string, files []GistFile, message string) (string, error) {
	body, err := rpc(cfg, session, "save_gist", map[string]any{
		"p_message":     message,
		"p_id":          id,
		"p_title":       title,
		"p_description": description,
		"p_visibility":  visibility,
		"p_files":       files,
	})
	if err != nil {
		return "", describeDBError(err)
	}
	var newID string
	if err := json.Unmarshal(body, &newID); err != nil || newID == "" {
		return "", fmt.Errorf("レスポンスの解析に失敗しました: %s", string(body))
	}
	return newID, nil
}

func deleteGist(cfg Config, session *Session, id string) error {
	if _, err := rpc(cfg, session, "delete_gist", map[string]any{"p_id": id}); err != nil {
		return describeDBError(err)
	}
	return nil
}

func gistTitle(title string, files []string) string {
	if title != "" {
		return title
	}
	if len(files) > 0 {
		return files[0]
	}
	return "(無題)"
}

func visibilityLabel(v string) string {
	switch v {
	case "public":
		return green("公開")
	case "unlisted":
		return yellow("限定公開")
	case "private":
		return red("非公開")
	}
	return v
}
