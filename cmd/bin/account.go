package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ---- /auth/v1/user(自分のアカウント情報) ----

type identity struct {
	Provider     string `json:"provider"`
	CreatedAt    string `json:"created_at"`
	LastSignInAt string `json:"last_sign_in_at"`
	IdentityData struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		UserName          string `json:"user_name"`
	} `json:"identity_data"`
}

type mfaFactor struct {
	Status       string `json:"status"`
	FactorType   string `json:"factor_type"`
	FriendlyName string `json:"friendly_name"`
}

type authUser struct {
	ID           string      `json:"id"`
	Email        string      `json:"email"`
	CreatedAt    string      `json:"created_at"`
	LastSignInAt string      `json:"last_sign_in_at"`
	Identities   []identity  `json:"identities"`
	Factors      []mfaFactor `json:"factors"`
}

// fetchAuthUser は/auth/v1/userを呼ぶ。401ならrefresh_tokenで1回だけ更新して再試行する。
func fetchAuthUser(cfg Config, session *Session) (*authUser, error) {
	do := func() ([]byte, int, error) {
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.SupabaseURL, "/")+"/auth/v1/user", nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("apikey", cfg.AnonKey)
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, 0, wrapNetworkError(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return body, res.StatusCode, nil
	}
	body, status, err := do()
	if err != nil {
		return nil, err
	}
	if status == 401 {
		if err := refreshAccessToken(cfg, session); err != nil {
			return nil, err
		}
		if body, status, err = do(); err != nil {
			return nil, err
		}
	}
	if status >= 300 {
		return nil, apiError(status, body)
	}
	var u authUser
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf(T("レスポンスの解析に失敗しました: %w"), err)
	}
	return &u, nil
}

// ---- 共通プロフィール(public.user_profiles、誰でも読める) ----

type profile struct {
	UserID          string  `json:"user_id"`
	Handle          *string `json:"handle"`
	DisplayName     *string `json:"display_name"`
	AvatarURL       *string `json:"avatar_url"`
	Locale          *string `json:"locale"`
	Timezone        *string `json:"timezone"`
	CreatedAt       string  `json:"created_at"`
	HandleChangedAt *string `json:"handle_changed_at"`
}

const profileColumns = "user_id,handle,display_name,avatar_url,locale,timezone,created_at,handle_changed_at"

