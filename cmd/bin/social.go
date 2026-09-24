package main

// スター・タグ・APIトークンのコマンド。
//
//   bin star <id> / bin unstar <id> / bin starred [-u <handle>]
//   bin tag <id> [<tag>...] [--clear]
//   bin token create <name> [--read] [--expires <days>] / bin token list / bin token revoke <id|prefix>
//
// APIトークン(lbt_...)は環境変数 BIN_TOKEN に入れると、bin login の代わりに使える(CI・スクリプト向け)。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

func cmdStar(args []string, on bool) {
	p := parseArgs(args, nil, nil)
	usage := "bin star <id>"
	if !on {
		usage = "bin unstar <id>"
	}
	if len(p.positional) != 1 {
		usageExit(usage)
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := requireSession()
	body, err := rpc(cfg, session, "star_gist", map[string]any{"p_id": id, "p_star": on})
	if err != nil {
		fail(describeDBError(err))
	}
	var out struct {
		StarCount int `json:"star_count"`
	}
	_ = json.Unmarshal(body, &out)
	if on {
		success(T("スターを付けました(★%d)"), out.StarCount)
	} else {
		success(T("スターを外しました(★%d)"), out.StarCount)
	}
}

func cmdStarred(args []string) {
	p := parseArgs(args, map[string]string{"-u": "user", "--user": "user", "-n": "limit", "--limit": "limit"}, nil)
	cfg, session := optionalSession()
	var userID, label string
	if handle, ok := p.value("user"); ok && handle != "" {
		id, err := resolveHandle(cfg, strings.TrimPrefix(handle, "@"))
		if err != nil {
			fail(err)
		}
		userID, label = id, fmt.Sprintf(T("@%s がスターしたGist"), strings.TrimPrefix(handle, "@"))
	} else {
		if session == nil {
			fail(errors.New(T("ログインしていません。先に `bin login` を実行するか、-u <handle> を指定してください")))
		}
		userID, label = userIDFromToken(session.AccessToken), T("スターしたGist")
	}
	limit := 30
	if v, ok := p.value("limit"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	body, err := rpc(cfg, session, "list_starred", map[string]any{"p_user": userID, "p_limit": limit, "p_offset": 0})
	if err != nil {
		fail(describeDBError(err))
	}
	var out struct {
		Total int           `json:"total"`
		Items []GistSummary `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		fail(err)
	}
	printGistTable(label, nil, out.Items, out.Total, true)
}

func cmdTag(args []string) {
	p := parseArgs(args, nil, map[string]string{"--clear": "clear"})
	if len(p.positional) < 1 {
		usageExit("bin tag <id> [<tag>...] [--clear]")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	tags := p.positional[1:]
	if len(tags) == 0 && !p.bools["clear"] {
		// 指定が無ければ今のタグを表示するだけ
		cfg, session := optionalSession()
		g, err := getGist(cfg, session, id)
		if err != nil {
			fail(err)
		}
		if len(g.Tags) == 0 {
			fmt.Println(dim(T("タグはありません")))
			return
		}
		fmt.Println(formatTags(g.Tags))
		return
	}
	// 「a,b c」のようなカンマ区切りも受け付ける
	var list []string
	for _, t := range tags {
		for _, part := range strings.Split(t, ",") {
			if part = strings.TrimSpace(part); part != "" {
				list = append(list, part)
			}
		}
	}
	if list == nil {
		list = []string{}
	}
	cfg, session := requireSession()
	body, err := rpc(cfg, session, "set_gist_tags", map[string]any{"p_id": id, "p_tags": list})
	if err != nil {
		fail(describeDBError(err))
	}
	var saved []string
	_ = json.Unmarshal(body, &saved)
	if len(saved) == 0 {
		success(T("タグを外しました"))
		return
	}
	success(T("タグを設定しました: %s"), formatTags(saved))
}

func formatTags(tags []string) string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = cyan("#" + t)
	}
	return strings.Join(out, " ")
}

// ---- APIトークン ----

type apiToken struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	Scope      string  `json:"scope"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	Expired    bool    `json:"expired"`
	Token      string  `json:"token"`
}

func requireLoginSession() (Config, *Session) {
	cfg, session := requireSession()
	if session.APIToken {
		fail(errors.New(T("APIトークンの発行・一覧・失効は、BIN_TOKEN ではなく bin login でログインした状態で行ってください")))
	}
	return cfg, session
}

func cmdToken(args []string) {
	if len(args) == 0 {
		usageExit("bin token create <name> [--read] [--expires <days>] | bin token list | bin token revoke <id>")
	}
	switch args[0] {
	case "create", "new":
		p := parseArgs(args[1:], map[string]string{"--expires": "expires"}, map[string]string{"--read": "read"})
		if len(p.positional) != 1 {
			usageExit("bin token create <name> [--read] [--expires <days>]")
		}
		cfg, session := requireLoginSession()
		req := map[string]any{"p_name": p.positional[0], "p_scope": "write", "p_expires_days": nil}
		if p.bools["read"] {
			req["p_scope"] = "read"
		}
		if v, ok := p.value("expires"); ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				fail(errors.New(T("--expires には有効期限の日数(1以上)を指定してください")))
			}
			req["p_expires_days"] = n
		}
		body, err := rpc(cfg, session, "create_api_token", req)
		if err != nil {
			fail(describeDBError(err))
		}
		var t apiToken
		if err := json.Unmarshal(body, &t); err != nil {
			fail(err)
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", green("✓"), fmt.Sprintf(T("APIトークン「%s」を発行しました(%s)"), t.Name, scopeLabel(t.Scope)))
		fmt.Fprintln(os.Stderr, yellow("!")+" "+T("このトークンは今しか表示されません。安全な場所に保存してください"))
		fmt.Fprintln(os.Stderr, dim("  "+T("使い方: BIN_TOKEN=<トークン> bin list / curl -H \"Authorization: Bearer <トークン>\" …")))
		// トークン自体は標準出力へ(`token=$(bin token create ci)` のように受け取れるように)
		fmt.Println(t.Token)
	case "list", "ls":
		cfg, session := requireLoginSession()
		body, err := rpc(cfg, session, "list_api_tokens", map[string]any{})
		if err != nil {
			fail(describeDBError(err))
		}
		var list []apiToken
		if err := json.Unmarshal(body, &list); err != nil {
			fail(err)
		}
		if len(list) == 0 {
			fmt.Println(dim(T("APIトークンはありません(bin token create <名前> で発行)")))
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		for _, t := range list {
			last := T("未使用")
			if t.LastUsedAt != nil {
				last = fmt.Sprintf(T("最終使用 %s"), formatDate(*t.LastUsedAt))
			}
			exp := T("無期限")
			if t.ExpiresAt != nil {
				exp = fmt.Sprintf(T("期限 %s"), formatDate(*t.ExpiresAt))
				if t.Expired {
					exp = red(T("期限切れ"))
				}
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n", dim(t.ID[:8]), bold(t.Name), cyan(t.Prefix+"…"), scopeLabel(t.Scope), dim(last), dim(exp))
		}
		w.Flush()
	case "revoke", "rm", "delete":
		if len(args) != 2 {
			usageExit("bin token revoke <id>")
		}
		cfg, session := requireLoginSession()
		body, err := rpc(cfg, session, "list_api_tokens", map[string]any{})
		if err != nil {
			fail(describeDBError(err))
		}
		var list []apiToken
		_ = json.Unmarshal(body, &list)
		var target *apiToken
		for i := range list {
			if strings.HasPrefix(list[i].ID, args[1]) || list[i].Prefix == args[1] || strings.HasPrefix(args[1], list[i].Prefix) {
				if target != nil {
					fail(errors.New(T("複数のトークンに一致します。IDをもう少し長く指定してください")))
				}
				target = &list[i]
			}
		}
		if target == nil {
			fail(errors.New(T("一致するAPIトークンがありません(bin token list で確認)")))
		}
		if _, err := rpc(cfg, session, "revoke_api_token", map[string]any{"p_id": target.ID}); err != nil {
			fail(describeDBError(err))
		}
		success(T("APIトークン「%s」を失効させました"), target.Name)
	default:
		usageExit("bin token create <name> [--read] [--expires <days>] | bin token list | bin token revoke <id>")
	}
}

func scopeLabel(scope string) string {
	if scope == "read" {
		return yellow(T("読み取り専用"))
	}
	return green(T("読み書き"))
}

// tokenOwner はAPIトークンの持ち主のユーザーID(bin-serverの /api/bin/token で確認する)
var tokenOwnerCache = map[string]string{}

func tokenOwner(cfg Config, token string) string {
	if id, ok := tokenOwnerCache[token]; ok {
		return id
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.SupabaseURL, "/")+"/bin/token", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fail(errors.New(T("APIトークンが無効か、期限切れ・失効済みです(BIN_TOKEN を確認してください)")))
	}
	var out struct {
		UserID string `json:"user_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	tokenOwnerCache[token] = out.UserID
	return out.UserID
}
