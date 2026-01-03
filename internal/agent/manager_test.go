package agent

import (
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestGetManager(t *testing.T) {
	tests := []struct {
		name     string
		run      *model.Run
		wantType string
	}{
		{
			name: "opencode run returns OpenCodeManager",
			run: &model.Run{
				Agent:             "opencode",
				OpenCodeSessionID: "ses_123",
				ServerPort:        4321,
			},
			wantType: "*agent.OpenCodeManager",
		},
		{
			name: "claude run returns TmuxManager",
			run: &model.Run{
				Agent:       "claude",
				TmuxSession: "orch-test-001",
			},
			wantType: "*agent.TmuxManager",
		},
		{
			name: "opencode run missing session ID falls back to TmuxManager",
			run: &model.Run{
				Agent:             "opencode",
				OpenCodeSessionID: "",
				ServerPort:        4321,
			},
			wantType: "*agent.TmuxManager",
		},
		{
			name: "opencode run missing server port falls back to TmuxManager",
			run: &model.Run{
				Agent:             "opencode",
				OpenCodeSessionID: "ses_123",
				ServerPort:        0,
			},
			wantType: "*agent.TmuxManager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := GetManager(tt.run)
			got := "*agent." + typeName(manager)
			if got != tt.wantType {
				t.Errorf("GetManager() = %v, want %v", got, tt.wantType)
			}
		})
	}
}

func typeName(v interface{}) string {
	switch v.(type) {
	case *TmuxManager:
		return "TmuxManager"
	case *OpenCodeManager:
		return "OpenCodeManager"
	default:
		return "unknown"
	}
}

