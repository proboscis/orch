# Master/Worker/Client Manual E2E

This guide validates the cluster-facing CLI behavior by running real `orch`
commands (not `go test`).

## Scope

This checklist verifies the command-plane path:

```
orch client CLI -> orch-master daemon -> orch-worker (host manager)
```

It covers:

1. `master` lifecycle commands
2. `worker` lifecycle commands (single long-lived host worker)
3. local client run/ps/show/stop flow
4. remote master reachability via `--remote`
5. backend coverage handoff for `tmux` / `zellij` / `opencode` / `claude` / `codex`
6. one worker managing multiple runs on the same host

This file validates the cluster-facing command plane first.

Backend-specific run/send/capture behavior is covered by the companion checklist:

- [Backend Matrix Manual E2E](./e2e-backend-matrix.md)

Treat both files together as the complete manual E2E suite:

- `docs/e2e-master-worker-client.md`
- `docs/e2e-backend-matrix.md`

Automation-first entrypoint for the local single-host flow:

- `scripts/e2e-master-worker-client-local.sh`

Parameterized automation entrypoint for target-host runs:

- `scripts/e2e-master-worker-client-target.sh`

For targets that need custom SSH flags or a nonstandard port, prefer passing a
full command via `TARGET_SSH_CMD` instead of relying on a simple host alias.

## Prerequisites

- `git` installed
- `tmux` installed (for non-dry run session checks)
- run from repo root (where `./cmd/orch` exists)
- Unless otherwise noted, these examples assume the project uses the `local`
  file backend via `issues.path`. If you use the GitHub backend instead,
  replace manual issue-file creation with equivalent GitHub issue setup.

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

# starting again should not create a duplicate host worker
"$ORCH_BIN" worker start
"$ORCH_BIN" worker status
```

Expected:

- initial `master status` reports `Status: not running`
- initial `worker status` may report `No workers registered` before any worker
  is started
- after `master start`, status reports `Status: running`
- `worker start` brings up one host worker for the local host
- repeating `worker start` should not create an extra duplicate worker for the
  same host/profile

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

## 5b) Single Worker, Multiple Runs On One Host

```bash
cat > "$ISSUES/issues/mwc-local-live-2.md" <<'EOF'
---
type: issue
id: mwc-local-live-2
title: Local live run 2
status: open
---

# Local live run 2
EOF

RUN_ID_1="$(date +%Y%m%d-%H%M%S)-a"
RUN_ID_2="$(date +%Y%m%d-%H%M%S)-b"

"$ORCH_BIN" --project "$PROJECT_ID" run mwc-local-live \
  --run-id "$RUN_ID_1" \
  --agent custom \
  --agent-cmd "echo cli-e2e-a; sleep 20" \
  --json

"$ORCH_BIN" --project "$PROJECT_ID" run mwc-local-live-2 \
  --run-id "$RUN_ID_2" \
  --agent custom \
  --agent-cmd "echo cli-e2e-b; sleep 20" \
  --json

"$ORCH_BIN" worker status
"$ORCH_BIN" --project "$PROJECT_ID" ps --json
```

Expected:

- both runs become active at the same time
- `worker status` still shows one host worker, not one worker per run
- run multiplicity comes from one worker managing multiple sessions on the host

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
"$ORCH_BIN" worker stop --all || true
"$ORCH_BIN" master kill || true
chmod -R u+w "$ROOT" || true
rm -rf "$ROOT"
```

## 8) Zeus Full Flow (Master + Worker + Run + PR + Close + Stop)

Use this when you want a true end-to-end check against a real remote host.

Target used in examples:

- host: `zeus`
- repo: `/home/kento/repos/doeff`
- issues path: use the actual `issues.path` from `/home/kento/repos/doeff/.orch/config.yaml`
  (for example `/home/kento/repos/doeff-VAULT`)

