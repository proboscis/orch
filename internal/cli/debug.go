package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/daemon"
	"github.com/spf13/cobra"
)

type DebugLogger struct {
	enabled bool
}

func NewDebugLogger() *DebugLogger {
	enabled := globalOpts.LogLevel == "debug" ||
		os.Getenv("ORCH_DEBUG") == "1" ||
		os.Getenv("ORCH_DEBUG") == "true" ||
		os.Getenv("ORCH_DEBUG") == "yes"
	return &DebugLogger{enabled: enabled}
}

func (d *DebugLogger) IsEnabled() bool {
	return d.enabled
}

func (d *DebugLogger) Printf(format string, args ...interface{}) {
	if d == nil || !d.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "[DEBUG] %s\n", fmt.Sprintf(format, args...))
}

func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug <run>",
		Short: "Debug a run by showing daemon perspective",
		Long: `Show what the daemon sees for a run.

This command is useful for debugging why a run has a particular status.
It shows:
- Agent liveness check results
- Session status (for opencode)
- Git state (commits ahead, uncommitted changes)
- PR status
- Daemon log location`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebug(args[0])
		},
	}
	return cmd
}

func runDebug(ref string) error {
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return err
	}

	run, err := resolveRunAPI(ctx, api, ref)
	if err != nil {
		return fmt.Errorf("run not found: %w", err)
	}

	fmt.Printf("=== Debug Info for %s#%s ===\n\n", run.IssueID, run.RunID)

	fmt.Printf("Current Status: %s\n", run.Status)
	fmt.Printf("Agent: %s\n", run.Agent)
	fmt.Printf("Branch: %s\n", run.Branch)
	fmt.Printf("Worktree: %s\n", run.WorktreePath)
	if run.PRUrl != "" {
		fmt.Printf("PR URL: %s\n", run.PRUrl)
	}
	fmt.Println()

	fmt.Printf("--- Agent Status ---\n")
	if run.AliveKnown {
		fmt.Printf("Agent Alive: %v\n", run.Alive)
	} else {
		fmt.Printf("Agent Alive: unknown\n")
	}

	if run.Agent == "opencode" {
		fmt.Printf("Server Port: %d\n", run.ServerPort)
		fmt.Printf("Session ID: %s\n", run.OpenCodeSessionID)
	}
	fmt.Println()

	fmt.Printf("--- Git State ---\n")
	fmt.Printf("Worktree Exists: %v\n", run.WorktreeExists)
	if run.WorktreeExists {
		fmt.Printf("Branch State: %s\n", run.BranchState)
		if run.DiffStats != nil {
			fmt.Printf("Files Changed: %d\n", run.DiffStats.FilesChanged)
			fmt.Printf("Additions: +%d\n", run.DiffStats.Additions)
			fmt.Printf("Deletions: -%d\n", run.DiffStats.Deletions)
		}
	}
	fmt.Println()

	fmt.Printf("--- PR Status ---\n")
	if run.PRUrl != "" {
		fmt.Printf("PR URL: %s\n", run.PRUrl)
		fmt.Printf("PR Number: %d\n", run.PRNumber)
		fmt.Printf("PR State: %s\n", run.PRState)
	} else {
		fmt.Printf("PR: none\n")
	}
	fmt.Println()

	projectRoot, _ := getProjectRoot()
	fmt.Printf("--- Daemon Info ---\n")
	fmt.Printf("Daemon Running: %v\n", daemon.IsRunning(projectRoot))
	if daemon.IsRunning(projectRoot) {
		fmt.Printf("Daemon PID: %d\n", daemon.GetRunningPID(projectRoot))
	}
	fmt.Printf("Daemon Log: %s\n", daemon.LogFilePath(projectRoot))

	return nil
}
