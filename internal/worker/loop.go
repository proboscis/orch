package worker

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
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

const (
	defaultReconnectMinBackoff = 1 * time.Second
	defaultReconnectMaxBackoff = 60 * time.Second
)

type RunConfig struct {
	WorkerID          string
	Once              bool
	PollInterval      time.Duration
	HeartbeatInterval time.Duration

	// Test seams. The reconnect backoff window defaults to 1s..60s; stopCh
	// lets tests stop a long-lived loop without process signals.
	reconnectMinBackoff time.Duration
	reconnectMaxBackoff time.Duration
	stopCh              <-chan struct{}
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

// fatalWorkerError marks a session failure that reconnecting can never fix:
// the master was reached and explicitly refused registration (auth/policy).
type fatalWorkerError struct {
	err error
}

func (e *fatalWorkerError) Error() string { return e.err.Error() }
func (e *fatalWorkerError) Unwrap() error { return e.err }

// isFatalWorkerError separates fatal, non-retryable session failures (invalid
// client config, register-time rejection by a live master) from transient
// master-connection failures, which the run loop retries indefinitely.
func isFatalWorkerError(err error) bool {
	var cfgErr *daemon.ClientConfigError
	if errors.As(err, &cfgErr) {
		return true
	}
	var fatal *fatalWorkerError
	return errors.As(err, &fatal)
}

// classifyRegisterError decides whether a failed registration attempt should
// stop the worker. A rejection from a live master (bad auth token, policy)
// cannot be fixed by retrying; a transport failure means the master is
// unreachable and the loop should keep reconnecting.
func classifyRegisterError(err error) error {
	var rejected *daemon.WorkerRPCRejectedError
	if errors.As(err, &rejected) {
		return &fatalWorkerError{err: err}
	}
	return err
}

// reconnectJitterDelay returns a delay in [backoff/2, backoff) so that
// workers disconnected by the same outage do not reconnect in lockstep.
func reconnectJitterDelay(backoff time.Duration) time.Duration {
	half := backoff / 2
	if half <= 0 {
		return backoff
	}
	return half + time.Duration(rand.Int63n(int64(half)))
}

// RunExternalLoop runs the long-lived worker host loop. Master-connection
// failures are treated as retryable: the loop re-registers with exponential
// backoff plus jitter, indefinitely. It exits only on an explicit stop
// (signal), on fatal non-retryable errors (invalid client config, the master
// rejecting registration), or after one lease in Once mode, which keeps
// fail-fast semantics as a bounded one-shot probe.
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

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.reconnectMinBackoff <= 0 {
		cfg.reconnectMinBackoff = defaultReconnectMinBackoff
	}
	if cfg.reconnectMaxBackoff <= 0 {
		cfg.reconnectMaxBackoff = defaultReconnectMaxBackoff
	}
	if cfg.reconnectMaxBackoff < cfg.reconnectMinBackoff {
		cfg.reconnectMaxBackoff = cfg.reconnectMinBackoff
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

	// The executor outlives reconnect cycles: a master outage must not tear
	// down worker-local execution state.
	executor := newLeaseExecutor(workerID, host)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	registered := false
	defer func() {
		if registered {
			_ = client.UnregisterWorker(workerID)
		}
	}()

	backoff := cfg.reconnectMinBackoff
	resuming := false
	for {
		sessionStart := time.Now()
		sessionErr := runWorkerSession(client, cfg, workerID, host, executor, runtimeState, sigCh, resuming, &registered)
		if sessionErr == nil {
			return nil
		}
		if cfg.Once {
			return sessionErr
		}
		if isFatalWorkerError(sessionErr) {
			return sessionErr
		}

		// A session that stayed healthy well past the backoff cap starts a
		// fresh backoff ramp; consecutive quick failures keep escalating.
		if time.Since(sessionStart) >= 2*cfg.reconnectMaxBackoff {
			backoff = cfg.reconnectMinBackoff
		}
		delay := reconnectJitterDelay(backoff)
		if runtimeState != nil {
			runtimeState.markReconnecting(sessionErr)
		}
		log.Printf("orch-worker %s: master connection failed: %v; reconnecting in %s", workerID, sessionErr, delay.Round(time.Millisecond))
		select {
		case <-sigCh:
			return nil
		case <-cfg.stopCh:
			return nil
		case <-time.After(delay):
		}
		backoff *= 2
		if backoff > cfg.reconnectMaxBackoff {
			backoff = cfg.reconnectMaxBackoff
		}
		resuming = true
	}
}

// runWorkerSession registers the worker and serves heartbeats and lease polls
// until the connection to the master fails, a signal arrives, or (in Once
// mode) one lease completes. It returns nil for clean exits and the causing
// error otherwise; the caller decides between reconnecting and exiting.
func runWorkerSession(client Client, cfg RunConfig, workerID, host string, executor leaseExecutor, runtimeState *managedRuntimeStateWriter, sigCh <-chan os.Signal, resuming bool, registered *bool) error {
	if capClient, ok := client.(capabilityRegisterClient); ok {
		if _, err := capClient.RegisterWorkerWithCapabilities(workerID, "executor", host, "external", []string{"capture_session", "continue_run", "get_branch_state", "get_diff", "get_diff_stats", "send_message", "start_run", "stop_run"}); err != nil {
			return classifyRegisterError(err)
		}
	} else if _, err := client.RegisterWorker(workerID, "executor", host, "external"); err != nil {
		return classifyRegisterError(err)
	}
	*registered = true
	if runtimeState != nil {
		runtimeState.markRegistered()
	}
	if resuming {
		log.Printf("orch-worker %s: reconnected to master and re-registered", workerID)
	}

	// Heartbeats run on their own goroutine so a long-running effect
	// execution cannot starve them: a worker that stops heartbeating while
	// busy looks dead to the master, which then refuses new leases with
	// "no active workers available" and fails the lease the worker is still
	// faithfully executing. The proto client serializes concurrent requests
	// internally. On failure the goroutine reports but keeps ticking: while
	// an effect executes the poll loop cannot act on the report, and the
	// master must see heartbeats resume the moment the network recovers.
	heartbeatStop := make(chan struct{})
	heartbeatErrCh := make(chan error, 1)
	var heartbeatDone sync.WaitGroup
	heartbeatDone.Add(1)
	go func() {
		defer heartbeatDone.Done()
		ticker := time.NewTicker(cfg.HeartbeatInterval)
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
					continue
				}
				// Recovered without the poll loop noticing: withdraw any
				// stale failure report so a healthy session is not torn down.
				select {
				case <-heartbeatErrCh:
				default:
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

	pollTicker := time.NewTicker(cfg.PollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-sigCh:
			return nil
		case <-cfg.stopCh:
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
				// The lease result is not carried across sessions: on ack
				// loss the master re-dispatches the lease after its TTL and
				// completion stays idempotent (first verdict wins).
				return ackErr
			}

			if cfg.Once {
				return nil
			}
		}
	}
}

func daemonDefaultWorkerID(host string) string { return daemon.HostWorkerID(host) }
