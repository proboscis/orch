package cli

import (
	"testing"

	"github.com/proboscis/orch/internal/worker"
)

// ADR-0002 client-side ensure: run-dispatching commands against a remote
// master idempotently start the local managed worker for that master.
func TestEnsureLocalWorkerForRemoteMaster(t *testing.T) {
	var calls []string
	orig := startManagedWorkerFn
	defer func() { startManagedWorkerFn = orig }()
	startManagedWorkerFn = func(opts worker.ManagedOptions) (*worker.ManagedStartResult, error) {
		calls = append(calls, opts.RemoteAddr)
		return &worker.ManagedStartResult{OK: true, WorkerID: "host-test", Reused: true}, nil
	}

	ensureLocalWorkerForRemoteMaster("")
	if len(calls) != 0 {
		t.Fatalf("ensure with no remote called StartManaged %d times, want 0", len(calls))
	}

	ensureLocalWorkerForRemoteMaster("master.example:7777")
	if len(calls) != 1 || calls[0] != "master.example:7777" {
		t.Fatalf("ensure calls = %v, want exactly [master.example:7777]", calls)
	}
}

func TestEnsureLocalWorkerForRemoteMasterDisabledByEnv(t *testing.T) {
	t.Setenv("ORCH_WORKER_AUTOSTART", "0")

	var calls int
	orig := startManagedWorkerFn
	defer func() { startManagedWorkerFn = orig }()
	startManagedWorkerFn = func(opts worker.ManagedOptions) (*worker.ManagedStartResult, error) {
		calls++
		return &worker.ManagedStartResult{OK: true}, nil
	}

	ensureLocalWorkerForRemoteMaster("master.example:7777")
	if calls != 0 {
		t.Fatalf("ensure ran %d times with ORCH_WORKER_AUTOSTART=0, want 0", calls)
	}
}
