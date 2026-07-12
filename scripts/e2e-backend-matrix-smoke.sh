#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/run-control-common.sh"

export ROOT="${ROOT:-$(mktemp -d /tmp/orch-e2e-backends-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-}"
RUN_ZELLIJ_LANE="${RUN_ZELLIJ_LANE:-auto}"
RUN_OPENCODE_LANE="${RUN_OPENCODE_LANE:-0}"
RUN_REAL_CLAUDE_LANE="${RUN_REAL_CLAUDE_LANE:-0}"
RUN_REAL_CODEX_LANE="${RUN_REAL_CODEX_LANE:-0}"
REQUIRE_TMUX="${REQUIRE_TMUX:-1}"

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
echo "RUN_ZELLIJ_LANE=$RUN_ZELLIJ_LANE"
echo "RUN_OPENCODE_LANE=$RUN_OPENCODE_LANE"
echo "RUN_REAL_CLAUDE_LANE=$RUN_REAL_CLAUDE_LANE"
echo "RUN_REAL_CODEX_LANE=$RUN_REAL_CODEX_LANE"
mkdir -p "$ROOT"/{home,runtime,state,data,bin,repo/.orch,issues-store/issues,issues-store/runs,origin/example}

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
# Backend Matrix Automated E2E Repo
EOF

git -C "$PROJECT" init >/dev/null
git -C "$PROJECT" branch -M main
git -C "$PROJECT" config user.email e2e@example.com
git -C "$PROJECT" config user.name E2E

git init --bare "$ROOT/origin/example/backend-matrix.git" >/dev/null
git -C "$ROOT/origin/example/backend-matrix.git" symbolic-ref HEAD refs/heads/main
REPO_URL="file://$ROOT/origin/example/backend-matrix.git"
PROJECT_ID="example-backend-matrix"

git -C "$PROJECT" remote add origin "$REPO_URL"
git -C "$PROJECT" add .
git -C "$PROJECT" commit -m "init" >/dev/null
git -C "$PROJECT" push -u origin HEAD >/dev/null

for lane in tmux zellij claude codex opencode; do
  extra_body=""
  case "$lane" in
    claude)
      extra_body=$(cat <<'EOF'

When you start, reply with exactly:
BACKEND_MATRIX_READY_CLAUDE

Then wait for the next user message.

When you receive the next user message:
- reply with exactly `BACKEND_MATRIX_ACK_CLAUDE`
- echo the full user message verbatim
- then wait
EOF
)
      ;;
    codex)
      extra_body=$(cat <<'EOF'

When you start, reply with exactly:
BACKEND_MATRIX_READY_CODEX

Then wait for the next user message.

When you receive the next user message:
- reply with exactly `BACKEND_MATRIX_ACK_CODEX`
- echo the full user message verbatim
- then wait
EOF
)
      ;;
  esac
  cat > "$ISSUES/issues/e2e-$lane.md" <<EOF
---
type: issue
id: e2e-$lane
title: Backend matrix lane $lane
status: open
---

# Backend matrix lane $lane
$extra_body
EOF
done

cd "$PROJECT"

if ! command -v tmux >/dev/null 2>&1; then
  if [ "$REQUIRE_TMUX" = "1" ]; then
    echo "tmux is required for backend smoke" >&2
    exit 1
  fi