func TestGetSessionName(t *testing.T) {
	tests := []struct {
		name                string
		run                 *model.Run
		wantSessionContains string
	}{
		{
			name: "uses TmuxSession if set",
			run: &model.Run{
				IssueID:     "issue-1",
				RunID:       "run-1",
				TmuxSession: "custom-session",
			},
			wantSessionContains: "custom-session",
		},
		{
			name: "generates session from IssueID and RunID",
			run: &model.Run{
				IssueID: "issue-1",
				RunID:   "run-1",
			},
			wantSessionContains: "issue-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSessionName(tt.run)
			if got == "" {
				t.Error("getSessionName() returned empty string")
			}
			if tt.wantSessionContains != "" && got != tt.wantSessionContains && !containsSubstring(got, tt.wantSessionContains) {
				t.Errorf("getSessionName() = %v, want substring %v", got, tt.wantSessionContains)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestIsWaitingForInput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "claude prompt",
			output: "Some output\n↵ send\nMore output",
			want:   true,
		},
		{
			name:   "claude shortcuts prompt",
			output: "? for shortcuts",
			want:   true,
		},
		{
			name:   "accept edits prompt",
			output: "accept edits",
			want:   true,
		},
		{
			name:   "bypass permissions",
			output: "bypass permissions",
			want:   true,
		},
		{
			name:   "opencode type message",
			output: "Type your message",
			want:   true,
		},
		{
			name:   "opencode ctrl+s send",
			output: "ctrl+s send",
			want:   true,
		},
		{
			name:   "opencode ctrl+c interrupt",
			output: "ctrl+c interrupt",
			want:   true,
		},
		{
			name:   "no prompt",
			output: "Just some regular output\nwith no prompts",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsWaitingForInput(tt.output)
			if got != tt.want {
				t.Errorf("IsWaitingForInput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAgentExited(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "shell prompt with $",
			output: "some output\nuser@host:~/project$ ",
			want:   true,
		},
		{
			name:   "shell prompt with %",
			output: "some output\nuser@host % ",
			want:   true,
		},
		{
			name:   "zsh git prompt",
			output: "some output\n➜ project git:(main) ",
			want:   true,
		},
		{
			name:   "powerlevel prompt",
			output: "some output\nproject ❯ ",
			want:   true,
		},
		{
			name:   "claude still running",
			output: "Working on task...\n↵ send",
			want:   false,
		},
		{
			name:   "opencode still running",
			output: "Processing...\nctrl+s send",
			want:   false,
		},
		{
			name:   "opencode server output",
			output: "opencode server listening on port 4321",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAgentExited(tt.output)
			if got != tt.want {
				t.Errorf("IsAgentExited() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCompleted(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "task completed",
			output: "All done!\nTask completed successfully",
			want:   true,
		},
		{
			name:   "all tasks completed",
			output: "Summary:\nAll tasks completed",
			want:   true,
		},
		{
			name:   "session ended",
			output: "Goodbye!\nSession ended",
			want:   true,
		},
		{
			name:   "goodbye",
			output: "Thanks for using me!\nGoodbye",
			want:   true,
		},
		{
			name:   "not completed",
			output: "Still working on something...",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCompleted(tt.output)
			if got != tt.want {
				t.Errorf("IsCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAPILimited(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "cost limit",
			output: "Error: Cost limit reached. Please upgrade.",
			want:   true,
		},
		{
			name:   "rate limit exceeded",
			output: "Rate limit exceeded. Try again later.",
			want:   true,
		},
		{
			name:   "quota exceeded",
			output: "Quota exceeded for this month.",
			want:   true,
		},
		{
			name:   "insufficient quota",
			output: "Error: Insufficient quota",
			want:   true,
		},
		{
			name:   "resource exhausted",
			output: "Resource exhausted. Please wait.",
			want:   true,
		},
		{
			name:   "rate limit options link",
			output: "Visit /rate-limit-options for more info",
			want:   true,
		},
		{
			name:   "normal output",
			output: "Working on your request...",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAPILimited(tt.output)
			if got != tt.want {
				t.Errorf("IsAPILimited() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFailed(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "fatal error",
			output: "Fatal error: cannot proceed",
			want:   true,
		},
		{
			name:   "unrecoverable error",
			output: "Unrecoverable error occurred",
			want:   true,
		},
		{
			name:   "agent crashed",
			output: "Agent crashed unexpectedly",
			want:   true,
		},
		{
			name:   "session terminated",
			output: "Session terminated due to error",
			want:   true,
		},
		{
			name:   "auth failed",
			output: "Authentication failed. Please check credentials.",
			want:   true,
		},
		{
			name:   "normal error (not fatal)",
			output: "Error: file not found\nRetrying...",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFailed(tt.output)
			if got != tt.want {
				t.Errorf("IsFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTmuxManagerGetStatus(t *testing.T) {
	manager := &TmuxManager{SessionName: "test-session"}
	run := &model.Run{RunID: "test-run"}

	tests := []struct {
		name          string
		output        string
		outputChanged bool
		hasPrompt     bool
		want          model.Status
	}{
		{
			name:          "agent exited",
			output:        "user@host:~/project$ ",
			outputChanged: false,
			hasPrompt:     false,
			want:          model.StatusUnknown,
		},
		{
			name:          "completed",
			output:        "Task completed successfully",
			outputChanged: false,
			hasPrompt:     false,
			want:          model.StatusDone,
		},
		{
			name:          "api limited",
			output:        "Rate limit exceeded",
			outputChanged: false,
			hasPrompt:     false,
			want:          model.StatusBlockedAPI,
		},
		{
			name:          "failed",
			output:        "Fatal error occurred",
			outputChanged: false,
			hasPrompt:     false,
			want:          model.StatusFailed,
		},
		{
			name:          "output changed = running",
			output:        "Working...",
			outputChanged: true,
			hasPrompt:     false,
			want:          model.StatusRunning,
		},
		{
			name:          "has prompt = blocked",
			output:        "Regular output",
			outputChanged: false,
			hasPrompt:     true,
			want:          model.StatusBlocked,
		},
		{
			name:          "no change no prompt = empty",
			output:        "Regular output",
			outputChanged: false,
			hasPrompt:     false,
			want:          "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &RunState{}
			got := manager.GetStatus(run, tt.output, state, tt.outputChanged, tt.hasPrompt)
			if got != tt.want {
				t.Errorf("GetStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenCodeManagerGetStatus(t *testing.T) {
	manager := &OpenCodeManager{Port: 4321, SessionID: "ses_123"}

	tests := []struct {
		name       string
		runStatus  model.Status
		wantStatus model.Status
	}{
		{
			name:       "booting becomes running",
			runStatus:  model.StatusBooting,
			wantStatus: model.StatusRunning,
		},
		{
			name:       "queued becomes running",
			runStatus:  model.StatusQueued,
			wantStatus: model.StatusRunning,
		},
		{
			name:       "running stays empty (no change)",
			runStatus:  model.StatusRunning,
			wantStatus: "",
		},
		{
			name:       "blocked stays empty (no change)",
			runStatus:  model.StatusBlocked,
			wantStatus: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &model.Run{Status: tt.runStatus}
			state := &RunState{}
			got := manager.GetStatus(run, "", state, false, false)
			if got != tt.wantStatus {
				t.Errorf("GetStatus() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestTmuxManagerDeadStatus(t *testing.T) {
	manager := &TmuxManager{SessionName: "test-session"}
	got := manager.DeadStatus()
	if got != model.StatusFailed {
		t.Errorf("TmuxManager.DeadStatus() = %v, want %v", got, model.StatusFailed)
	}
}

func TestOpenCodeManagerDeadStatus(t *testing.T) {
	manager := &OpenCodeManager{Port: 4321, SessionID: "ses_123"}
	got := manager.DeadStatus()
	if got != model.StatusUnknown {
		t.Errorf("OpenCodeManager.DeadStatus() = %v, want %v", got, model.StatusUnknown)
	}
}
