#!/usr/bin/env bash
set -euo pipefail

# Full deploy of the current checkout to every orch component:
#
#   1. local:  build + install CLI/TUI, kill local daemons (make install;
#              daemons restart on demand with the new binary)
#   2. remote: cross-compile + install the orch binary (install-orch-zeus.sh)
#   3. remote: restart master + worker so they run the new binary (a running
#              master/worker keeps the old binary until restarted)
#   4. local:  restart the local worker — it loses its master connection and
#              exits when the remote master restarts, so it must come back last
#
# Configuration (env):
#   REMOTE_HOST     ssh host running the remote master+worker (default: zeus)
#   MASTER_ADDR     master address the LOCAL worker connects to (default: zeus:7777)
#   REMOTE_ORCH     orch binary path on the remote (default: ~/.local/bin/orch)
#   ORCH_LOCAL_BIN  locally installed orch binary (default: ~/.local/bin/orch)

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${REMOTE_HOST:-zeus}"
MASTER_ADDR="${MASTER_ADDR:-zeus:7777}"
REMOTE_ORCH="${REMOTE_ORCH:-~/.local/bin/orch}"
ORCH_LOCAL_BIN="${ORCH_LOCAL_BIN:-$HOME/.local/bin/orch}"

cd "$ROOT_DIR"

# wait_registered LABEL CMD... — poll a `worker status` command until the
# worker reports an active master registration; fail fast with the last
# status output instead of reporting a half-deployed plane as success.
wait_registered() {
  local label="$1"
  shift
  local out=""
  for _ in $(seq 1 15); do
    out="$("$@" 2>&1 || true)"
    if printf '%s' "$out" | grep -q "Master Registration: active"; then
      printf '%s\n' "$out" | sed -n '1,8p'
      return 0
    fi
    sleep 1
  done
  echo "ERROR: $label did not register with the master:" >&2
  printf '%s\n' "$out" >&2
  return 1
}

echo "== [1/4] local install (CLI + TUI, restart local daemons) =="
make install

echo "== [2/4] remote binary install ($REMOTE_HOST) =="
REMOTE_HOST="$REMOTE_HOST" ./scripts/install-orch-zeus.sh

echo "== [3/4] restart remote master + worker ($REMOTE_HOST) =="
ssh "$REMOTE_HOST" "
  set -e
  $REMOTE_ORCH worker stop >/dev/null 2>&1 || true
  $REMOTE_ORCH master kill >/dev/null 2>&1 || true
  sleep 1
  $REMOTE_ORCH master start
  sleep 2
  $REMOTE_ORCH worker start
"
master_status="$(ssh "$REMOTE_HOST" "$REMOTE_ORCH master status")"
printf '%s\n' "$master_status"
if printf '%s' "$master_status" | grep -qi "stale binary"; then
  echo "ERROR: master on $REMOTE_HOST is still running a stale binary after restart" >&2
  exit 1
fi
wait_registered "remote worker ($REMOTE_HOST)" ssh "$REMOTE_HOST" "$REMOTE_ORCH worker status"

echo "== [4/4] restart local worker (master: $MASTER_ADDR) =="
ORCH_REMOTE="$MASTER_ADDR" "$ORCH_LOCAL_BIN" worker stop >/dev/null 2>&1 || true
ORCH_REMOTE="$MASTER_ADDR" "$ORCH_LOCAL_BIN" worker start
wait_registered "local worker" env ORCH_REMOTE="$MASTER_ADDR" "$ORCH_LOCAL_BIN" worker status

echo "Deploy complete: local CLI/TUI + $REMOTE_HOST master/worker + local worker are on the new binary."
