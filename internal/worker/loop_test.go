package worker

import (
	"sync"
	"testing"
	"time"

	"github.com/s22625/orch/internal/daemon"
)

type mockClient struct {
	mu          sync.Mutex
	heartbeats  int
	registered  bool
	acked       bool
	lease       *daemon.WorkerLease
	ackResult   string
	workerID    string
	workerType  string
	host        string
	mode        string
	regErr      error
	hbErr       error
	leaseErr    error
	ackErr      error
	unregErr    error
	unregCalled bool
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
	if m.regErr != nil {
		return nil, m.regErr
	}
	m.workerID = workerID
	m.workerType = workerType
	m.host = host
	m.mode = mode
	m.registered = true
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

func TestRunExternalLoopHeartbeatFailure(t *testing.T) {
	origNew := newLeaseExecutor
	t.Cleanup(func() { newLeaseExecutor = origNew })
	newLeaseExecutor = func(string, string) leaseExecutor { return &mockExecutor{} }

	client := &mockClient{hbErr: assertErr("heartbeat failed")}
	err := RunExternalLoop(client, RunConfig{WorkerID: "w4", PollInterval: 200 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond})
	if err == nil || err.Error() != "heartbeat failed" {
		t.Fatalf("RunExternalLoop() error = %v, want heartbeat failed", err)
	}
	if !client.unregCalled {
		t.Fatal("expected unregister call on heartbeat failure")
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
	currentWorkerHostname = func() (string, error) { return "zeus", nil }

	client := &mockClient{}
	err := RunExternalLoop(client, RunConfig{Once: true, PollInterval: 10 * time.Millisecond, HeartbeatInterval: time.Second})
	if err != nil {
		t.Fatalf("RunExternalLoop() error = %v", err)
	}
	if !client.registered {
		t.Fatal("expected worker registration")
	}
	if client.workerID != "host-zeus" {
		t.Fatalf("workerID = %q, want %q", client.workerID, "host-zeus")
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
