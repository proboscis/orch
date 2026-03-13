#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "== local run-control matrix =="
"$SCRIPT_DIR/e2e-run-control-local.sh"

if [ "${RUN_REMOTE_MATRIX:-1}" = "1" ]; then
  echo "== zeus run-control matrix =="
  "$SCRIPT_DIR/e2e-run-control-zeus.sh"
else
  echo "Skipping remote matrix because RUN_REMOTE_MATRIX=0"
fi

echo "RUN_CONTROL_MATRIX_E2E_OK"
