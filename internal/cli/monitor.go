package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/monitor"
	"github.com/spf13/cobra"
)

type monitorOptions struct {
	Issue           string
	Status          []string
	SortRuns        string
	SortIssues      string
	Agent           string
	Attach          bool
	ForceNew        bool
	Dashboard       bool
	IssuesDashboard bool
	ShowResolved    bool
	ShowClosed      bool
}

func newMonitorCmd() *cobra.Command {
	opts := &monitorOptions{}

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Interactive monitor for managing runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitor(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Filter to specific issue")
	cmd.Flags().StringSliceVar(&opts.Status, "status", nil, "Filter by status")
	cmd.Flags().StringVar(&opts.SortRuns, "sort-runs", string(monitor.SortByUpdated), "Sort runs by (updated|started|status|issue|agent|elapsed|name)")
	cmd.Flags().StringVar(&opts.SortIssues, "sort-issues", string(monitor.SortByName), "Sort issues by (name|status|title|priority|updated)")
	cmd.Flags().StringVarP(&opts.Agent, "agent", "a", "", "Control agent to launch in monitor chat pane")
	cmd.Flags().BoolVar(&opts.Attach, "attach", false, "Attach to existing monitor session if present")
	cmd.Flags().BoolVar(&opts.ForceNew, "new", false, "Force create a new monitor session")
	cmd.Flags().BoolVar(&opts.Dashboard, "dashboard", false, "Run dashboard UI (internal)")
	cmd.Flags().BoolVar(&opts.IssuesDashboard, "issues-dashboard", false, "Run issues dashboard UI (internal)")
	cmd.Flags().BoolVar(&opts.ShowResolved, "show-resolved", false, "Show resolved issues (internal)")
	cmd.Flags().BoolVar(&opts.ShowClosed, "show-closed", true, "Show closed issues (internal)")
	_ = cmd.Flags().MarkHidden("dashboard")
	_ = cmd.Flags().MarkHidden("issues-dashboard")
	_ = cmd.Flags().MarkHidden("show-resolved")
	_ = cmd.Flags().MarkHidden("show-closed")

	return cmd
}

func runMonitor(opts *monitorOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Auto-start daemon if not running
	vaultPath := st.VaultPath()
	if !daemon.IsRunning(vaultPath) {
		fmt.Fprintf(os.Stderr, "Starting orch daemon...\n")
		_, err := daemon.StartInBackground(vaultPath)
		if err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}
		// Give daemon a moment to initialize
		time.Sleep(200 * time.Millisecond)
		fmt.Fprintf(os.Stderr, "Daemon started\n")
	}

	// Load UI settings from .orch directory
	orchDir := monitor.GetOrchDir(st.VaultPath())
	settings := monitor.LoadUISettings(orchDir)

	var statuses []model.Status
	for _, s := range opts.Status {
		if s == "" {
			continue
		}
		statuses = append(statuses, model.Status(s))
	}

	// Use saved settings as fallbacks when flags aren't explicitly set
	runSortFallback := settings.RunSort
	if !monitor.IsValidSortKey(runSortFallback) {
		runSortFallback = monitor.SortByUpdated
	}
	issueSortFallback := settings.IssueSort
	if !monitor.IsValidSortKey(issueSortFallback) {
		issueSortFallback = monitor.SortByName
	}

	runSort, err := monitor.ParseSortKey(opts.SortRuns, runSortFallback)
	if err != nil {
		return err
	}
	issueSort, err := monitor.ParseSortKey(opts.SortIssues, issueSortFallback)
	if err != nil {
		return err
	}

	m := monitor.New(st, monitor.Options{
		Issue:        opts.Issue,
		Statuses:     statuses,
		RunSort:      runSort,
		IssueSort:    issueSort,
		Agent:        opts.Agent,
		Attach:       opts.Attach,
		ForceNew:     opts.ForceNew,
		OrchPath:     os.Args[0],
		GlobalFlags:  monitorGlobalFlagsWithVault(st.VaultPath()),
		ShowResolved: opts.ShowResolved,
		ShowClosed:   opts.ShowClosed,
		UISettings:   settings,
	})

	if opts.Dashboard {
		return m.RunDashboard()
	}
	if opts.IssuesDashboard {
		return m.RunIssuesDashboard()
	}

	return m.Start()
}

func monitorGlobalFlags() []string {
	var flags []string
	if globalOpts.VaultPath != "" {
		flags = append(flags, "--vault", globalOpts.VaultPath)
	}
	if globalOpts.Backend != "" {
		flags = append(flags, "--backend", globalOpts.Backend)
	}
	if globalOpts.LogLevel != "" {
		flags = append(flags, "--log-level", globalOpts.LogLevel)
	}
	return flags
}

// monitorGlobalFlagsWithVault returns global flags for child processes,
// ensuring the vault path is always included (even when loaded from config).
func monitorGlobalFlagsWithVault(vaultPath string) []string {
	var flags []string
	// Use the resolved vault path (from store) to ensure child processes
	// use the same vault, even when it was loaded from config file
	if vaultPath != "" {
		flags = append(flags, "--vault", vaultPath)
	} else if globalOpts.VaultPath != "" {
		flags = append(flags, "--vault", globalOpts.VaultPath)
	}
	if globalOpts.Backend != "" {
		flags = append(flags, "--backend", globalOpts.Backend)
	}
	if globalOpts.LogLevel != "" {
		flags = append(flags, "--log-level", globalOpts.LogLevel)
	}
	return flags
}
