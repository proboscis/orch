#!/usr/bin/env bash
set -euo pipefail

ORCH_BIN="${ORCH_BIN:-orch}"
REMOTE_ADDR="${1:-${ORCH_REMOTE:-}}"

ARGS=()
if [[ -n "$REMOTE_ADDR" ]]; then
  ARGS+=(--remote "$REMOTE_ADDR")
fi

echo "Using $ORCH_BIN ${ARGS[*]}"

set -x
"$ORCH_BIN" "${ARGS[@]}" worker start
"$ORCH_BIN" "${ARGS[@]}" worker start
"$ORCH_BIN" "${ARGS[@]}" worker status --json
"$ORCH_BIN" "${ARGS[@]}" worker stop --json
"$ORCH_BIN" "${ARGS[@]}" worker status --json
