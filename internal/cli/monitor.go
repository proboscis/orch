package cli

import (
	"fmt"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/monitor"
	"github.com/spf13/cobra"
)

type monitorOptions struct {
	Issue   string
	Status  string
	Attach  bool
	New     bool
	InTmux  bool // Internal flag: running inside the monitor tmux session
}

func newMonitorCmd() *cobra.Command {
	opts := &monitorOptions{}

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Interactive monitor for managing runs",
		Long: `Start an interactive terminal UI for managing multiple concurrent runs.

The monitor creates a tmux session with:
- Window 0: Dashboard showing all active runs
- Windows 1-9: Agent sessions for individual runs

Keyboard shortcuts in dashboard:
  1-9     Attach to run by index
  a       Answer mode - respond to blocked questions
  s       Stop mode - cancel a run
  n       New run - start a new issue
  r       Refresh run list
  ?       Show help
  q       Quit monitor

When viewing an agent window:
  Ctrl-b 0     Return to dashboard
  Ctrl-b n/p   Next/previous window
  Ctrl-b w     Window picker`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitor(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Filter to specific issue")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter by status (running,blocked,failed,pr_open,done)")
	cmd.Flags().BoolVar(&opts.Attach, "attach", false, "Auto-attach to existing monitor session")
	cmd.Flags().BoolVar(&opts.New, "new", false, "Force create new monitor (kill existing)")
	cmd.Flags().BoolVar(&opts.InTmux, "in-tmux", false, "Internal: running inside monitor tmux session")
	cmd.Flags().MarkHidden("in-tmux")

	return cmd
}

func runMonitor(opts *monitorOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Parse status filter
	var statusFilter []model.Status
	if opts.Status != "" {
		statusFilter = parseStatusList(opts.Status)
	}

	// Check for existing monitor session
	if monitor.HasMonitorSession() {
		if opts.New {
			// Kill existing and continue
			// Note: we don't import tmux here to avoid circular deps
			// The monitor will handle this
		} else if opts.Attach || !opts.InTmux {
			// Just attach to existing
			return monitor.AttachToMonitor()
		}
	}

	// Create and start monitor
	mon := monitor.New(st, &monitor.Options{
		Issue:  opts.Issue,
		Status: statusFilter,
		Attach: opts.Attach,
		New:    opts.New,
	})

	// If --in-tmux flag is set, we're already inside the monitor session
	// and should run the dashboard directly
	if opts.InTmux {
		return mon.Start()
	}

	// Otherwise, start will create a new tmux session if needed
	if err := mon.Start(); err != nil {
		return fmt.Errorf("monitor failed: %w", err)
	}

	return nil
}
