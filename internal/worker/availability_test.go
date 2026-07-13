package worker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/agent"
)

func TestLogAgentAvailabilityNamesWorkerHostAndEveryOutcome(t *testing.T) {
	probeCalls := 0
	probe := func() []agent.Availability {
		probeCalls++
		return []agent.Availability{
			{Agent: agent.AgentClaude, Available: false, Probe: "claude --version", ExitCode: 1, Path: "/usr/bin:/bin"},
			{Agent: agent.AgentCodex, Available: true, Probe: "codex --version", ExitCode: 0, Path: "/usr/bin:/bin"},
		}
	}
	var logLine string
	logf := func(format string, args ...any) {
		logLine = fmt.Sprintf(format, args...)
	}

	logAgentAvailability("worker-1", "host-1", probe, logf)

	if probeCalls != 1 {
		t.Fatalf("availability probe calls = %d, want 1", probeCalls)
	}
	for _, want := range []string{
		"orch-worker worker-1 on host-1: agent availability:",
		`claude=unavailable (probe "claude --version" exited 1)`,
		`codex=available (probe "codex --version" succeeded)`,
		"PATH=/usr/bin:/bin",
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("startup log = %q, want substring %q", logLine, want)
		}
	}
}
