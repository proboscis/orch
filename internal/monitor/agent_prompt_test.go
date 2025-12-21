package monitor

import (
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestDetectIssueIDConvention(t *testing.T) {
	tests := []struct {
		name        string
		issues      []*model.Issue
		wantPattern string
		wantExample string
		wantNextID  string
	}{
		{
			name:        "no issues returns default",
			issues:      nil,
			wantPattern: "<prefix>-<number> (e.g., proj-001, issue-42)",
			wantExample: "orch-001",
			wantNextID:  "orch-001",
		},
		{
			name: "orch prefix with zero padding",
			issues: []*model.Issue{
				{ID: "orch-001"},
				{ID: "orch-002"},
				{ID: "orch-003"},
			},
			wantPattern: "orch-<number> (zero-padded to 3 digits)",
			wantExample: "orch-001",
			wantNextID:  "orch-004",
		},
		{
			name: "proj prefix with varying digit lengths uses default 3",
			issues: []*model.Issue{
				{ID: "proj-1"},
				{ID: "proj-5"},
				{ID: "proj-10"},
			},
			wantPattern: "proj-<number> (zero-padded to 3 digits)",
			wantExample: "proj-001",
			wantNextID:  "proj-011",
		},
		{
			name: "mixed prefixes uses most common",
			issues: []*model.Issue{
				{ID: "orch-001"},
				{ID: "orch-002"},
				{ID: "orch-003"},
				{ID: "test-001"},
			},
			wantPattern: "orch-<number> (zero-padded to 3 digits)",
			wantExample: "orch-001",
			wantNextID:  "orch-004",
		},
		{
			name: "handles gaps in numbering",
			issues: []*model.Issue{
				{ID: "orch-001"},
				{ID: "orch-005"},
				{ID: "orch-010"},
			},
			wantPattern: "orch-<number> (zero-padded to 3 digits)",
			wantExample: "orch-001",
			wantNextID:  "orch-011",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, example, nextID := detectIssueIDConvention(tt.issues)

			if pattern != tt.wantPattern {
				t.Errorf("pattern = %q, want %q", pattern, tt.wantPattern)
			}
			if example != tt.wantExample {
				t.Errorf("example = %q, want %q", example, tt.wantExample)
			}
			if nextID != tt.wantNextID {
				t.Errorf("nextID = %q, want %q", nextID, tt.wantNextID)
			}
		})
	}
}

func TestSortIssuesByID(t *testing.T) {
	issues := []*model.Issue{
		{ID: "orch-010"},
		{ID: "orch-002"},
		{ID: "orch-001"},
		{ID: "orch-005"},
	}

	sortIssuesByID(issues)

	expected := []string{"orch-001", "orch-002", "orch-005", "orch-010"}
	for i, issue := range issues {
		if issue.ID != expected[i] {
			t.Errorf("issues[%d].ID = %q, want %q", i, issue.ID, expected[i])
		}
	}
}

func TestBuildFallbackControlPrompt(t *testing.T) {
	prompt := buildFallbackControlPrompt("/vault/path", "/work/dir")

	// Check that key elements are present
	if !contains(prompt, "/vault/path") {
		t.Error("prompt should contain vault path")
	}
	if !contains(prompt, "/work/dir") {
		t.Error("prompt should contain work directory")
	}
	if !contains(prompt, "orch issue create") {
		t.Error("prompt should contain issue create command")
	}
	if !contains(prompt, "orch run") {
		t.Error("prompt should contain run command")
	}
	// Should NOT contain ORCH_CMD protocol
	if contains(prompt, "ORCH_CMD:") {
		t.Error("prompt should NOT contain ORCH_CMD: protocol")
	}
}

func TestGetControlPromptInstruction(t *testing.T) {
	instruction := GetControlPromptInstruction()

	if !contains(instruction, "ORCH_CONTROL_PROMPT.md") {
		t.Error("instruction should reference ORCH_CONTROL_PROMPT.md file")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
