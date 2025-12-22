# orch

Orchestrator for managing multiple LLM CLIs (claude/codex/gemini) using a unified vocabulary of **issue/run/event**.

## Overview

orch operates **non-interactively** by default. When human input is needed, it uses events (`question`) to externalize the interaction, and `answer` + `tick` to resume.

## User Interaction Flow

```mermaid
sequenceDiagram
    participant U as User
    participant O as orch
    participant A as Agent (claude/codex)
    participant T as tmux

    Note over U: Start working on an issue
    U->>O: orch run my-issue
    O->>T: create session
    O->>A: start agent
    O-->>U: returns immediately

    Note over U: Check progress anytime
    U->>O: orch ps
    O-->>U: shows status (running/blocked/done)

    alt Agent needs help
        A->>O: emits question event
        A->>A: stops (blocked)
        Note over U: See blocked status
        U->>O: orch ps
        O-->>U: status: blocked
        U->>O: orch show my-issue
        O-->>U: shows pending question
        U->>O: orch answer my-issue q1 --text "answer"
        U->>O: orch tick my-issue
        O->>A: resume agent
    end

    alt Want to interact directly
        U->>O: orch attach my-issue
        O->>T: attach session
        Note over U,T: Direct terminal interaction
        U->>T: (type commands, paste images)
        U->>T: Ctrl+B D (detach)
    end

    alt Want to stop
        U->>O: orch stop my-issue
        O->>T: kill all sessions for issue
        O->>O: mark all runs canceled
    end

    alt Agent finishes
        A->>O: emits done/pr_open event
        U->>O: orch ps
        O-->>U: status: done or pr_open
    end
```

## State Machine

```mermaid
stateDiagram-v2
    [*] --> queued: orch run

    queued --> booting: agent starting
    booting --> running: agent ready

    running --> blocked: needs human input
    running --> pr_open: PR created
    running --> done: task complete
    running --> failed: error occurred
    running --> canceled: orch stop

    blocked --> running: orch answer + tick
    blocked --> canceled: orch stop

    pr_open --> done: PR merged

    done --> [*]
    failed --> [*]
    canceled --> [*]
```

## When to Use Each Command

```mermaid
flowchart TD
    START([Want to work on an issue?]) --> RUN[orch run ISSUE]

    RUN --> WAIT([Wait for agent...])
    WAIT --> CHECK{Check status}
    CHECK --> PS[orch ps]

    PS --> |running| DECIDE{Need to interact?}
    PS --> |blocked| BLOCKED([Agent needs help])
    PS --> |done/pr_open| DONE([Finished!])
    PS --> |failed| FAILED([Something went wrong])

    DECIDE --> |yes| ATTACH[orch attach]
    DECIDE --> |no| WAIT
    ATTACH --> |done interacting| WAIT

    BLOCKED --> SHOW[orch show --questions]
    SHOW --> ANSWER[orch answer RUN Q --text '...']
    ANSWER --> TICK[orch tick RUN]
    TICK --> WAIT

    FAILED --> RETRY{Retry?}
    RETRY --> |yes| RUN
    RETRY --> |no| END([End])

    DONE --> END

    style RUN fill:#4CAF50,color:#fff
    style PS fill:#2196F3,color:#fff
    style ATTACH fill:#FF9800,color:#fff
    style SHOW fill:#9C27B0,color:#fff
    style ANSWER fill:#9C27B0,color:#fff
    style TICK fill:#9C27B0,color:#fff
```

## Quick Reference

| Situation | Command |
|-----------|---------|
| Start working on an issue | `orch run ISSUE` |
| Continue from an existing run | `orch continue ISSUE#RUN_ID` |
| Continue from a branch | `orch continue ISSUE --branch BRANCH` |
| Check what's running | `orch ps` |
| Watch agent work / interact | `orch attach RUN` |
| See run details | `orch show RUN` |
| Agent is blocked - see why | `orch show RUN --questions` |
| Answer agent's question | `orch answer RUN QID --text "..."` |
| Resume after answering | `orch tick RUN` |
| Stop all runs for an issue | `orch stop ISSUE` |
| Stop a specific run | `orch stop ISSUE#RUN_ID` |
| Stop all runs globally | `orch stop --all` |
| Fix problems | `orch repair` |

## Statuses

| Status | Meaning | User Action |
|--------|---------|-------------|
| `queued` | Run created, waiting to start | Wait |
| `booting` | Agent is starting up | Wait |
| `running` | Agent is actively working | Wait, or `attach` to watch |
| `blocked` | Agent needs input | `show` → `answer` → `tick` |
| `pr_open` | PR created, awaiting review | Review the PR |
| `done` | Work completed | Nothing - celebrate! |
| `failed` | Run failed | Check logs, maybe retry |
| `canceled` | Manually stopped | Nothing |

## Background Monitoring

orch automatically runs a background daemon that monitors all running agents. You don't need to manage it manually.

**What the daemon does:**
- Monitors tmux sessions for all running runs
- Detects when agents finish (done/failed)
- Detects when agents are stuck or need input (blocked)
- Updates run status automatically

**If something goes wrong:**
```bash
orch repair    # Fixes daemon, stale states, orphaned sessions
```

## Configuration

```bash
# Set vault path (required)
export ORCH_VAULT=~/vault

# Or pass per-command
orch --vault ~/vault ps
```

## Vault Structure

```
vault/
├── issues/
│   └── <ISSUE_ID>.md      # Issue specification
└── runs/
    └── <ISSUE_ID>/
        └── <RUN_ID>.md    # Run log with events
```

## Vocabulary

| Term | Description |
|------|-------------|
| **Issue** | A unit of work/specification (e.g., `plc124`) |
| **Run** | A single execution attempt for an issue |
| **Event** | A single append-only record in a run |
| **RUN_REF** | Reference format: `ISSUE_ID#RUN_ID` or just `ISSUE_ID` (latest) |
