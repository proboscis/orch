package daemon

// Law tests for the worker-lease core (LL1–LL5 drafted in
// docs/design/worker-lease.md; Phase E2 of the coupling-core roadmap).
// LL1 (liveness fail-fast) is covered by
// TestWaitForWorkerLeaseCompletionFailsFastWhenWorkerLost; LL5 (snapshot
// sufficiency) by the executeLeaseEffect snapshot tests.

import (
	"io"
	"log"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/store"
	filestore "github.com/proboscis/orch/internal/store/file"
)

func newLeaseLawServer(t *testing.T, workerID string, effects []string) *SocketServer {
	t.Helper()
	server := NewSocketServer(func(issuesRoot string) (store.Store, error) {
		return filestore.New(issuesRoot)
	}, log.New(io.Discard, "", 0))
	if _, ttl := server.registerWorker(workerID, "external", "localhost", "external", effects); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for test worker")
	}
	return server
}

func leaseRecord(t *testing.T, server *SocketServer, leaseID string) WorkerLease {
	t.Helper()
	server.workerLeasesMu.RLock()
	defer server.workerLeasesMu.RUnlock()
	lease, ok := server.workerLeases[leaseID]
	if !ok {
		t.Fatalf("lease %s not found", leaseID)
	}
	return *lease
}

// LL3 completion finality: the first verdict wins; a late or duplicate ack
// is an idempotent no-op and never rewrites Success/Error/ResultJSON.
func TestLeaseLawCompletionFinality(t *testing.T) {
	server := newLeaseLawServer(t, "worker-ll3", []string{"stop_run"})
	lease, err := server.acquireWorkerLease("project-test", "stop_run", "orch-1", "run-1", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}
	if dispatched := server.leaseWorkForWorker("worker-ll3"); dispatched == nil {
		t.Fatal("expected initial dispatch")
	}

	if err := server.acknowledgeWorkerLease("worker-ll3", lease.LeaseID, true, "", `{"r":1}`); err != nil {
		t.Fatalf("first ack error = %v", err)
	}
	first := leaseRecord(t, server, lease.LeaseID)

	if err := server.acknowledgeWorkerLease("worker-ll3", lease.LeaseID, false, "late failure", `{"r":2}`); err != nil {
		t.Fatalf("late ack must be an idempotent no-op, got error %v", err)
	}
	rec := leaseRecord(t, server, lease.LeaseID)
	if !rec.Completed || rec.Success != first.Success || rec.Error != first.Error ||
		rec.ResultJSON != first.ResultJSON || !rec.CompletedAt.Equal(first.CompletedAt) {
		t.Fatalf("late ack rewrote the verdict: first=%+v now=%+v", first, rec)
	}
}

// LL2 dispatch exclusivity: a dispatched lease is not handed out again
// before its dispatch TTL expires; after expiry it re-dispatches with a
// monotonically increased DispatchCount.
func TestLeaseLawDispatchExclusiveUntilExpiry(t *testing.T) {
	server := newLeaseLawServer(t, "worker-ll2", []string{"capture_session"})
	lease, err := server.acquireWorkerLease("project-test", "capture_session", "orch-2", "run-2", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}

	first := server.leaseWorkForWorker("worker-ll2")
	if first == nil || first.DispatchCount != 1 {
		t.Fatalf("first dispatch = %+v, want DispatchCount 1", first)
	}
	if redispatch := server.leaseWorkForWorker("worker-ll2"); redispatch != nil {
		t.Fatalf("lease re-dispatched before expiry: %+v", redispatch)
	}

	server.workerLeasesMu.Lock()
	server.workerLeases[lease.LeaseID].ExpiresAt = time.Now().Add(-time.Second)
	server.workerLeasesMu.Unlock()

	second := server.leaseWorkForWorker("worker-ll2")
	if second == nil || second.DispatchCount != 2 {
		t.Fatalf("expected re-dispatch with DispatchCount 2 after expiry, got %+v", second)
	}
}

