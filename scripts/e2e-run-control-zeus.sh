#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/run-control-common.sh"

ZEUS_HOST="${ZEUS_HOST:-zeus}"
ORCH_BIN="${ORCH_BIN:-}"
TARGET_NAME="${TARGET_NAME:-zeus}"
TARGET_HOST="${TARGET_HOST:-$ZEUS_HOST}"
TS="${TS:-$(date +%Y%m%d-%H%M%S)}"
REMOTE_BASE="${REMOTE_BASE:-/tmp/orch-run-control-zeus-$TS}"
ISSUE_ID="${ISSUE_ID:-run-control-zeus-$TS}"
RUN_ID="${RUN_ID:-$TS-zeus-control}"
MESSAGE_LINE_1="${MESSAGE_LINE_1:-matrix-remote-line-1}"
MESSAGE_LINE_2="${MESSAGE_LINE_2:-matrix-remote-line-2}"
REMOTE_REPO_NAME="run-control-zeus-$TS.git"
TARGET_REMOTE_ADDR="${TARGET_REMOTE_ADDR:-127.0.0.1:7777}"

command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "python3 is required" >&2; exit 1; }

if [ -z "$ORCH_BIN" ]; then
  ORCH_BIN="$(mktemp /tmp/orch-zeus-current-XXXXXX)"
  go build -o "$ORCH_BIN" ./cmd/orch
fi
test -x "$ORCH_BIN" || { echo "ORCH_BIN not executable: $ORCH_BIN" >&2; exit 1; }

ssh "$ZEUS_HOST" "command -v tmux >/dev/null 2>&1 && command -v python3 >/dev/null 2>&1 && export PATH=\"\$HOME/.local/bin:\$PATH\" && command -v orch >/dev/null 2>&1"

