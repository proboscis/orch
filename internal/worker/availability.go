package worker

import (
	"log"
	"os"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/daemon"
)

// LogAgentAvailability probes every known adapter once at worker process
// startup and logs the result. The caller runs it concurrently with master
// registration rather than before it: the probe is diagnostic only, and a hung
// agent CLI (bounded by the per-probe timeout) must never delay registration —
// otherwise it would consume the managed-start ready budget and make
// `orch worker start` fail. The caller joins the probe before the process
// exits, so a healthy-looking worker still never hides the agent capabilities
// of its inherited environment.
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
