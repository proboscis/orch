package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
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
	NewControlAgent bool
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
	cmd.Flags().BoolVar(&opts.ForceNew, "new", false, "Restart layout only, preserving control agent session")
	cmd.Flags().BoolVar(&opts.NewControlAgent, "new-control-agent", false, "Also restart control agent session (implies --new for layout)")
	cmd.Flags().BoolVar(&opts.Dashboard, "dashboard", false, "Run dashboard UI (internal)")
	cmd.Flags().BoolVar(&opts.IssuesDashboard, "issues-dashboard", false, "Run issues dashboard UI (internal)")
	cmd.Flags().BoolVar(&opts.ShowResolved, "show-resolved", false, "Show resolved issues (internal)")
	cmd.Flags().BoolVar(&opts.ShowClosed, "show-closed", true, "Show closed issues (internal)")
	_ = cmd.Flags().MarkHidden("dashboard")
	_ = cmd.Flags().MarkHidden("issues-dashboard")
	_ = cmd.Flags().MarkHidden("show-resolved")
	_ = cmd.Flags().MarkHidden("show-closed")

	cmd.AddCommand(newMonitorListCmd())
	cmd.AddCommand(newMonitorKillCmd())

	return cmd
}

type monitorListOptions struct {
	All  bool
	JSON bool
}

func newMonitorListCmd() *cobra.Command {
	opts := &monitorListOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List running monitor instances",
		Long: `List all running orch-monitor instances.

By default, shows monitors for the current project only.
Use --all to show monitors from all projects.

Examples:
  orch monitor list           # List monitors for current project
  orch monitor list --all     # List all monitors across projects
  orch monitor list --json    # JSON output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitorList(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "List monitors from all projects")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output in JSON format")

	return cmd
}

func runMonitorList(opts *monitorListOptions) error {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return err
	}

	client, err := ensureDaemonReady(projectRoot)
	if err != nil {
		return err
	}
	defer client.Close()

	resp, err := client.ListMonitors(projectRoot, opts.All)
	if err != nil {
		return err
	}

	if opts.JSON || globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if len(resp.Monitors) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No monitors running")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPID\tTYPE\tVIEW\tPROJECT\tSTARTED")
	for _, mon := range resp.Monitors {
		started := formatTimeAgo(mon.StartedAt)
		project := mon.Project
		if len(project) > 30 {
			project = "..." + project[len(project)-27:]
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
			mon.ID, mon.PID, mon.Type, mon.View, project, started)
	}
	w.Flush()

	return nil
}

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

type monitorKillOptions struct {
	All    bool
	Global bool
}

func newMonitorKillCmd() *cobra.Command {
	opts := &monitorKillOptions{}

	cmd := &cobra.Command{
		Use:   "kill [MONITOR_ID]",
		Short: "Kill monitor instances",
		Long: `Kill one or more orch-monitor instances.

Examples:
  orch monitor kill mon-12345       # Kill specific monitor
  orch monitor kill --all           # Kill all monitors for current project
  orch monitor kill --all --global  # Kill all monitors everywhere`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			monitorID := ""
			if len(args) > 0 {
				monitorID = args[0]
			}
			return runMonitorKill(monitorID, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Kill all monitors for current project")
	cmd.Flags().BoolVar(&opts.Global, "global", false, "With --all, kill monitors from all projects")

	return cmd
}

func runMonitorKill(monitorID string, opts *monitorKillOptions) error {
	if monitorID == "" && !opts.All {
		return fmt.Errorf("specify MONITOR_ID or use --all")
	}

	projectRoot, err := getProjectRoot()
	if err != nil {
		return err
	}

	client, err := ensureDaemonReady(projectRoot)
	if err != nil {
		return err
	}

	resp, err := client.KillMonitor(monitorID, opts.All, opts.Global, projectRoot)
	if err != nil {
		return err
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if resp.KilledCount == 0 {
		if !globalOpts.Quiet {
			if opts.All {
				fmt.Println("No monitors to kill")
			} else {
				fmt.Printf("Monitor not found: %s\n", monitorID)
			}
		}
		return nil
	}

	if !globalOpts.Quiet {
		if resp.KilledCount == 1 {
			fmt.Printf("Killed monitor: %s\n", resp.KilledIDs[0])
		} else {
			fmt.Printf("Killed %d monitors: %v\n", resp.KilledCount, resp.KilledIDs)
		}
		if resp.FailedCount > 0 {
			fmt.Printf("Failed to kill %d monitors: %v\n", resp.FailedCount, resp.FailedIDs)
		}
	}

	return nil
}

func ensureDaemonReady(projectRoot string) (*daemon.ProtoClient, error) {
	return requireDaemon()
}

func runMonitor(opts *monitorOptions) error {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("project root required for monitor: %w", err)
	}

	daemonClient, err := ensureDaemonReady(projectRoot)
	if err != nil {
		return err
	}
	_ = daemonClient.Close()

	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return err
	}

	api, err := getAPI()
	if err != nil {
		return err
	}

	orchDir := monitor.GetOrchDir(projectRoot)
	settings := monitor.LoadUISettings(orchDir)

	var statuses []model.Status
	for _, s := range opts.Status {
		if s == "" {
			continue
		}
		statuses = append(statuses, model.NormalizeStatus(s))
	}

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

	forceNew := opts.ForceNew || opts.NewControlAgent

	m := monitor.New(api, issuesRoot, monitor.Options{
		Issue:           opts.Issue,
		Statuses:        statuses,
		RunSort:         runSort,
		IssueSort:       issueSort,
		Agent:           opts.Agent,
		Attach:          opts.Attach,
		ForceNew:        forceNew,
		NewControlAgent: opts.NewControlAgent,
		OrchPath:        os.Args[0],
		GlobalFlags:     monitorGlobalFlags(projectRoot, issuesRoot),
		ShowResolved:    opts.ShowResolved,
		ShowClosed:      opts.ShowClosed,
		UISettings:      settings,
		ProjectRoot:     projectRoot,
	})

	if opts.Dashboard {
		return m.RunDashboard()
	}
	if opts.IssuesDashboard {
		return m.RunIssuesDashboard()
	}

	return m.Start()
}

func monitorGlobalFlags(projectRoot, issuesRoot string) []string {
	var flags []string
	if projectRoot != "" {
		flags = append(flags, "--project-root", projectRoot)
	} else if globalOpts.ProjectRoot != "" {
		flags = append(flags, "--project-root", globalOpts.ProjectRoot)
	}
	if issuesRoot != "" {
		flags = append(flags, "--issues-root", issuesRoot)
	} else if globalOpts.IssuesRoot != "" {
		flags = append(flags, "--issues-root", globalOpts.IssuesRoot)
	}
	if globalOpts.Backend != "" {
		flags = append(flags, "--backend", globalOpts.Backend)
	}
	if globalOpts.LogLevel != "" {
		flags = append(flags, "--log-level", globalOpts.LogLevel)
	}
	return flags
}
