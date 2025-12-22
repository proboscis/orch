package model

import (
	"testing"
	"time"
)

func TestParseRunRef(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		issueID string
		runID   string
	}{
		{
			name:    "full ref",
			input:   "plc124#20231220-100000",
			wantErr: false,
			issueID: "plc124",
			runID:   "20231220-100000",
		},
		{
			name:    "issue only",
			input:   "plc124",
			wantErr: false,
			issueID: "plc124",
			runID:   "",
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "with spaces",
			input:   "  plc124#20231220  ",
			wantErr: false,
			issueID: "plc124",
			runID:   "20231220",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseRunRef(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRunRef() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if ref.IssueID != tt.issueID {
					t.Errorf("IssueID = %v, want %v", ref.IssueID, tt.issueID)
				}
				if ref.RunID != tt.runID {
					t.Errorf("RunID = %v, want %v", ref.RunID, tt.runID)
				}
			}
		})
	}
}

func TestRunRefString(t *testing.T) {
	tests := []struct {
		ref  *RunRef
		want string
	}{
		{&RunRef{IssueID: "plc124", RunID: "20231220"}, "plc124#20231220"},
		{&RunRef{IssueID: "plc124", RunID: ""}, "plc124"},
	}

	for _, tt := range tests {
		got := tt.ref.String()
		if got != tt.want {
			t.Errorf("RunRef.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestRunRefIsLatest(t *testing.T) {
	ref1 := &RunRef{IssueID: "plc124", RunID: ""}
	if !ref1.IsLatest() {
		t.Error("expected IsLatest() = true for empty RunID")
	}

	ref2 := &RunRef{IssueID: "plc124", RunID: "20231220"}
	if ref2.IsLatest() {
		t.Error("expected IsLatest() = false for non-empty RunID")
	}
}

func TestRunDeriveState(t *testing.T) {
	ts := time.Now()
	run := &Run{
		IssueID: "plc124",
		RunID:   "20231220",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "queued"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeStatus, Name: "running"},
			{Timestamp: ts.Add(3 * time.Second), Type: EventTypeArtifact, Name: "worktree", Attrs: map[string]string{"path": "/tmp/wt"}},
			{Timestamp: ts.Add(4 * time.Second), Type: EventTypeArtifact, Name: "branch", Attrs: map[string]string{"name": "feature/test"}},
			{Timestamp: ts.Add(5 * time.Second), Type: EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": "run-plc124"}},
		},
	}

	run.DeriveState()

	if run.Status != StatusRunning {
		t.Errorf("Status = %v, want running", run.Status)
	}
	if run.WorktreePath != "/tmp/wt" {
		t.Errorf("WorktreePath = %v, want /tmp/wt", run.WorktreePath)
	}
	if run.Branch != "feature/test" {
		t.Errorf("Branch = %v, want feature/test", run.Branch)
	}
	if run.TmuxSession != "run-plc124" {
		t.Errorf("TmuxSession = %v, want run-plc124", run.TmuxSession)
	}
}

func TestGenerateRunID(t *testing.T) {
	id := GenerateRunID()
	if len(id) != 15 { // YYYYMMDD-HHMMSS
		t.Errorf("expected 15 char run ID, got %d: %s", len(id), id)
	}
}

func TestGenerateBranchName(t *testing.T) {
	branch := GenerateBranchName("plc124", "20231220")
	want := "issue/plc124/run-20231220"
	if branch != want {
		t.Errorf("GenerateBranchName() = %v, want %v", branch, want)
	}
}

func TestGenerateTmuxSession(t *testing.T) {
	session := GenerateTmuxSession("plc124", "20231220")
	want := "run-plc124-20231220"
	if session != want {
		t.Errorf("GenerateTmuxSession() = %v, want %v", session, want)
	}
}
