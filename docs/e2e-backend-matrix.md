# Backend Matrix Manual E2E (`tmux` / `zellij` / `opencode` / `claude` / `codex`)

This guide validates run-control behavior across multiplexer and send backends
using real `orch` commands (not `go test`).

This is the canonical instruction file for backend/session-control E2E.

Automation-first entrypoint:

- `scripts/e2e-backend-matrix-smoke.sh`
- `scripts/e2e-run-control-matrix.sh`

By default this automation runs a PR-safe smoke subset:

- `tmux`
- `claude` via shim
- `codex` via shim
- `zellij` only when enabled / available
- `opencode` only when explicitly enabled
- real `claude` only when `RUN_REAL_CLAUDE_LANE=1`
- real `codex` only when `RUN_REAL_CODEX_LANE=1`

Automation lane planning:

- [E2E Automation Plan](./e2e-automation-plan.md)

## Scope

The checklist covers these operations for each backend mode:

1. `run`
2. `attach`
3. `capture`
4. `send`
5. `stop`
6. `restart-from` (after stop)

Matrix dimensions:

- Multiplexer-backed runs (`tmux`, `zellij`) on the actual execution host
- OpenCode-backed runs (`opencode`) via real OpenCode server/session messaging
- Real `claude` and `codex` TUI sessions inside tmux/zellij
- Operator-host to target-host routing for `attach` / `capture` / `send`

## Strict Validation Requirement

For changes that affect any of the following:

- `orch attach`
- `orch capture`
- `orch send`
- session metadata / host attribution
- multiplexer routing
- tmux / zellij / opencode session interaction
- remote host execution / worker routing

the validation bar is strict:

- Real agent E2E is required.
- `custom --agent-cmd ...` runs are not sufficient on their own.
- Shims are not sufficient on their own.
- `orch attach`, `orch capture`, and `orch send` must all work in the same matrix.
- For remote-host runs, verify from the operator host against the actual target host.

If a real agent E2E cannot be run, the change is not fully verified and must be
reported as such.

## Prerequisites

- `git` installed
- `tmux` and `zellij` installed
- `opencode` installed and authenticated (for OpenCode lane)
- run from repo root (`./cmd/orch` available)

## 1) Create Isolated Sandbox

```bash
export ROOT="$(mktemp -d /tmp/orch-e2e-backends-XXXXXX)"
mkdir -p "$ROOT"/{home,runtime,state,data,bin,repo/.orch,issues-store/issues,origin/example}

export HOME="$ROOT/home"
export XDG_RUNTIME_DIR="$ROOT/runtime"
export XDG_STATE_HOME="$ROOT/state"
export XDG_DATA_HOME="$ROOT/data"
unset ORCH_PROJECT ORCH_REMOTE

go build -o "$ROOT/bin/orch" ./cmd/orch
ORCH_BIN="$ROOT/bin/orch"
```

## 2) Bootstrap Project + Issue Store

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
# Backend matrix manual E2E repo
EOF

git -C "$PROJECT" init
git -C "$PROJECT" config user.email e2e@example.com
git -C "$PROJECT" config user.name E2E

git init --bare "$ROOT/origin/example/backend-matrix.git"
REPO_URL="file://$ROOT/origin/example/backend-matrix.git"
PROJECT_ID="example-backend-matrix"

git -C "$PROJECT" remote add origin "$REPO_URL"
git -C "$PROJECT" add .
git -C "$PROJECT" commit -m "init"
git -C "$PROJECT" push -u origin HEAD

cd "$PROJECT"
```

## 3) Start Daemon and Register Repo Mapping

```bash
"$ORCH_BIN" master start
"$ORCH_BIN" daemon repo register "$REPO_URL"
"$ORCH_BIN" daemon repo list
```

Expected:

- daemon is running
- `daemon repo list` includes `example-backend-matrix`

## 4) Create Test Issues

```bash
for lane in tmux zellij opencode; do
  cat > "$ISSUES/issues/e2e-$lane.md" <<EOF
---
type: issue
id: e2e-$lane
title: Backend matrix lane $lane
status: open
---

# Backend matrix lane $lane
EOF
done
```

## 5) Common Helpers

```bash
now_id() { date +%Y%m%d-%H%M%S; }

run_and_assert_ok() {
  local json="$1"
  python - <<'PY' "$json"
import json, sys
obj = json.loads(sys.argv[1])
assert obj.get('ok') is True, obj
print(obj)
PY
}
```

## 6) Lane A: `tmux` + custom agent

```bash
RUN_TMUX="$(now_id)-tmux"

OUT="$("$ORCH_BIN" --project "$PROJECT_ID" run e2e-tmux \
  --run-id "$RUN_TMUX" \
  --agent custom \
  --agent-cmd 'python3 -u /path/to/control-repl.py tmux' \
  --multiplexer tmux \
  --json)"
run_and_assert_ok "$OUT"

"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-tmux#$RUN_TMUX"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-tmux#$RUN_TMUX" <<'EOF'
tmux-send-line-1
tmux-send-line-2
EOF
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-tmux#$RUN_TMUX" --force

# restart-from requires previous run to be stopped/canceled/done.
"$ORCH_BIN" --project "$PROJECT_ID" restart-from "e2e-tmux#$RUN_TMUX" \
  --agent-cmd 'python3 -u /path/to/control-repl.py tmux-restart' \
  --json
```

Expected:

- `capture` returns non-empty session output
- heredoc/stdin `send` succeeds without `session not found`
- both multiline echo lines appear in `capture`
- `restart-from` succeeds only after stop

## 7) Lane B: `zellij` + custom agent

```bash
RUN_ZELLIJ="$(now_id)-zellij"

