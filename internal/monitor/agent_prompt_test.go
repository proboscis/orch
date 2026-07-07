package monitor

import (
	"os"
	"testing"

	"github.com/proboscis/orch/internal/model"
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
		if issue.ID != model.IssueID(expected[i]) {
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

func TestGetAvailableAgents(t *testing.T) {
	agents := getAvailableAgents()
	if agents != "opencode, claude, codex, gemini, custom" {
		t.Errorf("getAvailableAgents() = %q, want %q", agents, "opencode, claude, codex, gemini, custom")
	}
}

func TestGetGitBranch(t *testing.T) {
	cwd, _ := os.Getwd()
	branch := getGitBranch(cwd)
	if branch == "" {
		t.Skip("not in a git repository")
	}
}

func TestGetUncommittedChangesStatus(t *testing.T) {
	cwd, _ := os.Getwd()
	status := getUncommittedChangesStatus(cwd)
	if status != "Yes" && status != "No" && status != "Unknown" {
		t.Errorf("getUncommittedChangesStatus() = %q, want Yes/No/Unknown", status)
	}
}

func TestGetLastCommitMessage(t *testing.T) {
	cwd, _ := os.Getwd()
	msg := getLastCommitMessage(cwd)
	if msg == "" {
		t.Skip("no commits in repository")
	}
	if len(msg) > 80 {
		t.Errorf("getLastCommitMessage() should truncate to 80 chars, got %d", len(msg))
	}
}

func TestLoadExtraPromptNoFile(t *testing.T) {
	extra := loadExtraPrompt()
	if extra != "" {
		t.Log("extra prompt loaded from existing config")
	}
}

func TestControlPromptTemplateContainsNewSections(t *testing.T) {
	if !contains(controlPromptTemplate, "## Git Context") {
		t.Error("template should contain Git Context section")
	}
	if !contains(controlPromptTemplate, "## Available Agents") {
		t.Error("template should contain Available Agents section")
	}
	if !contains(controlPromptTemplate, "## Workflows") {
		t.Error("template should contain Workflows section")
	}
	if !contains(controlPromptTemplate, "### Handling Waiting Runs") {
		t.Error("template should contain Handling Waiting Runs subsection")
	}
	if !contains(controlPromptTemplate, "### Restarting Work") {
		t.Error("template should contain Restarting Work subsection")
	}
	if !contains(controlPromptTemplate, "## Troubleshooting") {
		t.Error("template should contain Troubleshooting section")
	}
	if !contains(controlPromptTemplate, "orch restart-from") {
		t.Error("template should contain orch restart-from command")
	}
	if !contains(controlPromptTemplate, "orch attach") {
		t.Error("template should contain orch attach command")
	}
	if !contains(controlPromptTemplate, "orch capture") {
		t.Error("template should contain orch capture command")
	}
	if !contains(controlPromptTemplate, "orch repair") {
		t.Error("template should contain orch repair command")
	}
}
