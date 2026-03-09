#!/usr/bin/env bash
set -euo pipefail

: "${ZEUS_HOST:?set ZEUS_HOST}"
: "${TARGET_SSH:?set TARGET_SSH}"
: "${TARGET_WORKER_ID:?set TARGET_WORKER_ID}"
: "${TARGET_REPO:?set TARGET_REPO}"
: "${ZEUS_PROJECT_ROOT:?set ZEUS_PROJECT_ROOT}"
: "${ZEUS_ISSUES_PATH:?set ZEUS_ISSUES_PATH}"
: "${PROJECT_ID:?set PROJECT_ID}"

TS="${TS:-$(date +%Y%m%d-%H%M%S)}"
ISSUE_ID="${ISSUE_ID:-target-e2e-$TS}"
RUN_ID="${RUN_ID:-$TS-target}"

echo "ZEUS_HOST=$ZEUS_HOST"
echo "TARGET_SSH=$TARGET_SSH"
echo "TARGET_WORKER_ID=$TARGET_WORKER_ID"
echo "ISSUE_ID=$ISSUE_ID"
echo "RUN_ID=$RUN_ID"

ssh "$TARGET_SSH" "ORCH_REMOTE=$ZEUS_HOST:7777 orch worker run --worker-id '$TARGET_WORKER_ID'" &
TARGET_WORKER_PID=$!

cleanup() {
  set +e
  ssh "$ZEUS_HOST" "orch --project '$PROJECT_ID' stop '$ISSUE_ID#$RUN_ID' --force" >/dev/null 2>&1 || true
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

ssh "$ZEUS_HOST" "cd '$ZEUS_PROJECT_ROOT' && orch daemon repo register '$ZEUS_PROJECT_ROOT'"

RUN_OUT="$(ssh "$ZEUS_HOST" "cd '$ZEUS_PROJECT_ROOT' && orch --project '$PROJECT_ID' run '$ISSUE_ID' --run-id '$RUN_ID' --on '$TARGET_WORKER_ID' --agent custom --agent-cmd 'printf target-e2e-ready; hostname; sleep 20' --multiplexer tmux --json")"
printf '%s\n' "$RUN_OUT"
printf '%s' "$RUN_OUT" | jq -e '.ok == true' >/dev/null

PS_OUT="$(ssh "$ZEUS_HOST" "orch --project '$PROJECT_ID' ps --issue '$ISSUE_ID' --json")"
printf '%s\n' "$PS_OUT"
printf '%s' "$PS_OUT" | jq -e --arg target "$TARGET_WORKER_ID" '.ok == true and (.items | length == 1) and (.items[0].target == $target)' >/dev/null

SHOW_OUT="$(ssh "$ZEUS_HOST" "orch --project '$PROJECT_ID' show '$ISSUE_ID#$RUN_ID' --json")"
printf '%s\n' "$SHOW_OUT"
printf '%s' "$SHOW_OUT" | jq -e '.ok == true' >/dev/null

echo "TARGET_HOST_E2E_OK"
