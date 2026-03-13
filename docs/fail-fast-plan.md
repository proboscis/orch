# Fail-Fast Plan

## Goal

When `orch` cannot continue, it should fail as early as possible with an error
that tells the operator:

1. what failed
2. why it likely failed
3. what to do next

## Review Findings

### 1. Managed worker startup acknowledged success too early

Previous behavior:

```text
orch worker start
  -> local host starts a background worker process
  -> CLI prints success immediately
  -> worker may exit before register/heartbeat
  -> later `orch run` fails with "no active workers available"
```

This is not fail-fast.

### 2. Scheduler error hid the difference between "no workers" and "worker failed during startup"

Previous behavior:

```text
no active workers available; start a local worker on the target host via 'orch worker start'
```

That message is acceptable when no worker exists, but it is misleading when a
managed worker process already exists or just crashed during startup.

## Plan

### Phase A: Worker startup readiness gate

- `orch worker start` must wait for worker registration/activation.
- If the child exits before registration, return an immediate actionable error.
- If the child fails to register within a short timeout, fail and clean up the
  orphaned process.
- Persist local pid/log/metadata under XDG dirs so `worker stop` and `worker status`
  survive daemon restarts.

### Phase B: Scheduler-side diagnosis

- When scheduling finds no active worker, inspect managed worker state.
- If a managed worker exists but is not active, return a startup-failure error
  instead of the generic "start a worker" message.

### Phase C: Tests

- unit test: startup waits for registration
- unit test: startup fails when child exits before register
- unit test: startup fails on registration timeout
- unit test: lease selection error distinguishes inactive managed worker state

## Desired Operator Experience

```text
bad
  worker start -> "success"
  run          -> "no active workers available"

good
  worker start -> "managed worker host-zeus exited before registering ..."
                -> "check /.../orch/workers/<profile>.log ..."
                -> "run orch --remote=... worker run --worker-id host-zeus manually ..."
```
