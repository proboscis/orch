package daemon

import (
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestFileURI(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", ""},
		{"/path/to/file.md", "file:///path/to/file.md"},
		{"/Users/test/vault/issues/orch-123.md", "file:///Users/test/vault/issues/orch-123.md"},
		{"https://github.com/owner/repo/issues/123", "https://github.com/owner/repo/issues/123"},
		{"http://example.com/page", "http://example.com/page"},
		{"https://api.github.com/repos/test", "https://api.github.com/repos/test"},
	}

	for _, tt := range tests {
		got := FileURI(tt.path)
		if got != tt.want {
			t.Errorf("FileURI(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestEncodeCursor(t *testing.T) {
	cursor := EncodeCursor(42)
	if cursor == "" {
		t.Error("EncodeCursor returned empty string")
	}

	offset, err := DecodeCursor(cursor)
	if err != nil {
		t.Errorf("DecodeCursor failed: %v", err)
	}
	if offset != 42 {
		t.Errorf("DecodeCursor = %d, want 42", offset)
	}
}

func TestDecodeCursorEmpty(t *testing.T) {
	offset, err := DecodeCursor("")
	if err != nil {
		t.Errorf("DecodeCursor empty should not error: %v", err)
	}
	if offset != 0 {
		t.Errorf("DecodeCursor empty = %d, want 0", offset)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Error("DecodeCursor should fail for invalid base64")
	}
}

func TestDecodeCursorNegative(t *testing.T) {
	cursor := EncodeCursor(-5)
	_, err := DecodeCursor(cursor)
	if err == nil {
		t.Error("DecodeCursor should fail for negative offset")
	}
}

func TestSummaryToRun(t *testing.T) {
	tests := []struct {
		name    string
		summary *RunSummary
		check   func(t *testing.T, run *model.Run)
	}{
		{
			name:    "nil summary returns nil",
			summary: nil,
			check: func(t *testing.T, run *model.Run) {
				if run != nil {
					t.Error("expected nil run for nil summary")
				}
			},
		},
		{
			name: "converts all fields correctly",
			summary: &RunSummary{
				IssueID:      "issue-123",
				RunID:        "run-456",
				ShortID:      "r456",
				Status:       "running",
				Phase:        "implement",
				Agent:        "claude",
				Model:        "claude-3-opus",
				Branch:       "feature/test",
				WorktreePath: "/tmp/worktree",
				TmuxSession:  "orch-issue-123",
				Multiplexer:  "tmux",
				PRUrl:        "https://github.com/test/repo/pull/1",
				StartedAt:    "2024-01-15T10:30:00Z",
				UpdatedAt:    "2024-01-15T11:00:00Z",
				URI:          "file:///path/to/run.json",
			},
			check: func(t *testing.T, run *model.Run) {
				if run == nil {
					t.Fatal("expected non-nil run")
				}
				if run.IssueID != "issue-123" {
					t.Errorf("IssueID = %q, want %q", run.IssueID, "issue-123")
				}
				if run.RunID != "run-456" {
					t.Errorf("RunID = %q, want %q", run.RunID, "run-456")
				}
				if run.Status != model.StatusRunning {
					t.Errorf("Status = %q, want %q", run.Status, model.StatusRunning)
				}
				if run.Phase != model.PhaseImplement {
					t.Errorf("Phase = %q, want %q", run.Phase, model.PhaseImplement)
				}
				if run.Agent != "claude" {
					t.Errorf("Agent = %q, want %q", run.Agent, "claude")
				}
				if run.Model != "claude-3-opus" {
					t.Errorf("Model = %q, want %q", run.Model, "claude-3-opus")
				}
				if run.Branch != "feature/test" {
					t.Errorf("Branch = %q, want %q", run.Branch, "feature/test")
				}
				if run.WorktreePath != "/tmp/worktree" {
					t.Errorf("WorktreePath = %q, want %q", run.WorktreePath, "/tmp/worktree")
				}
				if run.TmuxSession != "orch-issue-123" {
					t.Errorf("TmuxSession = %q, want %q", run.TmuxSession, "orch-issue-123")
				}
				if run.Multiplexer != "tmux" {
					t.Errorf("Multiplexer = %q, want %q", run.Multiplexer, "tmux")
				}
				if run.PRUrl != "https://github.com/test/repo/pull/1" {
					t.Errorf("PRUrl = %q, want %q", run.PRUrl, "https://github.com/test/repo/pull/1")
				}
				if run.StartedAt.IsZero() {
					t.Error("StartedAt should not be zero")
				}
				if run.UpdatedAt.IsZero() {
					t.Error("UpdatedAt should not be zero")
				}
			},
		},
		{
			name: "handles empty timestamps gracefully",
			summary: &RunSummary{
				IssueID:   "issue-789",
				RunID:     "run-xyz",
				Status:    "done",
				StartedAt: "",
				UpdatedAt: "",
			},
			check: func(t *testing.T, run *model.Run) {
				if run == nil {
					t.Fatal("expected non-nil run")
				}
				if !run.StartedAt.IsZero() {
					t.Error("StartedAt should be zero for empty string")
				}
				if !run.UpdatedAt.IsZero() {
					t.Error("UpdatedAt should be zero for empty string")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := SummaryToRun(tt.summary)
			tt.check(t, run)
		})
	}
}

func TestRunToSummaryRoundTrip(t *testing.T) {
	original := &model.Run{
		IssueID:      "issue-roundtrip",
		RunID:        "run-roundtrip-123",
		Status:       model.StatusBlocked,
		Phase:        model.PhasePlan,
		Agent:        "opencode",
		Model:        "gpt-4",
		Branch:       "fix/bug-123",
		WorktreePath: "/home/user/worktrees/fix-bug",
		TmuxSession:  "orch-roundtrip",
		Multiplexer:  "tmux",
		PRUrl:        "",
	}

	summary := RunToSummary(original)
	roundTripped := SummaryToRun(summary)

	if roundTripped.IssueID != original.IssueID {
		t.Errorf("IssueID mismatch: got %q, want %q", roundTripped.IssueID, original.IssueID)
	}
	if roundTripped.RunID != original.RunID {
		t.Errorf("RunID mismatch: got %q, want %q", roundTripped.RunID, original.RunID)
	}
	if roundTripped.Status != original.Status {
		t.Errorf("Status mismatch: got %q, want %q", roundTripped.Status, original.Status)
	}
	if roundTripped.Phase != original.Phase {
		t.Errorf("Phase mismatch: got %q, want %q", roundTripped.Phase, original.Phase)
	}
	if roundTripped.Agent != original.Agent {
		t.Errorf("Agent mismatch: got %q, want %q", roundTripped.Agent, original.Agent)
	}
	if roundTripped.Model != original.Model {
		t.Errorf("Model mismatch: got %q, want %q", roundTripped.Model, original.Model)
	}
	if roundTripped.Branch != original.Branch {
		t.Errorf("Branch mismatch: got %q, want %q", roundTripped.Branch, original.Branch)
	}
	if roundTripped.WorktreePath != original.WorktreePath {
		t.Errorf("WorktreePath mismatch: got %q, want %q", roundTripped.WorktreePath, original.WorktreePath)
	}
	if roundTripped.TmuxSession != original.TmuxSession {
		t.Errorf("TmuxSession mismatch: got %q, want %q", roundTripped.TmuxSession, original.TmuxSession)
	}
}

func TestRunToSummaryWithAlive(t *testing.T) {
	run := &model.Run{
		IssueID: "issue-alive",
		RunID:   "run-alive-123",
		Status:  model.StatusRunning,
	}

	t.Run("nil callback keeps AliveKnown false", func(t *testing.T) {
		summary := RunToSummaryWithAlive(run, nil)
		if summary.AliveKnown {
			t.Error("AliveKnown should be false when callback is nil")
		}
		if summary.Alive {
			t.Error("Alive should be false when callback is nil")
		}
	})

	t.Run("callback returning true sets Alive true", func(t *testing.T) {
		summary := RunToSummaryWithAlive(run, func(*model.Run) bool { return true })
		if !summary.AliveKnown {
			t.Error("AliveKnown should be true when callback provided")
		}
		if !summary.Alive {
			t.Error("Alive should be true when callback returns true")
		}
	})

	t.Run("callback returning false sets Alive false", func(t *testing.T) {
		summary := RunToSummaryWithAlive(run, func(*model.Run) bool { return false })
		if !summary.AliveKnown {
			t.Error("AliveKnown should be true when callback provided")
		}
		if summary.Alive {
			t.Error("Alive should be false when callback returns false")
		}
	})
}

func TestRunToFullWithAlive(t *testing.T) {
	run := &model.Run{
		IssueID: "issue-full-alive",
		RunID:   "run-full-alive-123",
		Status:  model.StatusRunning,
	}

	t.Run("nil callback keeps AliveKnown false", func(t *testing.T) {
		full := RunToFullWithAlive(run, nil)
		if full.AliveKnown {
			t.Error("AliveKnown should be false when callback is nil")
		}
		if full.Alive {
			t.Error("Alive should be false when callback is nil")
		}
	})

	t.Run("callback returning true sets Alive true", func(t *testing.T) {
		full := RunToFullWithAlive(run, func(*model.Run) bool { return true })
		if !full.AliveKnown {
			t.Error("AliveKnown should be true when callback provided")
		}
		if !full.Alive {
			t.Error("Alive should be true when callback returns true")
		}
	})

	t.Run("callback returning false sets Alive false", func(t *testing.T) {
		full := RunToFullWithAlive(run, func(*model.Run) bool { return false })
		if !full.AliveKnown {
			t.Error("AliveKnown should be true when callback provided")
		}
		if full.Alive {
			t.Error("Alive should be false when callback returns false")
		}
	})
}

func TestSummaryAliveInfo(t *testing.T) {
	t.Run("nil summary returns false, false", func(t *testing.T) {
		alive, known := SummaryAliveInfo(nil)
		if alive {
			t.Error("alive should be false for nil summary")
		}
		if known {
			t.Error("known should be false for nil summary")
		}
	})

	t.Run("returns correct values from summary", func(t *testing.T) {
		summary := &RunSummary{
			IssueID:    "test",
			RunID:      "test-run",
			Alive:      true,
			AliveKnown: true,
		}
		alive, known := SummaryAliveInfo(summary)
		if !alive {
			t.Error("alive should be true")
		}
		if !known {
			t.Error("known should be true")
		}
	})

	t.Run("returns false alive when not alive", func(t *testing.T) {
		summary := &RunSummary{
			IssueID:    "test",
			RunID:      "test-run",
			Alive:      false,
			AliveKnown: true,
		}
		alive, known := SummaryAliveInfo(summary)
		if alive {
			t.Error("alive should be false")
		}
		if !known {
			t.Error("known should be true")
		}
	})
}
