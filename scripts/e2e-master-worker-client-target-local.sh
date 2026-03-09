#!/usr/bin/env bash
set -euo pipefail

export ROOT="${ROOT:-$(mktemp -d /tmp/orch-target-e2e-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-}"
TARGET_NAME="${TARGET_NAME:-mac}"
TARGET_HOST="${TARGET_HOST:-mac}"
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

cleanup() {
  set +e
  if [ -n "${ORCH_BIN:-}" ] && [ -x "$ORCH_BIN" ]; then
    if [ -n "${PROJECT_ID:-}" ] && [ -n "${RUN_ID_1:-}" ]; then
      "$ORCH_BIN" --project "$PROJECT_ID" stop "target-local-live#$RUN_ID_1" --force >/dev/null 2>&1 || true
    fi
    if [ -n "${PROJECT_ID:-}" ] && [ -n "${RUN_ID_2:-}" ]; then
      "$ORCH_BIN" --project "$PROJECT_ID" stop "target-local-live-2#$RUN_ID_2" --force >/dev/null 2>&1 || true
    fi
    "$ORCH_BIN" master kill >/dev/null 2>&1 || true
  fi
  if [ -n "${TARGET_WORKER_PID:-}" ]; then
    kill "$TARGET_WORKER_PID" >/dev/null 2>&1 || true
  fi
  if [ "$KEEP_ROOT" != "1" ]; then
    chmod -R u+w "$ROOT" >/dev/null 2>&1 || true
    rm -rf "$ROOT"
  fi
}
trap cleanup EXIT

echo "ROOT=$ROOT"
echo "TARGET_NAME=$TARGET_NAME"
echo "TARGET_HOST=$TARGET_HOST"
echo "TARGET_WORKER_ID=$TARGET_WORKER_ID"
mkdir -p "$ROOT"/{home,runtime,state,data,bin,master-repo/.orch,issues-store/issues,issues-store/runs,origin/example}

export HOME="$ROOT/home"
export XDG_RUNTIME_DIR="$ROOT/runtime"
export XDG_STATE_HOME="$ROOT/state"
export XDG_DATA_HOME="$ROOT/data"
unset ORCH_PROJECT ORCH_REMOTE

if [ -z "$ORCH_BIN" ]; then
  go build -o "$ROOT/bin/orch" ./cmd/orch
  ORCH_BIN="$ROOT/bin/orch"
fi

MASTER_PROJECT="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ['ROOT'], 'master-repo'))))
PY
)"
TARGET_PROJECT="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ['ROOT'], 'target-repo'))))
PY
)"
ISSUES="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ['ROOT'], 'issues-store'))))
PY
)"

cat > "$MASTER_PROJECT/.orch/config.yaml" <<EOF
issues:
  path: $ISSUES
worktree_dir: worktrees
targets:
  - name: $TARGET_NAME
    host: $TARGET_HOST
    repo: $TARGET_PROJECT
EOF

cat > "$MASTER_PROJECT/README.md" <<'EOF'
# Target Host Local E2E Repo
EOF

cat > "$ISSUES/issues/target-local-live.md" <<'EOF'
---
type: issue
id: target-local-live
title: Target local live run
status: open
---

# Target local live run
EOF

cat > "$ISSUES/issues/target-local-live-2.md" <<'EOF'
---
type: issue
id: target-local-live-2
title: Target local live run 2
status: open
---

# Target local live run 2
EOF

git -C "$MASTER_PROJECT" init >/dev/null
git -C "$MASTER_PROJECT" branch -M main
git -C "$MASTER_PROJECT" config user.email e2e@example.com
git -C "$MASTER_PROJECT" config user.name E2E

git init --bare "$ROOT/origin/example/target-local-repo.git" >/dev/null
REPO_URL="file://$ROOT/origin/example/target-local-repo.git"
PROJECT_ID="example-target-local-repo"

