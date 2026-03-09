#!/usr/bin/env bash
set -euo pipefail

export ROOT="${ROOT:-$(mktemp -d /tmp/orch-remote-smoke-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-}"
REMOTE_ADDR="${REMOTE_ADDR:-127.0.0.1:60318}"

cleanup() {
  set +e
  if [ -n "${ORCH_BIN:-}" ] && [ -x "$ORCH_BIN" ]; then
    ORCH_REMOTE=skip "$ORCH_BIN" master kill >/dev/null 2>&1 || true
  fi
  if [ "$KEEP_ROOT" != "1" ]; then
    chmod -R u+w "$ROOT" >/dev/null 2>&1 || true
    rm -rf "$ROOT"
  fi
}
trap cleanup EXIT

echo "ROOT=$ROOT"
echo "REMOTE_ADDR=$REMOTE_ADDR"
mkdir -p "$ROOT"/{home,runtime,state,data,bin}

export HOME="$ROOT/home"
export XDG_RUNTIME_DIR="$ROOT/runtime"
export XDG_STATE_HOME="$ROOT/state"
export XDG_DATA_HOME="$ROOT/data"
unset ORCH_PROJECT ORCH_REMOTE

if [ -z "$ORCH_BIN" ]; then
  go build -o "$ROOT/bin/orch" ./cmd/orch
  ORCH_BIN="$ROOT/bin/orch"
fi

ORCH_REMOTE=skip "$ORCH_BIN" master start --listen "tcp://$REMOTE_ADDR" >/dev/null
STATUS_OUT="$("$ORCH_BIN" --remote "$REMOTE_ADDR" master status)"
printf '%s\n' "$STATUS_OUT"
printf '%s' "$STATUS_OUT" | grep "Status: running" >/dev/null

"$ORCH_BIN" --remote "$REMOTE_ADDR" master kill >/dev/null

echo "REMOTE_SMOKE_E2E_OK"
