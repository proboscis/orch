#!/usr/bin/env bash
set -euo pipefail

ROOT="${ROOT:-$(mktemp -d /tmp/orch-e2e-pr-ci-XXXXXX)}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ORCH_BIN="${ORCH_BIN:-$ROOT/bin/orch}"
RUN_ZELLIJ_LANE="${RUN_ZELLIJ_LANE:-0}"
RUN_OPENCODE_LANE="${RUN_OPENCODE_LANE:-0}"

cleanup() {
  set +e
  if [ "$KEEP_ROOT" != "1" ]; then
    chmod -R u+w "$ROOT" >/dev/null 2>&1 || true
    rm -rf "$ROOT"
  fi
}
trap cleanup EXIT

mkdir -p "$ROOT/bin"
if [ ! -x "$ORCH_BIN" ]; then
  go build -o "$ORCH_BIN" ./cmd/orch
fi

echo "== local host-worker =="
ROOT="$ROOT/local" ORCH_BIN="$ORCH_BIN" KEEP_ROOT=1 ./scripts/e2e-master-worker-client-local.sh

echo "== remote smoke =="
ROOT="$ROOT/remote-smoke" ORCH_BIN="$ORCH_BIN" KEEP_ROOT=1 ./scripts/e2e-master-worker-client-remote-smoke.sh

echo "== target-host local simulation =="
ROOT="$ROOT/target-local" ORCH_BIN="$ORCH_BIN" KEEP_ROOT=1 ./scripts/e2e-master-worker-client-target-local.sh

echo "== backend matrix smoke =="
ROOT="$ROOT/backends" ORCH_BIN="$ORCH_BIN" KEEP_ROOT=1 RUN_ZELLIJ_LANE="$RUN_ZELLIJ_LANE" RUN_OPENCODE_LANE="$RUN_OPENCODE_LANE" ./scripts/e2e-backend-matrix-smoke.sh

echo "PR_CI_E2E_OK"