func fetchProfileByID(cfg Config, session *Session, userID string) (*profile, error) {
	path := "/rest/v1/user_profiles?select=" + profileColumns + "&user_id=eq." + url.QueryEscape(userID)
	var body []byte
	var err error
	if session != nil {
		body, err = restRequest(cfg, session, http.MethodGet, path, "", nil)
	} else {
		body, err = anonRequest(cfg, http.MethodGet, path, "", nil)
	}
	if err != nil {
		return nil, err
	}
	var rows []profile
	_ = json.Unmarshal(body, &rows)
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func fetchProfileByHandle(cfg Config, handle string) (*profile, error) {
	body, err := anonRequest(cfg, http.MethodGet, "/rest/v1/user_profiles?select="+profileColumns+"&handle=ilike."+url.QueryEscape(escapeLike(handle)), "", nil)
	if err != nil {
		return nil, err
	}
	var rows []profile
	_ = json.Unmarshal(body, &rows)
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// resolveRenamedHandle はLapountのresolve-handle関数で、変更前のハンドルから現在のハンドルを引く。
func resolveRenamedHandle(cfg Config, handle string) string {
	body, _ := json.Marshal(map[string]string{"handle": handle})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.SupabaseURL, "/")+"/functions/v1/resolve-handle", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var out struct {
		Status string `json:"status"`
		Handle string `json:"handle"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.Status == "redirect" {
		return out.Handle
	}
	return ""
}

// ---- Gistの集計 ----

type gistStats struct {
	total, public, unlisted, private int
	files                            int
	bytes                            int64
	latest                           *GistSummary
	recent                           []GistSummary
	truncated                        bool
}

// collectStats はlist_gistsをページングして集計する。本人のセッションで呼べば非公開・限定公開も含まれる
// (DB側で公開範囲を判定するので、他人の分は公開Gistだけになる)。
func collectStats(cfg Config, session *Session, owner string) (*gistStats, error) {
	const page, cap = 100, 3000
	st := &gistStats{}
	for offset := 0; ; offset += page {
		items, total, err := listGists(cfg, session, listFilter{Owner: owner}, page, offset)
		if err != nil {
			return nil, err
		}
		st.total = total
		for i := range items {
			g := items[i]
			switch g.Visibility {
			case "public":
				st.public++
			case "unlisted":
				st.unlisted++
			case "private":
				st.private++
			}
			st.files += len(g.Files)
			for _, f := range g.Files {
				st.bytes += f.Size
			}
			if len(st.recent) < 5 {
				st.recent = append(st.recent, g)
			}
		}
		if len(st.recent) > 0 {
			st.latest = &st.recent[0]
		}
		if offset+page >= total || len(items) == 0 {
			break
		}
		if offset+page >= cap {
			st.truncated = true
			break
		}
	}
	return st, nil
}

func formatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
}

func strOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

func providerName(p string) string {
	names := map[string]string{"google": "Google", "github": "GitHub", "discord": "Discord", "twitch": "Twitch", "twitter": "X (Twitter)", "spotify": "Spotify", "email": T("メール")}
	if n, ok := names[p]; ok {
		return n
	}
	return p
}

func printRecent(cfg Config, st *gistStats) {
	if len(st.recent) == 0 {
		return
	}
	fmt.Println()
	sectionTitle("🕘", T("最近更新したGist"))
	for _, g := range st.recent {
		names := make([]string, len(g.Files))
		for i, f := range g.Files {
			names[i] = f.Filename
		}
		fmt.Printf("  %s  %s  %s  %s\n", cyan(g.ID), bold(gistTitle(g.Title, names)), visibilityLabel(g.Visibility), dim(formatDate(g.UpdatedAt)))
	}
}

// ---- コマンド ----

func cmdWhoami() {
	cfg, session := requireSession()
	userID := userIDFromToken(session.AccessToken)
	// Lapount独自発行のトークン等で/auth/v1/userが使えない場合もあるので、失敗しても他の情報は出す
	au, authErr := fetchAuthUser(cfg, session)
	if au != nil {
		userID = au.ID
	}
	prof, err := fetchProfileByID(cfg, session, userID)
	if err != nil {
		fail(err)
	}

	name := ""
	handle := ""
	if prof != nil {
		name = strOr(prof.DisplayName, "")
		handle = strOr(prof.Handle, "")
	}
	if name == "" && au != nil {
		name = au.Email
	}
	title := bold(name)
	if handle != "" {
		title += " " + dim("@"+handle)
	} else {
		title += " " + dim(T("(ハンドル名未設定)"))
	}
	fmt.Println(title)
	fmt.Println()

	sectionTitle("👤", T("アカウント"))
	printField(T("ユーザーID"), userID)
	if au != nil {
		printField(T("通知用メール"), au.Email)
		printField(T("登録日時"), formatDate(au.CreatedAt))
		printField(T("最終ログイン"), formatDate(au.LastSignInAt))
	}
	if prof != nil {
		printField(T("言語"), strOr(prof.Locale, "-"))
		printField(T("タイムゾーン"), strOr(prof.Timezone, "-"))
		if prof.HandleChangedAt != nil {
			printField(T("ハンドル変更"), formatDate(*prof.HandleChangedAt))
		}
	}
	if handle != "" {
		printField(T("ユーザーページ"), cfg.SiteURL+"/u/"+handle)
	}
	printField(T("プロフィール編集"), "https://account.lapius7.com/dashboard")

	if au != nil {
		fmt.Println()
		sectionTitle("🔗", T("連携しているログイン方法"))
		if len(au.Identities) == 0 {
			printField("", dim(T("(なし)")))
		}
		sort.Slice(au.Identities, func(i, j int) bool { return au.Identities[i].CreatedAt < au.Identities[j].CreatedAt })
		for _, id := range au.Identities {
			who := id.IdentityData.Email
			if who == "" {
				who = firstNonEmpty(id.IdentityData.PreferredUsername, id.IdentityData.UserName)
			}
			printField(providerName(id.Provider), fmt.Sprintf("%s %s", who, dim(fmt.Sprintf(T("最終ログイン %s"), formatDate(id.LastSignInAt)))))
		}

		fmt.Println()
		sectionTitle("🔐", T("2段階認証"))
		verified := 0
		for _, f := range au.Factors {
			if f.Status == "verified" {
				verified++
			}
		}
		if verified > 0 {
			printField(T("状態"), green(fmt.Sprintf(T("有効(%d件)"), verified)))
		} else {
			printField(T("状態"), yellow(T("未設定")))
		}
	} else if authErr != nil {
		fmt.Println()
		warn(T("アカウントの詳細(連携・2段階認証)は取得できませんでした: %v"), authErr)
	}

	fmt.Println()
	sectionTitle("📄", "Gist")
	st, err := collectStats(cfg, session, userID)
	if err != nil {
		warn(T("Gistの集計に失敗しました: %v"), err)
	} else {
		printField(T("合計"), fmt.Sprintf(T("%d件 %s"), st.total, dim(fmt.Sprintf(T("(公開 %d / 限定公開 %d / 非公開 %d)"), st.public, st.unlisted, st.private))))
		printField(T("ファイル"), fmt.Sprintf(Tn("%dファイル · %s", st.files), st.files, formatSize(st.bytes)))
		if st.latest != nil {
			printField(T("最終更新"), formatDate(st.latest.UpdatedAt))
		}
		if st.truncated {
			printField("", dim(T("(件数が多いため先頭3000件のみ集計)")))
		}
		printRecent(cfg, st)
	}

	fmt.Println()
	sectionTitle("💻", T("このCLI"))
	if claims := tokenExpiry(session.AccessToken); !claims.IsZero() {
		printField(T("アクセストークン"), fmt.Sprintf(T("有効期限 %s"), claims.Local().Format("2006-01-02 15:04"))+dim(T("(期限切れ後は自動更新)")))
	}
	printField(T("セッション保存先"), sessionFilePath())
	printField(T("接続先"), cfg.SiteURL)
	printField(T("バージョン"), version)
}

func cmdUser(args []string) {
	p := parseArgs(args, map[string]string{"-u": "user", "--user": "user"}, nil)
	handle, ok := p.value("user")
	if !ok && len(p.positional) == 1 {
		handle, ok = p.positional[0], true
	}
	if !ok || handle == "" {
		usageExit("bin user -u <handle>")
	}
	handle = strings.TrimPrefix(handle, "@")
	cfg, session := optionalSession()

	prof, err := fetchProfileByHandle(cfg, handle)
	if err != nil {
		fail(err)
	}
	if prof == nil {
		// ハンドル名が変更されていれば、新しいハンドルで引き直す
		if renamed := resolveRenamedHandle(cfg, handle); renamed != "" {
			warn(T("@%s は @%s に変更されています"), handle, renamed)
			handle = renamed
			prof, err = fetchProfileByHandle(cfg, handle)
			if err != nil {
				fail(err)
			}
		}
	}
	if prof == nil {
		fail(fmt.Errorf(T("@%s というユーザーは見つかりません"), handle))
	}

	fmt.Printf("%s %s\n\n", bold(strOr(prof.DisplayName, strOr(prof.Handle, handle))), dim("@"+strOr(prof.Handle, handle)))
	sectionTitle("👤", T("プロフィール"))
	printField(T("ユーザーページ"), cfg.SiteURL+"/u/"+strOr(prof.Handle, handle))
	printField(T("登録日"), formatDate(prof.CreatedAt))
	if prof.AvatarURL != nil && *prof.AvatarURL != "" {
		printField(T("アイコン"), *prof.AvatarURL)
	}
	isMe := session != nil && userIDFromToken(session.AccessToken) == prof.UserID
	if isMe {
		printField("", cyan(T("(あなたのアカウントです。詳しくは bin whoami)")))
	}

	fmt.Println()
	sectionTitle("📄", T("公開Gist"))
	// 本人のセッションで数えると非公開も含まれてしまうので、他人向けの表示は常に未ログイン扱いで集計する
	st, err := collectStats(cfg, nil, prof.UserID)
	if err != nil {
		fail(err)
	}
	printField(T("公開Gist"), fmt.Sprintf(T("%d件"), st.total))
	printField(T("ファイル"), fmt.Sprintf(Tn("%dファイル · %s", st.files), st.files, formatSize(st.bytes)))
	if st.latest != nil {
		printField(T("最終更新"), formatDate(st.latest.UpdatedAt))
	}
	if st.truncated {
		printField("", dim(T("(件数が多いため先頭3000件のみ集計)")))
	}
	printRecent(cfg, st)
	if st.total > len(st.recent) {
		fmt.Println(dim(fmt.Sprintf(T("\n  すべて表示: bin list -u %s"), strOr(prof.Handle, handle))))
	}
}

// cmdLogout はサーバー側でもこのCLIのセッション(refresh_token)を失効させてから、手元の保存分を消す。
// scope=localなので、ブラウザや他の端末のログインには影響しない。
func cmdLogout() {
	cfg, session := requireSession()
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.SupabaseURL, "/")+"/auth/v1/logout?scope=local", nil)
	if err == nil {
		req.Header.Set("apikey", cfg.AnonKey)
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}
	// サーバー側の失効に失敗しても(期限切れ・オフライン等)、手元のセッションは必ず消す
	doLogout()
	success(T("ログアウトしました。"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "-"
}

func tokenExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
		_ = json.Unmarshal(payload, &claims)
	}
	if claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}
