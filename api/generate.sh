#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# Generate Go code
protoc \
  --go_out="$ROOT_DIR" \
  --go_opt=paths=source_relative \
  -I "$SCRIPT_DIR" \
  "$SCRIPT_DIR/orch.proto"

# Generate Python code
protoc \
  --python_out="$ROOT_DIR/orch-monitor-tui/orch_monitor/api" \
  --pyi_out="$ROOT_DIR/orch-monitor-tui/orch_monitor/api" \
  -I "$SCRIPT_DIR" \
  "$SCRIPT_DIR/orch.proto"

echo "Generated:"
echo "  - api/orchpb/orch.pb.go"
echo "  - orch-monitor-tui/orch_monitor/api/orch_pb2.py"
echo "  - orch-monitor-tui/orch_monitor/api/orch_pb2.pyi"
