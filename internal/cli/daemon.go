package cli

import (
	"fmt"
	"os"

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
			vaultPath, err := getVaultPath()
			if err != nil {
				return nil
			}

			if !daemon.IsRunning(vaultPath) {
				return nil
			}

			return daemon.RestartDaemon(vaultPath)
		},
	}
}

func runDaemon() error {
	st, err := getStore()
	if err != nil {
		return err
	}

	vaultPath := st.VaultPath()

	if daemon.IsRunning(vaultPath) {
		pid := daemon.GetRunningPID(vaultPath)
		fmt.Fprintf(os.Stderr, "daemon already running (pid=%d)\n", pid)
		os.Exit(1)
		return nil
	}

	d := daemon.New(vaultPath, st)
	if daemonDebugMode {
		d.SetDebugMode(true)
	}
	return d.Run()
}

// ensureDaemon starts the daemon if it's not already running
// This is called from PersistentPreRun
func ensureDaemon() {
	// Only start daemon if we have a valid vault path
	vaultPath, err := getVaultPath()
	if err != nil {
		return // No vault configured, skip daemon
	}

	// Check if daemon is already running
	if daemon.IsRunning(vaultPath) {
		return
	}

	// Start daemon in background
	_, err = daemon.StartInBackground(vaultPath)
	if err != nil {
		// Log but don't fail - daemon is optional
		if globalOpts.LogLevel == "debug" {
			fmt.Fprintf(os.Stderr, "warning: failed to start daemon: %v\n", err)
		}
	}
}
