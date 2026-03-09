#!/usr/bin/env bash
set -euo pipefail

export ROOT="${ROOT:-$(mktemp -d /tmp/orch-e2e-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-}"

cleanup() {
  set +e
  if [ -n "${ORCH_BIN:-}" ] && [ -x "$ORCH_BIN" ]; then
    "$ORCH_BIN" worker stop --all >/dev/null 2>&1 || true
    "$ORCH_BIN" master kill >/dev/null 2>&1 || true
  fi
  if [ "$KEEP_ROOT" != "1" ]; then
    chmod -R u+w "$ROOT" >/dev/null 2>&1 || true
    rm -rf "$ROOT"
  fi
}
trap cleanup EXIT

echo "ROOT=$ROOT"
mkdir -p "$ROOT"/{home,runtime,state,data,bin,repo/.orch,issues-store/issues,issues-store/runs,outside,origin/example}

export HOME="$ROOT/home"
export XDG_RUNTIME_DIR="$ROOT/runtime"
export XDG_STATE_HOME="$ROOT/state"
export XDG_DATA_HOME="$ROOT/data"
unset ORCH_PROJECT ORCH_REMOTE

if [ -z "$ORCH_BIN" ]; then
  go build -o "$ROOT/bin/orch" ./cmd/orch
  ORCH_BIN="$ROOT/bin/orch"
fi

PROJECT="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ['ROOT'], 'repo'))))
PY
)"
ISSUES="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ['ROOT'], 'issues-store'))))
PY
)"

cat > "$PROJECT/.orch/config.yaml" <<EOF
issues:
  path: $ISSUES
EOF

cat > "$PROJECT/README.md" <<'EOF'
# Manual E2E Repo
EOF

cat > "$ISSUES/issues/mwc-local-live.md" <<'EOF'
---
type: issue
id: mwc-local-live
title: Local live run
status: open
---

# Local live run
EOF

cat > "$ISSUES/issues/mwc-local-live-2.md" <<'EOF'
---
type: issue
id: mwc-local-live-2
title: Local live run 2
status: open
---

# Local live run 2
EOF

git -C "$PROJECT" init >/dev/null
git -C "$PROJECT" branch -M main
git -C "$PROJECT" config user.email e2e@example.com
git -C "$PROJECT" config user.name E2E

git init --bare "$ROOT/origin/example/manual-e2e-repo.git" >/dev/null
REPO_URL="file://$ROOT/origin/example/manual-e2e-repo.git"
PROJECT_ID="example-manual-e2e-repo"

git -C "$PROJECT" remote add origin "$REPO_URL"
git -C "$PROJECT" add .
git -C "$PROJECT" commit -m "init" >/dev/null
git -C "$PROJECT" push -u origin HEAD >/dev/null

cd "$PROJECT"

echo "== master/worker lifecycle =="
MASTER_STATUS_BEFORE="$("$ORCH_BIN" master status)"
WORKER_STATUS_BEFORE="$("$ORCH_BIN" worker status)"
printf '%s\n' "$MASTER_STATUS_BEFORE"
printf '%s\n' "$WORKER_STATUS_BEFORE"
printf '%s' "$MASTER_STATUS_BEFORE" | grep 'Status: not running' >/dev/null

"$ORCH_BIN" master start >/dev/null
sleep 1
"$ORCH_BIN" worker start >/dev/null
sleep 2
WORKER_STATUS_AFTER_1="$("$ORCH_BIN" worker status)"
printf '%s\n' "$WORKER_STATUS_AFTER_1"
"$ORCH_BIN" worker start >/dev/null
WORKER_STATUS_AFTER_2="$("$ORCH_BIN" worker status)"
printf '%s\n' "$WORKER_STATUS_AFTER_2"
WORKER_JSON="$("$ORCH_BIN" worker status --json)"
printf '%s\n' "$WORKER_JSON"
printf '%s' "$WORKER_JSON" | jq -e '(.workers | length) == 1 and (.workers[0].id | startswith("host-"))' >/dev/null

echo "== repo mapping =="
REGISTER_OUT="$("$ORCH_BIN" daemon repo register "$REPO_URL")"
printf '%s\n' "$REGISTER_OUT"
printf '%s' "$REGISTER_OUT" | grep "Registered repo mapping: $PROJECT_ID ->" >/dev/null
"$ORCH_BIN" daemon repo list

echo "== local run flow =="
RUN_ID_1="$(date +%Y%m%d-%H%M%S)-a"
RUN_ID_2="$(date +%Y%m%d-%H%M%S)-b"
RUN_OUT_1="$("$ORCH_BIN" --project "$PROJECT_ID" run mwc-local-live \
  --run-id "$RUN_ID_1" \
  --agent custom \
  --agent-cmd 'echo cli-e2e-a; sleep 20' \
  --json)"
RUN_OUT_2="$("$ORCH_BIN" --project "$PROJECT_ID" run mwc-local-live-2 \
  --run-id "$RUN_ID_2" \
  --agent custom \
  --agent-cmd 'echo cli-e2e-b; sleep 20' \
  --json)"
printf '%s\n' "$RUN_OUT_1"
printf '%s\n' "$RUN_OUT_2"
printf '%s' "$RUN_OUT_1" | jq -e '.ok == true' >/dev/null
printf '%s' "$RUN_OUT_2" | jq -e '.ok == true' >/dev/null

PS_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" ps --json)"
printf '%s\n' "$PS_OUT"
printf '%s' "$PS_OUT" | jq -e --arg r1 "$RUN_ID_1" --arg r2 "$RUN_ID_2" '
  .ok == true and
  (
    [ .items[] | {issue_id, run_id} ] |
    any(.issue_id == "mwc-local-live" and .run_id == $r1)
  ) and
  (
    [ .items[] | {issue_id, run_id} ] |
    any(.issue_id == "mwc-local-live-2" and .run_id == $r2)
  )' >/dev/null

SHOW_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" show "mwc-local-live#$RUN_ID_1" --json)"
printf '%s\n' "$SHOW_OUT"
printf '%s' "$SHOW_OUT" | jq -e '.ok == true' >/dev/null

echo "== cleanup runs =="
"$ORCH_BIN" --project "$PROJECT_ID" stop "mwc-local-live#$RUN_ID_1" --force >/dev/null
"$ORCH_BIN" --project "$PROJECT_ID" stop "mwc-local-live-2#$RUN_ID_2" --force >/dev/null

echo "LOCAL_E2E_OK"