fi
command -v python3 >/dev/null 2>&1 || { echo "python3 is required for backend smoke" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required for backend smoke" >&2; exit 1; }

cat > "$ROOT/bin/control-repl.py" <<'EOF'
#!/usr/bin/env python3
import sys

lane = sys.argv[1] if len(sys.argv) > 1 else "lane"
print(f"READY:{lane}", flush=True)
for line in sys.stdin:
    text = line.rstrip("\r\n")
    if not text:
        continue
    print(f"ECHO:{lane}:{text}", flush=True)
    if text == "quit":
        break
EOF
chmod +x "$ROOT/bin/control-repl.py"

if [ "$RUN_REAL_CLAUDE_LANE" != "1" ]; then
cat > "$ROOT/bin/claude" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  echo "fake claude 0.0"
  exit 0
fi
printf 'fake claude ready\n'
sleep 30
EOF
fi
if [ "$RUN_REAL_CODEX_LANE" != "1" ]; then
cat > "$ROOT/bin/codex" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  echo "fake codex 0.0"
  exit 0
fi
# Mimic the real codex boot contract (ADR-0005 R1): codex writes a rollout
# file with session_meta {id, cwd} at boot. The launch ladder resolves the
# run's agent_session identity from it; a fake that skips this blocks the
# ladder for its full retry budget and the lane times out.
codex_home="${CODEX_HOME:-$HOME/.codex}"
day="$(date +%Y/%m/%d)"
mkdir -p "$codex_home/sessions/$day"
printf '{"timestamp":"t","type":"session_meta","payload":{"id":"fake-codex-%s","cwd":"%s"}}\n' "$$" "$PWD" \
  > "$codex_home/sessions/$day/rollout-fake-$$.jsonl"
printf 'fake codex ready\n'
sleep 30
EOF
fi
chmod +x "$ROOT/bin"/claude "$ROOT/bin"/codex 2>/dev/null || true
export PATH="$ROOT/bin:$PATH"

[ "$RUN_REAL_CLAUDE_LANE" != "1" ] || command -v claude >/dev/null 2>&1 || { echo "RUN_REAL_CLAUDE_LANE=1 but claude not found" >&2; exit 1; }
[ "$RUN_REAL_CODEX_LANE" != "1" ] || command -v codex >/dev/null 2>&1 || { echo "RUN_REAL_CODEX_LANE=1 but codex not found" >&2; exit 1; }

"$ORCH_BIN" master start --listen "tcp://127.0.0.1:0" >/dev/null
sleep 1
"$ORCH_BIN" worker start >/dev/null
sleep 2
"$ORCH_BIN" daemon repo register "$REPO_URL" >/dev/null

run_json_ok() {
  printf '%s' "$1" | jq -e '.ok == true' >/dev/null
}

capture_contains() {
  local ref="$1"
  local marker="$2"
  local attempts="${3:-5}"
  local delay="${4:-1}"
  local out
  local attempt
  for attempt in $(seq 1 "$attempts"); do
    out="$("$ORCH_BIN" --json --project "$PROJECT_ID" capture "$ref")"
    printf '%s\n' "$out"
    if printf '%s' "$out" | jq -e --arg marker "$marker" '.ok == true and (.content | contains($marker))' >/dev/null; then
      return 0
    fi
    sleep "$delay"
  done
  echo "capture for $ref never contained marker: $marker" >&2
  return 1
}

maybe_accept_claude_trust_prompt() {
  local ref="$1"
  local session_name="$2"
  local out

  out="$("$ORCH_BIN" --json --project "$PROJECT_ID" capture "$ref")"
  printf '%s\n' "$out"
  if printf '%s' "$out" | jq -e '.ok == true and (.content | contains("Quick safety check"))' >/dev/null; then
    tmux send-keys -t "$session_name" Enter
    sleep 2
  fi
}

capture_ok() {
  local ref="$1"
  local out
  out="$("$ORCH_BIN" --json --project "$PROJECT_ID" capture "$ref")"
  printf '%s\n' "$out"
  printf '%s' "$out" | jq -e '.ok == true' >/dev/null
}

send_ok() {
  local ref="$1"
  local msg="$2"
  local out
  out="$("$ORCH_BIN" --json --project "$PROJECT_ID" send "$ref" "$msg")"
  printf '%s\n' "$out"
  run_json_ok "$out"
}

send_stdin_ok() {
  local ref="$1"
  local payload
  local out
  payload="$(cat)"
  out="$(printf '%s' "$payload" | "$ORCH_BIN" --json --project "$PROJECT_ID" send "$ref")"
  printf '%s\n' "$out"
  run_json_ok "$out"
}

restart_ok() {
  local ref="$1"
  local extra_args=("${@:2}")
  local out
  out="$("$ORCH_BIN" --json --project "$PROJECT_ID" restart-from "$ref" "${extra_args[@]}")"
  printf '%s\n' "$out" >&2
  run_json_ok "$out"
  printf '%s' "$out" | jq -r '.issue_id + "#" + .run_id'
}

stop_run() {
  "$ORCH_BIN" --project "$PROJECT_ID" stop "$1" --force >/dev/null
}

echo "== tmux lane =="
RUN_TMUX="$(date +%Y%m%d-%H%M%S)-tmux"
OUT_TMUX="$("$ORCH_BIN" --json --project "$PROJECT_ID" run e2e-tmux \
  --run-id "$RUN_TMUX" \
  --agent custom \
  --agent-cmd "python3 -u $ROOT/bin/control-repl.py tmux" \
  --multiplexer tmux)"
printf '%s\n' "$OUT_TMUX"
run_json_ok "$OUT_TMUX"
capture_contains "e2e-tmux#$RUN_TMUX" "READY:tmux"
send_stdin_ok "e2e-tmux#$RUN_TMUX" <<'EOF'
tmux-send-line-1
tmux-send-line-2
EOF
TMUX_CAPTURE="$(capture_until_contains "\"$ORCH_BIN\" --project \"$PROJECT_ID\" capture \"e2e-tmux#$RUN_TMUX\" --lines 80" "ECHO:tmux:tmux-send-line-2" 30 1)"
printf '%s\n' "$TMUX_CAPTURE"
printf '%s\n' "$TMUX_CAPTURE" | grep -F "ECHO:tmux:tmux-send-line-1" >/dev/null
stop_run "e2e-tmux#$RUN_TMUX"
RESTART_TMUX="$(restart_ok "e2e-tmux#$RUN_TMUX" --agent custom --agent-cmd "python3 -u $ROOT/bin/control-repl.py tmux-restart" --multiplexer tmux)"
capture_contains "$RESTART_TMUX" "READY:tmux-restart"
stop_run "$RESTART_TMUX"

