#!/usr/bin/env bash
set -euo pipefail

# Full deploy of the current checkout to every orch component:
#
#   1. local:  build + install CLI/TUI, kill local daemons (make install;
#              daemons restart on demand with the new binary)
#   2. remote: cross-compile + install the orch binary (install-orch-remote.sh)
#   3. remote: restart master + worker so they run the new binary (a running
#              master/worker keeps the old binary until restarted)
#   4. local:  restart the local worker — it loses its master connection and
#              exits when the remote master restarts, so it must come back last
#
# Configuration (env):
#   REMOTE_HOST     ssh host running the remote master+worker (required)
#   MASTER_ADDR     master address the LOCAL worker connects to (default: $REMOTE_HOST:7777)
#   MASTER_LISTEN   listen address for the remote master (default: 0.0.0.0:7777).
#                   The daemon default is loopback-only (ADR-0003); running this
#                   multi-host deploy script is the explicit opt-in.
#   REMOTE_ORCH     orch binary path on the remote (default: ~/.local/bin/orch)
#   ORCH_LOCAL_BIN  locally installed orch binary (default: ~/.local/bin/orch)
#   LOCAL_LOGIN_SHELL  login shell used to restart the local worker
#                      (default: $SHELL, falling back to /bin/bash)
#   DEPLOY_REQUIRED_AGENTS  comma- or space-separated agents that must report
#                           available on every worker (default: empty/warn only)

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${REMOTE_HOST:?set REMOTE_HOST to the ssh host of the remote master}"
MASTER_ADDR="${MASTER_ADDR:-$REMOTE_HOST:7777}"
MASTER_LISTEN="${MASTER_LISTEN:-0.0.0.0:7777}"
REMOTE_ORCH="${REMOTE_ORCH:-~/.local/bin/orch}"
ORCH_LOCAL_BIN="${ORCH_LOCAL_BIN:-$HOME/.local/bin/orch}"
LOCAL_LOGIN_SHELL="${LOCAL_LOGIN_SHELL:-${SHELL:-/bin/bash}}"
DEPLOY_REQUIRED_AGENTS="${DEPLOY_REQUIRED_AGENTS:-}"

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

# shell_quote VALUE — quote one value for the remote account's command shell.
# ssh joins command arguments into a shell string, so the bash -lc payload must
# remain one argument after that additional parse.
shell_quote() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\"'\"'/g")"
}

# remote_login SCRIPT [ARG...] — run SCRIPT through the remote account's Bash
# login shell. Positional arguments keep deployment values out of shell code.
remote_login() {
  local script="$1"
  shift
  local command="bash -lc $(shell_quote "$script") bash"
  local arg
  for arg in "$@"; do
    command+=" $(shell_quote "$arg")"
  done
  ssh "$REMOTE_HOST" "$command"
}

reported_worker_log_path() {
  printf '%s\n' "$1" | sed -n 's/^Log: //p' | tail -1
}

fresh_worker_log_path() {
  local label="$1"
  local start_output="$2"
  local log_path

  if [[ "$start_output" != *"Started worker:"* ]]; then
    echo "ERROR: $label was not freshly started" >&2
    return 1
  fi
  log_path="$(reported_worker_log_path "$start_output")"
  if [[ -z "$log_path" ]]; then
    echo "ERROR: $label start did not report its log path" >&2
    return 1
  fi
  printf '%s\n' "$log_path"
}

latest_agent_availability_from_log() {
  grep 'agent availability' "$1" | tail -1
}

wait_for_fresh_agent_availability() {
  local label="$1"
  local previous_line="$2"
  shift 2
  local output=""
  local line=""

  for _ in $(seq 1 15); do
    if output="$("$@" 2>&1)"; then
      line="$(printf '%s\n' "$output" | grep 'agent availability:' | tail -1 || true)"
      if [[ -n "$line" && "$line" != "$previous_line" ]]; then
        printf '%s\n' "$line"
        return 0
      fi
    fi
    sleep 1
  done

  echo "ERROR: no fresh agent availability line appeared for $label" >&2
  if [[ -n "$output" ]]; then
    echo "Last log probe output:" >&2
    printf '%s\n' "$output" >&2
  fi
  return 1
}

show_agent_availability() {
  local label="$1"
  local line="$2"

  if [[ -z "$line" || "$line" != *"agent availability:"* ]]; then
    echo "ERROR: no fresh agent availability line found for $label" >&2
    return 1
  fi

  echo "Agent availability ($label):"
  printf '%s\n' "$line"

  if [[ "$line" == *"=unavailable ("* ]]; then
    echo "WARNING: $label reports one or more unavailable agents" >&2
  fi

  local required="${DEPLOY_REQUIRED_AGENTS//,/ }"
  if [[ -z "${required//[[:space:]]/}" ]]; then
    return 0
  fi

  local -a required_agents=()
  local agent
  read -r -a required_agents <<< "$required"
  for agent in "${required_agents[@]}"; do
    if [[ "$agent" == *[![:alnum:]_.-]* ]]; then
      echo "ERROR: invalid agent name in DEPLOY_REQUIRED_AGENTS: $agent" >&2
      return 1
    fi
    if [[ "$line" != *"{${agent}=available ("* && "$line" != *", ${agent}=available ("* ]]; then
      echo "ERROR: required agent $agent is not available on $label" >&2
      return 1
    fi
  done
}

