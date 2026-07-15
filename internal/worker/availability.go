package worker

import (
	"log"
	"os"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/daemon"
)

// LogAgentAvailability probes every known adapter once at worker process
// startup so a healthy-looking worker never hides the agent capabilities of
// its inherited environment. It is diagnostic-only and must never gate master
// registration: the caller runs it concurrently with registration, because a
// hanging agent CLI holds a probe for the full per-probe timeout — the same
// budget the managed launcher grants the worker to register.
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
