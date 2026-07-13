package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdapterAvailabilityFailureIncludesProbeExitAndPATH(t *testing.T) {
	for _, agentType := range []AgentType{AgentClaude, AgentCodex, AgentGemini, AgentOpenCode} {
		t.Run(string(agentType), func(t *testing.T) {
			binDir := t.TempDir()
			binPath := filepath.Join(binDir, string(agentType))
			if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 7\n"), 0755); err != nil {
				t.Fatalf("write fake %s: %v", agentType, err)
			}
			t.Setenv("PATH", binDir)

			adapter, err := GetAdapter(agentType)
			if err != nil {
				t.Fatalf("GetAdapter(%s): %v", agentType, err)
			}
			availability := adapter.ProbeAvailability()
			if availability.Available {
				t.Fatal("ProbeAvailability() reported an exiting probe as available")
			}

			diagnostic := availability.Diagnostic()
			for _, want := range []string{fmt.Sprintf(`probe %q exited 7`, agentType+" --version"), "PATH=" + binDir} {
				if !strings.Contains(diagnostic, want) {
					t.Fatalf("Diagnostic() = %q, want substring %q", diagnostic, want)
				}
			}
		})
	}
}

func TestKnownAgentTypesIncludesEveryAdapter(t *testing.T) {
	got := KnownAgentTypes()
	want := []AgentType{AgentClaude, AgentCodex, AgentGemini, AgentOpenCode, AgentCustom}
	if len(got) != len(want) {
		t.Fatalf("KnownAgentTypes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("KnownAgentTypes() = %v, want %v", got, want)
		}
	}
}

func TestProbeKnownAdaptersProbesEachVersionedCLIOnce(t *testing.T) {
	binDir := t.TempDir()
	callDir := t.TempDir()
	for _, agentType := range []AgentType{AgentClaude, AgentCodex, AgentGemini, AgentOpenCode} {
		callPath := filepath.Join(callDir, string(agentType)+".calls")
		script := fmt.Sprintf("#!/bin/sh\nprintf x >> %q\nexit 0\n", callPath)
		if err := os.WriteFile(filepath.Join(binDir, string(agentType)), []byte(script), 0755); err != nil {
			t.Fatalf("write fake %s: %v", agentType, err)
		}
	}
	t.Setenv("PATH", binDir)

	results := ProbeKnownAdapters()
	if len(results) != len(KnownAgentTypes()) {
		t.Fatalf("ProbeKnownAdapters() returned %d results, want %d", len(results), len(KnownAgentTypes()))
	}
	for _, result := range results {
		if !result.Available {
			t.Fatalf("ProbeKnownAdapters() result for %s = %+v, want available", result.Agent, result)
		}
		if result.Agent == AgentCustom {
			if !result.Deferred {
				t.Fatalf("custom availability = %+v, want deferred", result)
			}
			continue
		}
		calls, err := os.ReadFile(filepath.Join(callDir, string(result.Agent)+".calls"))
		if err != nil {
			t.Fatalf("read %s probe calls: %v", result.Agent, err)
		}
		if string(calls) != "x" {
			t.Fatalf("%s probe calls = %q, want exactly one", result.Agent, calls)
		}
	}
}

func TestFormatAvailabilityMapIncludesOutcomesAndSinglePATH(t *testing.T) {
	results := []Availability{
		{Agent: AgentClaude, Available: false, Probe: "claude --version", ExitCode: 1, Path: "/usr/bin:/bin"},
		{Agent: AgentCustom, Available: true, Probe: "custom command", Deferred: true, Path: "/usr/bin:/bin"},
	}

	got := FormatAvailabilityMap(results)
	for _, want := range []string{
		`claude=unavailable (probe "claude --version" exited 1)`,
		`custom=available (probe "custom command" deferred until launch)`,
		"PATH=/usr/bin:/bin",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatAvailabilityMap() = %q, want substring %q", got, want)
		}
	}
	if strings.Count(got, "PATH=") != 1 {
		t.Fatalf("FormatAvailabilityMap() = %q, want PATH exactly once", got)
	}
}
