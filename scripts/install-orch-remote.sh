#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${REMOTE_HOST:?set REMOTE_HOST to the ssh host of the remote master}"
REMOTE_INSTALL_DIR="${REMOTE_INSTALL_DIR:-~/.local/bin}"
REMOTE_TMP="/tmp/orch.$USER.$$"
TMP_BIN="$(mktemp "${TMPDIR:-/tmp}/orch-linux-amd64.XXXXXX")"
trap 'rm -f "$TMP_BIN"' EXIT

cd "$ROOT_DIR"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X github.com/proboscis/orch/internal/version.Version=$VERSION -X github.com/proboscis/orch/internal/version.Commit=$COMMIT -X github.com/proboscis/orch/internal/version.BuildDate=$BUILD_DATE"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$TMP_BIN" ./cmd/orch

ssh "$REMOTE_HOST" "mkdir -p $REMOTE_INSTALL_DIR"
scp "$TMP_BIN" "$REMOTE_HOST:$REMOTE_TMP"
ssh "$REMOTE_HOST" "install -m 0755 $REMOTE_TMP $REMOTE_INSTALL_DIR/orch && rm -f $REMOTE_TMP"

echo "Installed $REMOTE_INSTALL_DIR/orch on $REMOTE_HOST"
ssh "$REMOTE_HOST" "$REMOTE_INSTALL_DIR/orch master status"
