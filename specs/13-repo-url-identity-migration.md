# 13. Repo URL Identity Migration

Status: Draft
Owner: orch

## Goal

Eliminate path-based project identity from user-facing runtime flows.

Project identity MUST be derived from Git remote URL and carried as repo identity.

## Non-Goals

- Removing internal filesystem paths from daemon/worker execution internals.
- Removing filesystem-based issue/worktree storage.

## Source of Truth

Project identity is the normalized Git repository URL identity (represented in runtime as repo ID).

- User-facing selectors: `--project`, `ORCH_PROJECT`
- Runtime transport: `RequestContext.project_id`
- Daemon routing: `project_id -> workspace root`

## Current Problems

1. Runtime commands still expose path flags (`--repo-root`, `--worktree-dir`).
2. Some runtime requests still carry path fields (`project_root`, `repo_root`) as identity/fallback.
3. `daemon repo register` expects server path instead of repo URL.
4. Cross-host execution can diverge when request context is derived from local path rather than explicit repo identity.

## Target UX

### Runtime

```bash
orch --project github.com/org/repo run ISSUE_ID
orch --project github.com/org/repo ps
orch --project github.com/org/repo show ISSUE#RUN
orch --project github.com/org/repo stop ISSUE#RUN
```

### Registration

```bash
orch --remote zeus:7777 daemon repo register https://github.com/org/repo.git
```

Daemon clones/manages workspace root and stores mapping:

`project_id(repo URL identity) -> daemon-managed workspace root`

## Migration Scope

### CLI/API surfaces to migrate

- Remove path-based runtime flags:
  - `run --repo-root`
  - `run --worktree-dir`
  - `restart-from --repo-root`
  - `restart-from --worktree-dir`
- Remove legacy project-root env runtime identity dependency.
- Keep `--project`, `ORCH_PROJECT` as canonical selectors.

### Proto/handler behavior

- Runtime identity comes from `RequestContext.project_id`.
- `project_root`/`repo_root` remain internal compatibility transport until full removal.
- Handler must resolve execution workspace from daemon registry by `project_id`.

### Daemon registration

- `daemon repo register` accepts repo URL.
- Daemon derives repo ID from URL identity.
- Daemon ensures managed clone workspace exists.

## Phased Plan

### Phase 1 (this change set)

1. Add explicit spec and inventory.
2. Make CLI runtime identity-first:
   - prioritize `--project`/`ORCH_PROJECT`
   - no user-facing path scope requirement
3. Remove runtime path flags from run/restart-from.
4. Update daemon repo register to accept repo URL and create/maintain managed workspace.
5. Update docs and E2E examples.

### Phase 2

1. Remove path-fallback identity wiring from proto client and handlers.
2. Remove deprecated path-oriented env/flags and compatibility fields.
3. Simplify monitor/admin project filters to identity-only.

## Acceptance Criteria

1. User can run remote runtime commands with only `--project <repo-url-identity>`.
2. No runtime command requires legacy repo-root/project-root flags.
3. `daemon repo register <repo-url>` succeeds and creates stable daemon mapping.
4. E2E (remote master + worker) passes with identity-only project scope.
5. `go test ./...` and `make lint` pass.
