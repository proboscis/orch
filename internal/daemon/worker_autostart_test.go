package daemon

import (
	"fmt"
	"strings"
	"testing"

	orchpb "github.com/proboscis/orch/api/orchpb"
)

func registerColocatedTestWorker(server *SocketServer) {
	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     defaultWorkerID(),
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"start_run"},
	})
}

// ADR-0002: when no worker is active and the effect targets the master's own
// host, the master auto-starts a colocated worker and retries the lease once.
func TestAcquireLeaseAutostartsColocatedWorkerForLocalTarget(t *testing.T) {
	server := NewSocketServer(nil, &timingTestLogger{})
	spawned := 0
	server.spawnColocatedWorker = func() error {
		spawned++
		registerColocatedTestWorker(server)
		return nil
	}

	lease, err := server.acquireWorkerLeaseWithAutostart("project-test", "start_run", "iss-auto", "run-auto", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLeaseWithAutostart() error = %v, want lease via autostart", err)
	}
	if spawned != 1 {
		t.Fatalf("spawn count = %d, want 1", spawned)
	}
	if lease.WorkerID != defaultWorkerID() {
		t.Fatalf("lease.WorkerID = %s, want %s", lease.WorkerID, defaultWorkerID())
	}

	// The now-active worker is reused; no second spawn.
	if _, err := server.acquireWorkerLeaseWithAutostart("project-test", "start_run", "iss-auto2", "run-auto2", nil); err != nil {
		t.Fatalf("second acquire error = %v", err)
	}
	if spawned != 1 {
		t.Fatalf("spawn count after second acquire = %d, want 1 (idempotent)", spawned)
	}
}

// A run targeting a different host must never trigger a local autostart —
// orch cannot spawn processes on foreign hosts, and a colocated worker would
// be the wrong executor.
func TestAcquireLeaseDoesNotAutostartForForeignHostTarget(t *testing.T) {
	server := NewSocketServer(nil, &timingTestLogger{})
	spawned := 0
	server.spawnColocatedWorker = func() error {
		spawned++
		return nil
	}

	payload := &WorkerEffectPayload{StartRun: &StartRunOptions{
		TargetWorkerID: "host-otherbox",
		TargetHost:     "otherbox",
	}}
	_, err := server.acquireWorkerLeaseWithAutostart("project-test", "start_run", "iss-remote", "run-remote", payload)
	if err == nil {
		t.Fatal("expected no-worker error for foreign host target")
	}
	if spawned != 0 {
		t.Fatalf("spawn count = %d, want 0 (must not autostart for a foreign host)", spawned)
	}
}

func TestAcquireLeaseAutostartDisabledByEnv(t *testing.T) {
	t.Setenv("ORCH_WORKER_AUTOSTART", "0")

	server := NewSocketServer(nil, &timingTestLogger{})
	spawned := 0
	server.spawnColocatedWorker = func() error {
		spawned++
		return nil
	}

	_, err := server.acquireWorkerLeaseWithAutostart("project-test", "start_run", "iss-off", "run-off", nil)
	if err == nil {
		t.Fatal("expected no-worker error with autostart disabled")
	}
	if !strings.Contains(err.Error(), "no active workers available") {
		t.Fatalf("error = %v, want no active workers available", err)
	}
	if spawned != 0 {
		t.Fatalf("spawn count = %d, want 0 with ORCH_WORKER_AUTOSTART=0", spawned)
	}
}

// Autostart failure keeps the original no-worker error AND surfaces why the
// self-heal did not happen.
func TestAcquireLeaseAutostartFailurePropagates(t *testing.T) {
	server := NewSocketServer(nil, &timingTestLogger{})
	server.spawnColocatedWorker = func() error {
		return fmt.Errorf("simulated spawn failure")
	}

	_, err := server.acquireWorkerLeaseWithAutostart("project-test", "start_run", "iss-fail", "run-fail", nil)
	if err == nil {
		t.Fatal("expected error when autostart fails")
	}
	if !strings.Contains(err.Error(), "no active workers available") {
		t.Fatalf("error = %v, must keep the no-worker cause", err)
	}
	if !strings.Contains(err.Error(), "simulated spawn failure") {
		t.Fatalf("error = %v, must surface the autostart failure", err)
	}
}
