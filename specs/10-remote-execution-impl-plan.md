# Remote Execution — Implementation Plan

Reference: [specs/10-remote-execution.md](./10-remote-execution.md)

Status: Historical implementation plan. Runtime execution semantics are now
defined by [specs/12-orch-cluster-architecture.md](./12-orch-cluster-architecture.md)
and its implementation plan.

## Phase 0: TCP Transport

**Goal**: CLI can talk to a remote daemon. Zero functional changes — just a new transport.

### Changes

| File | Change |
|------|--------|
| `internal/daemon/socket.go` | Add `ListenTCP(addr string)` alongside existing Unix socket listener |
| `internal/daemon/proto_client.go` | Accept dial address string; switch on `unix://` vs `tcp://` scheme |
| `internal/daemon/daemon.go` | Parse `--listen` flag; start TCP listener if specified |
| `internal/cli/root.go` | Add `--remote` global flag; set on all subcommands |
| `internal/orchapi/daemon_client.go` | When `--remote` is set, pass TCP address to `ProtoClient`; skip `EnsureDaemonHealthy` auto-start |
| `internal/xdg/paths.go` | No change (Unix socket path unchanged for local daemon transport) |
| `cmd/orch/main.go` | Wire `--listen` for `daemon start` subcommand |

### Key Decisions

- TCP listener reuses the same `handleProtoConnection` loop (identical framing)
- Unix socket listener remains for local daemon transport (backward compatible)
- `--remote` takes precedence over local socket detection
- `ORCH_REMOTE` env var as fallback for `--remote` flag

### Validation

- `orch daemon start --listen tcp://0.0.0.0:7777` on remote host
- `orch --remote host:7777 ps` from client → shows runs from remote daemon
- `orch --remote host:7777 issue list` → shows issues from remote daemon
- All existing local behavior unchanged (no `--remote` flag = Unix socket)

### Estimated Scope

~150 lines of Go across 5 files.

---

## Phase 1: Executor Interface

**Goal**: Decouple command execution from `exec.Command`. Introduce `LocalExecutor` (wraps current behavior) and `SSHExecutor`.

### Changes

| File | Change |
|------|--------|
| `internal/executor/executor.go` (new) | `Executor` interface definition |
| `internal/executor/local.go` (new) | `LocalExecutor` — wraps `exec.Command` |
| `internal/executor/ssh.go` (new) | `SSHExecutor` — wraps `ssh <host> <cmd>` with ControlMaster |
| `internal/multiplexer/multiplexer.go` | Add `executor Executor` field to multiplexer structs |
| `internal/multiplexer/tmux.go` | Replace `exec.Command("tmux", ...)` with `m.executor.Run(ctx, "tmux", ...)` |
| `internal/multiplexer/zellij.go` | Same pattern as tmux |
| `internal/git/worktree.go` | Replace `exec.Command("git", ...)` with executor calls |

### Migration Strategy

1. Create `Executor` interface + `LocalExecutor`
2. Refactor `TmuxMultiplexer` to accept `Executor` in constructor
3. Default to `LocalExecutor` everywhere (zero behavioral change)
4. Add `SSHExecutor`
5. Wire target selection to pick the right executor

### SSH ControlMaster Setup

```go
type SSHExecutor struct {
    Host       string
    SocketDir  string   // e.g., ~/.ssh/orch-sockets/
}

func (e *SSHExecutor) Run(ctx context.Context, cmd string, args ...string) ([]byte, error) {
    sshArgs := []string{
        "-o", "ControlMaster=auto",
        "-o", fmt.Sprintf("ControlPath=%s/%%h-%%p", e.SocketDir),
        "-o", "ControlPersist=300",   // keep master alive 5 min
        e.Host,
        cmd,
    }
    sshArgs = append(sshArgs, args...)
    return exec.CommandContext(ctx, "ssh", sshArgs...).Output()
}
```

### Validation

- Existing tests pass with `LocalExecutor` (no behavior change)
- New test: `SSHExecutor` runs `echo hello` on a target → gets output
- `tmux new-session` via `SSHExecutor` creates session on remote host

