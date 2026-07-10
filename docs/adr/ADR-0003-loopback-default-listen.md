# ADR-0003: Loopback-default TCP listener — multi-host is opt-in

- Status: accepted
- Date: 2026-07-10

## Context

The daemon's TCP control socket is unauthenticated: any host that can reach
it can create/stop runs, read issues, and execute the full proto API. Until
this ADR the daemon bound `0.0.0.0:7777` **by default**, including when an
ordinary orch command auto-started it. A first-time user running the
install one-liner on a laptop silently exposed an unauthenticated control
plane to their LAN. The 2026-07-10 documentation audit flagged this as the
top pre-publication risk; single-host users — the default audience of a
published tool — get no benefit from the open bind.

The single-host zero-config flow does not need TCP from other hosts at all:
the ADR-0002 colocated worker registers to its local master via the
explicit `--remote=` local path, and every opencode-server consumer (prompt
injection, `opencode attach`, `orch models`) connects via `127.0.0.1` on
the same host.

## Decision

1. `DefaultTCPListenAddr` is `127.0.0.1:7777`. The daemon (including
   auto-started daemons) binds loopback only by default.
2. Multi-host operation is an **explicit opt-in**: start the master with
   `--listen tcp://0.0.0.0:7777`, or better a specific trusted interface
   (e.g. a Tailscale address). `scripts/deploy-all.sh` — a multi-host
   deploy by definition — passes `--listen` explicitly
   (`MASTER_LISTEN`, default `0.0.0.0:7777`).
3. The daemon logs a warning whenever it binds a non-loopback address,
   naming the exposure.
4. The opencode server is started with `--hostname 127.0.0.1` (was
   `0.0.0.0`) — all consumers are same-host.

## Consequences

- Existing multi-host operators must add `--listen tcp://0.0.0.0:7777` (or
  an interface address) to their master start command once. Workers and
  clients are unaffected; they dial out.
- Single-host users are unaffected functionally and no longer expose an
  unauthenticated port.
- SSH-tunnel-only setups now work with the default (no flag needed).
- The TCP API remains unauthenticated; this ADR narrows the default
  exposure but does not add authentication. An auth layer stays a
  separate decision.
