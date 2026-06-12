//go:build semgrepfixture

// Semgrep test fixture for worker-lease-mutation-surface.
// Parsed by `semgrep test`, never compiled.
package fixture

func badLeaseMapWrite(s Server, lease Lease) {
	// ruleid: worker-lease-mutation-surface
	s.workerLeases[lease.LeaseID] = lease
}

func badLeaseMapDelete(s Server, id string) {
	// ruleid: worker-lease-mutation-surface
	delete(s.workerLeases, id)
}

func badWorkerMapWrite(s Server, w Worker) {
	// ruleid: worker-lease-mutation-surface
	s.workers[w.ID] = w
}

func badWorkerMapDelete(s Server, id string) {
	// ruleid: worker-lease-mutation-surface
	delete(s.workers, id)
}

func okReadLease(s Server, id string) Lease {
	// ok: worker-lease-mutation-surface
	return s.workerLeases[id]
}

func okIterateWorkers(s Server) int {
	n := 0
	// ok: worker-lease-mutation-surface
	for range s.workers {
		n++
	}
	return n
}
