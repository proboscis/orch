# Invariant Enforcer — Reference

Taxonomy, detection hints, the canonical issue template, and the concrete orch
violations catalogued during the initial review.

---

## Canonical enforcement-issue template

Every `inv-*` issue MUST follow this shape. Copy it verbatim and fill in.

```markdown
## Invariant
<one sentence: the rule that must always hold>

## Why
<the junk/bug class this prevents; cite concrete past bugs where possible>

## Current status
- Documented in: <AGENTS.md section | "implied" | "none">
- Mechanically enforced: NO   (or: PARTIAL — <what is/isn't>)

## Enforcement to add (the deliverable)
- Mechanism: <semgrep rule | Go newtype/ADT | property test | review rubric item>
- BREAKING: fails `make lint` / CI / pre-commit. NO grandfather/allowlist.
- Rule test: <semgrep fixture (must-catch + must-not-false-positive) | property test>
- Ban these escape hatches around it: <list, e.g. unsafePerformIO, panic, _ = err, any>

## Drive to zero
- How violations are enumerated: <"the new type breaks the build at each site" | "semgrep lists them">
- Current known violations: <list or count + how to regenerate>
- Acceptance: zero remaining; any new violation fails the build.

## Acceptance criteria
- [ ] enforcement mechanism added and BREAKING
- [ ] enforcement rule has its own test
- [ ] all current violations fixed (count -> 0)
- [ ] known escape hatches banned
- [ ] `make lint` green, `go test ./...` green, TUI lint green
```

---

## Taxonomy (7 invariant classes for orch)

For each: what it is, detection hint, default mechanism.

### 1. Cross-language client drift (Go ↔ Python/Hy)
Protocol/business logic reimplemented in both the Go client and the Hy TUI, which
drifts. **The single largest bug source observed.**
- Detect: compare `internal/cli/root.go`, `internal/config/`, `internal/daemon/proto_client.go`
  against `orch-monitor-tui/orch_monitor/{config.py,proto_client.hy,daemon_api.py}` for
  parallel implementations (remote resolution, repo-id normalization, project scoping,
  status mapping).
- Mechanism: collapse to one source of truth (daemon-side or generated stubs) + semgrep
  banning client-side reimplementation in the Hy TUI.

### 2. Stringly-typed identifiers
IDs passed as `string`, allowing mix-ups (e.g., git URL vs normalized repo id).
- Detect: `rg 'func .*\b(issueID|runID|shortID|projectID|repoID) string' internal/`
- Mechanism: Go newtypes + smart constructors (compiler enumerates all sites).

### 3. Duplicated logic / multiple read-write sites
One concept implemented at N sites that drift (e.g., base-branch defaulting, session
name, status mapping).
- Detect: search the concept across packages; `rg 'baseBranch == ""' internal/git`.
- Mechanism: single function/owner; property/round-trip test to lock it.

### 4. Fail-fast / no silent fallback
Documented in AGENTS.md, NOT enforced. Silent fallbacks, swallowed errors, enum parsers
that accept unknown values.
- Detect: `rg '_ = ' internal/`, `rg 'recover\(' internal/daemon`, unsafe defaults in
  `ParseIssueStatus`/`NormalizeStatus` (`internal/model/event.go`).
- Mechanism: semgrep for the mechanical subset + make enum parsers return errors;
  review rubric for the undecidable remainder.

### 5. Daemon-SSOT — clients must not compute derived state
Enforced for Go `internal/cli|monitor` via semgrep, but **the Hy TUI is largely
uncovered** and violates it (git shell-out, client-side session names, client-side
sorting, 52+ `subprocess.run`).
- Detect: `rg 'subprocess.run' orch-monitor-tui/`, `rg 'git ' orch-monitor-tui/orch_monitor/config.py`.
- Mechanism: extend the TUI semgrep ruleset (the `tui-architecture-lint` pre-commit hook)
  to forbid client-side derivation; route through daemon APIs.

### 6. Enum exhaustiveness
`switch` over `Status`/`Phase`/`EventType`/`IssueStatus`/`BranchState` with no
compiler-enforced exhaustiveness; a new variant silently misses cases.
- Detect: review switches on these types; Go has no native exhaustiveness — use a linter
  (e.g., `go-check-sumtype`/`exhaustive`) + a default-case-must-error convention.
- Mechanism: exhaustive linter (breaking) + tests.

### 7. Enum parse safety
`ParseIssueStatus`/`NormalizeStatus` silently return a default for unknown input,
hiding corruption.
- Detect: `internal/model/event.go` parse functions.
- Mechanism: return `(T, error)`; callers fail fast. Property test: parse(render(x))==x
  for all variants and round-trip rejects unknowns.

---

## Concrete violations catalogued (initial review, this codebase)

Existing enforcement to NOT duplicate:
- `.semgrep/architecture.yaml` — 62+ ERROR rules, breaking, no allowlist. Scope:
  `internal/cli|monitor|daemon` only (excludes the Hy TUI except the thin
  `tui-architecture-lint` pre-commit hook).
- Types that ARE enums: `Status`, `Phase`, `IssueStatus`, `EventType`, `BranchState`,
  `Multiplexer` (`internal/model/event.go`, `internal/orchapi/types.go`).

Unenforced / violating:
- **Class 1 (drift):** `orch-monitor-tui/orch_monitor/config.py` (`resolve_remote_addr`,
  `resolve_project_identity`, `_parse_repo_id_from_url`), `proto_client.hy` status dicts —
  all duplicate Go logic in `internal/cli/root.go`, `internal/config/client.go`,
  `internal/daemon/proto_client.go`. Source of this session's bugs (removed `project_root`
  field referenced by Hy; remote support missing in TUI; project scoping missing).
- **Class 2 (IDs):** `RunID/IssueID/ProjectID/ShortID` are `string` across
  `internal/model/run.go`, `internal/orchapi/types.go`, and ~91 function signatures.
- **Class 3 (dup):** base-branch defaulting repeated in `internal/git/gogit.go`
  (`isMergedFast`, `getDiffStatsFast`, `getAheadBehind`); status mapping in both
  `internal/daemon/proto_client.go` and `proto_client.hy`.
- **Class 4 (fail-fast):** AGENTS.md "Fail Fast" prose-only; ~196 `_ =`, 7 `recover()`,
  unsafe `ParseIssueStatus`/`NormalizeStatus` defaults.
- **Class 5 (SSOT in TUI):** `config.py` shells out to `git config --get remote.origin.url`;
  `__main__.py` computes session names client-side and sorts sessions client-side;
  52+ `subprocess.run` in the TUI.
- **Class 6/7 (enum):** no exhaustiveness checking anywhere; parse fallbacks accept
  unknown values.

---

## Issue id convention

`inv-<slug>` (e.g., `inv-ssot-client-logic`, `inv-typed-ids`, `inv-fail-fast`). Tags:
`invariant`, `enforcement`, plus an area tag (`tui`, `model`, `daemon`, ...). Keep ids
stable so the skill is idempotent (check `orch issue list` before creating).
