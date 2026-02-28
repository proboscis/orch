# Remote Execution

## Overview

orch supports running agent sessions on remote machines while controlling them from a local client. A **master daemon** runs on an always-on server, managing all state (issues, runs, events). **Clients** connect over TCP and interact via the existing proto API. The master executes runs either locally or on remote targets via SSH.

## Architecture

```
Client (MacBook)                  Master (Zeus)                     Target (any machine)
┌──────────────┐                 ┌──────────────────────┐          ┌──────────────────┐
│ orch CLI/TUI │── TCP ────────▶│ orch daemon           │── SSH ──▶│ tmux + agent     │
│              │                │  ├── issues/ (fs)      │          │ git worktree     │
│ Control agent│                │  ├── runs/   (fs)      │          │                  │
│ (local)      │                │  ├── worktrees/ (local)│          └──────────────────┘
│              │                │  ├── tmux (local runs) │
└──────────────┘                │  └── scheduling        │
                                └──────────────────────┘
```

### Design Principles

1. **Master daemon is the single source of truth** — all state lives on the master
2. **Clients are thin** — CLI/TUI only talk to the daemon API, never touch state directly
3. **Execution is abstracted** — the daemon doesn't care if a run is local or remote; it goes through an `Executor` interface
4. **Control agent is client-side** — the interactive agent you discuss with runs on your machine, not the master

## Components

### 1. TCP Transport

The daemon listens on TCP in addition to the existing Unix socket.

```
daemon --listen tcp://0.0.0.0:7777    # remote-accessible
daemon --listen unix://~/.orch/sock   # local-only (existing, unchanged)
```

The proto wire format (4-byte length-prefix + protobuf) is transport-agnostic. No changes to the proto schema, framing, or request/response types.

#### Client Connection

```
# Explicit
orch --remote zeus:7777 ps

# Environment
ORCH_REMOTE=zeus:7777 orch ps

# Config file (~/.config/orch/client.yaml)
remote:
  default: zeus
  hosts:
    zeus:
      addr: zeus:7777
```

When `--remote` is set:
- `ProtoClient` dials `net.Dial("tcp", addr)` instead of `net.Dial("unix", socketPath)`
- `EnsureDaemonHealthy()` is skipped (no auto-start for remote daemons)
- All other CLI behavior is identical

### Project Identity in Remote Mode

Remote clients MUST NOT rely on client-local absolute paths for daemon store
resolution. The daemon process is global and may run on a different machine with
different filesystem paths.

Remote requests use a portable project identity derived from repo ID:

```
repoid:<repo-id>
```

Where `<repo-id>` is derived from Git remote metadata (or deterministic fallback).

- Client side: when `--remote` is active, request context fields are encoded as
  `repoid:<repo-id>` tokens instead of local absolute paths.
- Daemon side: token values are resolved to server-local project context using
  daemon repo context + server config (`ORCH_PROJECT_ROOT` / project config).
- Local mode remains path-based and unchanged.

#### Authentication

None. The transport relies on network-level security (Tailscale, VPN, private network). The daemon binds to a configurable address; operators restrict access at the network layer.

### 2. Executor Interface

All command execution (git, multiplexer, file I/O) goes through an `Executor` abstraction. This decouples the daemon from the assumption that everything is local.

```go
type Executor interface {
    // Run a command, return stdout
    Run(ctx context.Context, cmd string, args ...string) ([]byte, error)

    // Run a command, return combined output and exit code
    RunWithStatus(ctx context.Context, cmd string, args ...string) ([]byte, int, error)

    // File operations
    WriteFile(ctx context.Context, path string, content []byte, perm os.FileMode) error
    ReadFile(ctx context.Context, path string) ([]byte, error)
    MkdirAll(ctx context.Context, path string, perm os.FileMode) error
    Stat(ctx context.Context, path string) (os.FileInfo, error)
    Remove(ctx context.Context, path string) error
}
```

#### Implementations

| Implementation | Transport | Use Case |
|---------------|-----------|----------|
| `LocalExecutor` | `exec.Command` | Runs on the daemon's own machine (current behavior) |
| `SSHExecutor` | `ssh <host> <cmd>` | Runs on a remote machine via SSH |

Future implementations (not in initial scope):
- `K8sExecutor` — runs commands via `kubectl exec` in pods
- `DockerExecutor` — runs commands in containers

#### SSHExecutor Details

```go
type SSHExecutor struct {
    Host    string          // SSH target (e.g., "mac", "user@host")
    Options []string        // SSH options (e.g., "-o", "ControlMaster=auto")
}
```

- Uses SSH `ControlMaster` for connection multiplexing (avoids per-command TCP handshake)
- File write: pipes content via stdin (`ssh host 'cat > path'`)
- File read: `ssh host cat path`

### 3. Remote-Aware Multiplexer

The existing `Multiplexer` interface is unchanged. Instead, the multiplexer commands are executed through the `Executor`:

```go
type TmuxMultiplexer struct {
    executor Executor    // LocalExecutor or SSHExecutor
}

// NewSession creates a tmux session
func (t *TmuxMultiplexer) NewSession(cfg *SessionConfig) error {
    // Instead of: exec.Command("tmux", "new-session", ...)
    // Now:        t.executor.Run(ctx, "tmux", "new-session", ...)
    return t.executor.Run(ctx, "tmux", "new-session", "-d", "-s", cfg.SessionName, cfg.Command)
}
```

This means:
- `LocalExecutor` → `tmux new-session ...` (current behavior)
- `SSHExecutor{Host: "mac"}` → `ssh mac tmux new-session ...`

