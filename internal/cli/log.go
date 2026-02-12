package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type logDeps struct {
	getAPI func() (interface {
		GetDaemonStatus(context.Context) (*daemonStatus, error)
		GetDaemonLog(context.Context, int) (string, error)
	}, error)
	newCommand func(string, ...string) *exec.Cmd
	stdout     io.Writer
	stderr     io.Writer
}

type daemonStatus struct {
	LogPath string
}

type logAPIAdapter struct {
	api interface {
		GetDaemonStatus(context.Context) (*orchapi.DaemonStatus, error)
		GetDaemonLog(context.Context, int) (string, error)
	}
}

func (a logAPIAdapter) GetDaemonStatus(ctx context.Context) (*daemonStatus, error) {
	status, err := a.api.GetDaemonStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &daemonStatus{LogPath: status.LogPath}, nil
}

func (a logAPIAdapter) GetDaemonLog(ctx context.Context, lines int) (string, error) {
	return a.api.GetDaemonLog(ctx, lines)
}

func defaultLogDeps() *logDeps {
	return &logDeps{
		getAPI: func() (interface {
			GetDaemonStatus(context.Context) (*daemonStatus, error)
			GetDaemonLog(context.Context, int) (string, error)
		}, error) {
			api, err := getAPI()
			if err != nil {
				return nil, err
			}
			return logAPIAdapter{api: api}, nil
		},
		newCommand: exec.Command,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	}
}

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
			deps := defaultLogDeps()
			ctx := context.Background()
			api, err := deps.getAPI()
			if err != nil {
				return err
			}

			if follow {
				daemonStatus, err := api.GetDaemonStatus(ctx)
				if err != nil {
					return fmt.Errorf("failed to get daemon status: %w", err)
				}
				tailCmd := deps.newCommand("tail", "-f", "-n", fmt.Sprintf("%d", lines), daemonStatus.LogPath)
				tailCmd.Stdout = deps.stdout
				tailCmd.Stderr = deps.stderr
				return tailCmd.Run()
			}

			content, err := api.GetDaemonLog(ctx, lines)
			if err != nil {
				return err
			}

			fmt.Fprint(deps.stdout, content)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show (with -f)")

	return cmd
}
