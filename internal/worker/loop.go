package worker

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/store"
	filestore "github.com/s22625/orch/internal/store/file"
)

var currentWorkerHostname = os.Hostname

type Client interface {
	RegisterWorker(workerID, workerType, host, mode string) (*daemon.RegisterWorkerResponse, error)
	UnregisterWorker(workerID string) error
	WorkerHeartbeat(workerID string) error
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
	ExecuteWorkerLease(lease *daemon.WorkerLease) (*daemon.WorkerEffectResult, error)
}

var newLeaseExecutor = func() leaseExecutor {
	return daemon.NewSocketServer(func(issuesRoot string) (store.Store, error) {
		return filestore.New(issuesRoot)
	}, log.New(io.Discard, "", 0))
}

func RunExternalLoop(client Client, cfg RunConfig) error {
	if client == nil {
		return fmt.Errorf("client required")
	}

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
		workerID = daemonDefaultWorkerID(host)
	}

	if capClient, ok := client.(capabilityRegisterClient); ok {
		if _, err := capClient.RegisterWorkerWithCapabilities(workerID, "executor", host, "external", []string{"start_run", "continue_run", "stop_run"}); err != nil {
			return err
		}
	} else if _, err := client.RegisterWorker(workerID, "executor", host, "external"); err != nil {
		return err
	}
	defer client.UnregisterWorker(workerID)

	executor := newLeaseExecutor()

	heartbeatTicker := time.NewTicker(heartbeatInterval)
	pollTicker := time.NewTicker(pollInterval)
	defer heartbeatTicker.Stop()
	defer pollTicker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-sigCh:
			return nil
		case <-heartbeatTicker.C:
			if err := client.WorkerHeartbeat(workerID); err != nil {
				return err
			}
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

func daemonDefaultWorkerID(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}

	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	idHost := strings.Trim(b.String(), "-")
	if idHost == "" {
		idHost = "localhost"
	}
	return "host-" + idHost
}
