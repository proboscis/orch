package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/store/file"
	"github.com/spf13/cobra"
)

var daemonDebugMode bool

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background monitoring daemon",
		Long: `Manage the background monitoring daemon.

The daemon monitors all running agent sessions and updates their status.
It runs automatically in the background when needed.`,
	}

	runCmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}
	runCmd.Flags().BoolVar(&daemonDebugMode, "debug", false, "Enable verbose debug logging")
	cmd.AddCommand(runCmd)

	cmd.AddCommand(newDaemonListCmd())
	cmd.AddCommand(newDaemonKillCmd())
	cmd.AddCommand(newDaemonStatusCmd())

	return cmd
}

func newDaemonListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all running daemons",
		Long: `List all running orch daemons across all projects.

Shows PID, project directory, socket status, and uptime for each daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonList()
		},
	}
}

type daemonKillOptions struct {
	All     bool
	Project string
}

func newDaemonKillCmd() *cobra.Command {
	opts := &daemonKillOptions{}

	cmd := &cobra.Command{
		Use:   "kill",
		Short: "Kill running daemon(s)",
		Long: `Kill orch daemon(s).

By default, kills the daemon for the current project.
Use --all to kill all running daemons across all projects.
Use --project to kill a daemon for a specific project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonKill(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Kill all running daemons")
	cmd.Flags().StringVar(&opts.Project, "project", "", "Kill daemon for specific project path")

	return cmd
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status for current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStatus()
		},
	}
}

func runDaemonList() error {
	if err := daemon.CleanupStaleRegistrations(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cleanup stale registrations: %v\n", err)
	}

	infos, err := daemon.ListAllDaemons()
	if err != nil {
		return fmt.Errorf("failed to list daemons: %w", err)
	}

	if len(infos) == 0 {
		fmt.Println("No daemons running.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tPROJECT\tSOCKET\tUPTIME")

	for _, info := range infos {
		socketStatus := "ok"
		if !info.IsHealthy {
			socketStatus = "unavailable"
		}

		projectDisplay := info.ProjectRoot
		if home, err := os.UserHomeDir(); err == nil {
			if rel, err := filepath.Rel(home, info.ProjectRoot); err == nil && len(rel) < len(info.ProjectRoot) {
				projectDisplay = "~/" + rel
			}
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			info.PID,
			projectDisplay,
			socketStatus,
			formatUptime(info.Uptime),
		)
	}

	w.Flush()
	return nil
}

func runDaemonKill(opts *daemonKillOptions) error {
	if opts.All {
		count, err := daemon.KillAllDaemons()
		if err != nil {
			return fmt.Errorf("failed to kill daemons: %w", err)
		}
		if count == 0 {
			fmt.Println("No daemons were running.")
		} else {
			fmt.Printf("Killed %d daemon(s).\n", count)
		}
		return nil
	}

	var projectRoot string
	var err error

	if opts.Project != "" {
		projectRoot, err = filepath.Abs(opts.Project)
		if err != nil {
			return fmt.Errorf("invalid project path: %w", err)
		}
	} else {
		projectRoot, err = getProjectRoot()
		if err != nil {
			return fmt.Errorf("could not determine project root: %w\nUse --project to specify a path or --all to kill all daemons", err)
		}
	}

	if !daemon.IsRunning(projectRoot) {
		fmt.Printf("No daemon running for %s\n", projectRoot)
		return nil
	}

	pid := daemon.GetRunningPID(projectRoot)
	if err := daemon.KillDaemon(projectRoot); err != nil {
		return fmt.Errorf("failed to kill daemon (pid=%d): %w", pid, err)
	}

	fmt.Printf("Killed daemon (pid=%d) for %s\n", pid, projectRoot)
	return nil
}

func runDaemonStatus() error {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("could not determine project root: %w", err)
	}

	fmt.Printf("Project: %s\n", projectRoot)

	if !daemon.IsRunning(projectRoot) {
		fmt.Println("Status: not running")
		return nil
	}

	pid := daemon.GetRunningPID(projectRoot)
	fmt.Printf("Status: running (pid=%d)\n", pid)

	if daemon.IsDaemonSocketAvailable(projectRoot) {
		fmt.Println("Socket: available")
	} else {
		fmt.Println("Socket: unavailable")
	}

	if meta, err := daemon.ReadMetadata(projectRoot); err == nil {
		fmt.Printf("Started: %s\n", meta.StartedAt.Format(time.RFC3339))
		fmt.Printf("Uptime: %s\n", formatUptime(time.Since(meta.StartedAt)))
	}

	stale, err := daemon.IsStaleBinary(projectRoot)
	if err == nil && stale {
		fmt.Println("Warning: daemon is running stale binary (code updated since start)")
	}

	fmt.Printf("Log: %s\n", daemon.LogFilePath(projectRoot))

	return nil
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "daemon-restart",
		Short:  "Restart daemon with new binary",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := getProjectRoot()
			if err != nil {
				return nil
			}

			if !daemon.IsRunning(projectRoot) {
				return nil
			}

			return daemon.RestartDaemon(projectRoot)
		},
	}
}

func runDaemon() error {
	if daemon.IsRunning("") {
		pid := daemon.GetRunningPID("")
		fmt.Fprintf(os.Stderr, "daemon already running (pid=%d)\n", pid)
		os.Exit(1)
		return nil
	}

	storeFactory := func(issuesRoot string) (store.Store, error) {
		return file.New(issuesRoot)
	}

	d := daemon.New(storeFactory)
	if daemonDebugMode {
		d.SetDebugMode(true)
	}
	return d.Run()
}

func ensureDaemon() {
	if daemon.IsRunning("") {
		return
	}

	_, err := daemon.StartInBackground()
	if err != nil {
		if globalOpts.LogLevel == "debug" {
			fmt.Fprintf(os.Stderr, "warning: failed to start daemon: %v\n", err)
		}
	}
}

// testBypassDaemon allows unit tests to bypass daemon requirement
// Set this to true in tests along with a testStore for direct file operations
var testBypassDaemon bool

func requireDaemon() (*daemon.Client, error) {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return nil, err
	}

	client := daemon.NewClient(projectRoot)
	if client.IsAvailable() {
		return client, nil
	}

	_, err = daemon.StartInBackground()
	if err != nil {
		return nil, fmt.Errorf("daemon not running and failed to start: %w\nRun 'orch repair' to fix daemon issues", err)
	}

	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if client.IsAvailable() {
			return client, nil
		}
	}

	if !client.IsAvailable() {
		return nil, fmt.Errorf("daemon did not become available after starting\nRun 'orch repair' to fix daemon issues")
	}

	return client, nil
}