All multiplexer operations (SendKeys, CapturePane, AgentAlive, etc.) work identically — they're just commands executed through a different transport.

### 4. Remote-Aware Git Operations

Same pattern as multiplexer. Git operations go through the `Executor`:

```go
// Instead of: exec.Command("git", "worktree", "add", ...)
// Now:        executor.Run(ctx, "git", "-C", repoRoot, "worktree", "add", ...)
```

The target machine must have:
- Git installed
- The repository cloned at a known path
- SSH keys / credentials for pushing to remote

### 5. Run Target Configuration

Runs specify where they execute via `--on` flag or config:

```bash
# Explicit target
orch run my-issue --on mac

# Default: runs on the daemon's own machine (local executor)
orch run my-issue
```

#### Config

```yaml
# .orch/config.yaml (on master)
targets:
  mac:
    host: mac                    # SSH host (from ~/.ssh/config or Tailscale)
    repo: /Users/me/repos/proj   # Git repo path on target
  zeus:
    host: localhost              # Special: means "this machine"
    repo: /home/me/repos/proj
```

#### Run Record

The `Run` proto message gains a `target` field:

```protobuf
message Run {
    // ... existing fields ...
    string target = 27;          // Execution target (e.g., "mac", "zeus", "")
}
```

Empty target means local (daemon's own machine).

### 6. Control Agent (Client-Side)

The control agent is the interactive agent you discuss with for planning and architecture. It always runs on the **client machine**, never on the master or a target.

#### Current API Split

`GetControlAgentLaunch` currently bundles server-side and client-side concerns. For remote, these split:

| Concern | Source | API |
|---------|--------|-----|
| Prompt content (issue list, repo context) | Daemon (remote) | `GetControlAgentConfig` (new) |
| Agent config (type, model, extra args) | Daemon (remote) | `GetControlAgentConfig` (new) |
| OpenCode server lifecycle | Client (local) | Client-side logic |
| Session create/resume | Client (local) | Client-side logic |
| control-session.json | Client (local) | Client-side file |

New proto message:

```protobuf
message GetControlAgentConfigRequest {
    string project_root = 1;
}

message GetControlAgentConfigResponse {
    bool ok = 1;
    string error = 2;
    string prompt_content = 3;       // Full ORCH_CONTROL_PROMPT.md content
    string agent = 4;               // Agent type (claude, opencode, etc.)
    string model = 5;               // Model name
    string model_variant = 6;       // Model variant
    repeated string extra_args = 7; // Additional CLI args
}
```

The client:
1. Calls `GetControlAgentConfig` on the remote daemon → gets prompt + config
2. Writes `ORCH_CONTROL_PROMPT.md` locally
3. Manages local opencode server / tmux session
4. Handles `control-session.json` locally
5. Resumes or creates the agent session locally

`GetControlAgentLaunch` remains for backward compatibility when daemon and client are on the same machine (local mode).

### 7. `orch attach` (Remote)

`orch attach` needs to reach the multiplexer session on the target machine.

```
# Local target (unchanged):
tmux attach-session -t run-abc

# Remote target:
ssh <target-host> -t tmux attach-session -t run-abc
```

The `GetAttachInfo` response gains a `host` field:

```protobuf
message GetAttachInfoResponse {
    // ... existing fields ...
    string target_host = 10;     // SSH host for remote targets (empty = local)
}
```

CLI behavior:
- If `target_host` is empty → attach locally (current behavior)
- If `target_host` is set → `ssh -t <host> tmux attach -t <session>`

### 8. `orch monitor` (Remote)

`orch monitor` works against the remote daemon with one key difference: the control agent pane runs locally.

```
Monitor layout (on client machine):
┌─────────────────┬──────────────────────────┐
│ Runs TUI        │ Issues TUI               │
│ (remote daemon) │ (remote daemon)          │
│                 ├──────────────────────────┤
│                 │ Control Agent             │
│                 │ (LOCAL - client machine)  │
└─────────────────┴──────────────────────────┘
```

- Runs/Issues panes: query remote daemon via TCP (existing orchapi calls)
- Control agent pane: uses `GetControlAgentConfig` (remote) for prompt, manages session locally
- Monitor registration: registers with remote daemon for heartbeat/lifecycle

## Command Changes

### New Flags

| Command | Flag | Description |
|---------|------|-------------|
| `orch daemon start` | `--listen <addr>` | TCP listen address (e.g., `tcp://0.0.0.0:7777`) |
| `orch run` | `--on <target>` | Execution target name |
| `orch *` (global) | `--remote <addr>` | Connect to remote daemon |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `ORCH_REMOTE` | Default remote daemon address (e.g., `zeus:7777`) |

### Config Files

**Client config** (`~/.config/orch/client.yaml`):
```yaml
remote:
  default: zeus
  hosts:
    zeus:
      addr: zeus:7777
```

**Server config** (`.orch/config.yaml` on master):
```yaml
listen: tcp://0.0.0.0:7777

targets:
  mac:
    host: mac
    repo: /Users/me/repos/project
```

## Invariants

1. **All state on master** — issues/, runs/, events are only on the master's filesystem
2. **No auth layer** — network-level security only (Tailscale/VPN)
3. **Proto API unchanged** — existing request/response types work for both local and remote
4. **Executor is the only abstraction** — no "remote daemon" or "worker" process on targets
5. **Control agent is always local** — never managed by the remote daemon
6. **Target machines need only: git, tmux/zellij, agent CLI** — no orch binary required
7. **Remote identity is path-agnostic** — client-local `project_root`/`issues_root` paths are not used as authoritative lookup keys on remote daemons
