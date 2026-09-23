#!/usr/bin/env bash
# bin (bin.lapius7.com CLI) のワンライナーインストーラー。
#
#   curl -fsSL https://bin.lapius7.com/install.sh | bash
#
# Goは不要。bin.lapius7.com/cli/ からOS・CPUに合ったビルド済みバイナリを取得して
# ~/.local/bin/bin に置く(BIN_INSTALL_DIRで変更可)。
# ("| sh" だとdash等でpipefailや$'...'が使えないため、必ずbashで実行する)
#
# 表示言語はCLI本体と同じ規則: BIN_LANG → LC_ALL → LC_MESSAGES → LANG。
# 未設定・C・POSIXは日本語、ja/en/ko以外の言語は英語。
set -euo pipefail

BASE_URL="${BIN_BASE_URL:-https://bin.lapius7.com}"
INSTALL_DIR="${BIN_INSTALL_DIR:-$HOME/.local/bin}"

detect_lang() {
  local v base
  for v in "${BIN_LANG:-}" "${LC_ALL:-}" "${LC_MESSAGES:-}" "${LANG:-}"; do
    [ -z "$v" ] && continue
    base="$(printf '%s' "$v" | tr '[:upper:]' '[:lower:]' | sed 's/[_.@-].*//')"
    case "$base" in
      ja|en|ko) echo "$base"; return ;;
      c|posix|"") continue ;;
      *) echo "en"; return ;;
    esac
  done
  echo "ja"
}
UI_LANG="$(detect_lang)"

# m <ja> <en> <ko>: 表示言語の文言を返す
m() {
  case "$UI_LANG" in
    en) printf '%s' "$2" ;;
    ko) printf '%s' "$3" ;;
    *) printf '%s' "$1" ;;
  esac
}

if [ -t 1 ]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; RESET=$'\033[0m'
  RED=$'\033[31m'; GREEN=$'\033[32m'; CYAN=$'\033[36m'; YELLOW=$'\033[33m'
else
  BOLD=""; DIM=""; RESET=""; RED=""; GREEN=""; CYAN=""; YELLOW=""
fi

info() { printf "%s→%s %s\n" "$CYAN" "$RESET" "$1"; }
ok()   { printf "%s✓%s %s\n" "$GREEN" "$RESET" "$1"; }
warn() { printf "%s!%s %s\n" "$YELLOW" "$RESET" "$1"; }
err()  { printf "%s✗%s %s\n" "$RED" "$RESET" "$1" >&2; }

printf "%sbin%s — bin.lapius7.com CLI installer\n\n" "$BOLD" "$RESET"

WIN_URL="${BASE_URL}/cli/bin-windows-amd64.exe"
case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *)
    err "$(m "未対応のOSです: $(uname -s)(Windowsは ${WIN_URL} を直接ダウンロードしてください)" \
            "Unsupported OS: $(uname -s) (on Windows, download ${WIN_URL} directly)" \
            "지원하지 않는 OS입니다: $(uname -s) (Windows는 ${WIN_URL} 을 직접 다운로드하세요)")"
    exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) err "$(m "未対応のCPUです: $(uname -m)" "Unsupported CPU: $(uname -m)" "지원하지 않는 CPU입니다: $(uname -m)")"; exit 1 ;;
esac

VERSION="$(curl -fsSL "${BASE_URL}/cli/VERSION" 2>/dev/null || echo "?")"
info "$(m "bin ${VERSION} (${OS}/${ARCH}) をダウンロード中" "Downloading bin ${VERSION} (${OS}/${ARCH})" "bin ${VERSION} (${OS}/${ARCH}) 다운로드 중")"

# 上書き前に入っていたバージョン(更新の前後を表示するため)。1行目が「bin <バージョン>」
installed_version() { "$1" version 2>/dev/null | head -n1 | sed 's/^bin //'; }
PREV=""
[ -x "${INSTALL_DIR}/bin" ] && PREV="$(installed_version "${INSTALL_DIR}/bin" || true)"

