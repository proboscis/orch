package cli

import (
	"context"
	"fmt"
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
			projectRoot, err := getProjectRoot()
			if err != nil {
				return err
			}

			if follow {
				logPath := daemon.LogFilePath(projectRoot)
				tailCmd := exec.Command("tail", "-f", "-n", fmt.Sprintf("%d", lines), logPath)
				tailCmd.Stdout = os.Stdout
				tailCmd.Stderr = os.Stderr
				return tailCmd.Run()
			}

			ctx := context.Background()
			api, err := getAPI()
			if err != nil {
				return err
			}

			content, err := api.GetDaemonLog(ctx, lines)
			if err != nil {
				return err
			}

			fmt.Print(content)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show (with -f)")

	return cmd
}
