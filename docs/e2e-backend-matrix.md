# Backend Matrix Manual E2E (`tmux` / `zellij` / `opencode`)

This guide validates run-control behavior across multiplexer and send backends
using real `orch` commands (not `go test`).

## Scope

The checklist covers these operations for each backend mode:

1. `run`
2. `capture`
3. `send`
4. `stop`
5. `restart-from` (after stop)

Matrix dimensions:

- Multiplexer-backed runs (`tmux`, `zellij`) with a custom agent
- OpenCode-backed runs (`opencode`) via OpenCode server/session messaging

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
  --agent-cmd 'echo tmux-lane-ready; sleep 30' \
  --multiplexer tmux \
  --json)"
run_and_assert_ok "$OUT"

"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-tmux#$RUN_TMUX"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-tmux#$RUN_TMUX" "tmux-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-tmux#$RUN_TMUX" --force

# restart-from requires previous run to be stopped/canceled/done.
"$ORCH_BIN" --project "$PROJECT_ID" restart-from "e2e-tmux#$RUN_TMUX" \
  --agent-cmd 'echo tmux-lane-restart; sleep 10' \
  --json
```

Expected:

- `capture` returns non-empty session output
- `send` succeeds without `session not found`
- `restart-from` succeeds only after stop

## 7) Lane B: `zellij` + custom agent

```bash
RUN_ZELLIJ="$(now_id)-zellij"

OUT="$("$ORCH_BIN" --project "$PROJECT_ID" run e2e-zellij \
  --run-id "$RUN_ZELLIJ" \
  --agent custom \
  --agent-cmd 'echo zellij-lane-ready; sleep 30' \
  --multiplexer zellij \
  --json)"
run_and_assert_ok "$OUT"

"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-zellij#$RUN_ZELLIJ"
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-zellij#$RUN_ZELLIJ" "zellij-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-zellij#$RUN_ZELLIJ" --force

"$ORCH_BIN" --project "$PROJECT_ID" restart-from "e2e-zellij#$RUN_ZELLIJ" \
  --agent-cmd 'echo zellij-lane-restart; sleep 10' \
  --json
```

Expected:

- `send` routes via run multiplexer (`zellij`) instead of daemon default
- no fallback-to-tmux session lookup failure

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

## 9) Lane D/E: `claude` and `codex` send checks

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

Then run send-path checks:

```bash
RUN_CLAUDE="$(now_id)-claude"
"$ORCH_BIN" --project "$PROJECT_ID" run e2e-claude --run-id "$RUN_CLAUDE" --agent claude --json
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-claude#$RUN_CLAUDE" "claude-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-claude#$RUN_CLAUDE"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-claude#$RUN_CLAUDE" --force

RUN_CODEX="$(now_id)-codex"
"$ORCH_BIN" --project "$PROJECT_ID" run e2e-codex --run-id "$RUN_CODEX" --agent codex --json
"$ORCH_BIN" --project "$PROJECT_ID" send "e2e-codex#$RUN_CODEX" "codex-send-check"
"$ORCH_BIN" --project "$PROJECT_ID" capture "e2e-codex#$RUN_CODEX"
"$ORCH_BIN" --project "$PROJECT_ID" stop "e2e-codex#$RUN_CODEX" --force
```

Expected:

- `claude` send path uses standard `SendKeys` behavior
- `codex` send path preserves codex submit behavior (`literal` + Enter on tmux)

## 10) Cleanup

```bash
"$ORCH_BIN" master kill || true
chmod -R u+w "$ROOT" || true
rm -rf "$ROOT"
```

## Troubleshooting Notes

- `restart-from` on a live run is expected to fail. Stop first.
- `restart-from` for `--agent custom` requires `--agent-cmd`; otherwise the
  continued run fails with `custom agent requires --agent-cmd`.
- If `capture` is empty for multiplexer lanes, inspect run events for latest
  non-empty `session.multiplexer` artifact and verify it is preserved.
- If zellij `send` fails with `session not found`, verify send-path multiplexer
  selection uses `run.Multiplexer` before daemon default.
