package worker

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/daemon"
)

type mockClient struct {
	mu           sync.Mutex
	heartbeats   int
	registered   bool
	regSuccesses int
	regAttempts  []time.Time
	acked        bool
	lease        *daemon.WorkerLease
	ackResult    string
	workerID     string
	workerType   string
	host         string
	mode         string
	regErr       error
	hbErr        error
	leaseErr     error
	ackErr       error
	unregErr     error
	unregCalled  bool
}

type mockCapClient struct {
	mockClient
	capabilities []string
}

func (m *mockCapClient) RegisterWorkerWithCapabilities(workerID, workerType, host, mode string, capabilities []string) (*daemon.RegisterWorkerResponse, error) {
	m.capabilities = append([]string(nil), capabilities...)
	return m.RegisterWorker(workerID, workerType, host, mode)
}

func (m *mockClient) RegisterWorker(workerID, workerType, host, mode string) (*daemon.RegisterWorkerResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.regAttempts = append(m.regAttempts, time.Now())
	if m.regErr != nil {
		return nil, m.regErr
	}
	m.workerID = workerID
	m.workerType = workerType
	m.host = host
	m.mode = mode
	m.registered = true
	m.regSuccesses++
	return &daemon.RegisterWorkerResponse{OK: true, WorkerID: workerID}, nil
}

func (m *mockClient) UnregisterWorker(workerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unregCalled = true
	return m.unregErr
}
func (m *mockClient) WorkerHeartbeat(workerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeats++
	return m.hbErr
}

func (m *mockClient) heartbeatCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heartbeats
}

func (m *mockClient) registerAttempts() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Time(nil), m.regAttempts...)
}

func (m *mockClient) registerSuccessCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.regSuccesses
}

func (m *mockClient) unregisterCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unregCalled
}

// setConnectivity flips all connection-shaped RPCs between healthy (nil) and
// failing with the given error, simulating a master outage and recovery.
func (m *mockClient) setConnectivity(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.regErr = err
	m.hbErr = err
	m.leaseErr = err
}

func (m *mockClient) setHeartbeatErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hbErr = err
}

func (m *mockClient) LeaseWork(workerID string) (*daemon.LeaseWorkResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leaseErr != nil {
		return nil, m.leaseErr
	}
	if m.lease == nil {
		return &daemon.LeaseWorkResponse{OK: true}, nil
	}
	lease := m.lease
	m.lease = nil
	return &daemon.LeaseWorkResponse{OK: true, Lease: lease}, nil
}

func (m *mockClient) AcknowledgeEffect(workerID, leaseID string, success bool, effectErr, resultJSON string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ackErr != nil {
		return m.ackErr
	}
	m.acked = true
	m.ackResult = resultJSON
	return nil
}

func (m *mockClient) Close() error { return nil }

type mockExecutor struct{}

func (m *mockExecutor) ExecuteWorkerLease(lease *daemon.WorkerLease) (*daemon.WorkerEffectResult, error) {
	return &daemon.WorkerEffectResult{StartRunResult: &daemon.StartRunResult{RunID: "run-1"}}, nil
}