mkdir -p "$INSTALL_DIR"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
if ! curl -fsSL "${BASE_URL}/cli/bin-${OS}-${ARCH}" -o "$TMP"; then
  err "$(m "ダウンロードに失敗しました" "Download failed" "다운로드에 실패했습니다")"
  exit 1
fi
chmod +x "$TMP"
mv "$TMP" "${INSTALL_DIR}/bin"
trap - EXIT
ok "$(m "インストール先" "Installed to" "설치 위치"): ${DIM}${INSTALL_DIR}/bin${RESET}"

# 入ったバイナリを実際に実行して、配布中の最新版と同じバージョンかを確かめる
NOW="$(installed_version "${INSTALL_DIR}/bin" || true)"
if [ -z "$NOW" ]; then
  warn "$(m "インストールしたbinを実行できませんでした" "Could not run the installed bin" "설치한 bin 을 실행하지 못했습니다")"
elif [ "$VERSION" != "?" ] && [ "$NOW" != "$VERSION" ]; then
  warn "$(m "インストールされたのは ${NOW} で、配布中の最新版 ${VERSION} と一致しません。もう一度実行してください" \
            "Installed ${NOW}, which does not match the latest release ${VERSION}. Please run the installer again" \
            "설치된 버전은 ${NOW} 이며 배포 중인 최신 버전 ${VERSION} 과 일치하지 않습니다. 다시 실행해 주세요")"
else
  if [ -n "$PREV" ] && [ "$PREV" != "$NOW" ]; then
    ok "$(m "バージョン" "Version" "버전"): ${DIM}${PREV}${RESET} → ${BOLD}${NOW}${RESET}"
  elif [ -n "$PREV" ]; then
    ok "$(m "バージョン" "Version" "버전"): ${BOLD}${NOW}${RESET} ${DIM}($(m "すでに最新版でした" "already up to date" "이미 최신 버전이었습니다"))${RESET}"
  else
    ok "$(m "バージョン" "Version" "버전"): ${BOLD}${NOW}${RESET}"
  fi
  [ "$VERSION" != "?" ] && printf "  %s%s%s\n" "$DIM" "$(m "配布中の最新版と一致しています(${BASE_URL}/cli に表示されている版と同じです)" "Matches the latest release (the one shown at ${BASE_URL}/cli)" "배포 중인 최신 버전과 일치합니다 (${BASE_URL}/cli 에 표시된 버전과 같습니다)")" "$RESET"
fi

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    printf "\n"
    warn "$(m "PATHに ${INSTALL_DIR} が通っていません" "${INSTALL_DIR} is not in your PATH" "PATH에 ${INSTALL_DIR} 가 없습니다")"
    printf "  %s\n" "$(m "ご使用のシェルの設定ファイル(~/.bashrc, ~/.zshrc 等)に以下を追記してください:" "Add this to your shell config (~/.bashrc, ~/.zshrc, etc.):" "사용 중인 셸 설정 파일 (~/.bashrc, ~/.zshrc 등) 에 다음을 추가하세요:")"
    printf "  %sexport PATH=\"\$PATH:%s\"%s\n" "$DIM" "$INSTALL_DIR" "$RESET"
    ;;
esac

printf "\n%s%s%s\n\n" "$BOLD" "$(m "🎉 bin のインストールが完了しました！" "🎉 bin is installed!" "🎉 bin 설치가 완료되었습니다!")" "$RESET"
printf "%s\n" "$(m "次のステップ:" "Next steps:" "다음 단계:")"
printf "  %s1.%s %sbin login%s              %s\n" "$BOLD" "$RESET" "$CYAN" "$RESET" "$(m "ブラウザでログイン" "Log in with your browser" "브라우저로 로그인")"
printf "  %s2.%s %sbin create main.go%s     %s\n" "$BOLD" "$RESET" "$CYAN" "$RESET" "$(m "Gistを作成" "Create a gist" "Gist 만들기")"
printf "\n%s%s%s ${BASE_URL}/cli\n" "$DIM" "$(m "詳細:" "More:" "자세히:")" "$RESET"
