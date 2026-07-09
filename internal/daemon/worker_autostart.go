package daemon

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ADR-0002: worker availability is a reconciled precondition, not a manual
// setup step. When lease acquisition finds no eligible worker and the effect
// targets the master's own host, the master ensures a colocated managed
// worker and retries the lease once. Foreign-host targets keep failing fast:
// orch cannot spawn processes on other machines.

const colocatedWorkerStartTimeout = 30 * time.Second

// workerAutostartEnabled gates the master-side reconciler on this process's
// environment. The env var name ORCH_WORKER_AUTOSTART is the shared contract
// with the client-side helper in internal/cli (the client and the master are
// separate processes; each reads its own environment).
func workerAutostartEnabled() bool {
	return strings.TrimSpace(os.Getenv("ORCH_WORKER_AUTOSTART")) != "0"
}

// colocatedWorkerEligible reports whether the payload's effect would execute
// on this master's own host: explicitly targeted at it, or untargeted/local.
func colocatedWorkerEligible(payload *WorkerEffectPayload) bool {
	preferredWorkerID, _, _ := preferredWorkerPreferenceForPayload(payload)
	return preferredWorkerID == defaultWorkerID()
}

// acquireWorkerLeaseWithAutostart is acquireWorkerLease plus the ADR-0002
// reconciler.
func (s *SocketServer) acquireWorkerLeaseWithAutostart(projectID, effect, issueID, runID string, payload *WorkerEffectPayload) (*WorkerLease, error) {
	lease, err := s.acquireWorkerLease(projectID, effect, issueID, runID, payload)
	if err == nil {
		return lease, nil
	}
	if !workerAutostartEnabled() || !colocatedWorkerEligible(payload) {
		return nil, err
	}
	if ensureErr := s.ensureColocatedWorker(); ensureErr != nil {
		return nil, fmt.Errorf("%v (worker autostart failed: %v)", err, ensureErr)
	}
	return s.acquireWorkerLease(projectID, effect, issueID, runID, payload)
}

// ensureColocatedWorker brings up the managed worker for this host if it is
// not already active. Single-flight: concurrent lease failures serialize on
// the mutex, and the losers find the worker active and return immediately.
func (s *SocketServer) ensureColocatedWorker() error {
	s.workerAutostartMu.Lock()
	defer s.workerAutostartMu.Unlock()

	workerID := defaultWorkerID()
	s.workersMu.RLock()
	registration := s.workers[workerID]
	s.workersMu.RUnlock()
	if s.workerIsActive(registration, time.Now()) {
		return nil
	}

	spawn := s.spawnColocatedWorker
	if spawn == nil {
		spawn = spawnColocatedWorkerProcess
	}
	s.logger.Printf("no active worker for %s; auto-starting colocated worker (ADR-0002; ORCH_WORKER_AUTOSTART=0 disables)", workerID)
	return spawn()
}

// spawnColocatedWorkerProcess execs this binary's own `worker start`, which
// carries every managed-worker semantic (idempotency, split-brain guard,
// stale-PID recovery, state/pid/log file conventions) and exits zero only
// once the worker is registered with this master.
func spawnColocatedWorkerProcess() error {
	// A test binary must never exec itself as `worker start` — that would
	// re-run the test suite recursively. Tests exercise the reconciler
	// through the spawnColocatedWorker seam instead.
	if flag.Lookup("test.v") != nil {
		return fmt.Errorf("refusing to autostart a worker from a test binary (inject the spawnColocatedWorker seam)")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve orch executable: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), colocatedWorkerStartTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, "worker", "start", "--worker-id", defaultWorkerID())
	// The colocated worker must register to THIS master through the local
	// socket, never to whatever ORCH_REMOTE the daemon process inherited.
	cmd.Env = append(os.Environ(), "ORCH_REMOTE=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		return fmt.Errorf("orch worker start failed: %w: %s", err, msg)
	}
	return nil
}
