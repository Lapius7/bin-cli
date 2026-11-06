# bin — bin.lapius7.com CLIクライアント

[LapBin](https://bin.lapius7.com/)(Lapountでログインできるコード共有サービス)を、ターミナルの`bin`コマンドから使うためのCLI。Go標準ライブラリのみ、依存パッケージなし。

## インストール

npm(Linux / macOS / Windows):

```bash
pnpm add -g @lapius/bin-cli
```

OS/CPUに合ったビルド済みバイナリが入る(Linux/macOSでは初回の起動時にnodeのシムがネイティブバイナリに置き換わり、以後はnodeを経由しない)。更新は同じコマンドで行う。

npmがない場合:

```bash
curl -fsSL https://bin.lapius7.com/install.sh | bash
```

Goは不要。OS/CPUに合ったビルド済みバイナリを`~/.local/bin/bin`に置く(`BIN_INSTALL_DIR`で変更可)。Windowsは`https://bin.lapius7.com/cli/bin-windows-amd64.exe`を直接ダウンロードする。

## 使い方

```bash
bin login                                   # ブラウザでLapountにログイン
bin login --device                          # SSH先など、手元でブラウザが開けない場合
bin create main.go util.go -d "説明"         # 作成(既定は限定公開)。URLだけが標準出力に出る
cat build.log | bin create -f build.log --private
bin list                                    # 自分のGist(-u <handle> で他人の公開Gist)
bin view <id>                               # 表示(-f <file> で1ファイルだけ生出力)
bin edit <id> main.go --remove old.go -t "新しいタイトル" --public
bin patch <id> main.go --insert 3 --text "// 追加"   # 特定の行だけ追加(--append / --replace 4-6 / --delete 2-3 / --after "文字列" / --before "文字列")
bin edit <id> js/script.js --lines 70-82 --as script.js  # 行範囲とGist上の名前は別オプションでも指定できる(file:A-B=名前 と同じ)
bin edit <id> a.js --lines 1-9 --as x.js b.js --lines 5- --as y.js  # 複数ファイルでは各ファイルの直後に付けると、そのファイルだけに効く
bin patch <id> new.js --append --from main.go:70-84  # 無いファイルは追加系なら新規作成。本文は --text・--from <ファイル>[:A-B]・`-`(標準入力)
bin create main.go:70-84 / bin edit <id> main.go:70-=part.js   # ローカルの行範囲だけ送る(70- は末尾まで、-20 は先頭から、=名前でGist上の名前)
bin clone <id> [dir]                        # ファイルをダウンロード
bin fork <id> -t "派生版"                     # コピーして自分の新しいGistを作る(自分のGistも可、公開範囲は元のまま)
bin delete <id>                             # 削除(確認あり、-yで省略)
bin delete <id1> <id2> -y                  # 複数まとめて削除(--tag / -q / --since で絞り込みも可、--dry-run で確認のみ)
bin diff [<id>] [file]                     # ローカルと Gist の差分(--rev ~1 で過去版と比較)
bin mv <id> old.js new.js                  # ファイル名変更
bin cp <id> a.js b.js                      # コピー(--to <id> / --new で別 Gist へ)
bin undo [<id>] / bin restore <id> ~2      # 直前の変更を取り消す / 過去のリビジョンへ戻す(--dry-run あり)
bin open|copy|share [<id>]                 # ブラウザで開く / クリップボードへ / URL・埋め込み・QR を表示
bin search <text>                          # 自分の Gist を全文検索
bin watch main.js                          # 新規 Gist を作り、保存するたびに自動反映
bin watch ./src README.md -t "メモ"         # フォルダ・複数ファイルも監視(消したファイルも反映)
bin watch <id> main.js                     # 既存 Gist へ反映(--mirror で手元に無いファイルを消す)
bin alias set blog <id>                    # 短い名前を付ける(以降 bin edit blog …)
bin visibility private --tag draft         # 公開範囲を一括変更 / bin bulk tag --add x
bin edit <id> --editor                     # $EDITOR で編集して反映
bin create --clipboard -f note.txt         # クリップボードから作成

# ログイン不要の匿名ペースト(/try。合計30MB・100ファイル、保存1〜100日)
bin try create main.go --days 30             # 共有URLを作る(既定7日)。編集用トークンはこの端末に保存
echo hi | bin try create -f note.txt -t メモ  # 標準入力からも可
bin try view <id|url> [-f <file>]            # 表示
bin try edit <id|url> new.go --remove old.go # 追加・上書き・削除(--token <トークン> で他の端末からも)
bin try delete <id|url> -y                   # 削除
bin try list / bin try token <id>            # 作成済み一覧 / トークン表示
bin create dir/ --exclude "*.log"          # --include / --exclude / .binignore で絞り込み
bin patch <id> a.js --replace @init --text "..."  # 行番号の代わりに @関数名 や /開始/,/終了/ も指定可
bin star <id> / bin starred               # スター・スターしたGistの一覧
bin tag <id> python cli                     # タグを設定(bin list --tag python で絞り込み)
bin tags                                    # 自分のタグ一覧(-u <handle> で他人)
bin scan [<file|dir>... | <id>]             # Token・秘密鍵の混入チェック(見つかれば終了コード1。保存時にも警告)
bin comment list|add|edit|rm ...            # コメント(add は -f <file> --line 5-7 で行コメント)
bin collection list|show|create|add|remove|delete ...  # コレクション
bin stats <id> [--days 30]                  # 閲覧・ダウンロード数(自分のGistのみ)
bin forks <id>                              # フォーク一覧
bin run <id>                                # スクリプトを実行(実行前に内容を表示して確認)
bin version                                 # バージョン表示。配布中の最新版かどうかも確かめる
```

`<id>`にはGistのURLもそのまま渡せる。

## AIエージェントに使わせる

Claude Code や Codex などのAIに `bin` の使い方を教えるには、次の文をそのまま渡す。

```text
https://bin.lapius7.com/cli/llms.txt を読んで、bin CLI の使い方を把握してください。
そのうえで、次の作業を bin コマンドで行ってください: <やりたいこと>
```

`llms.txt` は、AI向けに標準出力の扱い、非対話での認証(`BIN_TOKEN`)、公開範囲の既定値、コマンド一覧、制限をまとめたもの。ファイルとして渡す場合は `curl -fsSL https://bin.lapius7.com/cli/llms.txt -o llms.txt` で取得する。CIやエージェントには、読み取り専用などに絞った `bin token create` のトークンを `BIN_TOKEN` で渡す。

### 手元のフォルダと同期

```bash
bin clone <id> && cd <id>   # .lapbin.json(取得した時点のリビジョンと各ファイルのSHA-256)も置かれる
bin status                  # 手元の変更と、Gist側に新しいリビジョンがあるか
bin pull                    # Gist側の変更を取り込む(両方で変えたファイルがあれば止まる。--forceでGist側を優先)
bin push -m "メモ"          # 手元のフォルダの内容でGistを更新(Gist側が先に更新されていたら止まる)
bin sync                    # pull してから、手元の変更があれば push
```

git でも取得できる(読み取り専用): `git clone https://bin.lapius7.com/<id>.git`。非公開のGistはパスワードにAPIトークン。

### git と同じ操作感で使う(`bin g`)

`bin g <gitのサブコマンド>` で、git とほぼ同じ引数・出力のまま Gist を扱える(既存の `bin status` などはそのまま)。
origin=Gist、ブランチは `main` だけ。コミットは手元(`.lapbin/`)に貯まり、`push` の時に1コミット=1リビジョンとして保存される。

```bash
bin g clone https://bin.lapius7.com/<id>.git && cd <id>
bin g status -sb
bin g add -A && bin g commit -m "fix"     # -am、--amend(未pushのみ)、-m なしでエディタも可
bin g log --oneline --stat -5             # Gistのリビジョンもコミットとして並ぶ
bin g diff --cached / bin g diff HEAD~2 --stat / bin g show HEAD:main.py
bin g pull                                # 未pushのコミットはGist側の変更の上に積み直す
bin g push                                # Gist側が先に進んでいたら拒否(-f で上書き)

bin g init my-snippets && cd my-snippets  # 新規: 最初の push で限定公開のGistを作成(--public/--private)
```

対応: `init clone status add rm mv restore checkout switch reset commit log show diff fetch pull push remote branch stash rev-parse ls-files clean`。
`git config` の alias(例: `alias.st=status` → `bin g st`)、`-C <dir>`、`--no-pager`、ページャ(`GIT_PAGER`/`PAGER`)、ルートの `.gitignore` にも対応。
merge・rebase・tag などGistに無い概念は使えない(Gistのタグは `bin tag`)。

### APIトークン(CI・スクリプト用)

```bash
bin token create ci --expires 90     # 発行(トークンは標準出力にだけ出る。--read で読み取り専用)
BIN_TOKEN=sbt_… bin list            # bin login の代わりに使う
bin token list / bin token revoke <id>
```

トークンで使えるのはLapBinの操作だけ(bin-serverが許可リストで制限し、持ち主として動く短命のJWTに差し替えて中継する)。発行・失効は bin login したセッションからのみ。Webの /settings/tokens でも管理できる。

## 仕組み

- ログインはsca-cliと同じ2方式。`bin login`はローカルにランダムポートの一時HTTPサーバーを立て、`oauth-connect-token`(mint)で`http://127.0.0.1:<port>/callback`向けの使い捨てトークンを発行して`/oauth/v2/authorize?token=...`を開いてもらう。`--device`はdevice code方式(`oauth-device-code`関数)
- 通信はすべて`https://bin.lapius7.com/api/`(bin-server内のリバースプロキシ)経由。ANON_KEYはプロキシだけが付与するので、このCLIには一切含まれない
- Gistの読み書きは`bin`スキーマのDB関数(`get_gist`/`list_gists`/`save_gist`/`delete_gist`)を呼ぶだけ。権限チェックはDB側(RLS+SECURITY DEFINER関数)で行われる
- セッションは`~/.config/bin/session.json`(0600)。アクセストークン失効時は保存済みのrefresh_tokenで自動更新する

## ビルド・配布

```bash
./build.sh    # 全OS/CPU向けにクロスコンパイルし、web/bin.lapius7.com/server/cli-dist/ へinstall.shと一緒に配置
```

バージョンは`日付-コミットの短縮ハッシュ`(未コミットの変更があれば`-dirty`付き)。同時に`VERSION`と`build.json`(バージョン・ビルド日時・コミット・各バイナリのSHA-256)も書き出し、https://bin.lapius7.com/cli の「配布中の最新版」はこれを読んで表示する。インストーラーは最後に入ったバイナリの`bin version`を実行して、配布中の版と一致するか照合する。

bin-serverがそのディレクトリを`/install.sh`・`/cli/*`として配信するので、再起動は不要。

npm版は`v*`タグをpushするとGitHub Actions(`.github/workflows/release.yml`)が`npm/build.mjs`でビルドしてnpmに公開し、GitHub Releaseにもバイナリを添付する(npmのTrusted Publisherで認証するのでトークンは不要)。手元での確認は`node npm/build.mjs 0.0.0-dev --pack`。
npm版は`-X main.channel=npm`付きでビルドされ、`bin version`の最新版確認と更新方法の案内がnpm向けになる。

## 表示言語

日本語・英語・韓国語に対応。`BIN_LANG`(ja / en / ko)、無ければ `LC_ALL`・`LC_MESSAGES`・`LANG` から判断する(未設定・C・POSIXは日本語、それ以外の未対応言語は英語)。インストーラーも同じ規則。

文言は日本語の文字列をそのままキーにして `T("…")` で包み(gettext方式)、英語・韓国語は `cmd/bin/messages.go` に持つ。新しい文言を足したら対応表にも追加すること(無いとその言語でも日本語のまま表示される)。英語の単数形は `Tn()` と `messagesOne`。

開発中のサーバーに向けたい場合は`BIN_SUPABASE_URL`/`BIN_ANON_KEY`/`BIN_SITE_URL`(環境変数または`~/.config/bin/config.env`)で上書きできる。
