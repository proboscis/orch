#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TMP_BIN="$(mktemp "${TMPDIR:-/tmp}/orch-local.XXXXXX")"
trap 'rm -f "$TMP_BIN"' EXIT

cd "$ROOT_DIR"
go build -o "$TMP_BIN" ./cmd/orch

mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP_BIN" "$INSTALL_DIR/orch"

if [[ "$(uname -s)" == "Darwin" ]] && command -v codesign >/dev/null 2>&1; then
  codesign --force --sign - "$INSTALL_DIR/orch" >/dev/null 2>&1 || true
fi

echo "Installed $INSTALL_DIR/orch"
"$INSTALL_DIR/orch" master status
