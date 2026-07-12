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
		{
			name:    "github issue with run",
			input:   "gh-267#20260118-041509",
			wantErr: false,
			issueID: "gh-267",
			runID:   "20260118-041509",
		},
		{
			name:    "github issue only",
			input:   "gh-267",
			wantErr: false,
			issueID: "gh-267",
			runID:   "",
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
				if ref.IssueID != IssueID(tt.issueID) {
					t.Errorf("IssueID = %v, want %v", ref.IssueID, tt.issueID)
				}
				if ref.RunID != RunID(tt.runID) {
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
			{Timestamp: ts.Add(4500 * time.Millisecond), Type: EventTypeArtifact, Name: "target", Attrs: map[string]string{"name": "mac", "host": "mac", "worker_id": "host-mac"}},
			{Timestamp: ts.Add(5 * time.Second), Type: EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": "run-plc124"}},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.Status != StatusRunning {
		t.Errorf("Status = %v, want running", run.Status)
	}
	if run.WorktreePath != "/tmp/wt" {
		t.Errorf("WorktreePath = %v, want /tmp/wt", run.WorktreePath)
	}
	if run.Branch != "feature/test" {
		t.Errorf("Branch = %v, want feature/test", run.Branch)
	}
	if run.Target != "mac" {
		t.Errorf("Target = %v, want mac", run.Target)
	}
	if run.TargetHost != "mac" {
		t.Errorf("TargetHost = %v, want mac", run.TargetHost)
	}
	if run.TargetWorkerID != "host-mac" {
		t.Errorf("TargetWorkerID = %v, want host-mac", run.TargetWorkerID)
	}
	if run.SessionName != "run-plc124" {
		t.Errorf("SessionName = %v, want run-plc124", run.SessionName)
	}
}

func TestRunDeriveStateAgentSession(t *testing.T) {
	ts := time.Now()
	run := &Run{
		IssueID: "plc130",
		RunID:   "20260713",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "running"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": "claude", "id": "11111111-1111-1111-1111-111111111111", "generation": "1"}},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.AgentSessionID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("AgentSessionID = %q, want the recorded artifact id", run.AgentSessionID)
	}
	if run.AgentSessionGeneration != 1 {
		t.Errorf("AgentSessionGeneration = %d, want 1", run.AgentSessionGeneration)
	}
}

func TestRunDeriveStateAgentSessionLatestWins(t *testing.T) {
	// ADR-0005 R5: revive appends a new-generation agent_session artifact;
	// the fold must take the LATEST one, never the first.
	ts := time.Now()
	run := &Run{
		IssueID: "plc131",
		RunID:   "20260713",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "running"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": "claude", "id": "gen-one-id", "generation": "1"}},
			{Timestamp: ts.Add(2 * time.Second), Type: EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": "claude", "id": "gen-two-id", "generation": "2"}},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.AgentSessionID != "gen-two-id" {
		t.Errorf("AgentSessionID = %q, want gen-two-id (latest artifact wins)", run.AgentSessionID)
	}
	if run.AgentSessionGeneration != 2 {
		t.Errorf("AgentSessionGeneration = %d, want 2", run.AgentSessionGeneration)
	}
}

func TestRunDeriveStateAgentSessionMalformedGeneration(t *testing.T) {
	ts := time.Now()
	run := &Run{
		IssueID: "plc132",
		RunID:   "20260713",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "running"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": "codex", "id": "codex-rollout-id", "generation": "not-a-number"}},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.AgentSessionID != "codex-rollout-id" {
		t.Errorf("AgentSessionID = %q, want codex-rollout-id (id folds even when generation is malformed)", run.AgentSessionID)
	}
	if run.AgentSessionGeneration != 0 {
		t.Errorf("AgentSessionGeneration = %d, want 0 for malformed generation", run.AgentSessionGeneration)
	}
}

func TestRunDeriveStatePrefersLastNonEmptySessionMultiplexer(t *testing.T) {
	ts := time.Now()
	run := &Run{
		IssueID: "plc125",
		RunID:   "20231221",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "queued"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": "run-plc125", "multiplexer": "tmux"}},
			{Timestamp: ts.Add(2 * time.Second), Type: EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": "run-plc125"}},
			{Timestamp: ts.Add(3 * time.Second), Type: EventTypeStatus, Name: "running"},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.SessionName != "run-plc125" {
		t.Errorf("SessionName = %v, want run-plc125", run.SessionName)
	}
	if run.Multiplexer != "tmux" {
		t.Errorf("Multiplexer = %v, want tmux", run.Multiplexer)
	}
}

func TestRunDeriveStateFallsBackToSessionHost(t *testing.T) {
	ts := time.Now()
	run := &Run{
		IssueID: "plc126",
		RunID:   "20231222",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "queued"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": "run-plc126", "host": "mac-host", "multiplexer": "tmux"}},
			{Timestamp: ts.Add(2 * time.Second), Type: EventTypeStatus, Name: "running"},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.TargetHost != "mac-host" {
		t.Errorf("TargetHost = %v, want mac-host", run.TargetHost)
	}
	if run.Multiplexer != "tmux" {
		t.Errorf("Multiplexer = %v, want tmux", run.Multiplexer)
	}
}

func TestRunDeriveStateFallsBackToSessionWorkerID(t *testing.T) {
	ts := time.Now()
	run := &Run{
		IssueID: "plc127",
		RunID:   "20231223",
		Events: []*Event{
			{Timestamp: ts, Type: EventTypeStatus, Name: "queued"},
			{Timestamp: ts.Add(time.Second), Type: EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": "run-plc127", "worker_id": "host-mac-host"}},
			{Timestamp: ts.Add(2 * time.Second), Type: EventTypeStatus, Name: "running"},
		},
	}

	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}

	if run.TargetWorkerID != "host-mac-host" {
		t.Errorf("TargetWorkerID = %v, want host-mac-host", run.TargetWorkerID)
	}
}

func TestRunDeriveStateRejectsUnknownStatus(t *testing.T) {
	run := &Run{
		IssueID: "plc128",
		RunID:   "20231224",
		Events: []*Event{
			{Timestamp: time.Now(), Type: EventTypeStatus, Name: "bogus"},
		},
	}

	if err := run.DeriveState(); err == nil {
		t.Fatal("DeriveState() error = nil, want unknown status error")
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

func TestGenerateSessionName(t *testing.T) {
	session := GenerateSessionName("plc124", "20231220")
	want := "run-plc124-20231220"
	if session != want {
		t.Errorf("GenerateSessionName() = %v, want %v", session, want)
	}
}

func TestGenerateWorktreeName(t *testing.T) {
	got := GenerateWorktreeName("plc124", "20231220-100000", "claude")
	want := string(GenerateShortID("plc124", "20231220-100000")) + "_claude_20231220-100000"
	if got != want {
		t.Errorf("GenerateWorktreeName() = %v, want %v", got, want)
	}
}
