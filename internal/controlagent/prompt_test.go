package controlagent

import (
	"strings"
	"testing"
)

func TestBuildFallbackControlPrompt(t *testing.T) {
	prompt := buildFallbackControlPrompt("/vault/path", "/work/dir")
	for _, want := range []string{"/vault/path", "/work/dir", "orch issue create", "orch run"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("fallback prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "ORCH_CMD:") {
		t.Error("fallback prompt should not contain the legacy ORCH_CMD protocol")
	}
}

func TestGetControlPromptInstruction(t *testing.T) {
	if instruction := GetControlPromptInstruction(); !strings.Contains(instruction, "ORCH_CONTROL_PROMPT.md") {
		t.Errorf("instruction should reference ORCH_CONTROL_PROMPT.md: %q", instruction)
	}
}

func TestControlPromptTemplateContainsOperationalSections(t *testing.T) {
	for _, want := range []string{
		"## Git Context",
		"## Available Agents",
		"## Workflows",
		"### Handling Waiting Runs",
		"### Restarting Work",
		"## Troubleshooting",
		"orch restart-from",
		"orch attach",
		"orch capture",
		"orch repair",
	} {
		if !strings.Contains(controlPromptTemplate, want) {
			t.Errorf("control prompt template missing %q", want)
		}
	}
}