git -C "$MASTER_PROJECT" remote add origin "$REPO_URL"
git -C "$MASTER_PROJECT" add .
git -C "$MASTER_PROJECT" commit -m "init" >/dev/null
git -C "$MASTER_PROJECT" push -u origin HEAD >/dev/null
git clone "$REPO_URL" "$TARGET_PROJECT" >/dev/null

cd "$MASTER_PROJECT"

echo "== master and target worker =="
"$ORCH_BIN" master start >/dev/null
sleep 1
"$ORCH_BIN" worker run --worker-id "$TARGET_WORKER_ID" >"$ROOT/target-worker.log" 2>&1 &
TARGET_WORKER_PID=$!
sleep 2

WORKER_JSON="$("$ORCH_BIN" worker status --json)"
printf '%s\n' "$WORKER_JSON"
printf '%s' "$WORKER_JSON" | jq -e --arg worker "$TARGET_WORKER_ID" '(.workers | length) == 1 and (.workers[0].id == $worker)' >/dev/null

echo "== repo mapping =="
REGISTER_OUT="$("$ORCH_BIN" daemon repo register "$REPO_URL")"
printf '%s\n' "$REGISTER_OUT"
printf '%s' "$REGISTER_OUT" | grep "Registered repo mapping: $PROJECT_ID ->" >/dev/null

echo "== target-host run flow =="
RUN_ID_1="$(date +%Y%m%d-%H%M%S)-target-a"
RUN_ID_2="$(date +%Y%m%d-%H%M%S)-target-b"
RUN_OUT_1="$("$ORCH_BIN" --project "$PROJECT_ID" run target-local-live \
  --run-id "$RUN_ID_1" \
  --on "$TARGET_NAME" \
  --agent custom \
  --agent-cmd 'printf target-e2e-a; hostname; sleep 20' \
  --multiplexer tmux \
  --json)"
RUN_OUT_2="$("$ORCH_BIN" --project "$PROJECT_ID" run target-local-live-2 \
  --run-id "$RUN_ID_2" \
  --on "$TARGET_NAME" \
  --agent custom \
  --agent-cmd 'printf target-e2e-b; hostname; sleep 20' \
  --multiplexer tmux \
  --json)"
printf '%s\n' "$RUN_OUT_1"
printf '%s\n' "$RUN_OUT_2"
printf '%s' "$RUN_OUT_1" | jq -e '.ok == true' >/dev/null
printf '%s' "$RUN_OUT_2" | jq -e '.ok == true' >/dev/null

PS_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" ps --json)"
printf '%s\n' "$PS_OUT"
printf '%s' "$PS_OUT" | jq -e \
  --arg r1 "$RUN_ID_1" \
  --arg r2 "$RUN_ID_2" \
  --arg target "$TARGET_NAME" \
  --arg target_host "$TARGET_HOST" \
  --arg repo "$TARGET_PROJECT/worktrees" '
  .ok == true and
  ([.items[] | select(.run_id == $r1 or .run_id == $r2)] | length) == 2 and
  ([.items[] | select((.run_id == $r1 or .run_id == $r2) and .target == $target and .target_host == $target_host and (.worktree_path | startswith($repo)))] | length) == 2
' >/dev/null

SHOW_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" show "target-local-live#$RUN_ID_1" --json)"
printf '%s\n' "$SHOW_OUT"
printf '%s' "$SHOW_OUT" | jq -e --arg target "$TARGET_NAME" --arg target_host "$TARGET_HOST" '.ok == true and .target == $target and .target_host == $target_host' >/dev/null

echo "== cleanup runs =="
"$ORCH_BIN" --project "$PROJECT_ID" stop "target-local-live#$RUN_ID_1" --force >/dev/null
"$ORCH_BIN" --project "$PROJECT_ID" stop "target-local-live-2#$RUN_ID_2" --force >/dev/null

echo "TARGET_HOST_LOCAL_E2E_OK"