echo "== [1/4] local install (CLI + TUI, restart local daemons) =="
make install

echo "== [2/4] remote binary install ($REMOTE_HOST) =="
REMOTE_HOST="$REMOTE_HOST" ./scripts/install-orch-remote.sh

remote_orch_script='
  REMOTE_ORCH="$1"
  case "$REMOTE_ORCH" in
    "~/"*) REMOTE_ORCH="$HOME/${REMOTE_ORCH#\~/}" ;;
  esac
  "$REMOTE_ORCH" "${@:2}"
'
availability_log_script='set -o pipefail; grep "agent availability" "$1" | tail -1'
# The old master registration can remain active briefly after worker stop, so
# wait_registered alone may pass before the new process finishes its probes.
# Snapshot the old line and require the restarted process to append a new one.
remote_previous_status="$(remote_login "$remote_orch_script" "$REMOTE_ORCH" worker status 2>/dev/null || true)"
remote_previous_log="$(reported_worker_log_path "$remote_previous_status")"
remote_previous_availability=""
if [[ -n "$remote_previous_log" ]]; then
  remote_previous_availability="$(remote_login "$availability_log_script" "$remote_previous_log" 2>/dev/null || true)"
fi

echo "== [3/4] restart remote master + worker ($REMOTE_HOST) =="
remote_restart_script='
  set -e
  REMOTE_ORCH="$1"
  case "$REMOTE_ORCH" in
    "~/"*) REMOTE_ORCH="$HOME/${REMOTE_ORCH#\~/}" ;;
  esac
  MASTER_LISTEN="$2"
  "$REMOTE_ORCH" worker stop >/dev/null 2>&1 || true
  "$REMOTE_ORCH" master kill >/dev/null 2>&1 || true
  sleep 1
  "$REMOTE_ORCH" master start --listen "$MASTER_LISTEN"
  sleep 2
  "$REMOTE_ORCH" worker start
'
remote_restart_output="$(remote_login "$remote_restart_script" "$REMOTE_ORCH" "tcp://$MASTER_LISTEN")"
printf '%s\n' "$remote_restart_output"
if ! remote_worker_log="$(fresh_worker_log_path "remote worker ($REMOTE_HOST)" "$remote_restart_output")"; then
  exit 1
fi
master_status="$(remote_login "$remote_orch_script" "$REMOTE_ORCH" master status)"
printf '%s\n' "$master_status"
if printf '%s' "$master_status" | grep -qi "stale binary"; then
  echo "ERROR: master on $REMOTE_HOST is still running a stale binary after restart" >&2
  exit 1
fi
wait_registered "remote worker ($REMOTE_HOST)" remote_login "$remote_orch_script" "$REMOTE_ORCH" worker status
if ! remote_availability="$(wait_for_fresh_agent_availability "remote worker ($REMOTE_HOST)" "$remote_previous_availability" remote_login "$availability_log_script" "$remote_worker_log")"; then
  exit 1
fi
show_agent_availability "remote worker ($REMOTE_HOST)" "$remote_availability"

echo "== [4/4] restart local worker (master: $MASTER_ADDR) =="
local_previous_status="$(env ORCH_REMOTE="$MASTER_ADDR" "$ORCH_LOCAL_BIN" worker status 2>/dev/null || true)"
local_previous_log="$(reported_worker_log_path "$local_previous_status")"
local_previous_availability=""
if [[ -n "$local_previous_log" && -f "$local_previous_log" ]]; then
  local_previous_availability="$(latest_agent_availability_from_log "$local_previous_log" || true)"
fi
local_restart_script='
  set -e
  ORCH_REMOTE="$1"
  ORCH_LOCAL_BIN="$2"
  "$ORCH_LOCAL_BIN" worker stop >/dev/null 2>&1 || true
  "$ORCH_LOCAL_BIN" worker start
'
local_restart_output="$("$LOCAL_LOGIN_SHELL" -lc "$local_restart_script" orch "$MASTER_ADDR" "$ORCH_LOCAL_BIN")"
printf '%s\n' "$local_restart_output"
if ! local_worker_log="$(fresh_worker_log_path "local worker" "$local_restart_output")"; then
  exit 1
fi
wait_registered "local worker" env ORCH_REMOTE="$MASTER_ADDR" "$ORCH_LOCAL_BIN" worker status
if ! local_availability="$(wait_for_fresh_agent_availability "local worker" "$local_previous_availability" latest_agent_availability_from_log "$local_worker_log")"; then
  exit 1
fi
show_agent_availability "local worker" "$local_availability"

echo "Deploy complete: local CLI/TUI + $REMOTE_HOST master/worker + local worker are on the new binary."
