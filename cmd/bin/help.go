package main

// help.go: コマンドごとの詳細ヘルプ。
//   bin help <コマンド> [サブコマンド] / bin <コマンド> [サブコマンド] -h|--help
// 日本語・英語を持ち、韓国語は英語で表示する。

import (
	"fmt"
	"strings"
)

type helpOpt struct{ flag, ja, en string }

type helpDoc struct {
	usage []string
	ja    string
	en    string
	opts  []helpOpt
	ex    []string
	subs  []string // 下位コマンド(あれば `bin help <cmd> <sub>` を案内)
}

func (d helpDoc) desc() string {
	if lang == "ja" || d.en == "" {
		return d.ja
	}
	return d.en
}

func (o helpOpt) text() string {
	if lang == "ja" || o.en == "" {
		return o.ja
	}
	return o.en
}

var (
	optsVis = []helpOpt{
		{"--public", "一覧に載せる(誰でも見つけられる)", "Listed publicly"},
		{"--unlisted", "URLを知っている人だけが見られる(既定)", "Anyone with the URL can view (default)"},
		{"--private", "自分だけが見られる", "Only you can view"},
	}
	optsFiles = []helpOpt{
		{"-f, --filename <名前>", "標準入力・クリップボードの内容につけるファイル名", "File name for stdin / clipboard content"},
		{"--include <glob>", "フォルダ内で対象にするファイル(複数指定可)", "Only include matching files in folders (repeatable)"},
		{"--exclude <glob>", "フォルダ内で除外するファイル(複数指定可。.binignore も使える)", "Exclude matching files in folders (repeatable; .binignore works too)"},
		{"<file>:A-B", "ファイルの行範囲だけ送る(70- は末尾まで、-20 は先頭から、=名前でGist上の名前)", "Send only a line range (70- to the end, -20 from the start, =name to rename)"},
	}
	optsFilter = []helpOpt{
		{"-u, --user <handle>", "ユーザーを指定する", "Limit to a user"},
		{"-n, --limit <数>", "表示する件数", "Number of items"},
		{"-q, --query <文字>", "タイトル・ファイル名・本文を検索する", "Search titles, file names and contents"},
		{"-l, --lang <拡張子>", "言語(拡張子)で絞り込む", "Filter by language (extension)"},
		{"--since <期間>", "期間で絞り込む(例: 7d, 2w)", "Filter by period (e.g. 7d, 2w)"},
		{"--sort <順>", "並び順(updated, created, stars など)", "Sort order (updated, created, stars ...)"},
		{"--tag <タグ>", "タグで絞り込む", "Filter by tag"},
	}
)