if [ "$RUN_ZELLIJ_LANE" = "1" ] || { [ "$RUN_ZELLIJ_LANE" = "auto" ] && command -v zellij >/dev/null 2>&1; }; then
  echo "== zellij lane =="
  RUN_ZELLIJ="$(date +%Y%m%d-%H%M%S)-zellij"
  OUT_ZELLIJ="$("$ORCH_BIN" --json --project "$PROJECT_ID" run e2e-zellij \
    --run-id "$RUN_ZELLIJ" \
    --agent custom \
    --agent-cmd "python3 -u $ROOT/bin/control-repl.py zellij" \
    --multiplexer zellij)"
  printf '%s\n' "$OUT_ZELLIJ"
  run_json_ok "$OUT_ZELLIJ"
  capture_ok "e2e-zellij#$RUN_ZELLIJ"
  send_stdin_ok "e2e-zellij#$RUN_ZELLIJ" <<'EOF'
zellij-send-line-1
zellij-send-line-2
EOF
  capture_ok "e2e-zellij#$RUN_ZELLIJ"
  stop_run "e2e-zellij#$RUN_ZELLIJ"
  RESTART_ZELLIJ="$(restart_ok "e2e-zellij#$RUN_ZELLIJ" --agent custom --agent-cmd "python3 -u $ROOT/bin/control-repl.py zellij-restart" --multiplexer zellij)"
  capture_ok "$RESTART_ZELLIJ"
  stop_run "$RESTART_ZELLIJ"
else
  echo "== zellij lane skipped =="
fi

echo "== claude lane =="
RUN_CLAUDE="$(date +%Y%m%d-%H%M%S)-claude"
OUT_CLAUDE="$("$ORCH_BIN" --json --project "$PROJECT_ID" run e2e-claude \
  --run-id "$RUN_CLAUDE" \
  --agent claude \
  --multiplexer tmux)"
printf '%s\n' "$OUT_CLAUDE"
run_json_ok "$OUT_CLAUDE"
if [ "$RUN_REAL_CLAUDE_LANE" = "1" ]; then
  CLAUDE_SESSION="$(printf '%s' "$OUT_CLAUDE" | jq -r '.session_name')"
  maybe_accept_claude_trust_prompt "e2e-claude#$RUN_CLAUDE" "$CLAUDE_SESSION"
  attach_expect_live "$ORCH_BIN" --project "$PROJECT_ID" attach "e2e-claude#$RUN_CLAUDE"
  capture_contains "e2e-claude#$RUN_CLAUDE" "BACKEND_MATRIX_READY_CLAUDE" 60 2
  send_stdin_ok "e2e-claude#$RUN_CLAUDE" <<'EOF'
