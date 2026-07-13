package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const availabilityProbeTimeout = 5 * time.Second

// Availability is the complete result of checking one agent CLI in the
// current process environment. It deliberately retains the probe and PATH:
// workers inherit their launcher's environment, so both are required to make
// an unavailable result actionable on the host that evaluated it.
type Availability struct {
	Agent     AgentType
	Available bool
	Probe     string
	ExitCode  int
	Failure   string
	Path      string
	Deferred  bool
}

// Diagnostic formats an availability result for a run failure. Unlike the
// startup map, a per-run failure includes PATH inline so the error remains
// self-contained as it crosses the worker/master protocol boundary.
func (a Availability) Diagnostic() string {
	return a.outcome() + "; PATH=" + displayPATH(a.Path)
}

func (a Availability) outcome() string {
	switch {
	case a.Deferred:
		return fmt.Sprintf("probe %q deferred until launch", a.Probe)
	case a.Available:
		return fmt.Sprintf("probe %q succeeded", a.Probe)
	case a.ExitCode >= 0:
		return fmt.Sprintf("probe %q exited %d", a.Probe, a.ExitCode)
	case a.Failure != "":
		return fmt.Sprintf("probe %q failed: %s", a.Probe, a.Failure)
	default:
		return fmt.Sprintf("probe %q failed", a.Probe)
	}
}

func displayPATH(path string) string {
	if path == "" {
		return "<empty>"
	}
	return path
}

func probeCommand(agentType AgentType, binary, displayCommand string, args ...string) Availability {
	result := Availability{
		Agent:    agentType,
		Probe:    displayCommand,
		ExitCode: -1,
		Path:     os.Getenv("PATH"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), availabilityProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	err := cmd.Run()
	if err == nil {
		result.Available = true
		result.ExitCode = 0
		return result
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Failure = fmt.Sprintf("timed out after %s", availabilityProbeTimeout)
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		result.ExitCode = exitErr.ExitCode()
		return result
	}
	result.Failure = err.Error()
	return result
}

func unavailableProbe(agentType AgentType, probe, failure string) Availability {
	return Availability{
		Agent:    agentType,
		Probe:    probe,
		ExitCode: -1,
		Failure:  failure,
		Path:     os.Getenv("PATH"),
	}
}

// KnownAgentTypes returns every adapter type in stable log order.
func KnownAgentTypes() []AgentType {
	return []AgentType{AgentClaude, AgentCodex, AgentGemini, AgentOpenCode, AgentCustom}
}

// ProbeKnownAdapters probes every known adapter once. Probes run concurrently
// so one slow or broken CLI delays worker startup by at most the per-probe
// timeout rather than the sum of all timeouts; result order remains stable.
func ProbeKnownAdapters() []Availability {
	types := KnownAgentTypes()
	results := make([]Availability, len(types))
	var wg sync.WaitGroup
	for i, agentType := range types {
		wg.Add(1)
		go func() {
			defer wg.Done()
			adapter, err := GetAdapter(agentType)
			if err != nil {
				results[i] = unavailableProbe(agentType, string(agentType)+" adapter", err.Error())
				return
			}
			results[i] = adapter.ProbeAvailability()
		}()
	}
	wg.Wait()
	return results
}

// FormatAvailabilityMap formats the one-time worker startup report. PATH is
// emitted once because every probe runs in the same inherited environment.
func FormatAvailabilityMap(results []Availability) string {
	entries := make([]string, 0, len(results))
	path := ""
	for _, result := range results {
		state := "unavailable"
		if result.Available {
			state = "available"
		}
		entries = append(entries, fmt.Sprintf("%s=%s (%s)", result.Agent, state, result.outcome()))
		if path == "" {
			path = result.Path
		}
	}
	return "{" + strings.Join(entries, ", ") + "}; PATH=" + displayPATH(path)
}
