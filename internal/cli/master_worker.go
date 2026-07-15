package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/worker"
	"github.com/spf13/cobra"
)

var requireDaemonForWorker = func() (worker.Client, error) {
	return requireDaemon()
}
var runExternalWorkerLoop = worker.RunExternalLoop
var logWorkerAgentAvailability = worker.LogAgentAvailability

func newMasterCmd() *cobra.Command {
	cmd := newDaemonCmd()
	cmd.Use = "master"
	cmd.Short = "Manage orch-master control plane"
	cmd.Hidden = true
	cmd.Long = `Manage orch-master control plane.

The canonical command name is 'orch daemon'. 'orch master' is kept as a hidden
compatibility alias for existing deployments.`
	return cmd
}

func newWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage orch-worker execution plane",
		Long: `Manage orch-worker execution plane.

Workers run as long-lived host managers and execute work assigned by
orch-master via worker protocol APIs. Single-host mode is implemented as
co-located daemon+worker with the same semantics as distributed mode.`,
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
		Short: "Run long-lived orch-worker host loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			worker.ScrubInheritedMultiplexerEnv()
			// Probe availability concurrently with registration below: the
			// probe result carries no data the register RPC depends on, and
			// a probe can block for up to its own timeout, so running it
			// ahead of registration would let one hung agent CLI consume the
			// caller's entire ready-wait budget before registration is even
			// attempted (see worker-start-ready-wait-eaten-by-probe-timeout).
			go logWorkerAgentAvailability(workerID)

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
	var workerID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show orch-worker status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkerStatus(workerID)
		},
	}
	cmd.Flags().StringVar(&workerID, "worker-id", "", "worker id to inspect (default: local host worker)")
	return cmd
}

func runWorkerStatus(workerID string) error {
	status, err := worker.StatusManaged(worker.ManagedOptions{
		WorkerID:   workerID,
		RemoteAddr: getRemoteAddr(),
	})
	if err != nil {
		return err
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	if globalOpts.Quiet {
		return nil
	}
	fmt.Printf("Worker ID: %s\n", status.WorkerID)
	fmt.Printf("Profile: %s\n", status.Profile)
	if status.Local.Managed {
		if status.Local.ProcessExists {
			fmt.Printf("Local Process: %s (pid: %d)\n", status.Local.State, status.Local.PID)
		} else if status.Local.PID > 0 {
			fmt.Printf("Local Process: %s (last pid: %d)\n", status.Local.State, status.Local.PID)
		} else {
			fmt.Printf("Local Process: %s\n", status.Local.State)
		}
		if status.Local.LogPath != "" {
			fmt.Printf("Log: %s\n", status.Local.LogPath)
		}
		if !status.Local.RegisteredAt.IsZero() {
			fmt.Printf("Registered: %s\n", status.Local.RegisteredAt.Format(time.RFC3339))
		}
		if !status.Local.LastHeartbeatAt.IsZero() {
			fmt.Printf("Last Heartbeat: %s\n", humanizeWorkerTime(status.Local.LastHeartbeatAt))
		}
		if !status.Local.ReconnectingSince.IsZero() {
			fmt.Printf("Reconnecting Since: %s\n", status.Local.ReconnectingSince.Format(time.RFC3339))
		}
		if !status.Local.ExitedAt.IsZero() {
			fmt.Printf("Exited: %s\n", status.Local.ExitedAt.Format(time.RFC3339))
		}
		if status.Local.LastError != "" {
			fmt.Printf("Last Error: %s\n", status.Local.LastError)
		}
	} else {
		fmt.Println("Local Process: missing")
	}

	switch status.Master.State {
	case "active", "stale":
		fmt.Printf("Master Registration: %s\n", status.Master.State)
		if status.Master.Registration != nil {
			fmt.Printf("Master Host: %s\n", status.Master.Registration.Host)
			if !status.Master.Registration.LastHeartbeat.IsZero() {
				fmt.Printf("Master Heartbeat: %s\n", humanizeWorkerTime(status.Master.Registration.LastHeartbeat))
			}
		}
	case "not_registered":
		fmt.Println("Master Registration: not registered")
	case "unreachable":
		fmt.Printf("Master Registration: unreachable (%s)\n", status.Master.Error)
	default:
		fmt.Printf("Master Registration: %s\n", status.Master.State)
	}
	if status.Diagnostic != "" {
		fmt.Printf("Diagnostic: %s\n", status.Diagnostic)
	}
	return nil
}

func newWorkerStartCmd() *cobra.Command {
	var workerID string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start managed orch-worker host process",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(getRemoteAddr()) == "" {
				if err := ensureLocalWorkerMaster(); err != nil {
					return err
				}
			}

			resp, err := worker.StartManaged(worker.ManagedOptions{
				WorkerID:   workerID,
				RemoteAddr: getRemoteAddr(),
			})
			if err != nil {
				return err
			}

			if globalOpts.JSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if !globalOpts.Quiet {
				if resp.Reused {
					fmt.Printf("Worker already running: %s (pid: %d)\n", resp.WorkerID, resp.PID)
				} else {
					fmt.Printf("Started worker: %s (pid: %d)\n", resp.WorkerID, resp.PID)
				}
				if len(resp.OrphanPIDs) > 0 {
					fmt.Printf("Stopped orphan worker processes claiming %s: %s\n", resp.WorkerID, formatWorkerPIDs(resp.OrphanPIDs))
				}
				if resp.LogPath != "" {
					fmt.Printf("Log: %s\n", resp.LogPath)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workerID, "worker-id", "", "worker id to start (default: local host worker)")
	return cmd
}

func newWorkerStopCmd() *cobra.Command {
	var workerID string
	var stopAll bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop managed orch-worker host process",
		Long: `Stop managed orch-worker host process.

Stopping reconciles the worker-id invariant: besides the supervised process
recorded in the local state file, any other process on this host claiming the
same worker id (started manually, by an older binary, or with a different
--remote connection string) is stopped as well, so no orphan keeps contending
for the worker registration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := worker.StopManaged(worker.ManagedOptions{
				WorkerID:   workerID,
				RemoteAddr: getRemoteAddr(),
			}, stopAll)
			if err != nil {
				return err
			}

			if globalOpts.JSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if !globalOpts.Quiet {
				fmt.Printf("Stopped workers: %d\n", resp.StoppedCount)
				if len(resp.OrphanPIDs) > 0 {
					fmt.Printf("Stopped orphan worker processes: %s\n", formatWorkerPIDs(resp.OrphanPIDs))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workerID, "worker-id", "", "worker id to stop (default: local host worker)")
	cmd.Flags().BoolVar(&stopAll, "all", false, "stop all workers on this host (managed workers plus any orphan orch worker processes)")
	return cmd
}

func formatWorkerPIDs(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, fmt.Sprintf("%d", pid))
	}
	return strings.Join(parts, ", ")
}

func humanizeWorkerTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	now := time.Now()
	if now.Sub(ts) < 0 {
		return "just now"
	}
	return formatTimeAgo(ts)
}

func ensureLocalWorkerMaster() error {
	client, err := requireDaemonForWorker()
	if err != nil {
		return fmt.Errorf("orch-master is not running locally and failed to start: %w", err)
	}
	defer client.Close()
	return nil
}