### Estimated Scope

~300 lines for interface + two implementations. ~200 lines for multiplexer/git refactor.

---

## Phase 2: Target Configuration & `--on` Flag

**Goal**: `orch run my-issue --on mac` creates a worktree and starts an agent on the target machine.

### Changes

| File | Change |
|------|--------|
| `api/orch.proto` | Add `target` field to `Run` message and `StartRunRequest` |
| `internal/config/config.go` | Parse `targets:` config section |
| `internal/daemon/socket.go` | `processStartRunCore`: select executor based on target |
| `internal/daemon/monitor.go` | `monitorRun`: use run's executor for CapturePane/AgentAlive |
| `internal/cli/run.go` | Add `--on` flag to `orch run` |
| `internal/store/file/file.go` | Store target in run metadata |

### Executor Selection Flow

```
processStartRunCore(req):
    target = req.Target         // e.g., "mac" or ""
    if target == "" || target == "local":
        executor = LocalExecutor{}
    else:
        cfg = loadTargetConfig(target)
        executor = SSHExecutor{Host: cfg.Host}

    mux = TmuxMultiplexer{executor: executor}
    git = GitOps{executor: executor}

    // Rest of flow is identical:
    git.CreateWorktree(...)
    mux.NewSession(...)
    // etc.
```

### Repo Path Resolution

The target config specifies where the git repo lives on the target machine:

```yaml
targets:
  mac:
    host: mac
    repo: /Users/me/repos/project
```

Worktree path on target: `<repo>/.orch/worktrees/<issue>/<issue>-<run>-<agent>/`

### Validation

- `orch run test-issue --on mac` → creates worktree on MacBook via SSH
- `orch ps` → shows run with `target=mac`
- `orch capture <run>` → captures pane from MacBook's tmux
- `orch send <run> "hello"` → sends keys to MacBook's tmux session
- Daemon monitor loop correctly checks liveness on remote target

### Estimated Scope

~200 lines for target config + wiring. Proto regeneration.

---

## Phase 3: `orch attach` (Remote)

**Goal**: `orch attach <run>` SSHes into the target machine and attaches to the tmux session.

### Changes

| File | Change |
|------|--------|
| `api/orch.proto` | Add `target_host` to `GetAttachInfoResponse` |
| `internal/daemon/proto_handler.go` | Populate `target_host` from run's target config |
| `internal/cli/attach.go` | If `target_host` is set, `exec ssh -t <host> tmux attach -t <session>` |

### Behavior

```go
func runAttach(ref string) error {
    info := api.GetAttachInfo(ref)

    if info.TargetHost != "" {
        // Remote: SSH into target and attach
        return syscall.Exec("/usr/bin/ssh", []string{
            "ssh", "-t", info.TargetHost,
            info.Multiplexer, "attach-session", "-t", info.SessionName,
        }, os.Environ())
    }

    // Local: attach directly (existing behavior)
    return mux.AttachSession(info.SessionName)
}
```

### Validation

- `orch attach <remote-run>` → opens SSH + tmux on target machine
- Ctrl+B D detaches back to local shell
- `orch attach <local-run>` → unchanged behavior

### Estimated Scope

~50 lines.

---

## Phase 4: Control Agent Split

**Goal**: `orch monitor` works against a remote daemon with a local control agent.

### Changes

| File | Change |
|------|--------|
| `api/orch.proto` | Add `GetControlAgentConfigRequest/Response` messages |
| `internal/daemon/proto_handler.go` | Implement `handleProtoGetControlAgentConfig` |
| `internal/daemon/socket.go` | `processControlAgentConfig` — returns prompt + config without managing session |
| `internal/monitor/monitor.go` | When remote: use `GetControlAgentConfig` instead of `GetControlAgentLaunch` |
| `internal/monitor/control_session.go` | Client-side session management (already local, no change needed) |
| `internal/cli/agent.go` | When remote: fetch config from daemon, manage session locally |
| `orch-monitor-tui/orch_monitor/__main__.py` | Same split for Python TUI |

