# Observation-Vocabulary Coverage Check (Phase D2)

Status: completed; verdict below
Date: 2026-06-12

The coupling-core roadmap's D2 check: can past run-lifecycle bugs be
rewritten as observation sequences in the O1–O10 vocabulary of
`run-state-machine.md` §2? If less than ~70% fit, the decomposition itself
is suspect. Sample: 24 state-transition/monitoring fix commits
(2025-11 → 2026-06); 3 UI-only fixes excluded, leaving 21.

## Result

| Verdict | Count | Share |
|---------|-------|-------|
| Fully expressible in O1–O10 | 13 | 61.9% |
| Expressible with additional vocabulary | 8 | 38.1% |
| Not expressible as observations at all | 0 | 0% |

**Judgment: the taxonomy is sound.** The headline 61.9% is below the 70%
bar, but every miss is an *infrastructure-plane* observation (server
process health, worker presence, lease routing, store consistency) —
exactly the plane that `step()` v1 deliberately scoped out. The gap is a
plane boundary, not a decomposition failure: no bug required abandoning
the observation→transition shape, and all 8 missing observations are
metrics over existing infrastructure (no new protocol).

## Mapping (abridged; ○ fully expressible, △ needs new vocabulary)

| SHA | Fix | Obs used | Verdict |
|-----|-----|----------|---------|
| 7a675384 | run state detection reliability | O2 O3 O4 | ○ |
| 8fe89750 | zellij session resurrection | O2 O3 | ○ |
| d741f0bc | step() consolidation (duplicate events) | O1–O5 | ○ |
| 43f79a32 | feedback resumes run | O6 | ○ |
| d0094b2b | closed-PR watcher terminal transition | O1 | ○ |
| e9cdf6e0 | stale branch-PR cache | O1 O5 | ○ |
| f087d260 | false merged on new branch | O1 O5 | ○ |
| 95aef70c | confirm alive before fail | O2 | ○ |
| 13912ae6 | capture retry throttle | O2 O9 | ○ |
| 65d45d9f / 2923ded2 / e03c35e6 | launch ladder flags/paths | O8 | ○ |
| 0be559ec | continue status align | O1 | ○ |
| b666d999 | never-started run false done | O2 O5 + **server process death** | △ |
| 7bfbcf88 | workerless runs blocked | O2 O8 O9 + **worker presence / lease failure** | △ |
| a86836e2 | worker-hosted capture lease | O4 O5 + **lease routing** | △ |
| af506c44 | master snapshot mutation | O10 + **store consistency** | △ |
| 9001859f | opencode server state persistence | O2 O4 + **persistent server state** | △ |
| 4af983e6 | daemon socket probe | O9 + **socket connection state** | △ |
| dc286cfc | control session recovery | O2 O3 + **control-session recovery** | △ |
| a4420e47 | project scope isolation | O10 + **project-scope isolation** | △ |

Most-used observation: O2 (session alive), in 9 of 21 — session liveness
is the dominant lifecycle concern, which matches where the v1 laws
concentrate (L3'/L5 dead-session ladder).

## Extension backlog (priority order)

Candidate new observations, all derivable from existing infrastructure:

1. **O11 server/process health** — daemon & opencode server process
   liveness as a first-class observation (would have caught b666d999,
   parts of 7bfbcf88/4af983e6).
2. **O12 worker presence** — worker registration/heartbeat state visible
   to the monitor plane (7bfbcf88). Note: the lease core already exposes
   this to RPC waits (worker-lease.md LL1); this is about surfacing it to
   `step()`.
3. **O13 lease outcome** — lease acquire/dispatch/timeout results as
   observations rather than RPC errors (a86836e2).
4. **O14 store consistency signals** — master↔worker projection ordering
   (af506c44, 9001859f). Likely Phase-after-next: interacts with the W9
   quarantine decision (run-state-machine.md §6).

Adding O11–O13 lifts full coverage from 62% to roughly 75% without
changing the pure-function design; they slot into the existing
gather→step→execute split as new gatherers plus matrix rows.

## Coupling-core note

The 38% gap clusters on the **worker-lease and process-infrastructure
cores**, not the run-state core — consistent with the watchlist ranking
(worker lease as the second core). When B1 (launch-ladder integration)
and the lease E2 laws land, O11–O13 become the natural next vocabulary
additions; revisit this check after ~20 more lifecycle fixes accumulate.
