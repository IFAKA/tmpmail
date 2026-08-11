#!/bin/sh
set -eu
REPO="IFAKA/tmpmail"
BIN="tmpmail"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) echo "unsupported architecture: $ARCH" >&2; exit 1;; esac
case "$OS" in darwin|linux) ;; *) echo "unsupported OS: $OS" >&2; exit 1;; esac
URL="https://github.com/$REPO/releases/latest/download/${BIN}_${OS}_${ARCH}.tar.gz"
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$URL" -o "$TMP/a.tgz"
tar -xzf "$TMP/a.tgz" -C "$TMP" "$BIN"
DEST="${TMPMAIL_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$DEST"; install -m 755 "$TMP/$BIN" "$DEST/$BIN"
echo "installed $DEST/$BIN"
case ":$PATH:" in *":$DEST:"*) ;; *) echo "add $DEST to PATH";; esac
