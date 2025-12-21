package cli

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the background monitoring daemon",
		Hidden: true, // Users shouldn't call this directly
		Long: `Run the background monitoring daemon.

This command is normally started automatically by other orch commands.
You should not need to run this manually.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}

	return cmd
}

func runDaemon() error {
	st, err := getStore()
	if err != nil {
		return err
	}

	vaultPath := st.VaultPath()

	// Check if already running
	if daemon.IsRunning(vaultPath) {
		pid := daemon.GetRunningPID(vaultPath)
		fmt.Fprintf(os.Stderr, "daemon already running (pid=%d)\n", pid)
		os.Exit(1)
		return nil
	}

	// Create and run daemon
	d := daemon.New(vaultPath, st)
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
