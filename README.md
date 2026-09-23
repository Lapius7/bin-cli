# bin — bin.lapius7.com CLIクライアント

[LapBin](https://bin.lapius7.com/)(Lapountでログインできるコード共有サービス)を、ターミナルの`bin`コマンドから使うためのCLI。Go標準ライブラリのみ、依存パッケージなし。

## インストール

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
bin clone <id> [dir]                        # ファイルをダウンロード
bin delete <id>                             # 削除(確認あり、-yで省略)
```

`<id>`にはGistのURLもそのまま渡せる。

## 仕組み

- ログインはsca-cliと同じ2方式。`bin login`はローカルにランダムポートの一時HTTPサーバーを立て、`oauth-connect-token`(mint)で`http://127.0.0.1:<port>/callback`向けの使い捨てトークンを発行して`/oauth/v2/authorize?token=...`を開いてもらう。`--device`はdevice code方式(`oauth-device-code`関数)
- 通信はすべて`https://bin.lapius7.com/api/`(bin-server内のリバースプロキシ)経由。ANON_KEYはプロキシだけが付与するので、このCLIには一切含まれない
- Gistの読み書きは`bin`スキーマのDB関数(`get_gist`/`list_gists`/`save_gist`/`delete_gist`)を呼ぶだけ。権限チェックはDB側(RLS+SECURITY DEFINER関数)で行われる
- セッションは`~/.config/bin/session.json`(0600)。アクセストークン失効時は保存済みのrefresh_tokenで自動更新する

## ビルド・配布

```bash
./build.sh    # 全OS/CPU向けにクロスコンパイルし、web/bin.lapius7.com/server/cli-dist/ へinstall.shと一緒に配置
```

bin-serverがそのディレクトリを`/install.sh`・`/cli/*`として配信するので、再起動は不要。

## 表示言語

日本語・英語・韓国語に対応。`BIN_LANG`(ja / en / ko)、無ければ `LC_ALL`・`LC_MESSAGES`・`LANG` から判断する(未設定・C・POSIXは日本語、それ以外の未対応言語は英語)。インストーラーも同じ規則。

文言は日本語の文字列をそのままキーにして `T("…")` で包み(gettext方式)、英語・韓国語は `cmd/bin/messages.go` に持つ。新しい文言を足したら対応表にも追加すること(無いとその言語でも日本語のまま表示される)。英語の単数形は `Tn()` と `messagesOne`。

開発中のサーバーに向けたい場合は`BIN_SUPABASE_URL`/`BIN_ANON_KEY`/`BIN_SITE_URL`(環境変数または`~/.config/bin/config.env`)で上書きできる。
