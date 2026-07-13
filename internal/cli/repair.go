package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type repairOptions struct {
	DryRun bool
	Force  bool
}

func newRepairCmd() *cobra.Command {
	opts := &repairOptions{}

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair system state",
		Long: `Repair system state by fixing inconsistencies.

This command will:
- Restart the daemon if it's not running or unhealthy
- Mark "running" runs with no tmux session as failed
- Report orphaned, terminal-but-alive, and unreapable-kept sessions`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepair(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Report problems without fixing them")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Fix without confirmation")

	return cmd
}

func runRepair(opts *repairOptions) error {
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return err
	}

	problemsFound := 0
	problemsFixed := 0

	fmt.Println("Checking daemon registry...")
	registryFixed, err := repairDaemonRegistry(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	}
	if registryFixed > 0 {
		problemsFound += registryFixed
		if !opts.DryRun {
			problemsFixed += registryFixed
		}
	}

	fmt.Println("Checking daemon...")
	daemonFixed, err := repairDaemon(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	}
	if daemonFixed {
		problemsFound++
		if !opts.DryRun {
			problemsFixed++
		}
	}

	fmt.Println("Checking runs...")
	staleFixed, err := repairStaleRunsAPI(ctx, api, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	}
	problemsFound += staleFixed
	if !opts.DryRun {
		problemsFixed += staleFixed
	}

	fmt.Println("Checking for orphaned sessions...")
	repairResult, err := api.RepairState(ctx, &orchapi.RepairOptions{DryRun: opts.DryRun, Force: opts.Force})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	} else if repairResult != nil {
		problemsFound += repairResult.ProblemsFound
		if !opts.DryRun {
			problemsFixed += repairResult.ProblemsFixed
		}
		for _, detail := range repairResult.Details {
			if strings.Contains(detail, "session") {
				fmt.Printf("  %s\n", detail)
			}
		}
	}

	fmt.Println()
	if problemsFound == 0 {
		fmt.Println("No problems found.")
	} else if opts.DryRun {
		fmt.Printf("Found %d problems (dry-run, no changes made)\n", problemsFound)
	} else {
		fmt.Printf("Fixed %d/%d problems\n", problemsFixed, problemsFound)
	}

	if problemsFound > 0 && !opts.DryRun {
		os.Exit(1)
	}

	return nil
}

func repairDaemon(opts *repairOptions) (bool, error) {
	if daemon.IsRunning("") {
		pid := daemon.GetRunningPID("")
		fmt.Printf("  daemon running (pid=%d)\n", pid)

		stale, err := daemon.IsStaleBinary("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not check binary staleness: %v\n", err)
		} else if stale {
			fmt.Println("  WARNING: daemon is running stale binary (code updated since start)")
			if opts.DryRun {
				fmt.Println("  would restart daemon with new binary")
				return true, nil
			}

			oldMeta, _ := daemon.ReadMetadata("")
			if err := daemon.RestartDaemon(""); err != nil {
				return true, fmt.Errorf("failed to restart daemon: %w", err)
			}

			if waitForDaemonRestart(oldMeta, 2*time.Second) {
				newPid := daemon.GetRunningPID("")
				fmt.Printf("  restarted daemon with new binary (pid=%d)\n", newPid)
				return true, nil
			}
			return true, fmt.Errorf("daemon restart failed")
		}
		return false, nil
	}

	fmt.Println("  daemon not running")

	if opts.DryRun {
		fmt.Println("  would start daemon")
		return true, nil
	}

	daemon.RemovePID("")

	pid, err := daemon.StartInBackground()
	if err != nil {
		return true, fmt.Errorf("failed to start daemon: %w", err)
	}

	time.Sleep(200 * time.Millisecond)

	if daemon.IsRunning("") {
		fmt.Printf("  started daemon (pid=%d)\n", pid)
		return true, nil
	}

	return true, fmt.Errorf("daemon failed to start")
}

func repairStaleRunsAPI(ctx context.Context, api orchapi.OrchAPI, opts *repairOptions) (int, error) {
	filter := &orchapi.ListRunsFilter{
		Status: []orchapi.RunStatus{orchapi.RunStatusRunning, orchapi.RunStatusBooting},
	}
	result, err := api.ListRuns(ctx, filter)
	if err != nil {
		return 0, err
	}

	fixed := 0
	for _, run := range result.Runs {
		if run.AliveKnown && run.Alive {
			continue
		}
		if !run.AliveKnown {
			continue
		}

		fmt.Printf("  %s#%s: marked %s but agent not alive\n", run.IssueID, run.RunID, run.Status)
		fixed++

		newStatus := model.StatusFailed
		if run.Agent == "opencode" {
			newStatus = model.StatusUnknown
		}

		if opts.DryRun {
			fmt.Printf("    would mark as %s\n", newStatus)
			continue
		}

		ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}
		// Frozen client-plane status writer: scheduled to become a daemon API
		// verb (coupling-core roadmap Phase B3). Do not copy this pattern.
		event := &orchapi.Event{
			Type: "status", // nosemgrep: run-status-write-surface
			Name: string(newStatus),
		}
		if _, err := api.AppendEvent(ctx, ref, event); err != nil {
			fmt.Fprintf(os.Stderr, "    failed to update status: %v\n", err)
		} else {
			fmt.Printf("    marked as %s\n", newStatus)
		}
	}

	if fixed == 0 {
		fmt.Println("  all running runs have active agents")
	}

	return fixed, nil
}

func waitForDaemonRestart(oldMeta *daemon.DaemonMetadata, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !daemon.IsRunning("") {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		newMeta, err := daemon.ReadMetadata("")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if oldMeta == nil || newMeta.StartedAt.After(oldMeta.StartedAt) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func repairDaemonRegistry(opts *repairOptions) (int, error) {
	if err := daemon.CleanupStaleRegistrations(); err != nil {
		return 0, err
	}

	infos, err := daemon.ListAllDaemons()
	if err != nil {
		return 0, err
	}

	fixed := 0
	var unhealthy []*daemon.DaemonInfo

	for _, info := range infos {
		if !info.IsHealthy {
			unhealthy = append(unhealthy, info)
		}
	}

	if len(unhealthy) > 0 {
		fmt.Printf("  found %d unhealthy daemon(s) (running but socket unavailable):\n", len(unhealthy))
		for _, info := range unhealthy {
			fmt.Printf("    - pid=%d project=%s\n", info.PID, info.ProjectRoot)
			fixed++
			if !opts.DryRun && opts.Force {
				if err := daemon.KillDaemon(info.ProjectRoot); err != nil {
					fmt.Fprintf(os.Stderr, "      failed to kill: %v\n", err)
				} else {
					fmt.Printf("      killed unhealthy daemon\n")
				}
			}
		}
		if !opts.DryRun && !opts.Force && len(unhealthy) > 0 {
			fmt.Println("  use --force to kill unhealthy daemons")
		}
	}

	if len(infos) > 0 && len(unhealthy) == 0 {
		fmt.Printf("  %d daemon(s) registered, all healthy\n", len(infos))
	} else if len(infos) == 0 {
		fmt.Println("  no daemons registered")
	}

	return fixed, nil
}
