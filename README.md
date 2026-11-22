<div align="center">

<p align="center"><b>English</b> | <a href="README.ja.md">日本語</a></p>

<h1 align="center">LapBin CLI</h1>

<h3 align="center">A CLI for bin.lapius7.com, from your terminal.</h3>

<p align="center">Use <a href="https://bin.lapius7.com/">LapBin</a> (a code-sharing service with Lapount login) through the <code>bin</code> command. Written in Go; ships as a single binary.</p>

<p align="center">
  <a href="https://github.com/Lapius7/bin-cli/releases"><img alt="Release" src="https://img.shields.io/github/v/release/Lapius7/bin-cli?label=release&style=flat-square&color=e8833a&logo=github"></a>
  <a href="https://www.npmjs.com/package/@lapius/bin-cli"><img alt="npm" src="https://img.shields.io/npm/v/@lapius/bin-cli?style=flat-square&color=cb3837&logo=npm"></a>
  <a href="https://go.dev"><img alt="Go" src="https://img.shields.io/badge/Go-1.22-00add8?logo=go&logoColor=white&style=flat-square"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Lapius7/bin-cli?style=flat-square&color=97ca00"></a>
  <a href="https://github.com/Lapius7/bin-cli/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/Lapius7/bin-cli?style=flat-square&color=e3b341&logo=github"></a>
  <a href="https://github.com/Lapius7/bin-cli/commits/main"><img alt="Last commit" src="https://img.shields.io/github/last-commit/Lapius7/bin-cli?label=last%20commit&style=flat-square&color=2f81f7"></a>
</p>

<p align="center"><a href="#install">Install</a> · <a href="#usage">Usage</a> · <a href="#use-it-from-ai-agents">AI agents</a> · <a href="#build-and-distribution">Build &amp; distribution</a></p>

</div>

---

## Install

With npm (Linux / macOS / Windows).

```bash
pnpm add -g @lapius/bin-cli
```

This installs a prebuilt binary for your OS and CPU. On Linux and macOS, the node shim replaces itself with the native binary on first run, so node is not involved afterwards. Update with the same command.

Without npm.

```bash
curl -fsSL https://bin.lapius7.com/install.sh | bash
```

Go is not needed. The installer puts a prebuilt binary at `~/.local/bin/bin` (change it with `BIN_INSTALL_DIR`). On Windows, download `https://bin.lapius7.com/cli/bin-windows-amd64.exe` directly.

## Usage

<details>
<summary>Command examples (all)</summary>

