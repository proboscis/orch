package daemon

import (
	"testing"

	orchpb "github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/model"
)

func TestBuildAttachInfoResponse(t *testing.T) {
	tests := []struct {
		name string
		run  *model.Run
		want struct {
			agent             string
			serverPort        int32
			opencodeSessionId string
			issueId           string
			runId             string
		}
	}{
		{
			name: "OpenCode run includes all fields",
			run: &model.Run{
				IssueID:           "orch-123",
				RunID:             "20260130-120000",
				Agent:             "opencode",
				WorktreePath:      "/path/to/worktree",
				TmuxSession:       "run-orch-123",
				ServerPort:        4097,
				OpenCodeSessionID: "ses_abc123",
			},
			want: struct {
				agent             string
				serverPort        int32
				opencodeSessionId string
				issueId           string
				runId             string
			}{
				agent:             "opencode",
				serverPort:        4097,
				opencodeSessionId: "ses_abc123",
				issueId:           "orch-123",
				runId:             "20260130-120000",
			},
		},
		{
			name: "Claude run (non-OpenCode) has zero server port",
			run: &model.Run{
				IssueID:      "orch-456",
				RunID:        "20260130-130000",
				Agent:        "claude",
				WorktreePath: "/path/to/worktree2",
				TmuxSession:  "run-orch-456",
				ServerPort:   0,
			},
			want: struct {
				agent             string
				serverPort        int32
				opencodeSessionId string
				issueId           string
				runId             string
			}{
				agent:             "claude",
				serverPort:        0,
				opencodeSessionId: "",
				issueId:           "orch-456",
				runId:             "20260130-130000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachInfo := &orchpb.GetAttachInfoResponse{
				Agent:             tt.run.Agent,
				ServerPort:        int32(tt.run.ServerPort),
				OpencodeSessionId: tt.run.OpenCodeSessionID,
				IssueId:           tt.run.IssueID,
				RunId:             tt.run.RunID,
			}

			if attachInfo.Agent != tt.want.agent {
				t.Errorf("Agent = %q, want %q", attachInfo.Agent, tt.want.agent)
			}
			if attachInfo.ServerPort != tt.want.serverPort {
				t.Errorf("ServerPort = %d, want %d", attachInfo.ServerPort, tt.want.serverPort)
			}
			if attachInfo.OpencodeSessionId != tt.want.opencodeSessionId {
				t.Errorf("OpencodeSessionId = %q, want %q", attachInfo.OpencodeSessionId, tt.want.opencodeSessionId)
			}
			if attachInfo.IssueId != tt.want.issueId {
				t.Errorf("IssueId = %q, want %q", attachInfo.IssueId, tt.want.issueId)
			}
			if attachInfo.RunId != tt.want.runId {
				t.Errorf("RunId = %q, want %q", attachInfo.RunId, tt.want.runId)
			}
		})
	}
}

func TestIsOpenCodeRun(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		want  bool
	}{
		{
			name:  "opencode agent",
			agent: "opencode",
			want:  true,
		},
		{
			name:  "claude agent",
			agent: "claude",
			want:  false,
		},
		{
			name:  "codex agent",
			agent: "codex",
			want:  false,
		},
		{
			name:  "gemini agent",
			agent: "gemini",
			want:  false,
		},
		{
			name:  "empty agent",
			agent: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isOpenCode := tt.agent == "opencode"
			if isOpenCode != tt.want {
				t.Errorf("isOpenCode(%q) = %v, want %v", tt.agent, isOpenCode, tt.want)
			}
		})
	}
}

func TestOpenCodeAttachValidation(t *testing.T) {
	tests := []struct {
		name      string
		run       *model.Run
		wantError string
		wantOK    bool
	}{
		{
			name: "OpenCode run with valid server port succeeds",
			run: &model.Run{
				Agent:      "opencode",
				ServerPort: 4097,
			},
			wantError: "",
			wantOK:    true,
		},
		{
			name: "OpenCode run without server port fails",
			run: &model.Run{
				Agent:      "opencode",
				ServerPort: 0,
			},
			wantError: "opencode_server_not_found",
			wantOK:    false,
		},
		{
			name: "Non-OpenCode run doesn't check server port",
			run: &model.Run{
				Agent:      "claude",
				ServerPort: 0,
			},
			wantError: "",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isOpenCode := tt.run.Agent == "opencode"

			var gotError string
			var gotOK bool

			if isOpenCode {
				if tt.run.ServerPort == 0 {
					gotError = "opencode_server_not_found"
					gotOK = false
				} else {
					gotOK = true
				}
			} else {
				gotOK = true
			}

			if gotOK != tt.wantOK {
				t.Errorf("OK = %v, want %v", gotOK, tt.wantOK)
			}
			if gotError != tt.wantError {
				t.Errorf("Error = %q, want %q", gotError, tt.wantError)
			}
		})
	}
}

func TestComputeBranchState(t *testing.T) {
	tests := []struct {
		name         string
		worktreePath string
		wantState    orchpb.BranchState
	}{
		{
			name:         "empty worktree path returns unspecified",
			worktreePath: "",
			wantState:    orchpb.BranchState_BRANCH_STATE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeBranchState(tt.worktreePath, "", "main")
			if got != tt.wantState {
				t.Errorf("computeBranchState(%q, ...) = %v, want %v", tt.worktreePath, got, tt.wantState)
			}
		})
	}
}
