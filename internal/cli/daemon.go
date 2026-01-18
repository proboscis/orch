package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonDebugMode bool

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the background monitoring daemon",
		Hidden: true,
		Long: `Run the background monitoring daemon.

This command is normally started automatically by other orch commands.
You should not need to run this manually.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}

	cmd.Flags().BoolVar(&daemonDebugMode, "debug", false, "Enable verbose debug logging")

	return cmd
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
	st, err := getStore()
	if err != nil {
		return err
	}

	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("project root required for daemon: %w", err)
	}

	if daemon.IsRunning(projectRoot) {
		pid := daemon.GetRunningPID(projectRoot)
		fmt.Fprintf(os.Stderr, "daemon already running (pid=%d)\n", pid)
		os.Exit(1)
		return nil
	}

	d := daemon.New(projectRoot, st)
	if daemonDebugMode {
		d.SetDebugMode(true)
	}
	return d.Run()
}

func ensureDaemon() {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return
	}

	if daemon.IsRunning(projectRoot) {
		return
	}

	_, err = daemon.StartInBackground(projectRoot)
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

	_, err = daemon.StartInBackground(projectRoot)
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
