package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/s22625/orch/internal/worker"
	"github.com/spf13/cobra"
)

var requireDaemonForWorker = requireDaemon
var runExternalWorkerLoop = worker.RunExternalLoop

func newMasterCmd() *cobra.Command {
	cmd := newDaemonCmd()
	cmd.Use = "master"
	cmd.Short = "Manage orch-master control plane"
	cmd.Long = `Manage orch-master control plane.

This command is the preferred cluster terminology. It is behaviorally equivalent
to 'orch daemon'.`
	return cmd
}

func newWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage orch-worker execution plane",
		Long: `Manage orch-worker execution plane.

Workers run as separate processes and execute leases from orch-master via
worker protocol APIs. Zeus single-host mode is implemented as co-located
external worker processes with the same semantics as distributed mode.`,
	}

	cmd.AddCommand(newWorkerStatusCmd())
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())
	cmd.AddCommand(newWorkerRunCmd())

	return cmd
}

func newWorkerRunCmd() *cobra.Command {
	var workerID string
	var once bool
	var pollInterval time.Duration
	var heartbeatInterval time.Duration

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run external orch-worker lease loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireDaemonForWorker()
			if err != nil {
				return err
			}
			defer client.Close()

			return runExternalWorkerLoop(client, worker.RunConfig{
				WorkerID:          workerID,
				Once:              once,
				PollInterval:      pollInterval,
				HeartbeatInterval: heartbeatInterval,
			})
		},
	}

	cmd.Flags().StringVar(&workerID, "worker-id", "", "worker id for registration")
	cmd.Flags().BoolVar(&once, "once", false, "process at most one lease before exiting")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 200*time.Millisecond, "lease poll interval")
	cmd.Flags().DurationVar(&heartbeatInterval, "heartbeat-interval", 5*time.Second, "worker heartbeat interval")

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
	client, err := requireDaemonForWorker()
	if err != nil {
		return err
	}
	defer client.Close()

	resp, err := client.ListWorkers()
	if err != nil {
		return err
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if len(resp.Workers) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No workers registered")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTYPE\tMODE\tHOST\tSTATUS\tLAST HEARTBEAT")
	now := time.Now()
	for _, worker := range resp.Workers {
		status := "stale"
		if worker.Active {
			status = "active"
		}

		lastHeartbeat := "-"
		if !worker.LastHeartbeat.IsZero() {
			if now.Sub(worker.LastHeartbeat) < 0 {
				lastHeartbeat = "just now"
			} else {
				lastHeartbeat = formatTimeAgo(worker.LastHeartbeat)
			}
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			worker.ID, worker.WorkerType, worker.Mode, worker.Host, status, lastHeartbeat)
	}
	w.Flush()

	return nil
}

func newWorkerStartCmd() *cobra.Command {
	var workerID string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start managed external orch-worker process",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runDaemonStart(); err != nil {
				return err
			}

			client, err := requireDaemonForWorker()
			if err != nil {
				return err
			}
			defer client.Close()

			resp, err := client.StartExternalWorker(workerID)
			if err != nil {
				return err
			}

			if globalOpts.JSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if !globalOpts.Quiet {
				fmt.Printf("Started external worker: %s (pid: %d)\n", resp.WorkerID, resp.PID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workerID, "worker-id", "", "worker id to start (auto-generated if empty)")
	return cmd
}

func newWorkerStopCmd() *cobra.Command {
	var workerID string
	var stopAll bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop managed external orch-worker process",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireDaemonForWorker()
			if err != nil {
				return err
			}
			defer client.Close()

			resp, err := client.StopExternalWorker(workerID, stopAll)
			if err != nil {
				return err
			}

			if globalOpts.JSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if !globalOpts.Quiet {
				fmt.Printf("Stopped external workers: %d\n", resp.StoppedCount)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workerID, "worker-id", "", "worker id to stop (default: local host worker)")
	cmd.Flags().BoolVar(&stopAll, "all", false, "stop all managed external workers")
	return cmd
}
