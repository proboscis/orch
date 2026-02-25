# Daemon State API Specification

## Overview

All run state modifications MUST go through the daemon API. No direct file writes.

## Motivation

1. **Single point of control** - Daemon enforces state transition rules
2. **Authority model** - Distinguish user commands from daemon observations
3. **Race condition prevention** - Daemon serializes concurrent writes
4. **Audit trail** - Track who changed what

## EventSource Authority Model

```
EventSourceUser   - Highest authority (CLI commands, TUI actions)
EventSourceAgent  - Medium authority (agent self-reported status)
EventSourceDaemon - Lowest authority (daemon inferences)
```

### Transition Rules

| From State | To State | User | Agent | Daemon |
|------------|----------|------|-------|--------|
| running    | waiting  | ✓    | ✓     | ✓      |
| running    | pr_open  | ✓    | ✓     | ✓      |
| running    | done     | ✓    | ✓     | ✓      |
| running    | canceled | ✓    | ✓     | ✓      |
| canceled   | running  | ✓    | ✗     | ✗      |
| canceled   | *        | ✓    | ✗     | ✗      |
| done       | *        | ✓    | ✗     | ✗      |
| failed     | *        | ✓    | ✗     | ✗      |

**Rule**: Terminal states (done/canceled/failed) can only be changed by EventSourceUser.

## API Specification

### Request: append_event

```json
{
  "type": "append_event",
  "issue_id": "my-issue",
  "run_id": "abc123",
  "event_type": "status",
  "event_name": "running",
  "event_attrs": {"key": "value"},
  "event_source": "user",
  "project_root": "/path/to/repo"
}
```

#### Fields

| Field | Required | Description |
|-------|----------|-------------|
| type | yes | Must be "append_event" |
| issue_id | yes | Issue ID |
| run_id | yes | Run ID |
| event_type | yes | Event type: "status", "phase", "artifact", "note" |
| event_name | yes | Event name (e.g., status value, artifact name) |
| event_attrs | no | Additional key-value attributes |
| event_source | yes | "user", "daemon", or "agent" |
| project_root | no | Project root for store resolution |

### Response: AppendEventResponse

```json
{
  "ok": true,
  "skipped": false,
  "reason": ""
}
```

#### Fields

| Field | Description |
|-------|-------------|
| ok | true if request was valid |
| skipped | true if event was not written due to transition rules |
| reason | Explanation if skipped (e.g., "cannot transition from canceled to pr_open") |

## Implementation

### Daemon Handler

```go
func (s *SocketServer) handleAppendEvent(req SendRequest, encoder *json.Encoder) {
    // 1. Resolve store
    // 2. Get current run
    // 3. Validate transition (if status event)
    // 4. Append event
    // 5. Return response
}
```

### Client Method

```go
func (c *Client) AppendEvent(issueID, runID string, event *model.Event, source model.EventSource) (*AppendEventResponse, error)
```

## Migration

### Phase 1: Add API (this spec)
- Add append_event handler to daemon
- Add AppendEvent method to client

### Phase 2: Migrate CLI
- run.go: Use client.AppendEvent for status events
- continue.go: Use client.AppendEvent for status events
- tick.go: Use client.AppendEvent for status events

### Phase 3: Migrate TUI
- monitor.go: Use daemon client for status changes
- merge_request.go: Use daemon client for PR events

### Phase 4: Remove direct writes
- Remove st.AppendEvent calls from CLI/TUI
- Store.AppendEvent becomes daemon-internal only

## Backward Compatibility

During migration:
- Direct st.AppendEvent still works
- Daemon API is preferred path
- Eventually direct writes will be removed