```bash
bin login                                   # log in to Lapount in the browser
bin login --device                          # when no browser is available locally, e.g. over SSH
bin create main.go util.go -d "description" # create (unlisted by default); only the URL goes to stdout
cat build.log | bin create -f build.log --private
bin list                                    # your gists (-u <handle> for someone else's public gists)
bin view <id>                               # show (-f <file> prints one file raw)
bin edit <id> main.go --remove old.go -t "new title" --public
bin patch <id> main.go --insert 3 --text "// added"   # change only some lines (--append / --replace 4-6 / --delete 2-3 / --after "text" / --before "text")
bin edit <id> js/script.js --lines 70-82 --as script.js  # a line range and the name on the gist can be given as separate options (same as file:A-B=name)
bin edit <id> a.js --lines 1-9 --as x.js b.js --lines 5- --as y.js  # with several files, options placed after a file apply to that file only
bin patch <id> new.js --append --from main.go:70-84  # a missing file is created for add operations. Body: --text, --from <file>[:A-B], or `-` (stdin)
bin create main.go:70-84 / bin edit <id> main.go:70-=part.js   # send only a local line range (70- = to the end, -20 = from the start, =name sets the name on the gist)
bin clone <id> [dir]                        # download the files
bin fork <id> -t "my variant"               # copy into a new gist of your own (works on your own gists too; visibility kept)
bin delete <id>                             # delete (asks first, -y skips)
bin delete <id1> <id2> -y                  # delete several (filter with --tag / -q / --since, --dry-run only checks)
bin diff [<id>] [file]                     # diff between local files and the gist (--rev ~1 compares with a past revision)
bin mv <id> old.js new.js                  # rename a file
bin cp <id> a.js b.js                      # copy (--to <id> / --new for another gist)
bin undo [<id>] / bin restore <id> ~2      # undo the last change / go back to a past revision (--dry-run available)
bin open|copy|share [<id>]                 # open in browser / copy to clipboard / show URL, embed and QR
bin search <text>                          # full-text search your gists
bin watch main.js                          # create a new gist and sync on every save
bin watch ./src README.md -t "notes"       # folders and several files too (deleted files are reflected)
bin watch <id> main.js                     # sync to an existing gist (--mirror removes files missing locally)
bin alias set blog <id>                    # give it a short name (then bin edit blog ...)
bin visibility private --tag draft         # change visibility in bulk / bin bulk tag --add x
bin edit <id> --editor                     # edit in $EDITOR and apply
bin create --clipboard -f note.txt         # create from the clipboard

# Anonymous paste without login (/try. 30 MB and 100 files total, kept 1 to 100 days)
bin try create main.go --days 30             # create a share URL (default 7 days). The edit token is saved on this machine
echo hi | bin try create -f note.txt -t memo  # stdin works too
bin try view <id|url> [-f <file>]            # show
bin try edit <id|url> new.go --remove old.go # add, overwrite, remove (--token <token> from another machine)
bin try delete <id|url> -y                   # delete
bin try list / bin try token <id>            # list what you created / show the token
bin create dir/ --exclude "*.log"          # narrow down with --include / --exclude / .binignore
bin patch <id> a.js --replace @init --text "..."  # instead of line numbers, @functionName or /start/,/end/ also work
bin star <id> / bin starred               # star / list starred gists
bin tag <id> python cli                     # set tags (filter with bin list --tag python)
bin tags                                    # your tags (-u <handle> for someone else's)
bin scan [<file|dir>... | <id>]             # check for leaked tokens and private keys (exit code 1 if found; also warns on save)
bin comment list|add|edit|rm ...            # comments (add takes -f <file> --line 5-7 for line comments)
bin collection list|show|create|add|remove|delete ...  # collections
bin stats <id> [--days 30]                  # views and downloads (your own gists only)
bin forks <id>                              # list forks
bin run <id>                                # run a script (shows the content and asks first)
bin version                                 # show the version, and whether it is the latest distributed
```

</details>

`<id>` also accepts a gist URL as is.

> [!NOTE]
> This README was rebuilt against the code and the `bin help` output. Operations that need a login, `bin link`, `bin g` and sync were not run for this rewrite.

## Use it from AI agents

To teach an AI such as Claude Code or Codex how to use `bin`, hand it the text below as is.

```text
Read https://bin.lapius7.com/cli/llms.txt and learn how to use the bin CLI.
Then do the following with bin commands: <what you want done>
```

`llms.txt` collects what an AI needs: how stdout behaves, non-interactive auth (`BIN_TOKEN`), default visibility, the command list, and limits. To pass it as a file, fetch it with `curl -fsSL https://bin.lapius7.com/cli/llms.txt -o llms.txt`. For CI and agents, give a `bin token create` token narrowed to read-only or similar through `BIN_TOKEN`.

### Sync with a local folder

```bash
bin clone <id> && cd <id>   # also writes .lapbin.json (the revision at fetch time and each file's SHA-256)
bin status                  # local changes, and whether the gist has a newer revision
bin pull                    # take in changes from the gist (stops if both sides changed a file; --force prefers the gist)
bin push -m "note"          # update the gist from the local folder (stops if the gist moved first)
bin sync                    # pull, then push if there are local changes
```

You can also fetch with git (read-only): `git clone https://bin.lapius7.com/<id>.git`. For a private gist, the password is an API token.

### Git-like usage (`bin g`)

`bin g <git subcommand>` handles a gist with nearly the same arguments and output as git (existing commands such as `bin status` are unchanged).
origin is the gist, and `main` is the only branch. Commits accumulate locally (`.lapbin/`) and are saved on `push` as one revision per commit.