### Flow (Remote Mode)

```
Client (monitor):
  1. api.GetControlAgentConfig(project)        → {prompt, agent, model, args}
  2. Write ORCH_CONTROL_PROMPT.md locally
  3. Read local control-session.json
  4. Start/resume local opencode server
  5. Create/resume opencode session
  6. Attach to local pane
```

### Backward Compatibility

`GetControlAgentLaunch` remains unchanged for local daemon transport. The monitor checks if connected to a remote daemon and uses the appropriate API.

### Validation

- `orch monitor` with `ORCH_REMOTE=zeus:7777` → runs/issues from Zeus, control agent local
- `--new` restarts local layout, preserves local control session
- `--new-control-agent` clears local control session, creates fresh
- Control agent can call `orch run --on mac my-issue` via remote daemon

### Estimated Scope

~200 lines Go + ~100 lines Python.

---

## Phase 5: Client Config File

**Goal**: Persistent client configuration so you don't need `--remote` every time.

### Changes

| File | Change |
|------|--------|
| `internal/config/client.go` (new) | Parse `~/.config/orch/client.yaml` |
| `internal/cli/root.go` | Load client config; apply `remote.default` if no `--remote` flag |

### Config Format

```yaml
# ~/.config/orch/client.yaml
remote:
  default: zeus               # default --remote target
  hosts:
    zeus:
      addr: zeus:7777
    cloud:
      addr: 10.0.0.5:7777
```

### Validation

- With config: `orch ps` → connects to zeus:7777 automatically
- `orch --remote cloud ps` → overrides default
- `orch --remote "" ps` → bypass remote default and use local daemon transport
- No config file → existing behavior unchanged

### Estimated Scope

~100 lines.

---

## Phase 6: Remote Project Identity (Path-Agnostic)

**Goal**: Make remote daemon store resolution independent of client-local
absolute paths.

### Problem

Remote clients currently send `project_root` values derived from
the client machine. These paths are not valid on the remote daemon host, which
causes `no store available` errors.

### Approach

- Keep proto schema unchanged for now.
- In remote mode, encode request context as `repoid:<repo-id>` tokens derived
  from portable repo ID.
- On daemon side, decode token and resolve server-local project context from
  daemon repo registry (`repo_id -> project_root`).
- Add daemon repo registry commands so users can register mappings explicitly:
  `orch --remote <addr> daemon repo register <repo-url>` and inspect
  with `orch --remote <addr> daemon repo list`.
- Do not reintroduce path-derived project identity on either local or remote transport.

### Validation

- `orch --remote zeus:7777 ps` works without passing server filesystem paths
- `orch --remote zeus:7777 --project github.com/acme/repo ps` still resolves using
  repo identity token in remote mode
- Local daemon transport remains unchanged

### Estimated Scope

~150-220 lines Go.

---

## Dependency Graph

```
Phase 0 (TCP Transport)
    │
    ▼
Phase 1 (Executor Interface)
    │
    ▼
Phase 2 (Target Config + --on)  ──▶  Phase 3 (orch attach remote)
    │
    ▼
Phase 4 (Control Agent Split)
    │
    ▼
Phase 5 (Client Config)
```

Phases 0 and 1 are independent and can be developed in parallel.
Phase 3 can start as soon as Phase 2 is done.
Phase 4 can start as soon as Phase 0 is done (doesn't need executor).
Phase 5 is purely additive, can be done at any point after Phase 0.

## Out of Scope (Future)

| Feature | Why Deferred |
|---------|-------------|
| K8s executor | Different execution model (pods vs SSH); needs its own spec |
| Multi-remote aggregation | `orch ps --all` across multiple daemons; needs federation design |
| TLS/auth | Not needed for Tailscale/VPN networks |
| Remote daemon auto-provisioning | `orch remote setup zeus` — nice-to-have, not required |
| Worker pull model | Only needed if SSH push model proves insufficient at scale |
