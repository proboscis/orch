package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/s22625/orch/internal/daemon"
	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "View orch logs",
	}

	cmd.AddCommand(newLogDaemonCmd())
	return cmd
}

func newLogDaemonCmd() *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "View daemon logs",
		Long: `View the background daemon logs.

The daemon monitors all running agent sessions and updates their status.
Use this command to debug monitoring issues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			issuesRoot, err := getIssuesRoot()
			if err != nil {
				return err
			}

			logPath := daemon.LogFilePath(issuesRoot)
			if _, err := os.Stat(logPath); os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "No daemon log found at: %s\n", logPath)
				return nil
			}

			if follow {
				tailCmd := exec.Command("tail", "-f", "-n", fmt.Sprintf("%d", lines), logPath)
				tailCmd.Stdout = os.Stdout
				tailCmd.Stderr = os.Stderr
				return tailCmd.Run()
			}

			f, err := os.Open(logPath)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(os.Stdout, f)
			return err
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show (with -f)")

	return cmd
}