OUT="$("$ORCH_BIN" --project "$PROJECT_ID" run e2e-zellij \
  --run-id "$RUN_ZELLIJ" \
  --agent custom \
  --agent-cmd 'python3 -u /path/to/control-repl.py zellij' \
  --multiplexer zellij \
  --json)"
run_and_assert_ok "$OUT"

"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-zellij#$RUN_ZELLIJ"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-zellij#$RUN_ZELLIJ" <<'EOF'
zellij-send-line-1
zellij-send-line-2
EOF
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-zellij#$RUN_ZELLIJ" --force

"$ORCH_BIN" --project "$PROJECT_ID" restart-from "e2e-zellij#$RUN_ZELLIJ" \
  --agent-cmd 'python3 -u /path/to/control-repl.py zellij-restart' \
  --json
```

Expected:

- `send` routes via run multiplexer (`zellij`) instead of daemon default
- no fallback-to-tmux session lookup failure
- heredoc/stdin send succeeds in smoke automation without transport errors
- strict multiline preservation for zellij should be verified in lab/manual runs when capture is reliable

## 8) Lane C: `opencode`

```bash
RUN_OPENCODE="$(now_id)-opencode"

OUT="$("$ORCH_BIN" --project "$PROJECT_ID" run e2e-opencode \
  --run-id "$RUN_OPENCODE" \
  --agent opencode \
  --json)"
run_and_assert_ok "$OUT"

"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-opencode#$RUN_OPENCODE"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-opencode#$RUN_OPENCODE" "opencode-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-opencode#$RUN_OPENCODE" --force

"$ORCH_BIN" --project "$PROJECT_ID" restart-from "e2e-opencode#$RUN_OPENCODE" --json
```

Expected:

- OpenCode send returns quickly after API ACK
- run can be stopped and continued via `restart-from`

## 9) Lane D/E: `claude` and `codex`

Real validation requirement:

- Launch the actual `claude` / `codex` TUI in `tmux` or `zellij`.
- Verify `attach`, `capture`, and `send` against that real session.

PR-safe smoke fallback:

If the real CLIs are unavailable in CI/sandbox, use lightweight shims:

```bash
cat > "$ROOT/bin/claude" <<'EOF'
#!/usr/bin/env bash
printf 'fake claude ready\n'
sleep 30
EOF

cat > "$ROOT/bin/codex" <<'EOF'
#!/usr/bin/env bash
printf 'fake codex ready\n'
sleep 30
EOF

chmod +x "$ROOT/bin/claude" "$ROOT/bin/codex"
export PATH="$ROOT/bin:$PATH"
```

Then run smoke checks:

```bash
RUN_CLAUDE="$(now_id)-claude"
"$ORCH_BIN" --project "$PROJECT_ID" run e2e-claude --run-id "$RUN_CLAUDE" --agent claude --json
"$ORCH_BIN" --project "$PROJECT_ID" attach "e2e-claude#$RUN_CLAUDE"
"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-claude#$RUN_CLAUDE"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-claude#$RUN_CLAUDE" "claude-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-claude#$RUN_CLAUDE" --force

RUN_CODEX="$(now_id)-codex"
"$ORCH_BIN" --project "$PROJECT_ID" run e2e-codex --run-id "$RUN_CODEX" --agent codex --json
"$ORCH_BIN" --project "$PROJECT_ID" attach "e2e-codex#$RUN_CODEX"
"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-codex#$RUN_CODEX"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-codex#$RUN_CODEX" "codex-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-codex#$RUN_CODEX" --force
```

Optional real-agent automation:

```bash
RUN_REAL_CLAUDE_LANE=1 RUN_REAL_CODEX_LANE=1 ./scripts/e2e-backend-matrix-smoke.sh
```

Real-lane expectation:

- `attach` reaches the live Claude/Codex TUI session
- `capture` shows the ready marker from the issue prompt
- heredoc `send` produces the ack marker and preserves the multiline payload

Expected:

- Real lanes: actual Claude/Codex TUI accepts attach/capture/send
- Smoke lanes: `claude` send path uses standard `SendKeys` behavior
- Smoke lanes: `codex` send path preserves codex submit behavior (`literal` + Enter on tmux)
- Real lanes: heredoc/stdin send is validated against the live Claude/Codex session

## 10) Remote Host Matrix

Use the run-control matrix script to prove operator-host control over an
actual target-host run:

```bash
./scripts/e2e-run-control-matrix.sh
```

Expected:

- local-host matrix passes
- remote Zeus matrix passes
- `orch ps` shows the actual execution host in `HOST` for both local and remote runs
- Zeus OpenCode runs stay `running` / `waiting` after session creation when the session is still alive
- `attach`, `capture`, and `send` all succeed in that matrix
- local and remote run-control automation should use heredoc/stdin send for the multiline path
- for headless automation, `attach` may complete as an interactive preflight
  rather than staying attached to the TUI

## 11) Cleanup

```bash
"$ORCH_BIN" master kill || true
chmod -R u+w "$ROOT" || true
rm -rf "$ROOT"
```

## Troubleshooting Notes

- `restart-from` on a live run is expected to fail. Stop first.
- `restart-from` for `--agent custom` requires `--agent-cmd`; otherwise the
  continued run fails with `custom agent requires --agent-cmd`.
- If only `custom` or shim-backed lanes were run, treat validation as partial
  for changes that affect real agent TUIs.
- If `capture` is empty for multiplexer lanes, inspect run events for latest
  non-empty `session.multiplexer` artifact and verify it is preserved.
- If zellij `send` fails with `session not found`, verify send-path multiplexer
  selection uses `run.Multiplexer` before daemon default.
