package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ---- bin scan: Token・秘密鍵などの混入チェック ----

type secretRule struct {
	name string
	re   *regexp.Regexp
}

var secretRules = []secretRule{
	{"SourceBin Token", regexp.MustCompile(`\b(?:sbt|lbt)_[0-9a-f]{48}\b`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{32,}`)},
	{"GitHub token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{50,}`)},
	{"AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"Stripe key", regexp.MustCompile(`\b[sr]k_(?:live|test)_[0-9A-Za-z]{20,}`)},
	{"npm token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"Private key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY`)},
	{"Credential in URL", regexp.MustCompile(`://[^/\s:@]+:[^/\s:@]{6,}@`)},
	{"Secret assignment", regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|passw(?:or)?d|token)\b["']?\s*[:=]\s*["'][^"'\s]{12,}["']`)},
}

type secretHit struct {
	file, kind, sample string
	line               int
}

func maskSample(s string) string {
	r := []rune(s)
	if len(r) <= 8 {
		return strings.Repeat("•", len(r))
	}
	return string(r[:6]) + strings.Repeat("•", 6) + string(r[len(r)-2:])
}

func scanFiles(files []GistFile) []secretHit {
	var hits []secretHit
	for _, f := range files {
		for i, l := range strings.Split(f.Content, "\n") {
			for _, r := range secretRules {
				if m := r.re.FindString(l); m != "" {
					hits = append(hits, secretHit{f.Filename, r.name, maskSample(m), i + 1})
					break
				}
			}
		}
	}
	return hits
}

// warnSecrets は保存前に呼び、見つかれば stderr に警告する(保存は止めない)
func warnSecrets(files []GistFile) {
	hits := scanFiles(files)
	if len(hits) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", yellow("!"), fmt.Sprintf(T("Token・秘密鍵らしき文字列が %d 件あります(詳しくは bin scan)"), len(hits)))
	for i, h := range hits {
		if i >= 3 {
			break
		}
		fmt.Fprintf(os.Stderr, "  %s  %s\n", dim(fmt.Sprintf("%s:%d", h.file, h.line)), h.kind)
	}
}

func cmdScan(args []string) {
	p := parseArgs(args, nil, nil)
	var files []GistFile
	label := ""
	if len(p.positional) == 1 && looksLikeGistRef(p.positional[0]) {
		_, _, g := pickGist(p, "bin scan [<id> | <file|dir>...]")
		files, label = g.Files, g.ID
	} else {
		if len(p.positional) == 0 {
			p.positional = []string{"."}
		}
		var skips []skipped
		for _, a := range p.positional {
			collectPath(a, &files, &skips)
		}
		label = strings.Join(p.positional, " ")
	}
	hits := scanFiles(files)
	if len(hits) == 0 {
		fmt.Fprintf(os.Stderr, "%s %s\n", green("✔"), fmt.Sprintf(T("Token・秘密鍵らしき文字列は見つかりませんでした(%s・%dファイル)"), label, len(files)))
		return
	}
	fmt.Println(rightAlign("  "+bold(T("見つかった秘密情報")), dim(fmt.Sprintf(T("%d件"), len(hits)))))
	fmt.Println(rule())
	for _, h := range hits {
		fmt.Printf("  %s  %s  %s\n", red("●"), padRight(h.kind, 20), dim(fmt.Sprintf("%s:%d", h.file, h.line))+"  "+h.sample)
	}
	fmt.Println()
	fmt.Println(dim("  " + T("公開する前に消すか、環境変数に移してください。誤検出のこともあります。")))
	os.Exit(1)
}

// ---- 共通 ----

func rpcJSON(cfg Config, session *Session, fn string, args map[string]any, out any) {
	body, err := rpc(cfg, session, fn, args)
	if err != nil {
		fail(describeDBError(err))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			fail(err)
		}
	}
}

func handleOf(o GistOwner) string {
	if o.Handle != nil {
		return "@" + *o.Handle
	}
	return "?"
}

// ---- bin comment ----

type gistComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Filename  *string   `json:"filename"`
	Line      *int      `json:"line"`
	LineEnd   *int      `json:"line_end"`
	CreatedAt string    `json:"created_at"`
	Edited    bool      `json:"edited"`
	Author    GistOwner `json:"author"`
}

func cmdComment(args []string) {
	usage := "bin comment list <id> | add <id> <text> [-f <file> --line A[-B]] | edit <comment-id> <text> | rm <comment-id>"
	if len(args) == 0 {
		usageExit(usage)
	}
	sub, rest := args[0], args[1:]
	p := parseArgs(rest, map[string]string{"-f": "filename", "--filename": "filename", "--line": "line"}, nil)
	switch sub {
	case "list", "ls":
		if len(p.positional) != 1 {
			usageExit("bin comment list <id>")
		}
		id, err := parseGistID(p.positional[0])
		if err != nil {
			fail(err)
		}
		cfg, session := optionalSession()
		var list []gistComment
		rpcJSON(cfg, session, "list_comments", map[string]any{"p_id": id}, &list)
		if len(list) == 0 {
			fmt.Println(dim(T("コメントはありません")))
			return
		}
		fmt.Println(rightAlign("  "+bold(T("コメント")), dim(fmt.Sprintf(T("%d件"), len(list)))))
		fmt.Println(rule())
		for _, c := range list {
			where := ""
			if c.Filename != nil {
				where = *c.Filename
				if c.Line != nil {
					where += ":" + strconv.Itoa(*c.Line)
					if c.LineEnd != nil {
						where += "-" + strconv.Itoa(*c.LineEnd)
					}
				}
				where = "  " + cyan(where)
			}
			edited := ""
			if c.Edited {
				edited = dim(" " + T("(編集済み)"))
			}
			fmt.Printf("  %s  %s%s%s  %s\n", dim("#"+strconv.FormatInt(c.ID, 10)), bold(handleOf(c.Author)), where, edited, dim(relTime(c.CreatedAt)))
			for _, l := range strings.Split(strings.TrimSpace(c.Body), "\n") {
				fmt.Println("    " + l)
			}
			fmt.Println()
		}
	case "add":
		if len(p.positional) < 2 {
			usageExit("bin comment add <id> <text> [-f <file> --line A[-B]]")
		}
		id, err := parseGistID(p.positional[0])
		if err != nil {
			fail(err)
		}
		cfg, session := requireSession()
		a := map[string]any{"p_id": id, "p_body": strings.Join(p.positional[1:], " "), "p_filename": nil, "p_line": nil, "p_line_end": nil}
		if name, ok := p.value("filename"); ok {
			a["p_filename"] = name
			if ln, ok := p.value("line"); ok {
				s, e := ln, ""
				if i := strings.Index(ln, "-"); i > 0 {
					s, e = ln[:i], ln[i+1:]
				}
				sn, err := strconv.Atoi(s)
				if err != nil {
					fail(fmt.Errorf(T("--line は 5 や 3-7 の形で指定してください")))
				}
				a["p_line"] = sn
				if en, err := strconv.Atoi(e); err == nil && en != sn {
					a["p_line_end"] = en
				}
			}
		}
		var c gistComment
		rpcJSON(cfg, session, "add_comment", a, &c)
		doneCard(T("コメントしました"), []cardRow{{"ID", "#" + strconv.FormatInt(c.ID, 10)}}, gistURL(cfg, id))
	case "edit":
		if len(p.positional) < 2 {
			usageExit("bin comment edit <comment-id> <text>")
		}
		cid, err := strconv.ParseInt(strings.TrimPrefix(p.positional[0], "#"), 10, 64)
		if err != nil {
			fail(fmt.Errorf(T("コメント ID は数字で指定してください")))
		}
		cfg, session := requireSession()
		rpcJSON(cfg, session, "edit_comment", map[string]any{"p_comment": cid, "p_body": strings.Join(p.positional[1:], " ")}, nil)
		doneCard(T("コメントを編集しました"), nil, "")
	case "rm", "delete":
		if len(p.positional) != 1 {
			usageExit("bin comment rm <comment-id>")
		}
		cid, err := strconv.ParseInt(strings.TrimPrefix(p.positional[0], "#"), 10, 64)
		if err != nil {
			fail(fmt.Errorf(T("コメント ID は数字で指定してください")))
		}
		cfg, session := requireSession()
		rpcJSON(cfg, session, "delete_comment", map[string]any{"p_comment": cid}, nil)
		doneCard(T("コメントを削除しました"), nil, "")
	default:
		usageExit(usage)
	}
}

// ---- bin collection ----

func cmdCollection(args []string) {
	usage := "bin collection list [-u <handle>] | show <id> | create <title> [--public|--unlisted|--private] | add <collection> <gist> | remove <collection> <gist> | delete <collection>"
	if len(args) == 0 {
		usageExit(usage)
	}
	sub, rest := args[0], args[1:]
	p := parseArgs(rest, map[string]string{"-u": "user", "--user": "user", "-d": "description", "--description": "description"}, visibilityFlags)
	pos := p.positional
	switch sub {
	case "list", "ls":
		cfg, session := optionalSession()
		owner := ""
		if h, ok := p.value("user"); ok {
			id, err := resolveHandle(cfg, strings.TrimPrefix(h, "@"))
			if err != nil {
				fail(err)
			}
			owner = id
		} else {
			cfg, session = requireSession()
			owner = userIDFromToken(session.AccessToken)
		}
		var list []struct {
			ID, Title, Visibility string
			ItemCount             int    `json:"item_count"`
			UpdatedAt             string `json:"updated_at"`
		}
		body, err := rpc(cfg, session, "list_collections", map[string]any{"p_owner": owner})
		if err != nil {
			fail(describeDBError(err))
		}
		_ = json.Unmarshal(body, &list)
		var raw []map[string]any
		_ = json.Unmarshal(body, &raw)
		if len(raw) == 0 {
			fmt.Println(dim(T("コレクションはありません(bin collection create <タイトル> で作成)")))
			return
		}
		fmt.Println(rightAlign("  "+bold(T("コレクション")), dim(fmt.Sprintf(T("%d件"), len(raw)))))
		fmt.Println(rule())
		for _, c := range raw {
			cnt, _ := c["item_count"].(float64)
			fmt.Printf("  %s  %s  %s  %s\n", padRight(visibilityLabel(fmt.Sprint(c["visibility"])), 12), bold(fmt.Sprint(c["title"])), dim(fmt.Sprintf(Tn("%dファイル", int(cnt)), int(cnt))), dim(fmt.Sprint(c["id"])))
		}
	case "show":
		if len(pos) != 1 {
			usageExit("bin collection show <id>")
		}
		cfg, session := optionalSession()
		var c struct {
			ID, Title, Description, Visibility string
			Owner                              GistOwner
			Items                              []GistSummary
			HiddenCount                        int `json:"hidden_count"`
		}
		rpcJSON(cfg, session, "get_collection", map[string]any{"p_id": pos[0]}, &c)
		if c.ID == "" {
			fail(fmt.Errorf(T("コレクションが見つかりません")))
		}
		fmt.Println(rightAlign("  "+bold(c.Title)+"  "+dim(handleOf(c.Owner)), visibilityLabel(c.Visibility)))
		if c.Description != "" {
			fmt.Println("  " + dim(c.Description))
		}
		fmt.Println(rule())
		printGistTable("", nil, c.Items, len(c.Items), false)
	case "create", "new":
		if len(pos) < 1 {
			usageExit("bin collection create <title>")
		}
		cfg, session := requireSession()
		vis, ok := pickVisibility(p)
		if !ok {
			vis = "unlisted"
		}
		desc, _ := p.value("description")
		var id string
		rpcJSON(cfg, session, "save_collection", map[string]any{"p_id": nil, "p_title": strings.Join(pos, " "), "p_description": desc, "p_visibility": vis}, &id)
		doneCard(T("コレクションを作成しました"), []cardRow{{"ID", id}, {T("公開範囲"), visibilityLabel(vis)}}, cfg.SiteURL+"/c/"+id)
	case "add", "remove", "rm":
		if len(pos) != 2 {
			usageExit("bin collection " + sub + " <collection> <gist>")
		}
		gid, err := parseGistID(pos[1])
		if err != nil {
			fail(err)
		}
		cfg, session := requireSession()
		rpcJSON(cfg, session, "set_collection_gist", map[string]any{"p_collection": pos[0], "p_gist": gid, "p_in": sub == "add"}, nil)
		if sub == "add" {
			doneCard(T("コレクションに追加しました"), []cardRow{{"Gist", gid}}, cfg.SiteURL+"/c/"+pos[0])
		} else {
			doneCard(T("コレクションから外しました"), []cardRow{{"Gist", gid}}, "")
		}
	case "delete":
		if len(pos) != 1 {
			usageExit("bin collection delete <collection>")
		}
		if !confirm(T("コレクション %s を削除しますか?"), pos[0]) {
			return
		}
		cfg, session := requireSession()
		rpcJSON(cfg, session, "delete_collection", map[string]any{"p_id": pos[0]}, nil)
		doneCard(T("コレクションを削除しました"), nil, "")
	default:
		usageExit(usage)
	}
}

// ---- bin stats / forks / tags ----

func cmdStats(args []string) {
	p := parseArgs(args, map[string]string{"--days": "days"}, nil)
	days := 30
	if v, ok := p.value("days"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	cfg, session, g := ownGist(p, "bin stats <id> [--days N]")
	var st struct {
		Views, Downloads int
		Days             []struct {
			Day              string
			Views, Downloads int
		}
	}
	rpcJSON(cfg, session, "get_gist_stats", map[string]any{"p_id": g.ID, "p_days": days}, &st)
	fmt.Println(rightAlign("  "+bold(gistTitle(g.Title, namesOf(g.Files))), dim(fmt.Sprintf(T("直近 %d 日"), days))))
	fmt.Println(rule())
	fmt.Printf("  %s %s   %s %s\n\n", dim(T("閲覧")), bold(strconv.Itoa(st.Views)), dim(T("ダウンロード")), bold(strconv.Itoa(st.Downloads)))
	max := 1
	for _, d := range st.Days {
		if d.Views > max {
			max = d.Views
		}
	}
	for _, d := range st.Days {
		n := d.Views * 30 / max
		if d.Views > 0 && n == 0 {
			n = 1
		}
		fmt.Printf("  %s  %s %s\n", dim(d.Day), cyan(strings.Repeat("█", n)), dim(strconv.Itoa(d.Views)))
	}
}

func cmdForks(args []string) {
	p := parseArgs(args, nil, nil)
	if len(p.positional) != 1 {
		usageExit("bin forks <id>")
	}
	id, err := parseGistID(p.positional[0])
	if err != nil {
		fail(err)
	}
	cfg, session := optionalSession()
	var out struct {
		Total int
		Items []GistSummary
	}
	rpcJSON(cfg, session, "list_forks", map[string]any{"p_id": id, "p_limit": 50, "p_offset": 0}, &out)
	if len(out.Items) == 0 {
		fmt.Println(dim(T("フォークはありません")))
		return
	}
	printGistTable(T("フォーク"), nil, out.Items, len(out.Items), true)
}

func cmdTags(args []string) {
	p := parseArgs(args, map[string]string{"-u": "user", "--user": "user"}, nil)
	cfg, session := optionalSession()
	owner := any(nil)
	if h, ok := p.value("user"); ok {
		id, err := resolveHandle(cfg, strings.TrimPrefix(h, "@"))
		if err != nil {
			fail(err)
		}
		owner = id
	} else if s, err := loadSession(); err == nil && s != nil {
		session = s
		owner = userIDFromToken(s.AccessToken)
	}
	var tags []struct {
		Tag   string
		Count int
	}
	rpcJSON(cfg, session, "list_tags", map[string]any{"p_owner": owner, "p_limit": 100}, &tags)
	if len(tags) == 0 {
		fmt.Println(dim(T("タグはありません")))
		return
	}
	fmt.Println(rightAlign("  "+bold(T("タグ")), dim(fmt.Sprintf(T("%d件"), len(tags)))))
	fmt.Println(rule())
	for _, t := range tags {
		fmt.Printf("  %s  %s\n", cyan(padRight("#"+t.Tag, 24)), dim(strconv.Itoa(t.Count)))
	}
}
