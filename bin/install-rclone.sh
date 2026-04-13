#!/bin/bash
# Install the official rclone binary.
#
# Usage:
#   ./bin/install-rclone.sh [install-dir]
#   curl -fsSL https://raw.githubusercontent.com/bkosm/akb/main/bin/install-rclone.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/bkosm/akb/main/bin/install-rclone.sh | bash -s -- "$HOME/.bin"
#
# Default install directory: /usr/local/bin
# The Homebrew rclone build omits CGO (cmount tag), so rclone mount (FUSE) does not work.
# This script always installs the official binary from rclone.org.

set -euo pipefail

INSTALL_DIR="${1:-/usr/local/bin}"

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) OS_KEY="osx" ;;
  Linux)  OS_KEY="linux" ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

case "$ARCH" in
  arm64|aarch64) ARCH_KEY="arm64" ;;
  x86_64)        ARCH_KEY="amd64" ;;
  *) echo "Unsupported arch: $ARCH" >&2; exit 1 ;;
esac

URL="https://downloads.rclone.org/rclone-current-${OS_KEY}-${ARCH_KEY}.zip"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading rclone for ${OS_KEY}/${ARCH_KEY}..."
curl -fsSL "$URL" -o "$TMP/rclone.zip"
unzip -q "$TMP/rclone.zip" -d "$TMP"

mkdir -p "$INSTALL_DIR"
cp "$TMP"/rclone-*-"${OS_KEY}"-"${ARCH_KEY}"/rclone "$INSTALL_DIR/rclone"
chmod +x "$INSTALL_DIR/rclone"

echo "rclone installed to $INSTALL_DIR/rclone"
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  echo "Note: $INSTALL_DIR is not on your PATH."
  echo "Add to your shell profile: export PATH=\"$INSTALL_DIR:\$PATH\""
fi
