package worker

import (
	"log"
	"os"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/daemon"
)

// LogAgentAvailability probes every known adapter once at worker process
// startup. This runs before master registration so a healthy-looking worker
// never hides the agent capabilities of its inherited environment.
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