```bash
bin g clone https://bin.lapius7.com/<id>.git && cd <id>
bin g status -sb
bin g add -A && bin g commit -m "fix"     # -am and --amend (unpushed only) work; without -m an editor opens
bin g log --oneline --stat -5             # gist revisions are listed as commits
bin g diff --cached / bin g diff HEAD~2 --stat / bin g show HEAD:main.py
bin g pull                                # unpushed commits are replayed on top of the gist's changes
bin g push                                # refused if the gist moved first (-f overwrites)

bin g init my-snippets && cd my-snippets  # new: the first push creates an unlisted gist (--public/--private)
```

Supported: `init clone status add rm mv restore checkout switch reset commit log show diff fetch pull push remote branch stash rev-parse ls-files clean`.
Also supported: `git config` aliases (e.g. `alias.st=status` → `bin g st`), `-C <dir>`, `--no-pager`, pagers (`GIT_PAGER`/`PAGER`), and the root `.gitignore`.
Concepts a gist does not have, such as merge, rebase and tag, are unavailable (gist tags are `bin tag`).

### API tokens (for CI and scripts)

```bash
bin token create ci --expires 90     # issue (the token goes to stdout only; --read for read-only)
BIN_TOKEN=sbt_… bin list            # use instead of bin login
bin token list / bin token revoke <id>
```

A token can do LapBin operations only (bin-server restricts it with an allow list and relays it as a short-lived JWT acting as the owner). Issuing and revoking are possible only from a session logged in with `bin login`. You can also manage tokens at /settings/tokens on the web.

## How it works

- Login uses the same two methods as sca-cli. `bin login` starts a temporary HTTP server on a random local port, mints a one-shot token for `http://127.0.0.1:<port>/callback` with `oauth-connect-token` (mint), and has you open `/oauth/v2/authorize?token=...`. `--device` uses the device-code flow (the `oauth-device-code` function).
- All traffic goes through `https://bin.lapius7.com/api/` (a reverse proxy inside bin-server). Only the proxy adds the ANON_KEY, so this CLI contains none.
- Reading and writing gists just calls DB functions in the `bin` schema (`get_gist` / `list_gists` / `save_gist` / `delete_gist`). Permission checks happen on the DB side (RLS and SECURITY DEFINER functions).
- The session is kept in `~/.config/bin/session.json` (0600). When the access token expires, the saved refresh_token renews it automatically.

## Build and distribution

```bash
./build.sh    # cross-compile for every OS/CPU and place the result with install.sh in web/bin.lapius7.com/server/cli-dist/
```

The version is `date-short commit hash` (with `-dirty` when there are uncommitted changes). The script also writes `VERSION` and `build.json` (version, build time, commit, SHA-256 of each binary). The "latest distributed" shown at https://bin.lapius7.com/cli is read from them. The installer finally runs `bin version` on the installed binary and checks it against the distributed version.

bin-server serves that directory as `/install.sh` and `/cli/*`, so no restart is needed.

For npm, pushing a `v*` tag makes GitHub Actions (`.github/workflows/release.yml`) build with `npm/build.mjs`, publish to npm, and attach binaries to a GitHub Release (it authenticates with npm Trusted Publisher, so no token is needed). To check locally, run `node npm/build.mjs 0.0.0-dev --pack`.
The npm build uses `-X main.channel=npm`, so the latest-version check and the update hint in `bin version` point to npm.

## Display language

Japanese, English and Korean are supported. The language comes from `BIN_LANG` (ja / en / ko), otherwise from `LC_ALL`, `LC_MESSAGES` and `LANG` (unset, C and POSIX mean Japanese; any other unsupported language means English). The installer follows the same rule.

Messages use the Japanese string itself as the key, wrapped in `T("…")` (gettext style); English and Korean live in `cmd/bin/messages.go`. When you add a message, add it to the table too (without it, that language also shows Japanese). English plurals use `Tn()` and `messagesOne`.

To point it at a development server, override `BIN_SUPABASE_URL` / `BIN_ANON_KEY` / `BIN_SITE_URL` (environment variables or `~/.config/bin/config.env`).

## License

MIT ([LICENSE](LICENSE))
