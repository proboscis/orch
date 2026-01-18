package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/pr"
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
	st, err := getStore()
	if err != nil {
		return err
	}

	run, err := resolveRun(st, ref)
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

	mgr := agent.GetManager(run)
	isAlive := mgr.IsAlive(run)
	fmt.Printf("--- Agent Status ---\n")
	fmt.Printf("Agent Alive: %v\n", isAlive)

	if run.Agent == "opencode" {
		fmt.Printf("Server Port: %d\n", run.ServerPort)
		fmt.Printf("Session ID: %s\n", run.OpenCodeSessionID)

		if run.ServerPort > 0 {
			client := agent.NewOpenCodeClient(run.ServerPort)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			serverRunning := client.IsServerRunning(ctx)
			fmt.Printf("Server Running: %v\n", serverRunning)

			if serverRunning {
				sessions, _ := client.GetSessionIDs(ctx)
				sessionExists := sessions[run.OpenCodeSessionID]
				fmt.Printf("Session in /session list: %v (total sessions: %d)\n", sessionExists, len(sessions))

				statusMap, statusErr := client.GetSessionStatus(ctx, run.WorktreePath)
				if statusErr == nil {
					fmt.Printf("Sessions in /session/status: %d\n", len(statusMap))
					if status, ok := statusMap[run.OpenCodeSessionID]; ok {
						fmt.Printf("Session Status (from status map): %s\n", status)
					} else {
						fmt.Printf("Session Status (from status map): (not in map)\n")
					}
				} else {
					fmt.Printf("Session Status Error: %v\n", statusErr)
				}

				if run.OpenCodeSessionID != "" {
					status, found, _ := client.GetSingleSessionStatus(ctx, run.OpenCodeSessionID, run.WorktreePath)
					fmt.Printf("GetSingleSessionStatus found=%v status=%s\n", found, status)
				}
			}
		}
	}
	fmt.Println()

	if run.WorktreePath != "" {
		fmt.Printf("--- Git State ---\n")
		if _, err := os.Stat(run.WorktreePath); os.IsNotExist(err) {
			fmt.Printf("Worktree Exists: false\n")
		} else {
			fmt.Printf("Worktree Exists: true\n")

			repoRoot, err := git.FindMainRepoRoot(run.WorktreePath)
			if err == nil {
				cfg, _ := config.Load()
				baseBranch := "origin/main"
				if cfg != nil && cfg.BaseBranch != "" {
					remote, branch := git.ParseRemoteBranch(cfg.BaseBranch)
					baseBranch = git.RemoteBranchRef(remote, branch)
				}

				aheadCount, err := git.GetAheadCount(repoRoot, run.Branch, baseBranch)
				if err == nil {
					fmt.Printf("Commits Ahead of %s: %d\n", baseBranch, aheadCount)
				} else {
					fmt.Printf("Commits Ahead: (error: %v)\n", err)
				}
			}

			hasUncommitted := git.HasUncommittedChanges(run.WorktreePath)
			fmt.Printf("Uncommitted Changes: %v\n", hasUncommitted)
		}
	}
	fmt.Println()

	if run.Branch != "" {
		fmt.Printf("--- PR Status ---\n")
		prInfo, err := pr.LookupInfo("", run.Branch)
		if err != nil {
			fmt.Printf("PR Lookup Error: %v\n", err)
		} else if prInfo == nil {
			fmt.Printf("PR: none\n")
		} else {
			fmt.Printf("PR URL: %s\n", prInfo.URL)
			fmt.Printf("PR Number: %d\n", prInfo.Number)
			fmt.Printf("PR State: %s\n", prInfo.State)
		}
	}
	fmt.Println()

	issuesRoot, _ := getIssuesRoot()
	fmt.Printf("--- Daemon Info ---\n")
	fmt.Printf("Daemon Running: %v\n", daemon.IsRunning(issuesRoot))
	if daemon.IsRunning(issuesRoot) {
		fmt.Printf("Daemon PID: %d\n", daemon.GetRunningPID(issuesRoot))
	}
	fmt.Printf("Daemon Log: %s\n", daemon.LogFilePath(issuesRoot))

	return nil
}
