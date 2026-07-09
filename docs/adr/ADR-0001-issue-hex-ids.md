# ADR-0001: Derived hex IDs for issues (docker-style handles)

- Status: accepted (2026-07-09)
- Deciders: maintainer + implementation session
- Scope: issue identity & resolution, file backend, CLI display

## Context

Issue identity today is the human-chosen name string (frontmatter `id:`,
filename as fallback), and every lookup is an exact match on that string
(`FileStore.ResolveIssue`, `internal/store/file/file.go`). Runs, by contrast,
already have a hex handle: the run short ID is *derived* — the first 6 chars of
`sha256(issueID + "#" + runID)` — never stored, and the RUN_REF grammar
reserves `^[0-9a-f]{2,6}$` for it (`internal/orchapi/types.go`).

We want docker-style ergonomics for issues: keep the human name, but give every
issue a stable hex ID usable in *all* issue operations, with unique-prefix
resolution. Two structural constraints shape the design:

1. The file backend's store-of-record is Markdown files that are also created
   by hand and by other tools (Obsidian vaults). Any *stored* ID needs a
   backfill/migration story for files that don't have one.
2. The hex namespace is already partially occupied: 2–6 hex chars mean "run
   short ID" in every RUN_REF position. An issue named `beef` or `0001` is
   already unreachable as a bare RUN_REF today (pre-existing footgun).

## Decision

### 1. The issue hex ID is derived, not stored

```
full  = lowercase hex sha256(issue ID string)   (64 chars)
short = full[:8]                                 (display form)
```

Same principle as the run short ID: computable anywhere (client or daemon,
with or without store access), zero migration, hand-authored issue files get
an ID for free, and the frontmatter format is unchanged.

Renaming an issue would change its hex ID — acceptable: rename is not a
supported operation, and the name *is* the identity today.

### 2. Resolution contract (single choke point at the store boundary)

Every issue-identifying argument accepted by the file store resolves as:

1. **Exact match on the issue ID** (current behavior, always wins), else
2. **Hex-prefix match**: if the argument matches `^[0-9a-f]{7,64}$`, compare
   it as a prefix of every issue's full hex ID.
   - exactly one match → that issue
   - multiple matches → fail loud with the candidate list (name + short hex)
   - zero matches → `issue not found` (unchanged)

This lives inside `FileStore` (issue lookups *and* the issue-ID inputs of run
lookups: `GetRun`, `GetLatestRun`, `ListRuns` filter, `DeleteRun`,
`CreateRunForExistingIssue`), so every daemon handler and every CLI command —
`issue show/edit/close`, `resolve`, `run`, `show`, `capture`, `send`, `stop`,
`wait`, `restart-from`, … — becomes hex-capable without per-command changes.

### 3. Grammar partition (no ambiguity by construction)

| Pattern | Meaning |
|---|---|
| `[0-9a-f]{2,6}` | run short ID (unchanged) |
| `[0-9a-f]{7,64}` | issue hex ref (new) |
| anything else | issue name (unchanged) |

The minimum issue-hex prefix is **7** chars so the two hex namespaces are
disjoint at the syntax level: `orch show 3f2a91c8` can only mean an issue,
`orch show 3f2a91` can only mean a run.

### 4. Creation guard

`issue create` (and implicit issue creation on `CreateRun`) rejects new issue
IDs matching `^[0-9a-f]{2,64}$` — such names would be shadowed by the run
short ID grammar (2–6) or collide with issue hex refs (7+). Pre-existing
issues with hex-lookalike names keep working because exact match has priority.
The error message suggests a non-hex prefix (e.g. `issue-0001`).

### 5. Display

- `orch issue list` gains an `ID` column (short form); JSON output gains
  `hex_id` (short) and `hex_id_full`.
- `orch issue create` prints the short hex alongside the name.
- `orch issue show` prints the short hex.
Since the ID is derived, no proto/wire change is needed — clients compute it
from the issue ID they already have.

### 6. Out of scope

- GitHub backend: `gh-<number>` handles are already short and GitHub numbers
  are the natural key; the legacy JSON github path is untouched.
- Cross-project global IDs: the hash deliberately does not include the
  project ID. Stores are project-scoped and multi-store lookups already have
  ambiguity detection (`ambiguous_issue_id`); the same-name-same-hex property
  across projects changes nothing about resolution.

## Consequences

- Zero migration; `.md` files remain byte-identical.
- Deterministic: the same name always yields the same hex, everywhere. This is
  a feature (IDs can be computed offline) and a non-goal violation for
  docker-purists (recreating a deleted issue with the same name yields the
  same ID) — accepted, because the name is the identity.
- 8-char display prefix has a birthday bound around tens of thousands of
  issues per store; ambiguity is detected and fail-loud, and any longer prefix
  (up to 64) disambiguates.

## Alternatives considered

- **Stored random UID in frontmatter (full docker semantics)** — rejected:
  requires backfilling every existing/hand-authored issue file, or mutating
  files on read; the file store is fail-loud on malformed files and other
  tools write these files too.
- **Allowing short (<7) issue-hex prefixes** — rejected: collides with the
  run short ID grammar; resolution would depend on lookup order instead of
  syntax.
- **Hashing `project_id + name`** — rejected: the store doesn't know its
  project ID, resolution is store-scoped anyway, and it would break offline
  computability from the name alone.

## Verification

- `internal/model`: derivation is stable (golden value), short form is
  `full[:8]`, ref-classification helpers respect the grammar partition.
- `internal/store/file`: resolve by full hex / 8-prefix / 7-prefix; exact name
  beats hex-prefix; ambiguous prefix errors with candidates; run lookups
  (`GetRun`, `ListRuns`, `GetLatestRun`) accept issue hex refs; creation guard
  rejects `^[0-9a-f]{2,64}$` names on both `CreateIssue` and implicit-create
  `CreateRun`.
- daemon: `GetIssue` proto handler resolves a hex ref end-to-end.
- CLI: `issue list` renders the ID column; `issue create` prints the hex.