cleanup() {
  set +e
  "$ORCH_BIN" --remote "$ZEUS_HOST:7777" --project "${PROJECT_ID:-missing}" stop "$ISSUE_ID#$RUN_ID" --force >/dev/null 2>&1 || true
  ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; ORCH_REMOTE=$TARGET_REMOTE_ADDR orch worker stop >/dev/null 2>&1 || true; orch --remote= master kill >/dev/null 2>&1 || true; rm -rf '$REMOTE_BASE'" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "ZEUS_HOST=$ZEUS_HOST"
echo "REMOTE_BASE=$REMOTE_BASE"
echo "ISSUE_ID=$ISSUE_ID"
echo "RUN_ID=$RUN_ID"

ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; rm -rf '$REMOTE_BASE'; mkdir -p '$REMOTE_BASE'/repo/.orch '$REMOTE_BASE'/issues-store/issues '$REMOTE_BASE'/issues-store/runs '$REMOTE_BASE'/origin/example"

ssh "$ZEUS_HOST" "cat > '$REMOTE_BASE/repo/.orch/config.yaml' <<'EOF'
issues:
  path: $REMOTE_BASE/issues-store
agent_multiplexer: tmux
targets:
  - name: $TARGET_NAME
    host: $TARGET_HOST
EOF"

ssh "$ZEUS_HOST" "cat > '$REMOTE_BASE/repo/README.md' <<'EOF'
# Run Control Zeus E2E Repo
EOF"

ssh "$ZEUS_HOST" "cat > '$REMOTE_BASE/control-repl.py' <<'EOF'
import sys

print(\"READY\", flush=True)
for line in sys.stdin:
    text = line.rstrip(\"\\r\\n\")
    if not text:
        continue
    print(f\"ECHO:{text}\", flush=True)
    if text == \"quit\":
        break
EOF
chmod +x '$REMOTE_BASE/control-repl.py'"

ssh "$ZEUS_HOST" "git -C '$REMOTE_BASE/repo' init >/dev/null && \
  git -C '$REMOTE_BASE/repo' branch -M main && \
  git -C '$REMOTE_BASE/repo' config user.email e2e@example.com && \
  git -C '$REMOTE_BASE/repo' config user.name E2E && \
  git init --bare '$REMOTE_BASE/origin/example/$REMOTE_REPO_NAME' >/dev/null && \
  git -C '$REMOTE_BASE/repo' remote add origin 'file://$REMOTE_BASE/origin/example/$REMOTE_REPO_NAME' && \
  git -C '$REMOTE_BASE/repo' add . && \
  git -C '$REMOTE_BASE/repo' commit -m 'init' >/dev/null && \
  git -C '$REMOTE_BASE/repo' push -u origin HEAD >/dev/null"

REGISTER_OUT="$(ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; cd '$REMOTE_BASE/repo' && orch --remote= daemon repo register '$REMOTE_BASE/repo'")"
printf '%s\n' "$REGISTER_OUT"
PROJECT_ID="$(printf '%s\n' "$REGISTER_OUT" | sed -n 's/^Registered repo mapping: \([^ ]*\) -> .*$/\1/p' | head -n 1)"
[ -n "$PROJECT_ID" ]
echo "PROJECT_ID=$PROJECT_ID"

ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; orch --remote= master kill >/dev/null 2>&1 || true; orch --remote= master start --listen tcp://0.0.0.0:7777 >/dev/null"
for _ in $(seq 1 30); do
  if ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; orch --remote '$TARGET_REMOTE_ADDR' master status >/dev/null" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; orch --remote '$TARGET_REMOTE_ADDR' master status >/dev/null"
ssh "$ZEUS_HOST" "export PATH=\"\$HOME/.local/bin:\$PATH\"; ORCH_REMOTE=$TARGET_REMOTE_ADDR orch worker start >/dev/null"

"$ORCH_BIN" --remote "$ZEUS_HOST:7777" --project "$PROJECT_ID" issue create "$ISSUE_ID" --title "Run control Zeus matrix" >/dev/null

RUN_OUT="$("$ORCH_BIN" --remote "$ZEUS_HOST:7777" --project "$PROJECT_ID" run "$ISSUE_ID" \
  --run-id "$RUN_ID" \
  --on "$TARGET_NAME" \
  --agent custom \
  --agent-cmd "python3 -u $REMOTE_BASE/control-repl.py" \
  --multiplexer tmux \
  --json)"
printf '%s\n' "$RUN_OUT"
printf '%s' "$RUN_OUT" | jq -e '.ok == true' >/dev/null

CAPTURE_CMD="\"$ORCH_BIN\" --remote \"$ZEUS_HOST:7777\" --project \"$PROJECT_ID\" capture \"$ISSUE_ID#$RUN_ID\" --lines 50"
FIRST_CAPTURE="$(capture_until_contains "$CAPTURE_CMD" "READY")"
printf '%s\n' "$FIRST_CAPTURE"

SEND_OUT="$("$ORCH_BIN" --json --remote "$ZEUS_HOST:7777" --project "$PROJECT_ID" send "$ISSUE_ID#$RUN_ID" <<EOF
$MESSAGE_LINE_1
$MESSAGE_LINE_2
EOF
)"
printf '%s\n' "$SEND_OUT"
printf '%s' "$SEND_OUT" | jq -e '.ok == true' >/dev/null

SECOND_CAPTURE="$(capture_until_contains "$CAPTURE_CMD" "ECHO:$MESSAGE_LINE_2")"
printf '%s\n' "$SECOND_CAPTURE"
printf '%s\n' "$SECOND_CAPTURE" | grep -F "ECHO:$MESSAGE_LINE_1" >/dev/null

attach_expect_live "$ORCH_BIN" --remote "$ZEUS_HOST:7777" --project "$PROJECT_ID" attach "$ISSUE_ID#$RUN_ID"

"$ORCH_BIN" --remote "$ZEUS_HOST:7777" --project "$PROJECT_ID" send "$ISSUE_ID#$RUN_ID" quit >/dev/null || true
"$ORCH_BIN" --remote "$ZEUS_HOST:7777" --project "$PROJECT_ID" stop "$ISSUE_ID#$RUN_ID" --force >/dev/null

echo "RUN_CONTROL_ZEUS_E2E_OK"
