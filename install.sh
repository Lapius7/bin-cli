#!/usr/bin/env bash
# bin (bin.lapius7.com CLI) のワンライナーインストーラー。
#
#   curl -fsSL https://bin.lapius7.com/install.sh | bash
#
# Goは不要。bin.lapius7.com/cli/ からOS・CPUに合ったビルド済みバイナリを取得して
# ~/.local/bin/bin に置く(BIN_INSTALL_DIRで変更可)。
# ("| sh" だとdash等でpipefailや$'...'が使えないため、必ずbashで実行する)
set -euo pipefail

BASE_URL="${BIN_BASE_URL:-https://bin.lapius7.com}"
INSTALL_DIR="${BIN_INSTALL_DIR:-$HOME/.local/bin}"

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

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) err "未対応のOSです: $(uname -s)(Windowsは ${BASE_URL}/cli/bin-windows-amd64.exe を直接ダウンロードしてください)"; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) err "未対応のCPUです: $(uname -m)"; exit 1 ;;
esac

VERSION="$(curl -fsSL "${BASE_URL}/cli/VERSION" 2>/dev/null || echo "?")"
info "bin ${VERSION} (${OS}/${ARCH}) をダウンロード中"

mkdir -p "$INSTALL_DIR"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
if ! curl -fsSL "${BASE_URL}/cli/bin-${OS}-${ARCH}" -o "$TMP"; then
  err "ダウンロードに失敗しました"
  exit 1
fi
chmod +x "$TMP"
mv "$TMP" "${INSTALL_DIR}/bin"
trap - EXIT
ok "インストール先: ${DIM}${INSTALL_DIR}/bin${RESET}"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    printf "\n"
    warn "PATHに ${INSTALL_DIR} が通っていません"
    printf "  ご使用のシェルの設定ファイル(~/.bashrc, ~/.zshrc 等)に以下を追記してください:\n"
    printf "  %sexport PATH=\"\$PATH:%s\"%s\n" "$DIM" "$INSTALL_DIR" "$RESET"
    ;;
esac

printf "\n%s🎉 bin のインストールが完了しました！%s\n\n" "$BOLD" "$RESET"
printf "次のステップ:\n"
printf "  %s1.%s %sbin login%s              ブラウザでログイン\n" "$BOLD" "$RESET" "$CYAN" "$RESET"
printf "  %s2.%s %sbin create main.go%s     Gistを作成\n" "$BOLD" "$RESET" "$CYAN" "$RESET"
printf "\n%s詳細:%s ${BASE_URL}/cli\n" "$DIM" "$RESET"
