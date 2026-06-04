---
name: orch-invariant-enforcer
description: |
  Use when reviewing the orch codebase to find architectural invariants that are
  documented or implied but NOT mechanically enforced, and to turn each into a
  breaking, tested enforcement via an orch issue. Produces one enforcement issue per
  invariant (not per violation) so each orch run adds structure instead of a patch.
  Trigger terms: invariant, enforce structure, enforcement, codebase invariant review,
  architecture lint, semgrep rule, fail-fast, daemon SSOT, newtype, typed ids,
  escape hatch, drive to zero, create enforcement issues.
version: 0.1.0
---

# Orch Invariant Enforcer

A methodology to convert "documented or implied but unenforced" codebase invariants
into **mechanical, breaking, tested enforcement** — and to file one orch issue per
invariant so an `orch run` can implement that enforcement.

## Why this exists

Agents work with bounded context. They cannot hold global invariants, so they **patch
locally** and the codebase rots. The only durable cure is enforcement that is
**mechanical** (the agent cannot violate it without the build failing) and **co-located**
(the constraint travels with the code the agent edits). Prompts/docs do not hold — only
mechanism does.

Core principles this skill operates under:

- **One issue per invariant, never per violation.** A per-violation fix is a patch and
  the same invariant re-rots elsewhere. A per-invariant issue adds the enforcement
  mechanism AND drives all current violations to zero.
- **Breaking, zero grandfathering.** If it is truly an invariant, every violation is a
  bug to fix now. No baseline/allowlist of tolerated violations.
- **Escape hatches are holes in the structure, to be closed** (`unsafePerformIO`,
  `undefined`, `panic` for control flow, `_ = err`, `interface{}`/`any`, `// nolint`,
  bare `except`, catch-all handlers). An undetected hatch is an enforcement gap, not a
  reason to weaken the invariant.
- **Prefer enforcement that ENUMERATES violations** (the compiler via newtype/ADT, or a
  semgrep rule) so the resolving run has a finite, mechanically-listed worklist.
- **The enforcement itself must be tested** — a wrong rule locks in wrongness. Every
  semgrep rule needs must-catch/must-not-false-positive fixtures; every type needs a
  property/round-trip test.

## Tool assignment (which mechanism closes which invariant class)

| Invariant class | Mechanism (the deliverable) |
|---|---|
| illegal state / state machine / IDs | type: newtype + ADT + (typestate); exhaustive switch |
| effect / layer boundary / "must not import / shell out / compute X" | semgrep rule (breaking) |
| ambient-authority denial / "this layer cannot do IO" | only a stronger type system (Haskell-style) closes this mechanically — scope to a boundary if/when needed; otherwise approximate with semgrep + capability passing |
| protocol / serialization / precedence round-trips | property test |
| semantically/mechanically undecidable remainder | hard review gate (rubric that BLOCKS merge) |

## Method

1. **Inventory existing enforcement first — do not duplicate.**
   - Read `.semgrep/architecture.yaml` (62+ breaking rules already exist), the `Makefile`
     `lint` target, `.github/workflows/quality.yaml`, `.pre-commit-config.yaml`.
   - Read `AGENTS.md` (== `CLAUDE.md`) for the stated principles.
   - Read `internal/model/` for which types are enums vs plain strings.
   - For each candidate invariant ask: *is it already mechanically enforced?* If yes, skip.

2. **Classify against the taxonomy** in `reference.md` (7 classes). For each unenforced
   invariant, locate current violations (use the grep/semgrep hints in `reference.md`)
   and pick the enforcement mechanism from the table above — preferring one that
   enumerates the violations.

3. **Draft one enforcement issue per invariant** using the canonical template in
   `reference.md`. The body must include: the invariant (one line), why (the junk/bug
   class it prevents), current enforcement status, the enforcement to add (mechanism +
   breaking + rule-test + hatches to ban), the drive-to-zero plan, and acceptance
   criteria.

4. **Confirmation gate.** Present the drafted issues to the user. Do NOT mass-create
   without confirmation — issue creation is state-changing and routes to the project's
   master.

5. **Create issues** from the target repo root so they route to that project:
   ```bash
   orch issue create <id> \
     --title "Enforce invariant: <name>" \
     --summary "<short>" \
     --tag invariant --tag enforcement --tag <area> <<'EOF'
   <body per template>
   EOF
   ```
   Use stable, descriptive ids (`inv-<slug>`). Before creating, check `orch issue list`
   / `orch issue show <id>` so re-running this skill does NOT duplicate existing issues
   (the hatch/invariant ratchet must be idempotent).

6. **Ratchet.** This skill is also the *discovery sensor*: when a run or review finds a
   new escape hatch, file (or extend) the invariant issue that bans it. Each closed
   hatch permanently raises the floor; never reopen it with a grandfather list.

## Granularity guard

If one invariant has many heterogeneous violations across unrelated areas, the
enforcement mechanism should still be a single rule/type (one issue), but the
drive-to-zero may be split into area-scoped sub-issues that all block on the
enforcement issue. Do not split the *enforcement* — only the cleanup.

## Output

A set of `inv-*` issues in the project's master, each resolvable by `orch run <id>`,
each producing: a breaking enforcement mechanism + its test + zero remaining violations
+ banned escape hatches. The set is the enforcement backlog.

See `reference.md` for the taxonomy, detection hints, the canonical issue template, and
the concrete violations catalogued for orch.