```bash
TS="$(date +%Y%m%d-%H%M%S)"
ISSUE_ID="zeus-e2e-$TS"
RUN_ID="$TS-sample"
E2E_ROOT="/tmp/orch-zeus-e2e-$TS"

ssh zeus "mkdir -p $E2E_ROOT/runtime $E2E_ROOT/state $E2E_ROOT/data"

ENV_PREFIX="XDG_RUNTIME_DIR=$E2E_ROOT/runtime XDG_STATE_HOME=$E2E_ROOT/state XDG_DATA_HOME=$E2E_ROOT/data XDG_CONFIG_HOME=$E2E_ROOT/config"

# launch master and worker on Zeus
ssh zeus "$ENV_PREFIX orch master start"
ssh zeus "$ENV_PREFIX orch worker start"
ssh zeus "$ENV_PREFIX orch master status"
ssh zeus "$ENV_PREFIX orch worker status"

# create sample issue
ssh zeus "cat > /home/kento/repos/doeff-VAULT/issues/$ISSUE_ID.md <<'EOF'
---
type: issue
id: $ISSUE_ID
title: Zeus E2E sample
status: open
---

# Zeus E2E sample
EOF"

# register repo mapping for strict project_id routing
#
# Preferred: register by repo URL.
# If the managed clone created from the repo URL does not contain the required
# project config (for example `.orch/config.yaml` is not committed), register
# the operational project root instead.
ssh zeus "$ENV_PREFIX orch daemon repo register /home/kento/repos/doeff"

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

ssh zeus "$ENV_PREFIX bash -lc 'cd /home/kento/repos/doeff && orch --project $PROJECT_ID run $ISSUE_ID --run-id $RUN_ID --agent custom --agent-cmd '\\''bash /tmp/orch-zeus-agent-$ISSUE_ID.sh'\\'' --json'"

# find and close the sample PR
BRANCH="issue/$ISSUE_ID/run-$RUN_ID"
ssh zeus "gh pr list --repo proboscis/doeff --head $BRANCH --state open --json number,url"
ssh zeus "gh pr close <PR_NUMBER> --repo proboscis/doeff --comment 'Closing sample Zeus E2E PR.' --delete-branch"

# stop the run at the end
ssh zeus "$ENV_PREFIX orch --project $PROJECT_ID stop $ISSUE_ID#$RUN_ID --force"

# cleanup
ssh zeus "rm -f /home/kento/repos/doeff-VAULT/issues/$ISSUE_ID.md /tmp/orch-zeus-agent-$ISSUE_ID.sh"
ssh zeus "$ENV_PREFIX orch worker stop --all"
```

Expected outcomes:

- master and worker report `Status: running`
- `orch run` returns `"ok": true`
- a PR is created for the run branch
- PR is closed successfully
- `orch stop <issue#run>` succeeds
- If you run Zeus in an isolated XDG sandbox, include `XDG_CONFIG_HOME` in the
  environment prefix; otherwise daemon project mappings from `~/.config/orch/projects`
  may leak into the test
- For file-backend projects whose config is not committed into the repo clone,
  repo-URL registration may be insufficient; use the operational project root
  instead

## 9) Required Backend Coverage

The core flow above does **not** fully exercise backend-specific behavior.
After sections 1-8 pass, run the companion backend matrix checklist and record
results for all of the following lanes:

- `tmux`
- `zellij`
- `opencode`
- `claude`
- `codex`

Use:

```bash
# companion checklist
sed -n '1,260p' docs/e2e-backend-matrix.md
```

Minimum acceptance criteria:

- `tmux`: `run`, `capture`, `send`, `stop`, `restart-from`
- `zellij`: `run`, `capture`, `send`, `stop`, `restart-from`
- `opencode`: `run`, `capture`, `send`, `stop`, `restart-from`
- `claude`: `run`, `capture`, `send`, `stop`
- `codex`: `run`, `capture`, `send`, `stop`

If you only run `docs/e2e-master-worker-client.md`, backend coverage is incomplete.

## 10) Zeus Master + Mac Target Flow (`--on mac`)

Use this when you want to verify the case where the control plane stays on
Zeus, but the run itself executes on a Mac target instead of on Zeus.

Additional prerequisites:

- Zeus-side project config includes a `targets` entry for the target Mac
- the SSH host alias resolves from Zeus before running orch
- the target Mac has the same repo cloned at the configured target `repo`
- the target Mac has the required runtime dependencies installed (`git`, chosen
  multiplexer, agent binary)
