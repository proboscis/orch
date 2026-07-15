# TestAgentOpenCodeNoMultiplexer flake — investigation notes (WIP)

Run: issue/integration-flake-opencode-no-multiplexer/run-20260715-221022 (claude arm)
This file is a session-death-resistant scratch log, per the revised verification
protocol (frequent WIP commits). It will be removed or trimmed before the PR is
finalized.

## Facts established so far (2026-07-15 22:17 JST)

### The failing test's actual mechanics (code reading)

`TestAgentOpenCodeNoMultiplexer` (test/integration/agent_test.go:191):

1. Requires REAL `opencode` in PATH (TestMain installs fake `claude`/`codex`
   shims but NOT `opencode`) — so the test is a no-op skip in any environment
   without opencode installed. CI passing "every time" is consistent with CI
   simply skipping it.
2. Starts `orch agent --backend opencode` with `cmd.Start()`, sleeps exactly
   1 second, kills the process, then requires
   `testRepo/.orch/control-agent.json` to exist.
3. **It never reads the command's stdout/stderr and never checks its exit
   error.** Any fail-fast error inside `orch agent` before the state save =
   missing state file = the exact observed failure, with the real error
   discarded.

`runOpenCodeAgent` (internal/cli/agent.go:151) sequence BEFORE the state file
is written (all through the daemon socket):

- `resolveExplicitProjectScope` → `getAPI()` (daemon connect)
- `GetControlAgentConfig` RPC — **fail-fast: returns error on failure** (agent.go:137)
- `loadControlAgentState` → `api.ReadFile` RPC
- `getIssuesRootForProjectIfConfigured` (config load)
- `writeControlPromptViaAPI` → `api.WriteFile` RPC (warning-only on failure)
- `exec.LookPath("opencode")` — fail-fast
- `saveControlAgentState` → `api.WriteFile` RPC ← the state file

So the 1s budget must cover: CLI process start + daemon connect + ≥4 RPC
round-trips through the test daemon. Any single slow/failing step starves it.

### Live host forensics

- Default tmux server currently hosts ONLY the two A/B run sessions
  (`run-integration-flake-opencode-no-multiplexer-20260715-221015` / `-221022`).
  => Killing those two sessions kills the tmux server itself ("no server
  running" from round 1 is the *session count reaching zero*, not necessarily
  an explicit `tmux kill-server`).
- Round-1 debris: uncleaned `$TMPDIR/orch-integration-*` dirs from 19:37–22:02
  (TestMain's `defer os.RemoveAll` never ran → the test PROCESS died mid-suite,
  consistent with "the investigation kills the investigator").
- Production worker `orch --remote=zeus:7777 worker run --worker-id
  host-CA-20035844` alive (started 22:10).
- Several `opencode serve --port 4096..4103` processes alive, started
  18:36–21:58 — fixed global port range shared by production AND test daemons
  (port-collision surface, needs code confirmation).

### Kill-path inventory (non-test code) — who can kill a tmux session

- reaper (`internal/daemon/reaper.go`): kills ONLY `run-<issue>-<run>` names
  for runs in the daemon's own store, guarded by interlocks. Test issue IDs
  (`tsv-test`, `run-builtins-*`) cannot collide with production session names.
- `stopRunSession` (socket.go): exact session name of own-store run; for
  opencode runs with ServerPort+SessionID it calls HTTP Abort on
  127.0.0.1:<port> — **cross-daemon surface if port collides**.
- `handleProtoRepairState` (proto_handler.go:3828): kills sessions from
  `mux.ListSessions()` (the WHOLE shared default server) classified as
  orphaned — but only with `--force` and no integration test calls repair.
- CLI `agent --new` / `--kill`: kills fixed name `orch-control-agent`
  (also kills a REAL production control-agent session if one exists — non-owned
  kill, but scoped to that name).
- test helpers: `killSession("orch-control-agent")`, kill of own
  `result.SessionName`, tmux_test.go / agent_session_test.go verified scoped
  (private sockets / unique names).

## Open questions being worked

1. Which concrete step inside `orch agent --backend opencode` exceeds 1s /
   fails under full-suite load (measure, not guess).
2. What killed the round-1 sessions/server — inspect round-1 debris logs.
3. opencode server port-range collision between test daemon and production
   daemon (managed_servers_db / process_manager reconcile behavior).
