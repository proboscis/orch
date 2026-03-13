#!/usr/bin/env bash
set -euo pipefail

attach_expect_live() {
  python3 - "$@" <<'PY'
import os
import pty
import signal
import subprocess
import sys
import time

cmd = sys.argv[1:]
master, slave = pty.openpty()
env = dict(os.environ)
env.setdefault("TERM", "xterm")
proc = subprocess.Popen(cmd, stdin=slave, stdout=slave, stderr=slave, close_fds=True, env=env)
os.close(slave)

deadline = time.time() + 1.5
captured = bytearray()

try:
    while time.time() < deadline:
        try:
            chunk = os.read(master, 4096)
            if chunk:
                captured.extend(chunk)
        except BlockingIOError:
            pass
        except OSError:
            break

        if proc.poll() is not None:
            text = captured.decode(errors="replace")
            if "terminal does not support clear" in text:
                sys.stderr.write(text)
                sys.stderr.write("\nattach reached interactive session but the test terminal is headless; treating as preflight success\n")
                sys.exit(0)
            sys.stderr.write(text)
            sys.stderr.write(f"\nattach exited early with code {proc.returncode}\n")
            sys.exit(1)
        time.sleep(0.1)
finally:
    try:
        proc.send_signal(signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=2)
    os.close(master)
PY
}

capture_until_contains() {
  local capture_cmd="$1"
  local expected="$2"
  local attempts="${3:-30}"
  local delay="${4:-1}"
  local output=""

  for _ in $(seq 1 "$attempts"); do
    output="$(eval "$capture_cmd")"
    if printf '%s' "$output" | grep -F "$expected" >/dev/null; then
      printf '%s\n' "$output"
      return 0
    fi
    sleep "$delay"
  done

  printf '%s\n' "$output"
  echo "expected capture output to contain: $expected" >&2
  return 1
}