- the target Mac runs one long-lived `orch-worker` for that host/profile

Example target config on Zeus:

```yaml
targets:
  - name: mac
    host: mac
    repo: /Users/<user>/repos/doeff
```

Checklist:

```bash
TS="$(date +%Y%m%d-%H%M%S)"
ISSUE_ID="mac-target-e2e-$TS"
RUN_ID="$TS-mac"
PROJECT_ID="proboscis-doeff"
BRANCH="issue/$ISSUE_ID/run-$RUN_ID"

# create sample issue in the Zeus-backed issue store
ssh zeus "cat > /home/kento/repos/doeff-VAULT/issues/$ISSUE_ID.md <<'EOF'
---
type: issue
id: $ISSUE_ID
title: Mac target E2E sample
status: open
---

# Mac target E2E sample
EOF"

# ensure Zeus resolves project identity to the operational root it should use
ssh zeus 'orch daemon repo register /home/kento/repos/doeff'

# ensure the target Mac worker is connected to Zeus
ssh mac 'ORCH_REMOTE=zeus:7777 orch worker start'
ssh mac 'ORCH_REMOTE=zeus:7777 orch worker status'

# run on the Mac target
#
# Run from the operational project root on Zeus so the daemon can discover the
# correct project config for local-mode CLI execution before dispatching to the
# remote target.
ssh zeus "cd /home/kento/repos/doeff && orch --project $PROJECT_ID run $ISSUE_ID \
  --run-id $RUN_ID \
  --on mac \
  --agent custom \
  --agent-cmd 'printf mac-target-ready; hostname; sleep 20' \
  --json"

# verify the run is tracked as a Mac-targeted run
ssh zeus "orch --project $PROJECT_ID ps --issue $ISSUE_ID --json"
ssh zeus "orch --project $PROJECT_ID show $ISSUE_ID#$RUN_ID --json"

# optional but recommended: capture the remote session output
ssh zeus "orch --project $PROJECT_ID capture $ISSUE_ID#$RUN_ID"

# stop and clean up
ssh zeus "orch --project $PROJECT_ID stop $ISSUE_ID#$RUN_ID --force"
ssh mac 'ORCH_REMOTE=zeus:7777 orch worker stop --all'
ssh zeus "rm -f /home/kento/repos/doeff-VAULT/issues/$ISSUE_ID.md"
```

Expected outcomes:

- `run` returns `"ok": true`
- `ps --json` shows `target: "mac"`
- `ps --json` or attach metadata exposes non-empty `target_host`
- `capture` output includes the custom marker (`mac-target-ready`) or target hostname
- `stop` succeeds for the Mac-targeted run
- repeated `orch worker start` on the Mac target should not create duplicate
  workers for the same host/profile
- If the target host cannot resolve the requested multiplexer in its remote SSH
  PATH, expect session creation to fail with `failed to create tmux session` or
  the equivalent multiplexer error

This is the minimum manual check for the user story:

```text
master = zeus
run target = mac
```

## Troubleshooting

- If `daemon repo register` fails right after `master start`, retry once after a short delay.
- If TCP remote status is unreachable, restart with `ORCH_REMOTE=skip` set for the `master start --listen ...` command.
- Ensure `--project` value matches the registered repository identity.
- For file-backend projects, ensure the issue file is created under the actual
  `issues.path` configured for the project being tested.
- For `--on mac` validation, ensure the Zeus-side target `repo` path matches the
  actual clone path on the Mac host. A valid repo identity mapping on Zeus does
  not replace the target repo path needed for SSH execution.
- For `--on mac`, verify plain SSH first from Zeus (`ssh <target> 'command -v tmux; hostname'`)
  before attributing failures to orch itself.
- For cross-host `master` (Zeus) + local `worker` validation, ensure the worker host can resolve the same project-root path and issue files used by the host work assignment. If issue files only exist on Zeus, `run` may fail with `issue not found` during worker execution.
- In this topology, verify run state on both sides when debugging: master (`orch --remote ... ps`) and worker-local issues store (`issues.path/runs/...`) to detect projection/store divergence.
