#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/run-control-common.sh"

export ROOT="${ROOT:-$(mktemp -d /tmp/orch-run-control-local-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-}"

cleanup() {
  set +e
  if [ -n "${ORCH_BIN:-}" ] && [ -x "$ORCH_BIN" ]; then
    if [ -n "${PROJECT_ID:-}" ] && [ -n "${ISSUE_ID:-}" ] && [ -n "${RUN_ID:-}" ]; then
      "$ORCH_BIN" --project "$PROJECT_ID" stop "$ISSUE_ID#$RUN_ID" --force >/dev/null 2>&1 || true
    fi
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
command -v python3 >/dev/null 2>&1 || { echo "python3 is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

echo "ROOT=$ROOT"
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
HELPER="$ROOT/bin/control-repl.py"

cat > "$PROJECT/.orch/config.yaml" <<EOF
issues:
  path: $ISSUES
agent_multiplexer: tmux
EOF

cat > "$PROJECT/README.md" <<'EOF'
# Run Control Local E2E Repo
EOF

cat > "$HELPER" <<'EOF'
import sys

print("READY", flush=True)
for line in sys.stdin:
    text = line.rstrip("\r\n")
    if not text:
        continue
    print(f"ECHO:{text}", flush=True)
    if text == "quit":
        break
EOF
chmod +x "$HELPER"

git -C "$PROJECT" init >/dev/null
git -C "$PROJECT" branch -M main
git -C "$PROJECT" config user.email e2e@example.com
git -C "$PROJECT" config user.name E2E

git init --bare "$ROOT/origin/example/run-control-local.git" >/dev/null
git -C "$ROOT/origin/example/run-control-local.git" symbolic-ref HEAD refs/heads/main
REPO_URL="file://$ROOT/origin/example/run-control-local.git"
PROJECT_ID="example-run-control-local"

git -C "$PROJECT" remote add origin "$REPO_URL"
git -C "$PROJECT" add .
git -C "$PROJECT" commit -m "init" >/dev/null
git -C "$PROJECT" push -u origin HEAD >/dev/null

cd "$PROJECT"

"$ORCH_BIN" master start >/dev/null
sleep 1
"$ORCH_BIN" worker start >/dev/null
sleep 1
"$ORCH_BIN" daemon repo register "$REPO_URL" >/dev/null

ISSUE_ID="${ISSUE_ID:-run-control-local}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-local-control}"
MESSAGE_LINE_1="${MESSAGE_LINE_1:-matrix-local-line-1}"
MESSAGE_LINE_2="${MESSAGE_LINE_2:-matrix-local-line-2}"

"$ORCH_BIN" issue create "$ISSUE_ID" --title "Run control local matrix" >/dev/null

RUN_OUT="$("$ORCH_BIN" --project "$PROJECT_ID" run "$ISSUE_ID" \
  --run-id "$RUN_ID" \
  --agent custom \
  --agent-cmd "python3 -u $HELPER" \
  --multiplexer tmux \
  --json)"
printf '%s\n' "$RUN_OUT"
printf '%s' "$RUN_OUT" | jq -e '.ok == true' >/dev/null

CAPTURE_CMD="\"$ORCH_BIN\" --project \"$PROJECT_ID\" capture \"$ISSUE_ID#$RUN_ID\" --lines 50"
FIRST_CAPTURE="$(capture_until_contains "$CAPTURE_CMD" "READY")"
printf '%s\n' "$FIRST_CAPTURE"

SEND_OUT="$("$ORCH_BIN" --json --project "$PROJECT_ID" send "$ISSUE_ID#$RUN_ID" <<EOF
$MESSAGE_LINE_1
$MESSAGE_LINE_2
EOF
)"
printf '%s\n' "$SEND_OUT"
printf '%s' "$SEND_OUT" | jq -e '.ok == true' >/dev/null

SECOND_CAPTURE="$(capture_until_contains "$CAPTURE_CMD" "ECHO:$MESSAGE_LINE_2")"
printf '%s\n' "$SECOND_CAPTURE"
printf '%s\n' "$SECOND_CAPTURE" | grep -F "ECHO:$MESSAGE_LINE_1" >/dev/null

attach_expect_live "$ORCH_BIN" --project "$PROJECT_ID" attach "$ISSUE_ID#$RUN_ID"

"$ORCH_BIN" --project "$PROJECT_ID" send "$ISSUE_ID#$RUN_ID" quit >/dev/null || true
"$ORCH_BIN" --project "$PROJECT_ID" stop "$ISSUE_ID#$RUN_ID" --force >/dev/null

echo "RUN_CONTROL_LOCAL_E2E_OK"