var helpDocs = map[string]helpDoc{
	"login": {
		usage: []string{"bin login [--device]"},
		ja:    "ブラウザでLapount(account.lapius7.com)にログインします。ログインすると Gist の作成・編集・削除などができます(閲覧や bin try は不要)。",
		en:    "Log in with Lapount (account.lapius7.com) in your browser. Needed to create, edit or delete gists (not for viewing or bin try).",
		opts:  []helpOpt{{"--device", "デバイスコード方式。SSH越しなど、手元でブラウザが開けないときに使う", "Device-code flow, for SSH sessions where no browser can open"}},
		ex:    []string{"bin login", "bin login --device"},
	},
	"logout": {
		usage: []string{"bin logout"},
		ja:    "ログアウトします。サーバー側でもこのCLIのセッションを失効させます。",
		en:    "Log out and revoke this CLI session on the server.",
	},
	"whoami": {
		usage: []string{"bin whoami"},
		ja:    "ログイン中のアカウント(連携・2段階認証・Gistの集計など)を表示します。",
		en:    "Show the logged-in account (linked services, 2FA, gist totals).",
	},
	"user": {
		usage: []string{"bin user -u <handle>"},
		ja:    "ユーザーの公開プロフィールと公開Gistの集計を表示します。",
		en:    "Show a user's public profile and public gist totals.",
		opts:  []helpOpt{{"-u, --user <handle>", "表示するユーザー", "User to show"}},
		ex:    []string{"bin user -u lapius"},
	},
	"create": {
		usage: []string{"bin create <file|dir>... [-t <title>] [-d <description>] [--public|--unlisted|--private]", "cat x | bin create -f x.txt"},
		ja:    "ファイル・フォルダ・標準入力からGistを作ります。既定は限定公開で、URLだけが標準出力に出ます(`url=$(bin create x.go)` のように使えます)。要ログイン。ログインなしで作るなら bin try create。",
		en:    "Create a gist from files, folders or stdin. Unlisted by default; only the URL goes to stdout (use `url=$(bin create x.go)`). Login required; for no-login use bin try create.",
		opts: append(append([]helpOpt{
			{"-t, --title <題>", "タイトル", "Title"},
			{"-d, --description <説明>", "説明", "Description"},
			{"-m, --message <メモ>", "変更メモ(最初のリビジョンのコミットメッセージ)", "Change message for the first revision"},
			{"--clipboard", "クリップボードの内容から作る", "Create from the clipboard"},
		}, optsVis...), optsFiles...),
		ex: []string{"bin create main.go util.go -d \"説明\"", "cat build.log | bin create -f build.log --private", "bin create src/ --exclude \"*.log\"", "bin create --clipboard -f note.txt"},
	},
	"list": {
		usage: []string{"bin list [-u <handle>] [-q <text>] [-l <lang>] [--since <期間>] [--sort <順>] [--tag <タグ>] [-n <数>]"},
		ja:    "自分(または -u で指定したユーザー)のGist一覧を表示します。",
		en:    "List your gists (or another user's with -u).",
		opts:  optsFilter,
		ex:    []string{"bin list", "bin list -q TODO -l go --since 7d", "bin list -u lapius --sort stars"},
	},
	"timeline": {
		usage: []string{"bin timeline [-q <text>] [-l <lang>] [--since <期間>] [--sort <順>] [--tag <タグ>] [-n <数>]"},
		ja:    "みんなの公開Gistを新しい順に表示します。",
		en:    "Show everyone's public gists, newest first.",
		opts:  optsFilter[1:],
	},
	"view": {
		usage: []string{"bin view <id|URL|別名> [-f <file>] [--raw]"},
		ja:    "Gistの内容を表示します。パイプ先に出すときは、1ファイルなら中身だけをそのまま出力します。",
		en:    "Show a gist. When piped and the gist has a single file, only its content is printed.",
		opts: []helpOpt{
			{"-f, --filename <名前>", "そのファイルだけを装飾なしで出力する", "Print just that file, undecorated"},
			{"--raw", "装飾なしで中身だけを出力する", "Print contents only"},
		},
		ex: []string{"bin view 483b71e6d6cf3c84", "bin view 483b71e6d6cf3c84 -f main.go > main.go"},
	},
	"edit": {
		usage: []string{"bin edit <id> [<file|dir>...] [-t <題>] [-d <説明>] [-m <メモ>] [--remove <path>] [--lines A-B] [--as <名前>]"},
		ja:    "ファイルを追加・上書き・削除します。同じ名前のファイルは上書きされ、変更があればリビジョンが1つ積まれます。",
		en:    "Add, overwrite or remove files. Files with the same name are overwritten; a change adds one revision.",
		opts: append([]helpOpt{
			{"--remove <path>", "ファイルを削除する(複数指定可)", "Remove a file (repeatable)"},
			{"--as <名前>", "Gist上のファイル名を指定する", "File name to use in the gist"},
			{"--lines A-B", "ローカルファイルの行範囲だけ送る", "Send only a line range of the local file"},
			{"-m, --message <メモ>", "変更メモ(変更履歴のコミットメッセージ)", "Change message"},
			{"-t, --title <題> / -d, --description <説明>", "タイトル・説明を変える", "Change title / description"},
		}, optsVis...),
		ex: []string{"bin edit 483b71e6d6cf3c84 main.go -m \"fix\"", "bin edit 483b71e6d6cf3c84 --remove old.go", "bin edit 483b71e6d6cf3c84 main.go:70-84"},
	},
	"patch": {
		usage: []string{"bin patch <id> <file> --append|--insert N|--replace A-B|--delete A-B [--text <文字>|--from <file>[:A-B]|-]"},
		ja:    "ファイルの特定の行だけを追加・置換・削除します。無いファイルは新規作成します。本文は --text、--from <ファイル>[:A-B]、または `-`(標準入力)で渡します。",
		en:    "Change only some lines of a file: append, insert, replace or delete. A missing file is created. Give the text with --text, --from <file>[:A-B], or `-` (stdin).",
		opts: []helpOpt{
			{"--append", "末尾に追加する", "Append at the end"},
			{"--insert N", "N行目の前に挿入する", "Insert before line N"},
			{"--after N / --before N", "N行目の後ろ・前に挿入する", "Insert after / before line N"},
			{"--replace A-B", "A〜B行を置き換える", "Replace lines A to B"},
			{"--delete A-B", "A〜B行を削除する", "Delete lines A to B"},
			{"--text <文字>", "渡す本文", "Text to use"},
			{"--from <file>[:A-B]", "ローカルファイル(の行範囲)を本文にする", "Use a local file (line range) as the text"},
			{"--dry-run", "保存せず、変更内容だけ表示する", "Show the change without saving"},
			{"-m, --message <メモ>", "変更メモ", "Change message"},
		},
		ex: []string{"bin patch 483b71e6d6cf3c84 main.go --replace 3-5 --text \"new\"", "echo x | bin patch 483b71e6d6cf3c84 log.txt --append -"},
	},
	"clone": {
		usage: []string{"bin clone <id> [<dir>] [--force]"},
		ja:    "Gistのファイルを、フォルダ構成ごとダウンロードします。",
		en:    "Download a gist's files, keeping the folder layout.",
		opts:  []helpOpt{{"--force", "既にあるファイルを上書きする", "Overwrite existing files"}},
	},
	"fork": {
		usage: []string{"bin fork <id> [-t <題>] [-d <説明>] [--public|--unlisted|--private]"},
		ja:    "Gistをコピーして、自分の新しいGistを作ります(自分のGistも可)。公開範囲は元のままです。",
		en:    "Copy a gist into a new gist of your own (your own gists too). Visibility stays as the original.",
		opts:  append([]helpOpt{{"-t, --title <題> / -d, --description <説明>", "フォーク後のタイトル・説明", "Title / description for the fork"}}, optsVis...),
	},
	"forks": {
		usage: []string{"bin forks <id>"},
		ja:    "Gistのフォーク一覧を表示します(公開のものと自分のもの)。",
		en:    "List forks of a gist (public ones and yours).",
	},
	"log": {
		usage: []string{"bin log <id> [-p]"},
		ja:    "変更履歴を git log 風に表示します。",
		en:    "Show the change history like git log.",
		opts:  []helpOpt{{"-p", "各リビジョンの差分も表示する", "Show the diff of each revision"}},
	},
	"diff": {
		usage: []string{"bin diff <id> [<file>] [--rev <リビジョン|ハッシュ>]", "bin diff <ローカルfile> <id>"},
		ja:    "ローカルとGist、またはリビジョン間の差分を表示します。",
		en:    "Show differences between local files and a gist, or between revisions.",
		opts:  []helpOpt{{"--rev <rev>", "比較するリビジョン番号・ハッシュ", "Revision number or hash to compare with"}},
	},
	"delete": {
		usage: []string{"bin delete <id> [-y]"},
		ja:    "Gistを削除します。元に戻せないため確認が出ます。",
		en:    "Delete a gist. Asks first because it cannot be undone.",
		opts:  []helpOpt{{"-y", "確認を省略する", "Skip the confirmation"}},
	},
	"restore": {
		usage: []string{"bin restore <id> <リビジョン|ハッシュ|~N> [--dry-run] [-m <メモ>]"},
		ja:    "Gistを過去のリビジョンの内容に戻します(新しいリビジョンとして積まれます)。~1 は1つ前です。",
		en:    "Restore a gist to an earlier revision (recorded as a new revision). ~1 means one back.",
		opts:  []helpOpt{{"--dry-run", "戻さずに内容だけ確認する", "Preview without restoring"}, {"-m, --message <メモ>", "変更メモ", "Change message"}},
	},
	"undo": {
		usage: []string{"bin undo [<id>] [--dry-run]"},
		ja:    "直前の変更を取り消します(bin restore <id> ~1 と同じ)。id を省略すると最後に使ったGistが対象です。",
		en:    "Undo the last change (same as bin restore <id> ~1). Without id, uses the last gist you touched.",
		opts:  []helpOpt{{"--dry-run", "戻さずに確認する", "Preview only"}},
	},
	"mv": {
		usage: []string{"bin mv <id> <旧パス> <新パス>"},
		ja:    "Gist内のファイル名(パス)を変更します。",
		en:    "Rename (move) a file inside a gist.",
	},
	"cp": {
		usage: []string{"bin cp <id> <file> [<新しい名前> | --to <id> [--as <名前>] | --new [--as <名前>]]"},
		ja:    "Gist内のファイルを複製します。別のGist(--to)や新しいGist(--new)へもコピーできます。",
		en:    "Copy a file within a gist, to another gist (--to) or into a new gist (--new).",
		opts: append([]helpOpt{
			{"--to <id>", "コピー先のGist", "Destination gist"},
			{"--new", "新しいGistとして作る", "Create a new gist"},
			{"--as <名前>", "コピー先でのファイル名", "File name at the destination"},
			{"-m, --message <メモ>", "変更メモ", "Change message"},
		}, optsVis...),
	},
	"open": {
		usage: []string{"bin open [<id>] [-f <file>] [--raw]"},
		ja:    "Gistをブラウザで開きます。id を省略すると最後に使ったGistです。",
		en:    "Open a gist in the browser. Without id, uses the last gist.",
		opts:  []helpOpt{{"-f, --filename <名前>", "そのファイルのページを開く", "Open that file's page"}, {"--raw", "生テキストのURLを開く", "Open the raw URL"}},
	},
	"copy": {
		usage: []string{"bin copy [<id>] [-f <file>] [--url]"},
		ja:    "Gistの内容(--url ならURL)をクリップボードにコピーします。",
		en:    "Copy a gist's content (or its URL with --url) to the clipboard.",
		opts:  []helpOpt{{"-f, --filename <名前>", "コピーするファイル", "File to copy"}, {"--url", "内容ではなくURLをコピーする", "Copy the URL instead of the content"}},
	},
	"share": {
		usage: []string{"bin share [<id>] [-f <file>] [--no-qr]"},
		ja:    "共有用のURLを表示します(端末にQRコードも出します)。",
		en:    "Print a share URL, with a QR code in the terminal.",
		opts:  []helpOpt{{"-f, --filename <名前>", "そのファイルのURLにする", "Use that file's URL"}, {"--no-qr", "QRコードを出さない", "Do not show the QR code"}},
	},
	"search": {
		usage: []string{"bin search <文字> [-u <handle>] [-l <lang>] [--since <期間>] [-n <数>]"},
		ja:    "本文も対象にしてGistを検索します。一致した最初の行も表示します。",
		en:    "Search gists including file contents; shows the first matching line.",
		opts:  optsFilter,
	},
	"watch": {
		usage: []string{"bin watch [<id>] <file|dir>... [-t <題>] [-d <説明>] [--public|--unlisted|--private] [--mirror] [--interval <秒>]"},
		ja:    "ファイルの変更を監視して、変わるたびにGistへ自動で反映します。id を省略すると新しいGistを作ります。Ctrl+C で止まります。",
		en:    "Watch files and push every change to the gist automatically. Without id, a new gist is created. Stop with Ctrl+C.",
		opts: append([]helpOpt{
			{"--mirror", "ローカルで消したファイルをGistからも消す", "Also remove files from the gist when deleted locally"},
			{"--interval <秒>", "変更を確認する間隔", "Polling interval in seconds"},
		}, optsVis...),
	},
	"alias": {
		usage: []string{"bin alias set <名前> <id>", "bin alias list", "bin alias rm <名前>"},
		ja:    "Gistの別名を登録します。以後 `bin edit <名前>` のように、IDの代わりに使えます。",
		en:    "Register a short name for a gist and use it instead of the ID, e.g. `bin edit <name>`.",
		ex:    []string{"bin alias set notes 483b71e6d6cf3c84", "bin view notes"},
	},
	"visibility": {
		usage: []string{"bin visibility <public|unlisted|private> [<id>...] [-q <文字> -l <言語> --since <期間> --tag <タグ>] [-y]"},
		ja:    "複数のGistの公開範囲をまとめて変えます。ID を並べるか、検索条件で対象を選びます。",
		en:    "Change visibility of many gists. Give IDs or select them with filters.",
		opts:  append([]helpOpt{{"-y", "確認を省略する", "Skip the confirmation"}}, optsFilter[2:]...),
	},
	"bulk": {
		usage: []string{"bin bulk <delete|visibility|tag|star|unstar> [<id>...] [絞り込み] [-y]"},
		ja:    "複数のGistにまとめて操作します。ID を並べるか、検索条件(-q, -l, --since, --tag)で対象を選びます。",
		en:    "Run one action on many gists. Give IDs or select them with filters (-q, -l, --since, --tag).",
		opts:  append([]helpOpt{{"-y", "確認を省略する", "Skip the confirmation"}}, optsFilter[2:]...),
		ex:    []string{"bin bulk delete -q tmp --since 30d", "bin bulk tag id1 id2 demo"},
	},
	"status": {
		usage: []string{"bin status"},
		ja:    "同期しているフォルダ(bin pull で取得したもの)の、Gistとの差を表示します。",
		en:    "Show how a synced folder (from bin pull) differs from its gist.",
	},
	"pull": {
		usage: []string{"bin pull [--force]"},
		ja:    "同期フォルダを、Gistの最新の内容に更新します。手元に未送信の変更があるときは止まります。",
		en:    "Update a synced folder from the gist. Stops if you have unsent local changes.",
		opts:  []helpOpt{{"-f, --force", "手元の変更を捨てて上書きする", "Discard local changes and overwrite"}},
	},
	"push": {
		usage: []string{"bin push [-m <メモ>] [--force]"},
		ja:    "同期フォルダの変更をGistへ送ります。",
		en:    "Send a synced folder's changes to the gist.",
		opts:  []helpOpt{{"-m, --message <メモ>", "変更メモ", "Change message"}, {"--force", "Gist側の新しい変更があっても上書きする", "Overwrite even if the gist changed"}},
	},
	"sync": {
		usage: []string{"bin sync [-m <メモ>]"},
		ja:    "pull と push をまとめて行います。",
		en:    "Pull and then push in one step.",
		opts:  []helpOpt{{"-m, --message <メモ>", "変更メモ", "Change message"}},
	},
	"run": {
		usage: []string{"bin run <id> [-f <file>] [-y] [-- <引数>...]"},
		ja:    "Gistのスクリプトをダウンロードして実行します。他人のコードを実行するため、確認が出ます。",
		en:    "Download and run a script from a gist. Asks first, since it runs someone else's code.",
		opts:  []helpOpt{{"-f, --filename <名前>", "実行するファイル", "File to run"}, {"-y", "確認を省略する", "Skip the confirmation"}, {"-- <引数>", "スクリプトに渡す引数", "Arguments for the script"}},
	},
	"star": {
		usage: []string{"bin star <id>"},
		ja:    "Gistにスターを付けます。",
		en:    "Star a gist.",
	},
	"unstar": {
		usage: []string{"bin unstar <id>"},
		ja:    "スターを外します。",
		en:    "Remove your star.",
	},
	"starred": {
		usage: []string{"bin starred [-u <handle>] [-n <数>]"},
		ja:    "スターを付けたGistの一覧を表示します。",
		en:    "List starred gists.",
		opts:  optsFilter[:2],
	},
	"tag": {
		usage: []string{"bin tag <id> [<タグ>...] [--clear]"},
		ja:    "Gistのタグを設定します(最大10個)。タグを渡さなければ今のタグを表示します。",
		en:    "Set a gist's tags (up to 10). With no tags, shows the current ones.",
		opts:  []helpOpt{{"--clear", "タグをすべて外す", "Remove all tags"}},
	},
	"tags": {
		usage: []string{"bin tags"},
		ja:    "自分のGistで使っているタグを、件数つきで一覧にします。",
		en:    "List the tags you use, with counts.",
	},
	"scan": {
		usage: []string{"bin scan <id|file|dir>..."},
		ja:    "トークンや秘密鍵らしい文字列が含まれていないかを調べます。公開前の確認に使います(誤検出もあります)。",
		en:    "Check for token- or private-key-like strings before publishing (false positives possible).",
	},
	"comment": {
		usage: []string{"bin comment list <id>", "bin comment add <id> <本文> [-f <file> --line A[-B]]", "bin comment edit <コメントID> <本文>", "bin comment rm <コメントID>"},
		ja:    "Gistのコメントを読む・書く・編集・削除します。行コメントは -f と --line で位置を指定します。要ログイン(書く場合)。",
		en:    "Read, write, edit or delete gist comments. Line comments use -f and --line. Login needed to write.",
		ex:    []string{"bin comment add 483b71e6d6cf3c84 \"ありがとう\" -f main.go --line 12-14"},
	},
	"collection": {
		usage: []string{"bin collection list [-u <handle>]", "bin collection show <id>", "bin collection create <題> [--public|--unlisted|--private]", "bin collection add <コレクション> <gist>", "bin collection remove <コレクション> <gist>", "bin collection delete <コレクション>"},
		ja:    "Gistをまとめるコレクションを操作します。",
		en:    "Manage collections of gists.",
	},
	"stats": {
		usage: []string{"bin stats <id> [--days <日数>]"},
		ja:    "Gistの閲覧数・ダウンロード数を日ごとに表示します(持ち主のみ)。",
		en:    "Show daily views and downloads of a gist (owner only).",
		opts:  []helpOpt{{"--days <日数>", "さかのぼる日数", "Number of days to show"}},
	},
	"token": {
		usage: []string{"bin token create <名前> [--read] [--expires <日数>]", "bin token list", "bin token revoke <id>"},
		ja:    "CI・スクリプト用のAPIトークン(sbt_…)を発行・一覧・失効します。環境変数 BIN_TOKEN に入れるとログイン不要で使えます。",
		en:    "Issue, list or revoke API tokens (sbt_…) for CI and scripts. Set BIN_TOKEN to use them without login.",
		opts:  []helpOpt{{"--read", "読み取り専用にする", "Read-only token"}, {"--expires <日数>", "有効日数(省略すると無期限)", "Days until it expires (default: never)"}},
	},
	"g": {
		usage: []string{"bin g <clone|init|status|add|commit|push|log|diff|show|pull|fetch> ..."},
		ja:    "git と同じ使い方でGistを扱います(origin=Gist、ブランチは main)。ステージ・コミットは手元に貯め、push で1コミット=1リビジョンとして保存します。オプションも git とほぼ同じ(--oneline, -p, --stat, --cached など)。",
		en:    "Use gists with git-like commands (origin = gist, branch main). Commits stay local until push, which saves one revision per commit. Options mirror git (--oneline, -p, --stat, --cached ...).",
		ex:    []string{"bin g clone 483b71e6d6cf3c84", "bin g add . && bin g commit -m x && bin g push", "bin g help"},
	},
	"version": {
		usage: []string{"bin version"},
		ja:    "CLIの版を表示します。",
		en:    "Show the CLI version.",
	},
	"try": {
		usage: []string{"bin try <create|view|edit|delete|list|token> ..."},
		ja:    "ログインなしで使える匿名ペースト(https://bin.lapius7.com/try)。ログイン済みのGistとは別の場所に保存され、1〜100日で自動的に削除されます。合計30MB・100ファイルまで。誰でもURLを知っていれば読めます。作成時の編集用トークンはこの端末に保存され、編集・削除に使います。",
		en:    "Anonymous pastes without login (https://bin.lapius7.com/try). Stored apart from logged-in gists and deleted automatically after 1-100 days. Up to 30MB total, 100 files. Anyone with the URL can read it. The edit token is saved on this device and used to edit or delete.",
		subs:  []string{"create", "view", "edit", "delete", "list", "token"},
		ex:    []string{"bin try create main.go --days 30", "bin try view <id>"},
	},
	"try create": {
		usage: []string{"bin try create <file|dir>... [-t <題>] [--days <日数>]", "cat x | bin try create -f x.txt"},
		ja:    "ファイルや標準入力から匿名ペーストを作ります。共有URLが標準出力に、編集用トークンは標準エラーに出ます(トークンはこの端末にも保存されます)。",
		en:    "Create an anonymous paste from files or stdin. The share URL goes to stdout and the edit token to stderr (also saved on this device).",
		opts: append([]helpOpt{
			{"-t, --title <題>", "タイトル(省略可)", "Title (optional)"},
			{"--days <日数>", "保存期間。1〜100日、既定は7日。期限が来ると自動で削除される", "Retention, 1-100 days (default 7); deleted automatically afterwards"},
		}, optsFiles...),
		ex: []string{"bin try create main.go --days 30", "bin try create ./ --days 1 --exclude \"*.log\" --exclude \"tmp/\"", "echo hi | bin try create -f note.txt -t メモ", "url=$(bin try create a.txt 2>/dev/null)"},
	},
	"try view": {
		usage: []string{"bin try view <id|URL> [-f <file>] [--raw]"},
		ja:    "匿名ペーストの内容を表示します。ID は20桁、または https://bin.lapius7.com/try/<id> のURLで指定します。",
		en:    "Show an anonymous paste. Give the 20-character ID or a https://bin.lapius7.com/try/<id> URL.",
		opts:  []helpOpt{{"-f, --filename <名前>", "そのファイルだけを装飾なしで出力する", "Print just that file"}, {"--raw", "装飾なしで中身だけを出力する", "Print contents only"}},
	},
	"try edit": {
		usage: []string{"bin try edit <id|URL> [<file|dir>...] [-t <題>] [--remove <path>] [--token <トークン>]"},
		ja:    "匿名ペーストにファイルを追加・上書き・削除します。作成した端末では保存済みのトークンが使われます。他の端末では --token で渡します。",
		en:    "Add, overwrite or remove files in an anonymous paste. The saved token is used on the device that created it; elsewhere pass --token.",
		opts: append([]helpOpt{
			{"--remove <path>", "ファイルを削除する(複数指定可)", "Remove a file (repeatable)"},
			{"-t, --title <題>", "タイトルを変える", "Change the title"},
			{"--token <トークン>", "編集用トークン", "Edit token"},
		}, optsFiles...),
	},
	"try delete": {
		usage: []string{"bin try delete <id|URL> [-y] [--token <トークン>]"},
		ja:    "匿名ペーストを削除します。元に戻せないため確認が出ます。",
		en:    "Delete an anonymous paste. Asks first because it cannot be undone.",
		opts:  []helpOpt{{"-y, --yes", "確認を省略する", "Skip the confirmation"}, {"--token <トークン>", "編集用トークン", "Edit token"}},
	},
	"try list": {
		usage: []string{"bin try list"},
		ja:    "この端末で作った匿名ペーストの一覧(URL・タイトル・期限)を表示します。",
		en:    "List anonymous pastes created on this device (URL, title, expiry).",
	},
	"try token": {
		usage: []string{"bin try token [<id|URL>]"},
		ja:    "この端末に保存してある編集用トークンを表示します。",
		en:    "Show the edit token saved on this device.",
	},
}

