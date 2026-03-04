# Master/Worker/Client Manual E2E

This guide validates the cluster-facing CLI behavior by running real `orch`
commands (not `go test`).

## Scope

This checklist verifies the command-plane path:

```
orch client CLI -> orch-master daemon -> orch-worker (external process)
```

It covers:

1. `master` lifecycle commands
2. `worker` lifecycle commands (external process)
3. local client run/ps/show/stop flow
4. remote master reachability via `--remote`

## Prerequisites

- `git` installed
- `tmux` installed (for non-dry run session checks)
- run from repo root (where `./cmd/orch` exists)

## 1) Create Isolated Sandbox

```bash
export ROOT="$(mktemp -d /tmp/orch-e2e-XXXXXX)"
mkdir -p "$ROOT"/{home,runtime,state,data,bin,repo/.orch,issues-store/issues,issues-store/runs,outside,origin/example}

export HOME="$ROOT/home"
export XDG_RUNTIME_DIR="$ROOT/runtime"
export XDG_STATE_HOME="$ROOT/state"
export XDG_DATA_HOME="$ROOT/data"
unset ORCH_PROJECT ORCH_REMOTE

go build -o "$ROOT/bin/orch" ./cmd/orch
ORCH_BIN="$ROOT/bin/orch"
```

## 2) Bootstrap Project + Issues

```bash
PROJECT="$(python - <<'PY'
import os, pathlib
print(pathlib.Path(os.path.realpath(os.path.join(os.environ['ROOT'], 'repo'))))
PY
)"
ISSUES="$(python - <<'PY'
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

git -C "$PROJECT" init
git -C "$PROJECT" config user.email e2e@example.com
git -C "$PROJECT" config user.name E2E

git init --bare "$ROOT/origin/example/manual-e2e-repo.git"
REPO_URL="file://$ROOT/origin/example/manual-e2e-repo.git"
PROJECT_ID="example-manual-e2e-repo"

git -C "$PROJECT" remote add origin "$REPO_URL"
git -C "$PROJECT" add .
git -C "$PROJECT" commit -m "init"
git -C "$PROJECT" push -u origin HEAD

# run runtime commands from the project root
cd "$PROJECT"
```

## 3) Master/Worker Lifecycle Checks

```bash
"$ORCH_BIN" master status
"$ORCH_BIN" worker status

"$ORCH_BIN" master start
"$ORCH_BIN" master status

"$ORCH_BIN" worker start
sleep 2
"$ORCH_BIN" worker status
```

Expected:

- initial status reports `Status: not running`
- after `master start`, status reports `Status: running`
- `worker start` reports managed external worker process started (allow a short delay before `worker status`)

## 4) Register Project Mapping

```bash
"$ORCH_BIN" daemon repo register "$REPO_URL"
"$ORCH_BIN" daemon repo list
```

Expected:

- `daemon repo register` prints `Registered repo mapping: <repo_id> -> <repo_url>`
- `daemon repo list` includes that `repo_id`

## 5) Local Client Live Run Flow

```bash
RUN_ID="$(date +%Y%m%d-%H%M%S)-local"

"$ORCH_BIN" --project "$PROJECT_ID" run mwc-local-live \
  --run-id "$RUN_ID" \
  --agent custom \
  --agent-cmd "echo cli-e2e; sleep 1" \
  --json

"$ORCH_BIN" --project "$PROJECT_ID" ps --issue mwc-local-live --json
"$ORCH_BIN" --project "$PROJECT_ID" show "mwc-local-live#$RUN_ID" --json
"$ORCH_BIN" --project "$PROJECT_ID" stop "mwc-local-live#$RUN_ID" --force
```

Expected:

- run command returns `"ok": true`
- `ps` returns at least one item for `mwc-local-live`
- `show` returns `"ok": true` and run metadata/events
- `ps` JSON includes `target` and `target_host` fields (populated when the run uses `--on <target>`)

## 6) Remote Master Reachability Smoke

Pick a free port first (example `60318` below).

```bash
"$ORCH_BIN" master kill || true

export ORCH_REMOTE=skip
"$ORCH_BIN" master start --listen tcp://127.0.0.1:60318
unset ORCH_REMOTE

"$ORCH_BIN" --remote 127.0.0.1:60318 master status
"$ORCH_BIN" --remote 127.0.0.1:60318 master kill
```

Expected:

- remote status prints `Status: running (remote=127.0.0.1:60318)`

## 7) Cleanup

