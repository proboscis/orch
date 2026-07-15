package worker

import (
	"log"
	"os"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/daemon"
)

// LogAgentAvailability probes every known adapter once at worker process
// startup and logs the result. It carries no data that master registration
// depends on, so callers should run it concurrently with (not ahead of)
// registration: a probe can block for up to the per-adapter probe timeout,
// and serializing registration behind it would let one hung agent CLI eat
// the caller's entire registration-readiness budget before the register RPC
// is even sent.
func LogAgentAvailability(workerID string) {
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	if workerID == "" {
		workerID = daemon.HostWorkerID(host)
	}
	logAgentAvailability(workerID, host, agent.ProbeKnownAdapters, log.Printf)
}

func logAgentAvailability(workerID, host string, probe func() []agent.Availability, logf func(string, ...any)) {
	results := probe()
	logf("orch-worker %s on %s: agent availability: %s", workerID, host, agent.FormatAvailabilityMap(results))
}
