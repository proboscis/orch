#!/usr/bin/env bash
set -euo pipefail

export ROOT="${ROOT:-$(mktemp -d /tmp/orch-session-reaper-e2e-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-}"

tmux_cmd() {
  env -u TMUX tmux "$@"
}

cleanup() {
  set +e
  if [ -n "${SESSION_NAME:-}" ]; then
    tmux_cmd kill-session -t "$SESSION_NAME" >/dev/null 2>&1 || true
  fi
  tmux_cmd kill-session -t orch-control-reaper-e2e >/dev/null 2>&1 || true
  tmux_cmd kill-session -t orch-monitor-reaper-e2e >/dev/null 2>&1 || true
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

command -v tmux >/dev/null 2>&1 || { echo "tmux is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "python3 is required" >&2; exit 1; }

echo "ROOT=$ROOT"
mkdir -p "$ROOT"/{home,runtime,state,data,bin,repo/.orch,issues-store/issues,issues-store/runs,origin/example}

export HOME="$ROOT/home"
export XDG_RUNTIME_DIR="$ROOT/runtime"
export XDG_STATE_HOME="$ROOT/state"
export XDG_DATA_HOME="$ROOT/data"
unset ORCH_PROJECT ORCH_REMOTE TMUX

if [ -z "$ORCH_BIN" ]; then
  go build -o "$ROOT/bin/orch" ./cmd/orch
  ORCH_BIN="$ROOT/bin/orch"
fi

PROJECT="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ["ROOT"], "repo"))))
PY
)"
ISSUES="$(python3 - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ["ROOT"], "issues-store"))))
PY
)"

cat > "$PROJECT/.orch/config.yaml" <<EOF
issues:
  path: $ISSUES
agent_multiplexer: tmux
no_pr: true
reaper:
  enabled: true
  terminal_grace_minutes: 0
  resolved_issue_grace_minutes: 60
  idle_ttl_hours: 168
EOF

cat > "$PROJECT/README.md" <<'EOF'
# Session reaper E2E
EOF

cat > "$ROOT/bin/fake-agent.py" <<'EOF'
import time

print("Task completed successfully", flush=True)
time.sleep(180)
EOF

git -C "$PROJECT" init >/dev/null
git -C "$PROJECT" branch -M main
git -C "$PROJECT" config user.email e2e@example.com
git -C "$PROJECT" config user.name E2E
git init --bare "$ROOT/origin/example/session-reaper.git" >/dev/null
git -C "$ROOT/origin/example/session-reaper.git" symbolic-ref HEAD refs/heads/main
REPO_URL="file://$ROOT/origin/example/session-reaper.git"
PROJECT_ID="example-session-reaper"
git -C "$PROJECT" remote add origin "$REPO_URL"
git -C "$PROJECT" add .
git -C "$PROJECT" commit -m init >/dev/null
git -C "$PROJECT" push -u origin HEAD >/dev/null

cd "$PROJECT"
"$ORCH_BIN" master start --listen tcp://127.0.0.1:0 >/dev/null
sleep 1
"$ORCH_BIN" daemon repo register "$REPO_URL" >/dev/null
"$ORCH_BIN" issue create reaper-e2e --title "Session reaper E2E" >/dev/null

# These reserved families prove L2: the run-session reaper must not touch them.
tmux_cmd new-session -d -s orch-control-reaper-e2e 'sleep 180'
tmux_cmd new-session -d -s orch-monitor-reaper-e2e 'sleep 180'

RUN_ID="$(date +%Y%m%d-%H%M%S)"
RUN_JSON="$("$ORCH_BIN" --json --project "$PROJECT_ID" run reaper-e2e \
  --run-id "$RUN_ID" \
  --agent custom \
  --agent-cmd "python3 -u $ROOT/bin/fake-agent.py" \
  --multiplexer tmux)"
printf '%s\n' "$RUN_JSON"
printf '%s' "$RUN_JSON" | jq -e '.ok == true' >/dev/null
SESSION_NAME="$(printf '%s' "$RUN_JSON" | jq -r '.session_name')"
RUN_FILE="$ISSUES/runs/reaper-e2e/$RUN_ID.md"

deadline=$((SECONDS + 30))
while :; do
  CAPTURE_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" capture "reaper-e2e#$RUN_ID" --lines 50 2>/dev/null || true)"
  if printf '%s' "$CAPTURE_OUT" | grep -F "Task completed successfully" >/dev/null; then
    break
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    printf '%s\n' "$CAPTURE_OUT" >&2
    echo "fake run did not expose its completion marker" >&2
    exit 1
  fi
  sleep 1
done
printf '%s\n' "$CAPTURE_OUT"

# Replay the stored incident state while deliberately leaving the fake agent's
# tmux session alive. Stop the daemon while installing the historical fixture,
# then restart it so its FileStore baseline is established from that state;
# production writes remain daemon-owned. The identity artifact is the same
# durable fact R1 records for claude/codex.
"$ORCH_BIN" master kill >/dev/null
EVENT_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf -- '- %s | status | done | source=daemon\n' "$EVENT_TS" >> "$RUN_FILE"
printf -- '- %s | artifact | agent_session | backend=custom | generation=1 | id=fake-session-e2e\n' "$EVENT_TS" >> "$RUN_FILE"
"$ORCH_BIN" master start --listen tcp://127.0.0.1:0 >/dev/null
sleep 1
"$ORCH_BIN" daemon repo register "$REPO_URL" >/dev/null
SHOW_JSON="$("$ORCH_BIN" --json --project "$PROJECT_ID" show "reaper-e2e#$RUN_ID" --tail 200)"
printf '%s' "$SHOW_JSON" | jq -e '.status == "done" and .agent_session_generation == 1' >/dev/null

REPAIR_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" repair --dry-run)"
printf '%s\n' "$REPAIR_OUT"
printf '%s' "$REPAIR_OUT" | grep -F "terminal-but-alive session: $SESSION_NAME" >/dev/null

deadline=$((SECONDS + 80))
while tmux_cmd has-session -t "$SESSION_NAME" >/dev/null 2>&1; do
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "session reaper did not kill $SESSION_NAME within one interval" >&2
    exit 1
  fi
  sleep 1
done

SHOW_JSON="$("$ORCH_BIN" --json --project "$PROJECT_ID" show "reaper-e2e#$RUN_ID" --tail 200)"
printf '%s\n' "$SHOW_JSON"
printf '%s' "$SHOW_JSON" | jq -e '
  .status == "done" and
  ([.events[] | select(.type == "status" and .name == "failed")] | length) == 0 and
  ([.events[] | select(.type == "artifact" and .name == "session_snapshot")] | length) >= 1 and
  ([.events[] | select(.type == "note" and .name == "daemon_notice" and .attrs.kind == "session_reaped" and .attrs.generation == "1" and .attrs.reason == "terminal_grace")] | length) >= 1
' >/dev/null

SNAPSHOT_PATH="$(printf '%s' "$SHOW_JSON" | jq -r '[.events[] | select(.type == "artifact" and .name == "session_snapshot")][-1].attrs.path')"
test -f "$SNAPSHOT_PATH"
grep -F "Task completed successfully" "$SNAPSHOT_PATH" >/dev/null
tmux_cmd has-session -t orch-control-reaper-e2e >/dev/null
tmux_cmd has-session -t orch-monitor-reaper-e2e >/dev/null

echo "SNAPSHOT_PATH=$SNAPSHOT_PATH"
echo "SESSION_REAPER_E2E_OK"
