package daemon

import (
	"testing"
	"time"

	orchpb "github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/xdg"
)

func TestProtoBranchStateToString(t *testing.T) {
	tests := []struct {
		name  string
		state orchpb.BranchState
		want  string
	}{
		{
			name:  "clean state",
			state: orchpb.BranchState_BRANCH_STATE_CLEAN,
			want:  "clean",
		},
		{
			name:  "dirty state",
			state: orchpb.BranchState_BRANCH_STATE_DIRTY,
			want:  "dirty",
		},
		{
			name:  "merged state",
			state: orchpb.BranchState_BRANCH_STATE_MERGED,
			want:  "merged",
		},
		{
			name:  "conflict state",
			state: orchpb.BranchState_BRANCH_STATE_CONFLICT,
			want:  "conflict",
		},
		{
			name:  "unspecified state returns empty",
			state: orchpb.BranchState_BRANCH_STATE_UNSPECIFIED,
			want:  "",
		},
		{
			name:  "invalid state returns empty",
			state: orchpb.BranchState(999),
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := protoBranchStateToString(tt.state)
			if got != tt.want {
				t.Errorf("protoBranchStateToString(%v) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestGetAttachInfoResponseMapping(t *testing.T) {
	tests := []struct {
		name     string
		proto    *orchpb.GetAttachInfoResponse
		wantResp *GetAttachInfoResponse
	}{
		{
			name: "maps OpenCode fields correctly",
			proto: &orchpb.GetAttachInfoResponse{
				IssueId:           "orch-123",
				RunId:             "20260130-120000",
				Agent:             "opencode",
				SessionName:       "run-orch-123",
				Multiplexer:       orchpb.Multiplexer_MULTIPLEXER_TMUX,
				WorktreePath:      "/path/to/worktree",
				ServerPort:        4097,
				OpencodeSessionId: "ses_abc123",
				TargetHost:        "user@mac",
			},
			wantResp: &GetAttachInfoResponse{
				IssueID:           "orch-123",
				RunID:             "20260130-120000",
				Agent:             "opencode",
				SessionName:       "run-orch-123",
				Multiplexer:       "tmux",
				WorktreePath:      "/path/to/worktree",
				ServerPort:        4097,
				OpenCodeSessionID: "ses_abc123",
				TargetHost:        "user@mac",
			},
		},
		{
			name: "maps tmux run fields correctly",
			proto: &orchpb.GetAttachInfoResponse{
				IssueId:      "orch-456",
				RunId:        "20260130-130000",
				Agent:        "claude",
				SessionName:  "run-orch-456",
				Multiplexer:  orchpb.Multiplexer_MULTIPLEXER_TMUX,
				WorktreePath: "/path/to/worktree2",
				ServerPort:   0,
			},
			wantResp: &GetAttachInfoResponse{
				IssueID:      "orch-456",
				RunID:        "20260130-130000",
				Agent:        "claude",
				SessionName:  "run-orch-456",
				Multiplexer:  "tmux",
				WorktreePath: "/path/to/worktree2",
				ServerPort:   0,
			},
		},
		{
			name: "handles zellij multiplexer",
			proto: &orchpb.GetAttachInfoResponse{
				Multiplexer: orchpb.Multiplexer_MULTIPLEXER_ZELLIJ,
			},
			wantResp: &GetAttachInfoResponse{
				Multiplexer: "zellij",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &GetAttachInfoResponse{
				IssueID:           tt.proto.IssueId,
				RunID:             tt.proto.RunId,
				Agent:             tt.proto.Agent,
				SessionName:       tt.proto.SessionName,
				Multiplexer:       protoMultiplexerToString(tt.proto.Multiplexer),
				WorktreePath:      tt.proto.WorktreePath,
				ServerPort:        int(tt.proto.ServerPort),
				OpenCodeSessionID: tt.proto.OpencodeSessionId,
				TargetHost:        tt.proto.TargetHost,
			}

			if got.IssueID != tt.wantResp.IssueID {
				t.Errorf("IssueID = %q, want %q", got.IssueID, tt.wantResp.IssueID)
			}
			if got.RunID != tt.wantResp.RunID {
				t.Errorf("RunID = %q, want %q", got.RunID, tt.wantResp.RunID)
			}
			if got.Agent != tt.wantResp.Agent {
				t.Errorf("Agent = %q, want %q", got.Agent, tt.wantResp.Agent)
			}
			if got.SessionName != tt.wantResp.SessionName {
				t.Errorf("SessionName = %q, want %q", got.SessionName, tt.wantResp.SessionName)
			}
			if got.Multiplexer != tt.wantResp.Multiplexer {
				t.Errorf("Multiplexer = %q, want %q", got.Multiplexer, tt.wantResp.Multiplexer)
			}
			if got.WorktreePath != tt.wantResp.WorktreePath {
				t.Errorf("WorktreePath = %q, want %q", got.WorktreePath, tt.wantResp.WorktreePath)
			}
			if got.ServerPort != tt.wantResp.ServerPort {
				t.Errorf("ServerPort = %d, want %d", got.ServerPort, tt.wantResp.ServerPort)
			}
			if got.OpenCodeSessionID != tt.wantResp.OpenCodeSessionID {
				t.Errorf("OpenCodeSessionID = %q, want %q", got.OpenCodeSessionID, tt.wantResp.OpenCodeSessionID)
			}
			if got.TargetHost != tt.wantResp.TargetHost {
				t.Errorf("TargetHost = %q, want %q", got.TargetHost, tt.wantResp.TargetHost)
			}
		})
	}
}

func TestCaptureSessionResponseMapping(t *testing.T) {
	tests := []struct {
		name  string
		proto *orchpb.CaptureSessionResponse
		want  *CaptureSessionResponse
	}{
		{
			name: "maps all fields",
			proto: &orchpb.CaptureSessionResponse{
				Content:       "captured content here",
				TimestampUnix: 1706600000,
				Source:        "tmux",
			},
			want: &CaptureSessionResponse{
				Content:   "captured content here",
				Timestamp: 1706600000,
				Source:    "tmux",
			},
		},
		{
			name: "handles empty content",
			proto: &orchpb.CaptureSessionResponse{
				Content:       "",
				TimestampUnix: 0,
				Source:        "",
			},
			want: &CaptureSessionResponse{
				Content:   "",
				Timestamp: 0,
				Source:    "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &CaptureSessionResponse{
				Content:   tt.proto.Content,
				Timestamp: tt.proto.TimestampUnix,
				Source:    tt.proto.Source,
			}

			if got.Content != tt.want.Content {
				t.Errorf("Content = %q, want %q", got.Content, tt.want.Content)
			}
			if got.Timestamp != tt.want.Timestamp {
				t.Errorf("Timestamp = %d, want %d", got.Timestamp, tt.want.Timestamp)
			}
			if got.Source != tt.want.Source {
				t.Errorf("Source = %q, want %q", got.Source, tt.want.Source)
			}
		})
	}
}

func TestGetControlAgentConfigResponseMapping(t *testing.T) {
	proto := &orchpb.GetControlAgentConfigResponse{
		PromptContent: "control prompt content",
		Agent:         "opencode",
		Model:         "anthropic/claude-opus-4-1",
		ModelVariant:  "max",
		ExtraArgs:     []string{"--foo", "bar"},
	}

	got := &GetControlAgentConfigResponse{
		OK:            true,
		PromptContent: proto.PromptContent,
		Agent:         proto.Agent,
		Model:         proto.Model,
		ModelVariant:  proto.ModelVariant,
		ExtraArgs:     proto.ExtraArgs,
	}

	if !got.OK {
		t.Fatalf("OK = false, want true")
	}
	if got.PromptContent != "control prompt content" {
		t.Fatalf("PromptContent = %q, want %q", got.PromptContent, "control prompt content")
	}
	if got.Agent != "opencode" {
		t.Fatalf("Agent = %q, want %q", got.Agent, "opencode")
	}
	if got.Model != "anthropic/claude-opus-4-1" {
		t.Fatalf("Model = %q, want %q", got.Model, "anthropic/claude-opus-4-1")
	}
	if got.ModelVariant != "max" {
		t.Fatalf("ModelVariant = %q, want %q", got.ModelVariant, "max")
	}
	if len(got.ExtraArgs) != 2 || got.ExtraArgs[0] != "--foo" || got.ExtraArgs[1] != "bar" {
		t.Fatalf("ExtraArgs = %v, want [--foo bar]", got.ExtraArgs)
	}
}

func TestGetDiffStatsResponseMapping(t *testing.T) {
	tests := []struct {
		name  string
		proto *orchpb.DiffStats
		want  *GetDiffStatsResponse
	}{
		{
			name: "maps all fields",
			proto: &orchpb.DiffStats{
				Additions:    100,
				Deletions:    50,
				FilesChanged: 10,
				Files:        []string{"file1.go", "file2.go"},
			},
			want: &GetDiffStatsResponse{
				Additions:    100,
				Deletions:    50,
				FilesChanged: 10,
				Files:        []string{"file1.go", "file2.go"},
			},
		},
		{
			name: "handles zero values",
			proto: &orchpb.DiffStats{
				Additions:    0,
				Deletions:    0,
				FilesChanged: 0,
				Files:        nil,
			},
			want: &GetDiffStatsResponse{
				Additions:    0,
				Deletions:    0,
				FilesChanged: 0,
				Files:        nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &GetDiffStatsResponse{
				Additions:    int(tt.proto.Additions),
				Deletions:    int(tt.proto.Deletions),
				FilesChanged: int(tt.proto.FilesChanged),
				Files:        tt.proto.Files,
			}

			if got.Additions != tt.want.Additions {
				t.Errorf("Additions = %d, want %d", got.Additions, tt.want.Additions)
			}
			if got.Deletions != tt.want.Deletions {
				t.Errorf("Deletions = %d, want %d", got.Deletions, tt.want.Deletions)
			}
			if got.FilesChanged != tt.want.FilesChanged {
				t.Errorf("FilesChanged = %d, want %d", got.FilesChanged, tt.want.FilesChanged)
			}
			if len(got.Files) != len(tt.want.Files) {
				t.Errorf("Files length = %d, want %d", len(got.Files), len(tt.want.Files))
			}
		})
	}
}

func TestSendMessageTimeoutMaintainsSafetyMargin(t *testing.T) {
	client := NewProtoClient("/tmp/project")

	minimum := openCodeSendAckTimeout + protoSendMessageTimeoutBuffer

	client.SetTimeout(2 * time.Second)
	if got := client.sendMessageTimeout(); got != minimum {
		t.Fatalf("sendMessageTimeout() = %s, want %s", got, minimum)
	}

	client.SetTimeout(minimum + 2*time.Second)
	if got := client.sendMessageTimeout(); got != minimum+2*time.Second {
		t.Fatalf("sendMessageTimeout() = %s, want %s", got, minimum+2*time.Second)
	}
}

func TestProtoClientDialTarget(t *testing.T) {
	tests := []struct {
		name        string
		daemonAddr  string
		wantNetwork string
		wantAddress string
		wantErr     bool
	}{
		{
			name:        "empty uses unix socket",
			daemonAddr:  "",
			wantNetwork: "unix",
			wantAddress: xdg.SocketPath(),
		},
		{
			name:        "tcp scheme",
			daemonAddr:  "tcp://127.0.0.1:7777",
			wantNetwork: "tcp",
			wantAddress: "127.0.0.1:7777",
		},
		{
			name:        "bare host port",
			daemonAddr:  "127.0.0.1:7777",
			wantNetwork: "tcp",
			wantAddress: "127.0.0.1:7777",
		},
		{
			name:        "unix scheme",
			daemonAddr:  "unix:///tmp/orch.sock",
			wantNetwork: "unix",
			wantAddress: "/tmp/orch.sock",
		},
		{
			name:       "unsupported scheme",
			daemonAddr: "http://127.0.0.1:7777",
			wantErr:    true,
		},
		{
			name:       "empty tcp target",
			daemonAddr: "tcp://",
			wantErr:    true,
		},
		{
			name:       "empty unix target",
			daemonAddr: "unix://",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewProtoClientWithAddress("/tmp/project", tt.daemonAddr)

			network, address, err := client.dialTarget()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("dialTarget() error = nil, want non-nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("dialTarget() error = %v", err)
			}
			if network != tt.wantNetwork {
				t.Fatalf("network = %q, want %q", network, tt.wantNetwork)
			}
			if address != tt.wantAddress {
				t.Fatalf("address = %q, want %q", address, tt.wantAddress)
			}
		})
	}
}
