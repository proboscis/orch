---
type: issue
id: orch-013
title: Add comprehensive unit tests
summary: Unit tests for cli, agent, git, tmux, config, daemon packages
status: resolved
priority: high
---

# Add comprehensive unit tests

Add unit tests for packages currently lacking test coverage.

## Current State

**Has tests:**
- `internal/model/` - event_test.go, run_test.go
- `internal/store/file/` - file_test.go
- `test/integration/` - integration_test.go

**Needs tests:**
- `internal/cli/` - all command implementations
- `internal/agent/` - adapter, claude, codex, gemini, custom
- `internal/git/` - worktree.go
- `internal/tmux/` - tmux.go
- `internal/config/` - config.go
- `internal/daemon/` - daemon.go, monitor.go, pid.go

## Implementation Plan

### 1. CLI Package Tests (`internal/cli/*_test.go`)

Focus on testable logic without requiring actual tmux/git:

```go
// ps_test.go - test filtering, sorting, output formatting
func TestFilterRuns(t *testing.T) { ... }
func TestFormatDuration(t *testing.T) { ... }
func TestColorStatus(t *testing.T) { ... }

// run_test.go - test option validation, branch/session name generation
func TestRunOptionsValidation(t *testing.T) { ... }

// show_test.go - test output formatting
func TestFormatRunDetails(t *testing.T) { ... }
```

### 2. Agent Package Tests (`internal/agent/*_test.go`)

```go
// adapter_test.go
func TestLaunchConfigEnv(t *testing.T) { ... }
func TestAgentTypeFromString(t *testing.T) { ... }

// claude_test.go
func TestClaudeCommandGeneration(t *testing.T) { ... }
```

### 3. Git Package Tests (`internal/git/worktree_test.go`)

```go
func TestGenerateBranchName(t *testing.T) { ... }
func TestGenerateWorktreePath(t *testing.T) { ... }
// Mock exec.Command for worktree creation tests
```

### 4. Tmux Package Tests (`internal/tmux/tmux_test.go`)

```go
func TestSessionNameValidation(t *testing.T) { ... }
func TestBuildTmuxArgs(t *testing.T) { ... }
```

### 5. Config Package Tests (`internal/config/config_test.go`)

```go
func TestLoadConfig(t *testing.T) { ... }
func TestConfigDefaults(t *testing.T) { ... }
func TestConfigMerge(t *testing.T) { ... }
```

### 6. Daemon Package Tests (`internal/daemon/*_test.go`)

```go
// pid_test.go
func TestPidFileOperations(t *testing.T) { ... }

// monitor_test.go
func TestRunStatusDetection(t *testing.T) { ... }
```

## Testing Patterns

### Use Table-Driven Tests
```go
func TestFormatDuration(t *testing.T) {
    tests := []struct {
        name     string
        duration time.Duration
        want     string
    }{
        {"seconds", 30 * time.Second, "30s ago"},
        {"minutes", 5 * time.Minute, "5m ago"},
        {"hours", 2 * time.Hour, "2h ago"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := formatDuration(tt.duration)
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

### Use Interfaces for Mocking
```go
// For tmux/git operations, consider interfaces:
type CommandRunner interface {
    Run(name string, args ...string) ([]byte, error)
}
```

### Test Fixtures
Create `testdata/` directories with sample:
- Issue markdown files
- Run markdown files
- Config files

## Run Tests

```bash
go test ./...
go test -v ./internal/cli/...
go test -cover ./...
```

## Success Criteria

- [ ] All packages have corresponding `*_test.go` files
- [ ] `go test ./...` passes
- [ ] Coverage > 60% for core logic
- [ ] No flaky tests (avoid real tmux/git when possible)
