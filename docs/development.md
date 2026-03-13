# Development Guide

This document covers versioning, branching, and contribution workflows for orch.

## Versioning (Semver)

orch follows [Semantic Versioning](https://semver.org/):

```
v<major>.<minor>.<patch>[-stage]

Examples:
  v0.2.0-beta   Beta release, second minor version
  v0.2.1        Patch/bugfix release
  v0.3.0        Feature release
  v1.0.0        Stable production release
```

### Version Bump Rules

| Change Type | Bump | Example |
|-------------|------|---------|
| Breaking API change | Major | v1.0.0 → v2.0.0 |
| New feature (backward compatible) | Minor | v0.2.0 → v0.3.0 |
| Bug fix | Patch | v0.2.0 → v0.2.1 |

### Release Stages

| Stage | Meaning | Audience |
|-------|---------|----------|
| `-alpha` | Early development, unstable | Internal only |
| `-beta` | Feature complete, needs testing | External testers |
| `-rc.N` | Release candidate, final polish | Wide testing |
| (none) | Stable | Production |

## Trunk-Based Development

We use **trunk-based development**:

```
main ───●───●───●───●───●───●────  (trunk - always deployable)
         ↖   ↖   ↖       ↓
      issue branches    release/v0.2.x (hotfixes only)
```

### Key Principles

1. **`main` is the trunk** - all PRs merge here
2. **Feature branches are short-lived** - hours to days, not weeks
3. **No long-lived `develop` branch** - simplicity over ceremony
4. **Releases are tagged from `main`** - no separate release branches for new versions
5. **Release branches only for hotfixes** - patching old versions when needed

### Why Trunk-Based?

- **Simple** - one integration point, less branching complexity
- **Fast feedback** - changes hit main quickly, issues found early
- **Works naturally with orch** - the issue/run model creates short-lived branches
- **Each run = short-lived branch → PR → merge** - matches orch workflow perfectly

## Development Workflow

### Using orch (Recommended)

```bash
# Start work on an issue
orch run my-issue
# Creates: issue/my-issue/run-YYYYMMDD-HHMMSS branch in isolated worktree

# Agent works autonomously
# Creates PR to main when done

# PR reviewed and merged → branch deleted
# Worktree cleaned up

# Release when ready
git tag v0.3.0
git push origin v0.3.0
```

### Manual Workflow

```bash
# Create feature branch
git checkout -b feature/add-notifications

# Make changes, commit
git add .
git commit -m "Add Slack notifications"

# Push and create PR
git push -u origin feature/add-notifications
gh pr create --base main

# After merge, clean up
git checkout main
git pull
git branch -d feature/add-notifications
```

## Branch Naming

| Type | Pattern | Example |
|------|---------|---------|
| Issue work (orch) | `issue/<issue-id>/run-<timestamp>` | `issue/orch-325/run-20260120-162011` |
| Manual feature | `feature/<description>` | `feature/add-slack-notifications` |
| Hotfix | `hotfix/<description>` | `hotfix/fix-daemon-crash` |
| Release | `release/v<version>` | `release/v0.2.x` |

### Branch Lifecycle

```
Create → Work → PR → Review → Merge → Delete
         ↑______________|
         (iterate as needed)
```

Branches should be:
- **Short-lived**: Merge within hours to days
- **Focused**: One feature/fix per branch
- **Clean**: Deleted after merge

## Contributing

1. **Find or create an issue** - All work starts with an issue
2. **Use orch when possible** - `orch run <issue-id>` handles branching
3. **Keep PRs small** - Easier to review, faster to merge
4. **Write clear commit messages** - Future you will thank you
5. **Test your changes** - `make test` before pushing

For direct CLI validation (without `go test`), see:
- [Master/Worker/Client Manual E2E](./e2e-master-worker-client.md)

### PR Guidelines

- **Title**: Clear, concise summary of the change
- **Body**: Reference the issue (e.g., "Fixes #123" or "Related: orch-330")
- **Size**: Prefer small, focused PRs over large ones
- **Tests**: Include tests for new functionality

## Releasing

Releases are created by tagging main:

```bash
# Ensure you're on main and up to date
git checkout main
git pull

# Create and push tag
git tag v0.3.0
git push origin v0.3.0
```

GitHub Actions will automatically:
- Build binaries for all platforms
- Create a GitHub Release
- Attach binaries to the release

See [GitHub Releases](https://github.com/proboscis/orch/releases) for published versions.