// LL2/LL3 corollary: a completed lease is never dispatched again, even
// after its dispatch TTL has expired.
func TestLeaseLawNoDispatchAfterCompletion(t *testing.T) {
	server := newLeaseLawServer(t, "worker-ll23", []string{"stop_run"})
	lease, err := server.acquireWorkerLease("project-test", "stop_run", "orch-3", "run-3", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}
	if dispatched := server.leaseWorkForWorker("worker-ll23"); dispatched == nil {
		t.Fatal("expected initial dispatch")
	}
	if err := server.acknowledgeWorkerLease("worker-ll23", lease.LeaseID, true, "", ""); err != nil {
		t.Fatalf("ack error = %v", err)
	}

	server.workerLeasesMu.Lock()
	server.workerLeases[lease.LeaseID].ExpiresAt = time.Now().Add(-time.Second)
	server.workerLeasesMu.Unlock()

	if redispatch := server.leaseWorkForWorker("worker-ll23"); redispatch != nil {
		t.Fatalf("completed lease was re-dispatched: %+v", redispatch)
	}
}

// LL4 restart amnesia is safe: leases are memory-only by design, so a
// caller holding a pre-restart lease ID observes an immediate "not found"
// failure, never a hang until the lease timeout.
func TestLeaseLawRestartAmnesiaFailsFast(t *testing.T) {
	server := newLeaseLawServer(t, "worker-ll4", []string{"stop_run"}) // fresh maps = post-restart state

	start := time.Now()
	_, err := server.waitForWorkerLeaseCompletion("lease-from-before-restart", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "lease not found") {
		t.Fatalf("error = %v, want lease-not-found", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("amnesia failure took %s, want immediate", elapsed)
	}
}

// Random walk over dispatch/expire/ack interleavings: DispatchCount is
// monotone, a completed lease never dispatches, and the first verdict is
// never rewritten — for any operation order.
func TestLeaseLawRandomWalkInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 25; trial++ {
		server := newLeaseLawServer(t, "worker-walk", []string{"stop_run"})
		lease, err := server.acquireWorkerLease("project-test", "stop_run", "orch-w", "run-w", nil)
		if err != nil {
			t.Fatalf("trial %d: acquireWorkerLease() error = %v", trial, err)
		}

		prevDispatch := 0
		var verdict *WorkerLease
		for op := 0; op < 40; op++ {
			switch rng.Intn(3) {
			case 0: // dispatch attempt
				got := server.leaseWorkForWorker("worker-walk")
				rec := leaseRecord(t, server, lease.LeaseID)
				if rec.DispatchCount < prevDispatch {
					t.Fatalf("trial %d op %d: DispatchCount decreased %d -> %d", trial, op, prevDispatch, rec.DispatchCount)
				}
				if got != nil && rec.Completed {
					t.Fatalf("trial %d op %d: dispatched a completed lease", trial, op)
				}
				prevDispatch = rec.DispatchCount
			case 1: // age the dispatch TTL past expiry
				server.workerLeasesMu.Lock()
				server.workerLeases[lease.LeaseID].ExpiresAt = time.Now().Add(-time.Millisecond)
				server.workerLeasesMu.Unlock()
			case 2: // ack with a random verdict
				success := rng.Intn(2) == 0
				errMsg := ""
				if !success {
					errMsg = "boom"
				}
				if err := server.acknowledgeWorkerLease("worker-walk", lease.LeaseID, success, errMsg, ""); err != nil {
					t.Fatalf("trial %d op %d: ack error = %v", trial, op, err)
				}
				rec := leaseRecord(t, server, lease.LeaseID)
				if verdict == nil {
					v := rec
					verdict = &v
				} else if rec.Success != verdict.Success || rec.Error != verdict.Error {
					t.Fatalf("trial %d op %d: verdict rewritten: first=%+v now=%+v", trial, op, verdict, rec)
				}
			}
		}
	}
}
