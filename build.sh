#!/usr/bin/env bash
# 全OS/CPU向けにクロスコンパイルし、bin.lapius7.comの配布ディレクトリ
# (web/bin.lapius7.com/server/cli-dist/)へinstall.shと一緒に配置する。
# bin-serverが/install.shと/cli/*としてそのまま配信するので、再起動は不要。
#
#   ./build.sh            # バージョンは日付+gitの短縮ハッシュ(未コミットの変更があれば末尾に-dirty)
#   VERSION=v1.2.0 ./build.sh
set -euo pipefail

cd "$(dirname "$0")"
OUT="${BIN_DIST_DIR:-/root/project/web/bin.lapius7.com/server/cli-dist}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
DIRTY=""
git diff --quiet HEAD 2>/dev/null || DIRTY="-dirty"
VERSION="${VERSION:-$(date +%Y.%m.%d)-${COMMIT}${DIRTY}}"

mkdir -p "$OUT"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os="${target%/*}"; arch="${target#*/}"
  ext=""; [ "$os" = windows ] && ext=".exe"
  echo "→ ${os}/${arch}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT}/bin-${os}-${arch}${ext}" ./cmd/bin
done
cp install.sh "${OUT}/install.sh"
printf "%s\n" "$VERSION" > "${OUT}/VERSION"

# /cli ページに「最新版」として出すビルド情報。インストール後に正しい版が入ったかを
# バージョン(bin version)とSHA-256(sha256sum)で照合できるようにする
{
  printf '{"version":"%s","commit":"%s","dirty":%s,"built_at":"%s","files":[' \
    "$VERSION" "$COMMIT" "$([ -n "$DIRTY" ] && echo true || echo false)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  sep=""
  for f in "${OUT}"/bin-*; do
    printf '%s{"name":"%s","size":%s,"sha256":"%s"}' "$sep" "$(basename "$f")" "$(wc -c < "$f" | tr -d ' ')" "$(sha256sum "$f" | cut -d' ' -f1)"
    sep=","
  done
  printf ']}\n'
} > "${OUT}/build.json"
echo "✓ ${VERSION} を ${OUT} に配置しました"
