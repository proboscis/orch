package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/proboscis/orch/internal/worker"
)

// workerAutostartEnabled gates the client-side ensure on this process's
// environment. The env var name ORCH_WORKER_AUTOSTART is the shared contract
// with the master-side reconciler in internal/daemon (separate processes;
// each reads its own environment — cli must not import daemon or config).
func workerAutostartEnabled() bool {
	return strings.TrimSpace(os.Getenv("ORCH_WORKER_AUTOSTART")) != "0"
}

// startManagedWorkerFn is a test seam over worker.StartManaged.
var startManagedWorkerFn = worker.StartManaged

// ensureLocalWorkerForRemoteMaster (ADR-0002) idempotently starts the local
// managed worker registered to the given remote master before dispatching a
// run there, so this host can execute targeted work without a manual
// `orch worker start`. Failure is a warning, not a dispatch blocker: the
// cluster may have other workers able to take the run. For a local master
// this is a no-op — the master's own reconciler ensures its colocated worker.
func ensureLocalWorkerForRemoteMaster(remoteAddr string) {
	if strings.TrimSpace(remoteAddr) == "" || !workerAutostartEnabled() {
		return
	}

	res, err := startManagedWorkerFn(worker.ManagedOptions{RemoteAddr: remoteAddr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not auto-start local worker for %s: %v (ORCH_WORKER_AUTOSTART=0 disables this)\n", remoteAddr, err)
		return
	}
	if res != nil && !res.Reused && !globalOpts.Quiet {
		fmt.Fprintf(os.Stderr, "Started local worker %s (registered to %s)\n", res.WorkerID, remoteAddr)
	}
}