```bash
"$ORCH_BIN" worker stop || true
"$ORCH_BIN" master kill || true
chmod -R u+w "$ROOT" || true
rm -rf "$ROOT"
```

## 8) Zeus Full Flow (Master + Worker + Run + PR + Close + Stop)

Use this when you want a true end-to-end check against a real remote host.

Target used in examples:

- host: `zeus`
- repo: `/home/kento/repos/doeff`
- issues path: `/home/kento/repos/doeff-issues`

```bash
TS="$(date +%Y%m%d-%H%M%S)"
ISSUE_ID="zeus-e2e-$TS"
RUN_ID="$TS-sample"
E2E_ROOT="/tmp/orch-zeus-e2e-$TS"

ssh zeus "mkdir -p $E2E_ROOT/runtime $E2E_ROOT/state $E2E_ROOT/data"

ENV_PREFIX="XDG_RUNTIME_DIR=$E2E_ROOT/runtime XDG_STATE_HOME=$E2E_ROOT/state XDG_DATA_HOME=$E2E_ROOT/data"

# launch master and worker on Zeus
ssh zeus "$ENV_PREFIX orch master start"
ssh zeus "$ENV_PREFIX orch worker start"
ssh zeus "$ENV_PREFIX orch master status"
ssh zeus "$ENV_PREFIX orch worker status"

# create sample issue
ssh zeus "cat > /home/kento/repos/doeff-issues/issues/$ISSUE_ID.md <<'EOF'
---
type: issue
id: $ISSUE_ID
title: Zeus E2E sample
status: open
---

# Zeus E2E sample
EOF"

# register repo mapping for strict project_id routing
ssh zeus "$ENV_PREFIX orch daemon repo register https://github.com/proboscis/doeff.git"

# runtime commands use repo identity scope
PROJECT_ID="proboscis-doeff"

# run with custom agent that makes a commit and creates a PR
ssh zeus "cat > /tmp/orch-zeus-agent-$ISSUE_ID.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p e2e
cat > e2e/$ISSUE_ID.md <<'EOT'
# Zeus E2E sample change
EOT
git add e2e/$ISSUE_ID.md
git commit -m 'chore(e2e): sample zeus run $ISSUE_ID'
git push -u origin HEAD
branch=$(git rev-parse --abbrev-ref HEAD)
gh pr create --repo proboscis/doeff --title 'chore(e2e): sample zeus run $ISSUE_ID' --body 'Automated sample PR from Zeus manual E2E.' --base main --head "$branch"
EOF
chmod +x /tmp/orch-zeus-agent-$ISSUE_ID.sh"

ssh zeus "$ENV_PREFIX orch --project $PROJECT_ID run $ISSUE_ID --run-id $RUN_ID --agent custom --agent-cmd 'bash /tmp/orch-zeus-agent-$ISSUE_ID.sh' --json"

# find and close the sample PR
BRANCH="issue/$ISSUE_ID/run-$RUN_ID"
ssh zeus "gh pr list --repo proboscis/doeff --head $BRANCH --state open --json number,url"
ssh zeus "gh pr close <PR_NUMBER> --repo proboscis/doeff --comment 'Closing sample Zeus E2E PR.' --delete-branch"

# stop the run at the end
ssh zeus "$ENV_PREFIX orch --project $PROJECT_ID stop $ISSUE_ID#$RUN_ID --force"

# cleanup
ssh zeus "rm -f /home/kento/repos/doeff-issues/issues/$ISSUE_ID.md /tmp/orch-zeus-agent-$ISSUE_ID.sh"
ssh zeus "$ENV_PREFIX orch worker stop"
```

Expected outcomes:

- master and worker report `Status: running`
- `orch run` returns `"ok": true`
- a PR is created for the run branch
- PR is closed successfully
- `orch stop <issue#run>` succeeds

## Troubleshooting

- If `daemon repo register` fails right after `master start`, retry once after a short delay.
- If TCP remote status is unreachable, restart with `ORCH_REMOTE=skip` set for the `master start --listen ...` command.
- Ensure `--project` value matches the registered repository identity.
- For cross-host `master` (Zeus) + local `worker` validation, ensure the worker host can resolve the same project-root path and issue files used by the lease. If issue files only exist on Zeus, `run` may fail with `issue not found` during worker execution.
- In this topology, verify run state on both sides when debugging: master (`orch --remote ... ps`) and worker-local issues store (`issues.path/runs/...`) to detect projection/store divergence.
