package worker

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	"github.com/proboscis/orch/internal/store"
	filestore "github.com/proboscis/orch/internal/store/file"
)

var currentWorkerHostname = os.Hostname

type Client interface {
	RegisterWorker(workerID, workerType, host, mode string) (*daemon.RegisterWorkerResponse, error)
	UnregisterWorker(workerID string) error
	WorkerHeartbeat(workerID string) error
	// LeaseWork/AcknowledgeEffect are compatibility RPC names for host work assignments.
	LeaseWork(workerID string) (*daemon.LeaseWorkResponse, error)
	AcknowledgeEffect(workerID, leaseID string, success bool, effectErr, resultJSON string) error
	Close() error
}

type capabilityRegisterClient interface {
	RegisterWorkerWithCapabilities(workerID, workerType, host, mode string, capabilities []string) (*daemon.RegisterWorkerResponse, error)
}

type RunConfig struct {
	WorkerID          string
	Once              bool
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
}

type leaseExecutor interface {
	// ExecuteWorkerLease executes one host work assignment. The type name is compatibility-only.
	ExecuteWorkerLease(lease *daemon.WorkerLease) (*daemon.WorkerEffectResult, error)
}

var newLeaseExecutor = func(workerID, host string) leaseExecutor {
	server := daemon.NewSocketServer(func(issuesRoot string) (store.Store, error) {
		return filestore.New(issuesRoot)
	}, log.New(io.Discard, "", 0))
	server.SetWorkerIdentity(workerID, host)
	return server
}

func RunExternalLoop(client Client, cfg RunConfig) (err error) {
	if client == nil {
		return fmt.Errorf("client required")
	}

	runtimeState := managedRuntimeStateFromEnv()
	defer func() {
		if runtimeState != nil {
			runtimeState.markExited(err)
		}
	}()

	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	heartbeatInterval := cfg.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 5 * time.Second
	}

	host, _ := currentWorkerHostname()
	if host == "" {
		host = "localhost"
	}
	workerID := cfg.WorkerID
	if workerID == "" {
		workerID = daemon.HostWorkerID(host)
	}
	if runtimeState != nil {
		runtimeState.markStarting(workerID)
	}

	if capClient, ok := client.(capabilityRegisterClient); ok {
		if _, err := capClient.RegisterWorkerWithCapabilities(workerID, "executor", host, "external", []string{"capture_session", "continue_run", "get_branch_state", "get_diff", "get_diff_stats", "send_message", "start_run", "stop_run"}); err != nil {
			return err
		}
	} else if _, err := client.RegisterWorker(workerID, "executor", host, "external"); err != nil {
		return err
	}
	if runtimeState != nil {
		runtimeState.markRegistered()
	}
	defer client.UnregisterWorker(workerID)

	executor := newLeaseExecutor(workerID, host)

	// Heartbeats run on their own goroutine so a long-running effect
	// execution cannot starve them: a worker that stops heartbeating while
	// busy looks dead to the master, which then refuses new leases with
	// "no active workers available" and fails the lease the worker is still
	// faithfully executing. The proto client serializes concurrent requests
	// internally.
	heartbeatStop := make(chan struct{})
	heartbeatErrCh := make(chan error, 1)
	var heartbeatDone sync.WaitGroup
	heartbeatDone.Add(1)
	go func() {
		defer heartbeatDone.Done()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatStop:
				return
			case <-ticker.C:
				if err := client.WorkerHeartbeat(workerID); err != nil {
					select {
					case heartbeatErrCh <- err:
					default:
					}
					return
				}
				if runtimeState != nil {
					runtimeState.markHeartbeat()
				}
			}
		}
	}()
	// LIFO: close the stop channel first, then wait for the goroutine to
	// finish so no heartbeat call races the caller after return.
	defer heartbeatDone.Wait()
	defer close(heartbeatStop)

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-sigCh:
			return nil
		case err := <-heartbeatErrCh:
			return err
		case <-pollTicker.C:
			leaseResp, err := client.LeaseWork(workerID)
			if err != nil {
				return err
			}
			if leaseResp == nil || leaseResp.Lease == nil || leaseResp.Lease.LeaseID == "" {
				if cfg.Once {
					return nil
				}
				continue
			}

			result, execErr := executor.ExecuteWorkerLease(leaseResp.Lease)
			errMsg := ""
			if execErr != nil {
				errMsg = execErr.Error()
			}
			resultJSON := daemon.EncodeWorkerEffectResult(result)
			if ackErr := client.AcknowledgeEffect(workerID, leaseResp.Lease.LeaseID, execErr == nil, errMsg, resultJSON); ackErr != nil {
				return ackErr
			}

			if cfg.Once {
				return nil
			}
		}
	}
}

func daemonDefaultWorkerID(host string) string { return daemon.HostWorkerID(host) }