real-claude-line-1
real-claude-line-2
EOF
  capture_contains "e2e-claude#$RUN_CLAUDE" "BACKEND_MATRIX_ACK_CLAUDE" 60 2
  capture_contains "e2e-claude#$RUN_CLAUDE" "real-claude-line-2" 60 2
else
  capture_contains "e2e-claude#$RUN_CLAUDE" "fake claude ready"
  send_ok "e2e-claude#$RUN_CLAUDE" "claude-send-check"
fi
# ADR-0005 R1: a claude run records its minted session UUID as agent_session.
SHOW_CLAUDE="$("$ORCH_BIN" --json --project "$PROJECT_ID" show "e2e-claude#$RUN_CLAUDE")"
printf '%s\n' "$SHOW_CLAUDE" | jq -e '(.agent_session_id | length) > 0 and .agent_session_generation == 1' >/dev/null \
  || { echo "claude run missing agent_session identity: $SHOW_CLAUDE" >&2; exit 1; }
stop_run "e2e-claude#$RUN_CLAUDE"

echo "== codex lane =="
RUN_CODEX="$(date +%Y%m%d-%H%M%S)-codex"
OUT_CODEX="$("$ORCH_BIN" --json --project "$PROJECT_ID" run e2e-codex \
  --run-id "$RUN_CODEX" \
  --agent codex \
  --multiplexer tmux)"
printf '%s\n' "$OUT_CODEX"
run_json_ok "$OUT_CODEX"
if [ "$RUN_REAL_CODEX_LANE" = "1" ]; then
  attach_expect_live "$ORCH_BIN" --project "$PROJECT_ID" attach "e2e-codex#$RUN_CODEX"
  capture_contains "e2e-codex#$RUN_CODEX" "BACKEND_MATRIX_READY_CODEX" 60 2
  send_stdin_ok "e2e-codex#$RUN_CODEX" <<'EOF'
real-codex-line-1
real-codex-line-2
EOF
  capture_contains "e2e-codex#$RUN_CODEX" "BACKEND_MATRIX_ACK_CODEX" 60 2
  capture_contains "e2e-codex#$RUN_CODEX" "real-codex-line-2" 60 2
else
  capture_contains "e2e-codex#$RUN_CODEX" "fake codex ready"
  send_ok "e2e-codex#$RUN_CODEX" "codex-send-check"
fi
# ADR-0005 R1: a codex run's identity is resolved from the boot rollout.
SHOW_CODEX="$("$ORCH_BIN" --json --project "$PROJECT_ID" show "e2e-codex#$RUN_CODEX")"
printf '%s\n' "$SHOW_CODEX" | jq -e '(.agent_session_id | length) > 0 and .agent_session_generation == 1' >/dev/null \
  || { echo "codex run missing agent_session identity: $SHOW_CODEX" >&2; exit 1; }
stop_run "e2e-codex#$RUN_CODEX"

if [ "$RUN_OPENCODE_LANE" = "1" ]; then
  if ! command -v opencode >/dev/null 2>&1; then
    echo "opencode lane requested but opencode not found" >&2
    exit 1
  fi
  echo "== opencode lane =="
  RUN_OPENCODE="$(date +%Y%m%d-%H%M%S)-opencode"
  OUT_OPENCODE="$("$ORCH_BIN" --json --project "$PROJECT_ID" run e2e-opencode \
    --run-id "$RUN_OPENCODE" \
    --agent opencode)"
  printf '%s\n' "$OUT_OPENCODE"
  run_json_ok "$OUT_OPENCODE"
  SEND_DRY="$("$ORCH_BIN" --json --project "$PROJECT_ID" send "e2e-opencode#$RUN_OPENCODE" "probe" --dry-run)"
  printf '%s\n' "$SEND_DRY"
  run_json_ok "$SEND_DRY"
  stop_run "e2e-opencode#$RUN_OPENCODE"
  RESTART_OPENCODE="$(restart_ok "e2e-opencode#$RUN_OPENCODE")"
  stop_run "$RESTART_OPENCODE"
else
  echo "== opencode lane skipped =="
fi

echo "BACKEND_MATRIX_SMOKE_OK"
