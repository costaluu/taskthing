#!/bin/sh
# Compile taskthing into dist/taskthing-<os>-<arch>[.exe].
#
#   scripts/build.sh                 # this machine only
#   scripts/build.sh all             # every released target
#   VERSION=1.2.3 scripts/build.sh all
#
# The asset names carry Node's process.platform/process.arch tokens (win32, x64)
# rather than Go's (windows, amd64): every taskthing binary ever released —
# the TS ones included — finds its update by those names, and so do the
# installers (scripts/install.sh, scripts/install.ps1).
set -eu

cd "$(dirname "$0")/.."
VERSION="${VERSION:-0.1.0}"
LDFLAGS="-s -w -X github.com/costaluu/taskthing/internal/app.Version=${VERSION}"

# GOOS/GOARCH → asset token.
token() {
  case "$1" in
    windows) printf win32 ;;
    *) printf '%s' "$1" ;;
  esac
  printf -- '-'
  case "$2" in
    amd64) printf x64 ;;
    *) printf '%s' "$2" ;;
  esac
}

if [ "${1:-native}" = "all" ]; then
  targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"
else
  targets="$(go env GOOS)/$(go env GOARCH)"
fi

mkdir -p dist
for target in $targets; do
  goos=${target%/*}
  goarch=${target#*/}
  out="dist/taskthing-$(token "$goos" "$goarch")"
  [ "$goos" = windows ] && out="$out.exe"
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/taskthing
  echo "taskthing: built $out ($VERSION)"
done
