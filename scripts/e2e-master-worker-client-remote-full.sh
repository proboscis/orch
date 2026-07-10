#!/usr/bin/env bash
set -euo pipefail

: "${REMOTE_HOST:?set REMOTE_HOST}"
: "${ZEUS_PROJECT_ROOT:?set ZEUS_PROJECT_ROOT}"
: "${ZEUS_ISSUES_PATH:?set ZEUS_ISSUES_PATH}"
: "${GH_REPO:?set GH_REPO}"

PROJECT_ID="${PROJECT_ID:-}"
GH_BASE_BRANCH="${GH_BASE_BRANCH:-main}"
ZEUS_ORCH_BIN="${ZEUS_ORCH_BIN:-orch}"
ZEUS_ENV_PREFIX="${ZEUS_ENV_PREFIX:-}"
TS="${TS:-$(date +%Y%m%d-%H%M%S)}"
ISSUE_ID="${ISSUE_ID:-remote-e2e-$TS}"
RUN_ID="${RUN_ID:-$TS-sample}"
AGENT_SCRIPT="${AGENT_SCRIPT:-/tmp/orch-remote-agent-$ISSUE_ID.sh}"
BRANCH="issue/$ISSUE_ID/run-$RUN_ID"
PR_NUMBER=""

cleanup() {
  set +e
  if [ -n "${PROJECT_ID:-}" ]; then
    ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN --project '$PROJECT_ID' stop '$ISSUE_ID#$RUN_ID' --force" >/dev/null 2>&1 || true
  fi
  if [ -n "${PR_NUMBER:-}" ]; then
    ssh "$REMOTE_HOST" "gh pr close '$PR_NUMBER' --repo '$GH_REPO' --comment 'Closing automated Remote E2E PR.' --delete-branch" >/dev/null 2>&1 || true
  fi
  ssh "$REMOTE_HOST" "rm -f '$ZEUS_ISSUES_PATH/issues/$ISSUE_ID.md' '$AGENT_SCRIPT'" >/dev/null 2>&1 || true
  ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN worker stop --all" >/dev/null 2>&1 || true
  ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN master kill" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN master start --listen tcp://0.0.0.0:7777" >/dev/null
ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN worker start" >/dev/null
ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN master status"
ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN worker status"

ssh "$REMOTE_HOST" "cat > '$ZEUS_ISSUES_PATH/issues/$ISSUE_ID.md' <<'EOF'
---
type: issue
id: $ISSUE_ID
title: Remote E2E sample
status: open
---

# Remote E2E sample
EOF"

REGISTER_OUT="$(ssh "$REMOTE_HOST" "cd '$ZEUS_PROJECT_ROOT' && $ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN daemon repo register '$ZEUS_PROJECT_ROOT'")"
printf '%s\n' "$REGISTER_OUT"
if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID="$(printf '%s\n' "$REGISTER_OUT" | sed -n 's/^Registered repo mapping: \([^ ]*\) -> .*$/\1/p' | head -n 1)"
fi
[ -n "$PROJECT_ID" ]
echo "PROJECT_ID=$PROJECT_ID"

ssh "$REMOTE_HOST" "cat > '$AGENT_SCRIPT' <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p e2e
cat > e2e/$ISSUE_ID.md <<'EOT'
# Remote E2E sample change
EOT
git add e2e/$ISSUE_ID.md
git commit -m 'chore(e2e): sample remote run $ISSUE_ID'
git push -u origin HEAD
branch=\$(git rev-parse --abbrev-ref HEAD)
gh pr create --repo '$GH_REPO' --title 'chore(e2e): sample remote run $ISSUE_ID' --body 'Automated sample PR from Remote E2E.' --base '$GH_BASE_BRANCH' --head \"\$branch\"
EOF
chmod +x '$AGENT_SCRIPT'"

RUN_OUT="$(ssh "$REMOTE_HOST" "cd '$ZEUS_PROJECT_ROOT' && $ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN --project '$PROJECT_ID' run '$ISSUE_ID' --run-id '$RUN_ID' --agent custom --agent-cmd 'bash $AGENT_SCRIPT' --json")"
printf '%s\n' "$RUN_OUT"
printf '%s' "$RUN_OUT" | jq -e '.ok == true' >/dev/null

PR_OUT="$(ssh "$REMOTE_HOST" "gh pr list --repo '$GH_REPO' --head '$BRANCH' --state open --json number,url")"
printf '%s\n' "$PR_OUT"
PR_NUMBER="$(printf '%s' "$PR_OUT" | jq -r '.[0].number')"
[ "$PR_NUMBER" != "null" ]
ssh "$REMOTE_HOST" "gh pr close '$PR_NUMBER' --repo '$GH_REPO' --comment 'Closing automated Remote E2E PR.' --delete-branch"
PR_NUMBER=""

ssh "$REMOTE_HOST" "$ZEUS_ENV_PREFIX $ZEUS_ORCH_BIN --project '$PROJECT_ID' stop '$ISSUE_ID#$RUN_ID' --force"
ssh "$REMOTE_HOST" "rm -f '$ZEUS_ISSUES_PATH/issues/$ISSUE_ID.md' '$AGENT_SCRIPT'"

echo "ZEUS_FULL_E2E_OK"
