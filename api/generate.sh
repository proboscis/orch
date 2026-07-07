#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# Ensure GOPATH/bin is in PATH for protoc-gen-go
export PATH="${GOPATH:-$HOME/go}/bin:$PATH"

if command -v protoc >/dev/null 2>&1; then
  PROTOC=(protoc)
else
  PROTOC=(uv run --with grpcio-tools python -m grpc_tools.protoc)
fi

# Create output directories
mkdir -p "$ROOT_DIR/api/orchpb"
mkdir -p "$ROOT_DIR/orch-monitor-tui/orch_monitor/api"

# Generate Go code
# Using module mode to place output at api/orchpb/orch.pb.go
"${PROTOC[@]}" \
  --go_out="$ROOT_DIR" \
  --go_opt=module=github.com/proboscis/orch \
  -I "$SCRIPT_DIR" \
  "$SCRIPT_DIR/orch.proto"

# Generate Python code
"${PROTOC[@]}" \
  --python_out="$ROOT_DIR/orch-monitor-tui/orch_monitor/api" \
  --pyi_out="$ROOT_DIR/orch-monitor-tui/orch_monitor/api" \
  -I "$SCRIPT_DIR" \
  "$SCRIPT_DIR/orch.proto"

# Create __init__.py for Python package
touch "$ROOT_DIR/orch-monitor-tui/orch_monitor/api/__init__.py"

echo "Generated:"
echo "  - api/orchpb/orch.pb.go"
echo "  - orch-monitor-tui/orch_monitor/api/orch_pb2.py"
echo "  - orch-monitor-tui/orch_monitor/api/orch_pb2.pyi"
echo "  - orch-monitor-tui/orch_monitor/api/__init__.py"