func TestRunExternalLoopOnceProcessesLease(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{lease: &daemon.WorkerLease{LeaseID: "lease-1", WorkerID: "w1", Effect: "start_run", Payload: &daemon.WorkerEffectPayload{StartRun: &daemon.StartRunOptions{}}}}
	err := RunExternalLoop(client, RunConfig{WorkerID: "w1", Once: true, PollInterval: 10 * time.Millisecond, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatalf("RunExternalLoop() error = %v", err)
	}
	if !client.registered {
		t.Fatal("expected worker registration")
	}
	if !client.acked {
		t.Fatal("expected lease acknowledgement")
	}
	if client.ackResult == "" {
		t.Fatal("expected non-empty result_json on acknowledgement")
	}
}

func TestRunExternalLoopOnceNoLeaseReturnsNil(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{}
	err := RunExternalLoop(client, RunConfig{WorkerID: "w2", Once: true, PollInterval: 10 * time.Millisecond, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatalf("RunExternalLoop() error = %v", err)
	}
	if !client.registered {
		t.Fatal("expected worker registration")
	}
	if client.acked {
		t.Fatal("expected no acknowledgement when no lease was polled")
	}
}

// Once mode is a bounded one-shot probe and keeps fail-fast semantics: no
// reconnect loop, the first error is returned as-is.
func TestRunExternalLoopRegisterFailure(t *testing.T) {
	client := &mockClient{regErr: assertErr("register failed")}
	err := RunExternalLoop(client, RunConfig{WorkerID: "w3", Once: true, PollInterval: 10 * time.Millisecond, HeartbeatInterval: time.Second})
	if err == nil || err.Error() != "register failed" {
		t.Fatalf("RunExternalLoop() error = %v, want register failed", err)
	}
	if client.unregCalled {
		t.Fatal("unexpected unregister call when register fails")
	}
}

// A live master explicitly rejecting registration (auth/policy) is fatal:
// retrying cannot help, so the long-lived loop must exit with the error
// instead of reconnecting forever.
func TestRunExternalLoopExitsOnRegisterRejection(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{regErr: &daemon.WorkerRPCRejectedError{Message: "unauthorized worker"}}
	done := make(chan error, 1)
	go func() {
		done <- RunExternalLoop(client, RunConfig{
			WorkerID:            "w-auth",
			PollInterval:        5 * time.Millisecond,
			HeartbeatInterval:   5 * time.Millisecond,
			reconnectMinBackoff: 5 * time.Millisecond,
			reconnectMaxBackoff: 20 * time.Millisecond,
		})
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "unauthorized worker") {
			t.Fatalf("RunExternalLoop() error = %v, want unauthorized worker", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit on register rejection")
	}
	if client.unregisterCalled() {
		t.Fatal("unexpected unregister call when registration was rejected")
	}
}

// Invalid client configuration can never be fixed by reconnecting and must
// exit immediately.
func TestRunExternalLoopExitsOnClientConfigError(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{regErr: &daemon.ClientConfigError{Message: `unsupported daemon address scheme: "grpc://remotebox"`}}
	done := make(chan error, 1)
	go func() {
		done <- RunExternalLoop(client, RunConfig{
			WorkerID:            "w-cfg",
			PollInterval:        5 * time.Millisecond,
			HeartbeatInterval:   5 * time.Millisecond,
			reconnectMinBackoff: 5 * time.Millisecond,
			reconnectMaxBackoff: 20 * time.Millisecond,
		})
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "unsupported daemon address scheme") {
			t.Fatalf("RunExternalLoop() error = %v, want config error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit on client config error")
	}
}

// The regression test for the 2026-07-07 incident: a transient master outage
// (every RPC failing with a connection error) must not kill the worker. The
// loop has to survive N consecutive connection failures, back off between
// attempts, re-register once the master is reachable again, and resume
// heartbeats — all without operator action.
func TestRunExternalLoopSurvivesOutageAndReconnects(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{}
	stopCh := make(chan struct{})
	minBackoff := 10 * time.Millisecond
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- RunExternalLoop(client, RunConfig{
			WorkerID:            "w-flaky",
			PollInterval:        2 * time.Millisecond,
			HeartbeatInterval:   2 * time.Millisecond,
			reconnectMinBackoff: minBackoff,
			reconnectMaxBackoff: 8 * minBackoff,
			stopCh:              stopCh,
		})
	}()

	waitFor := func(cond func() bool, msg string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			select {
			case err := <-loopDone:
				t.Fatalf("loop exited early (%s): %v", msg, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timeout waiting for %s", msg)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}

	waitFor(func() bool { return client.registerSuccessCount() >= 1 }, "initial registration")
	waitFor(func() bool { return client.heartbeatCount() >= 1 }, "initial heartbeat")

	// Master outage: every RPC fails the way a dropped TCP connection does.
	connErr := assertErr("daemon request failed: failed to read response length: read tcp: i/o timeout (reconnect failed: dial tcp remotebox:7777: i/o timeout)")
	client.setConnectivity(connErr)
	attemptsAtOutage := len(client.registerAttempts())

	// Survive at least 3 consecutive failed reconnect attempts.
	waitFor(func() bool { return len(client.registerAttempts()) >= attemptsAtOutage+3 }, "3 failed reconnect attempts")

	// Exponential backoff: each retry sleeps a jittered delay in
	// [backoff/2, backoff), so the gaps between consecutive attempts have
	// deterministic lower bounds (min/2, then min after doubling).
	attempts := client.registerAttempts()
	gap1 := attempts[attemptsAtOutage+1].Sub(attempts[attemptsAtOutage])
	gap2 := attempts[attemptsAtOutage+2].Sub(attempts[attemptsAtOutage+1])
	if gap1 < minBackoff/2 {
		t.Fatalf("first reconnect gap %v below backoff floor %v", gap1, minBackoff/2)
	}
	if gap2 < minBackoff {
		t.Fatalf("second reconnect gap %v below doubled backoff floor %v", gap2, minBackoff)
	}

	// Master comes back: the worker must re-register and resume heartbeats
	// without operator action.
	client.setConnectivity(nil)
	waitFor(func() bool { return client.registerSuccessCount() >= 2 }, "re-registration after outage")
	heartbeatsAtRecovery := client.heartbeatCount()
	waitFor(func() bool { return client.heartbeatCount() >= heartbeatsAtRecovery+3 }, "heartbeats resumed after reconnect")

	close(stopCh)
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("RunExternalLoop() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop")
	}
	if !client.unregisterCalled() {
		t.Fatal("expected unregister on clean shutdown")
	}
}

// Heartbeat failures alone (e.g. the master restarted and forgot the
// registration) must also route through the reconnect path: tear down the
// session, re-register, and resume — never exit.
func TestRunExternalLoopReregistersAfterHeartbeatFailures(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{}
	stopCh := make(chan struct{})
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- RunExternalLoop(client, RunConfig{
			WorkerID:            "w-hb",
			PollInterval:        2 * time.Millisecond,
			HeartbeatInterval:   2 * time.Millisecond,
			reconnectMinBackoff: 5 * time.Millisecond,
			reconnectMaxBackoff: 20 * time.Millisecond,
			stopCh:              stopCh,
		})
	}()

	waitFor := func(cond func() bool, msg string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			select {
			case err := <-loopDone:
				t.Fatalf("loop exited early (%s): %v", msg, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timeout waiting for %s", msg)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}

	waitFor(func() bool { return client.registerSuccessCount() >= 1 }, "initial registration")
	client.setHeartbeatErr(&daemon.WorkerRPCRejectedError{Message: "worker not found: w-hb"})
	waitFor(func() bool { return client.registerSuccessCount() >= 2 }, "re-registration after heartbeat failure")
	client.setHeartbeatErr(nil)

	heartbeatsAtRecovery := client.heartbeatCount()
	waitFor(func() bool { return client.heartbeatCount() >= heartbeatsAtRecovery+3 }, "heartbeats resumed")

	close(stopCh)
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("RunExternalLoop() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop")
	}
}

func TestRunExternalLoopRegistersCapabilitiesWhenSupported(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockCapClient{}
	err := RunExternalLoop(client, RunConfig{WorkerID: "w-cap", Once: true, PollInterval: 10 * time.Millisecond, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatalf("RunExternalLoop() error = %v", err)
	}
	if len(client.capabilities) == 0 {
		t.Fatal("expected capabilities to be sent during register")
	}
}

func TestRunExternalLoopDefaultsWorkerIDToStableHostIdentity(t *testing.T) {
	origNew := newLeaseExecutor
	origHost := currentWorkerHostname
	t.Cleanup(func() {
		newLeaseExecutor = origNew
		currentWorkerHostname = origHost
	})
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }
	currentWorkerHostname = func() (string, error) { return "remotebox", nil }

	client := &mockClient{}
	err := RunExternalLoop(client, RunConfig{Once: true, PollInterval: 10 * time.Millisecond, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatalf("RunExternalLoop() error = %v", err)
	}
	if !client.registered {
		t.Fatal("expected worker registration")
	}
	if client.workerID != "host-remotebox" {
		t.Fatalf("workerID = %q, want %q", client.workerID, "host-remotebox")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

// blockingExecutor blocks lease execution until released, simulating a
// long-running effect (e.g. a slow session launch).
type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingExecutor) ExecuteWorkerLease(lease *daemon.WorkerLease) (*daemon.WorkerEffectResult, error) {
	close(b.started)
	<-b.release
	return &daemon.WorkerEffectResult{}, nil
}

// Heartbeats must keep flowing while an effect executes: a worker that goes
// silent during a long lease looks dead to the master, which then refuses
// new leases ("no active workers available") and fails the very lease the
// worker is still executing.
func TestRunExternalLoopHeartbeatsDuringEffectExecution(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	blocking := &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
	newLeaseExecutor = func(string, string) leaseExecutor { return blocking }

	client := &mockClient{lease: &daemon.WorkerLease{LeaseID: "lease-1", WorkerID: "w1", Effect: "start_run", Payload: &daemon.WorkerEffectPayload{StartRun: &daemon.StartRunOptions{}}}}

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- RunExternalLoop(client, RunConfig{WorkerID: "w1", Once: true, PollInterval: 5 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond})
	}()

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("lease execution never started")
	}

	before := client.heartbeatCount()
	deadline := time.Now().Add(5 * time.Second)
	for client.heartbeatCount() < before+3 {
		if time.Now().After(deadline) {
			t.Fatalf("heartbeats stalled during effect execution: %d -> %d", before, client.heartbeatCount())
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(blocking.release)
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("RunExternalLoop() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not finish after effect release")
	}
}

// An in-flight effect must survive a heartbeat outage: the loop may not
// abandon or interrupt the execution, and the heartbeat goroutine must keep
// attempting (not exit on first failure) so the master sees the worker again
// the moment the network recovers.
func TestRunExternalLoopEffectSurvivesHeartbeatOutage(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	blocking := &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
	newLeaseExecutor = func(string, string) leaseExecutor { return blocking }

	client := &mockClient{lease: &daemon.WorkerLease{LeaseID: "lease-1", WorkerID: "w1", Effect: "start_run", Payload: &daemon.WorkerEffectPayload{StartRun: &daemon.StartRunOptions{}}}}

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- RunExternalLoop(client, RunConfig{WorkerID: "w1", Once: true, PollInterval: 5 * time.Millisecond, HeartbeatInterval: 5 * time.Millisecond})
	}()

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("lease execution never started")
	}

	// Heartbeats start failing mid-effect; attempts must continue anyway.
	client.setHeartbeatErr(assertErr("write tcp: broken pipe"))
	failingSince := client.heartbeatCount()
	deadline := time.Now().Add(5 * time.Second)
	for client.heartbeatCount() < failingSince+3 {
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat attempts stopped during outage: %d -> %d", failingSince, client.heartbeatCount())
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Network recovers before the effect completes; the effect then finishes
	// and is acknowledged as if nothing happened.
	client.setHeartbeatErr(nil)
	close(blocking.release)
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("RunExternalLoop() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not finish after effect release")
	}
	if !client.acked {
		t.Fatal("expected the in-flight lease to be acknowledged after the heartbeat outage")
	}
}
