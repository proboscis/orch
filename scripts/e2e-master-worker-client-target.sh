#!/usr/bin/env bash
set -euo pipefail

: "${ZEUS_HOST:?set ZEUS_HOST}"
: "${TARGET_SSH:?set TARGET_SSH}"
: "${TARGET_NAME:?set TARGET_NAME}"
: "${TARGET_HOST:?set TARGET_HOST}"
: "${TARGET_REPO:?set TARGET_REPO}"
: "${ZEUS_PROJECT_ROOT:?set ZEUS_PROJECT_ROOT}"
: "${ZEUS_ISSUES_PATH:?set ZEUS_ISSUES_PATH}"
PROJECT_ID="${PROJECT_ID:-}"
: "${ZEUS_ORCH_BIN:=orch}"
: "${TARGET_ORCH_BIN:=orch}"

TS="${TS:-$(date +%Y%m%d-%H%M%S)}"
ISSUE_ID="${ISSUE_ID:-target-e2e-$TS}"
RUN_ID="${RUN_ID:-$TS-target}"
TARGET_SSH_CMD="${TARGET_SSH_CMD:-ssh \"$TARGET_SSH\"}"
TARGET_WORKER_LOG="${TARGET_WORKER_LOG:-/tmp/orch-target-worker.log}"
TARGET_ENV_PREFIX="${TARGET_ENV_PREFIX:-}"
TARGET_WORKER_ID="${TARGET_WORKER_ID:-$(TARGET_HOST="$TARGET_HOST" python3 - <<'PY'
import os
host = os.environ["TARGET_HOST"].strip() or "localhost"
normalized = []
for ch in host:
    if ch.isalnum() or ch in "-_.":
        normalized.append(ch)
    else:
        normalized.append("-")
text = "".join(normalized).strip("-") or "localhost"
print("host-" + text)
PY
)}"

echo "ZEUS_HOST=$ZEUS_HOST"
echo "TARGET_SSH=$TARGET_SSH"
echo "TARGET_SSH_CMD=$TARGET_SSH_CMD"
echo "TARGET_NAME=$TARGET_NAME"
echo "TARGET_HOST=$TARGET_HOST"
echo "TARGET_WORKER_ID=$TARGET_WORKER_ID"
echo "ISSUE_ID=$ISSUE_ID"
echo "RUN_ID=$RUN_ID"
echo "ZEUS_ORCH_BIN=$ZEUS_ORCH_BIN"
echo "TARGET_ORCH_BIN=$TARGET_ORCH_BIN"
echo "TARGET_ENV_PREFIX=$TARGET_ENV_PREFIX"

eval "$TARGET_SSH_CMD \"$TARGET_ENV_PREFIX ORCH_REMOTE=$ZEUS_HOST:7777 $TARGET_ORCH_BIN worker run --worker-id '$TARGET_WORKER_ID'\" >'$TARGET_WORKER_LOG' 2>&1" &
TARGET_WORKER_PID=$!

cleanup() {
  set +e
  if [ -n "${PROJECT_ID:-}" ]; then
    ssh "$ZEUS_HOST" "$ZEUS_ORCH_BIN --project '$PROJECT_ID' stop '$ISSUE_ID#$RUN_ID' --force" >/dev/null 2>&1 || true
  fi
  ssh "$ZEUS_HOST" "rm -f '$ZEUS_ISSUES_PATH/issues/$ISSUE_ID.md'" >/dev/null 2>&1 || true
  kill "$TARGET_WORKER_PID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ssh "$ZEUS_HOST" "cat > '$ZEUS_ISSUES_PATH/issues/$ISSUE_ID.md' <<'EOF'
---
type: issue
id: $ISSUE_ID
title: Target host E2E sample
status: open
---

# Target host E2E sample
EOF"

REGISTER_OUT="$(ssh "$ZEUS_HOST" "cd '$ZEUS_PROJECT_ROOT' && $ZEUS_ORCH_BIN daemon repo register '$ZEUS_PROJECT_ROOT'")"
printf '%s\n' "$REGISTER_OUT"
if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID="$(printf '%s\n' "$REGISTER_OUT" | sed -n 's/^Registered repo mapping: \([^ ]*\) -> .*$/\1/p' | head -n 1)"
fi
[ -n "$PROJECT_ID" ]
echo "PROJECT_ID=$PROJECT_ID"

set +e
RUN_OUT="$(ssh "$ZEUS_HOST" "cd '$ZEUS_PROJECT_ROOT' && $ZEUS_ORCH_BIN --project '$PROJECT_ID' run '$ISSUE_ID' --run-id '$RUN_ID' --on '$TARGET_NAME' --agent custom --agent-cmd 'printf target-e2e-ready; hostname; sleep 20' --multiplexer tmux --json" 2>&1)"
RUN_RC=$?
set -e
printf '%s\n' "$RUN_OUT"
if [ "$RUN_RC" -ne 0 ]; then
  echo "== target worker log =="
  cat "$TARGET_WORKER_LOG" || true
fi
[ "$RUN_RC" -eq 0 ]
printf '%s' "$RUN_OUT" | jq -e '.ok == true' >/dev/null

WORKER_JSON="$(ssh "$ZEUS_HOST" "$ZEUS_ORCH_BIN worker status --json")"
printf '%s\n' "$WORKER_JSON"
printf '%s' "$WORKER_JSON" | jq -e --arg worker "$TARGET_WORKER_ID" '.workers | any(.id == $worker)' >/dev/null

PS_OUT="$(ssh "$ZEUS_HOST" "$ZEUS_ORCH_BIN --project '$PROJECT_ID' ps --issue '$ISSUE_ID' --json")"
printf '%s\n' "$PS_OUT"
printf '%s' "$PS_OUT" | jq -e --arg target "$TARGET_NAME" --arg target_host "$TARGET_HOST" '
  .ok == true and
  (.items | length == 1) and
  (.items[0].target == $target) and
  (.items[0].target_host == $target_host)
' >/dev/null

SHOW_OUT="$(ssh "$ZEUS_HOST" "$ZEUS_ORCH_BIN --project '$PROJECT_ID' show '$ISSUE_ID#$RUN_ID' --json")"
printf '%s\n' "$SHOW_OUT"
printf '%s' "$SHOW_OUT" | jq -e '.ok == true' >/dev/null

echo "TARGET_HOST_E2E_OK"
