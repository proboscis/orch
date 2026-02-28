package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newMasterCmd() *cobra.Command {
	cmd := newDaemonCmd()
	cmd.Use = "master"
	cmd.Short = "Manage orch-master control plane"
	cmd.Long = `Manage orch-master control plane.

This command is the preferred cluster terminology. It is behaviorally equivalent
to 'orch daemon' during compatibility period.`
	return cmd
}

func newWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage orch-worker execution plane",
		Long: `Manage orch-worker execution plane.

Current implementation runs worker co-located with orch-master on the same
host. Separate worker process mode will be added in later cluster phases.`,
	}

	cmd.AddCommand(newWorkerStatusCmd())
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())

	return cmd
}

func newWorkerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show orch-worker status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkerStatus()
		},
	}
}

func runWorkerStatus() error {
	return runDaemonStatus()
}

func newWorkerStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start orch-worker (embedded mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runDaemonStart(); err != nil {
				return err
			}
			fmt.Println("Worker mode: embedded with orch-master")
			return nil
		},
	}
}

func newWorkerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop orch-worker (embedded mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runDaemonKill(&daemonKillOptions{}); err != nil {
				return err
			}
			return nil
		},
	}
}
