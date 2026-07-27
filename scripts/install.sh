#!/bin/sh
# taskthing bootstrap installer (Linux / macOS).
#
#   curl -fsSL https://raw.githubusercontent.com/costaluu/taskthing/master/scripts/install.sh | sh
#
# Detects this machine's OS/arch, downloads the matching binary from the latest
# GitHub release into ~/.local/bin, and tells you to run `taskthing install`.
# It deliberately does NOT run the interactive first-run setup itself: piped
# through `curl | sh` there is no reliable TTY to drive the prompts (Spec 0005).
#
# Overrides: TASKTHING_REPO (owner/repo), TASKTHING_BIN_DIR (install directory).
set -eu

REPO="${TASKTHING_REPO:-costaluu/taskthing}"
BIN_DIR="${TASKTHING_BIN_DIR:-$HOME/.local/bin}"

# Map `uname` output onto the release asset's OS/arch tokens (the same tokens the
# in-binary updater matches on: process.platform / process.arch).
os=$(uname -s)
case "$os" in
  Linux) os_tag=linux ;;
  Darwin) os_tag=darwin ;;
  *) echo "taskthing: unsupported OS '$os' (ships Linux, macOS, Windows)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch_tag=x64 ;;
  aarch64 | arm64) arch_tag=arm64 ;;
  *) echo "taskthing: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

asset="taskthing-${os_tag}-${arch_tag}"
# The `latest/download` redirect resolves the newest release and serves the named
# asset — no API call, no JSON parsing, no rate limit.
url="https://github.com/${REPO}/releases/latest/download/${asset}"

download() { # download <url> <dest>
  if command -v curl >/dev/null 2>&1; then
    curl -fL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    echo "taskthing: need curl or wget to download" >&2
    exit 1
  fi
}

echo "taskthing: downloading ${asset} from ${REPO}..."
mkdir -p "$BIN_DIR"
tmp="${BIN_DIR}/.taskthing.download.$$"
# Download to a temp beside the target, then move into place, so a failed or
# interrupted download never leaves a half-written binary on the PATH.
if ! download "$url" "$tmp"; then
  rm -f "$tmp"
  echo "taskthing: could not download ${asset} from the latest ${REPO} release" >&2
  exit 1
fi
chmod +x "$tmp"
mv "$tmp" "$BIN_DIR/taskthing"

echo "taskthing: installed to ${BIN_DIR}/taskthing"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "taskthing: add ${BIN_DIR} to your PATH, then restart your shell" >&2 ;;
esac
echo "taskthing: now run \`taskthing install\`"
