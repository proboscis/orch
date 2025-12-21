package cli

import (
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/monitor"
	"github.com/spf13/cobra"
)

type monitorOptions struct {
	Issue     string
	Status    []string
	Attach    bool
	ForceNew  bool
	Dashboard bool
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
	cmd.Flags().BoolVar(&opts.Attach, "attach", false, "Attach to existing monitor session if present")
	cmd.Flags().BoolVar(&opts.ForceNew, "new", false, "Force create a new monitor session")
	cmd.Flags().BoolVar(&opts.Dashboard, "dashboard", false, "Run dashboard UI (internal)")
	_ = cmd.Flags().MarkHidden("dashboard")

	return cmd
}

func runMonitor(opts *monitorOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	var statuses []model.Status
	for _, s := range opts.Status {
		if s == "" {
			continue
		}
		statuses = append(statuses, model.Status(s))
	}

	m := monitor.New(st, monitor.Options{
		Issue:       opts.Issue,
		Statuses:    statuses,
		Attach:      opts.Attach,
		ForceNew:    opts.ForceNew,
		OrchPath:    os.Args[0],
		GlobalFlags: monitorGlobalFlags(),
	})

	if opts.Dashboard {
		return m.RunDashboard()
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