var helpAlias = map[string]string{
	"new": "create", "ls": "list", "tl": "timeline", "cat": "view", "show": "view", "download": "clone",
	"rm": "delete", "rename": "mv", "copy-file": "cp", "yank": "copy", "find": "search", "aliases": "alias",
	"vis": "visibility", "st": "status", "history": "log", "stars": "starred", "check": "scan",
	"comments": "comment", "col": "collection", "tokens": "token", "git": "g", "-v": "version", "--version": "version",
}

func helpKey(words []string) (string, bool) {
	var ws []string
	for _, w := range words {
		if w == "--" || strings.HasPrefix(w, "-") && w != "-v" {
			break
		}
		if a, ok := helpAlias[w]; ok && len(ws) == 0 {
			w = a
		} else if ok && len(ws) == 1 {
			w = a
		}
		ws = append(ws, w)
		if len(ws) == 2 {
			break
		}
	}
	for n := len(ws); n >= 1; n-- {
		k := strings.Join(ws[:n], " ")
		if _, ok := helpDocs[k]; ok {
			return k, true
		}
	}
	return "", false
}

// wantsHelp は -h / --help が(-- より前に)あるか
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func showHelp(words []string) bool {
	key, ok := helpKey(words)
	if !ok {
		return false
	}
	d := helpDocs[key]
	fmt.Println(bold("bin " + key))
	fmt.Println()
	fmt.Println("  " + d.desc())
	fmt.Println()
	fmt.Println(bold(T("使い方")))
	for _, u := range d.usage {
		fmt.Println("  " + cyan(u))
	}
	fmt.Println()
	if len(d.subs) > 0 {
		fmt.Println(bold(T("サブコマンド")))
		for _, s := range d.subs {
			sub := helpDocs[key+" "+s]
			fmt.Printf("  %s  %s\n", cyan(padLabel("bin "+key+" "+s, 16)), helpFirstSentence(sub.desc()))
		}
		fmt.Println()
		fmt.Println(dim(fmt.Sprintf(T("詳しくは bin %s <サブコマンド> --help"), key)))
		fmt.Println()
	}
	if len(d.opts) > 0 {
		fmt.Println(bold(T("オプション")))
		w := 0
		for _, o := range d.opts {
			if n := displayWidth(o.flag); n > w {
				w = n
			}
		}
		if w > 34 {
			w = 34
		}
		for _, o := range d.opts {
			if displayWidth(o.flag) > w {
				fmt.Printf("  %s\n  %s  %s\n", cyan(o.flag), spaces(w), o.text())
			} else {
				fmt.Printf("  %s  %s\n", cyan(padLabel(o.flag, w)), o.text())
			}
		}
		fmt.Println()
	}
	if len(d.ex) > 0 {
		fmt.Println(bold(T("例")))
		for _, e := range d.ex {
			fmt.Println("  " + dim("$") + " " + e)
		}
		fmt.Println()
	}
	return true
}

func helpFirstSentence(s string) string {
	for _, sep := range []string{"。", ". "} {
		if i := strings.Index(s, sep); i >= 0 {
			return s[:i+len(sep)]
		}
	}
	return s
}

// helpHintFor は usage 文字列から `bin help <cmd>` の案内を作る
func helpHintFor(usage string) string {
	f := strings.Fields(usage)
	if len(f) < 2 || f[0] != "bin" {
		return ""
	}
	key := f[1]
	if len(f) > 2 && !strings.ContainsAny(f[2], "<[|-") {
		if _, ok := helpDocs[key+" "+f[2]]; ok {
			key += " " + f[2]
		}
	}
	return fmt.Sprintf(T("詳しくは: bin help %s"), key)
}
