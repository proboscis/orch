package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
			name: "opencode run missing session ID still returns OpenCodeManager",
			run: &model.Run{
				Agent:             "opencode",
				OpenCodeSessionID: "",
				ServerPort:        4321,
			},
			wantType: "*agent.OpenCodeManager",
		},
		{
			name: "opencode run missing server port still returns OpenCodeManager",
			run: &model.Run{
				Agent:             "opencode",
				OpenCodeSessionID: "ses_123",
				ServerPort:        0,
			},
			wantType: "*agent.OpenCodeManager",
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

func TestOpenCodeManagerGetStatusBootingQueued(t *testing.T) {
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

func TestOpenCodeManagerGetStatusFromAPI(t *testing.T) {
	tests := []struct {
		name          string
		sessionStatus SessionStatus
		wantStatus    model.Status
	}{
		{
			name:          "busy session returns running",
			sessionStatus: SessionStatusBusy,
			wantStatus:    model.StatusRunning,
		},
		{
			name:          "idle session returns blocked",
			sessionStatus: SessionStatusIdle,
			wantStatus:    model.StatusBlocked,
		},
		{
			name:          "retry session returns blocked_api",
			sessionStatus: SessionStatusRetry,
			wantStatus:    model.StatusBlockedAPI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/session/status" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]SessionStatus{
						"ses_test123": tt.sessionStatus,
					})
				}
			}))
			defer server.Close()

			port := extractPort(server.URL)
			manager := &OpenCodeManager{Port: port, SessionID: "ses_test123", Directory: "/test"}

			run := &model.Run{Status: model.StatusRunning}
			state := &RunState{}
			got := manager.GetStatus(run, "", state, false, false)
			if got != tt.wantStatus {
				t.Errorf("GetStatus() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestOpenCodeManagerGetStatusSessionIdleInStatusMap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session/status":
			json.NewEncoder(w).Encode(map[string]SessionStatus{
				"ses_idle": SessionStatusIdle,
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{Port: port, SessionID: "ses_idle", Directory: "/test"}

	run := &model.Run{Status: model.StatusRunning}
	state := &RunState{}
	got := manager.GetStatus(run, "", state, false, false)
	if got != model.StatusBlocked {
		t.Errorf("GetStatus() for idle session = %v, want %v", got, model.StatusBlocked)
	}
}

func TestOpenCodeManagerGetStatusSessionDoesNotExist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session/status":
			json.NewEncoder(w).Encode(map[string]SessionStatus{})
		case "/session":
			json.NewEncoder(w).Encode([]Session{})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{Port: port, SessionID: "ses_missing", Directory: "/test"}

	run := &model.Run{Status: model.StatusRunning}
	state := &RunState{}
	got := manager.GetStatus(run, "", state, false, false)
	if got != model.StatusUnknown {
		t.Errorf("GetStatus() for non-existent session = %v, want %v", got, model.StatusUnknown)
	}
}

func extractPort(url string) int {
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == ':' {
			port, _ := strconv.Atoi(url[i+1:])
			return port
		}
	}
	return 0
}

func TestFormatOpenCodeMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		maxLines int
		wantLen  int
	}{
		{
			name:     "empty messages",
			messages: []Message{},
			maxLines: 10,
			wantLen:  0,
		},
		{
			name: "single message",
			messages: []Message{
				{
					Info:  MessageInfo{ID: "msg1", Role: "user"},
					Parts: []MessagePart{{Type: "text", Text: "Hello world"}},
				},
			},
			maxLines: 10,
			wantLen:  2,
		},
		{
			name: "multiple messages",
			messages: []Message{
				{
					Info:  MessageInfo{ID: "msg1", Role: "user"},
					Parts: []MessagePart{{Type: "text", Text: "Hello"}},
				},
				{
					Info:  MessageInfo{ID: "msg2", Role: "assistant"},
					Parts: []MessagePart{{Type: "text", Text: "Hi there!"}},
				},
			},
			maxLines: 10,
			wantLen:  4,
		},
		{
			name: "non-text parts ignored",
			messages: []Message{
				{
					Info: MessageInfo{ID: "msg1", Role: "assistant"},
					Parts: []MessagePart{
						{Type: "image", Text: ""},
						{Type: "text", Text: "Only this text"},
					},
				},
			},
			maxLines: 10,
			wantLen:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatOpenCodeMessages(tt.messages, tt.maxLines)

			if len(tt.messages) == 0 {
				if result != "" {
					t.Errorf("expected empty string for empty messages, got %q", result)
				}
				return
			}

			if result == "" && tt.wantLen > 0 {
				t.Errorf("expected non-empty result")
			}
		})
	}
}

func TestFormatOpenCodeMessagesLineLimit(t *testing.T) {
	messages := []Message{
		{
			Info:  MessageInfo{ID: "msg1", Role: "assistant"},
			Parts: []MessagePart{{Type: "text", Text: "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"}},
		},
	}

	result := FormatOpenCodeMessages(messages, 3)
	lines := countLines(result)

	if lines > 3 {
		t.Errorf("expected at most 3 lines, got %d", lines)
	}
}

func TestFormatOpenCodeMessagesPartOrdering(t *testing.T) {
	messages := []Message{
		{
			Info: MessageInfo{ID: "msg1", Role: "assistant"},
			Parts: []MessagePart{
				{Type: "text", Text: "First part"},
				{Type: "text", Text: "Second part"},
			},
		},
	}

	result := FormatOpenCodeMessages(messages, 100)

	firstIdx := findSubstringIndex(result, "First part")
	secondIdx := findSubstringIndex(result, "Second part")

	if firstIdx == -1 || secondIdx == -1 {
		t.Errorf("expected both parts in result, got %q", result)
		return
	}

	if firstIdx >= secondIdx {
		t.Errorf("expected First part before Second part, got %q", result)
	}
}

func TestFormatOpenCodeMessagesMessageOrdering(t *testing.T) {
	messages := []Message{
		{
			Info:  MessageInfo{ID: "msg1", Role: "user"},
			Parts: []MessagePart{{Type: "text", Text: "User message"}},
		},
		{
			Info:  MessageInfo{ID: "msg2", Role: "assistant"},
			Parts: []MessagePart{{Type: "text", Text: "Assistant message"}},
		},
	}

	result := FormatOpenCodeMessages(messages, 100)

	userIdx := findSubstringIndex(result, "User message")
	assistantIdx := findSubstringIndex(result, "Assistant message")

	if userIdx == -1 || assistantIdx == -1 {
		t.Errorf("expected both messages in result, got %q", result)
		return
	}

	if userIdx >= assistantIdx {
		t.Errorf("expected User message before Assistant message, got %q", result)
	}
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := 1
	for _, c := range s {
		if c == '\n' {
			count++
		}
	}
	return count
}

func findSubstringIndex(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestOpenCodeManagerIsAliveServerNotRunning(t *testing.T) {
	manager := &OpenCodeManager{Port: 59999, SessionID: "ses_test"}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	if got != false {
		t.Errorf("IsAlive() with no server = %v, want false", got)
	}
}

func TestOpenCodeManagerIsAliveSessionExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Return list of sessions including our target session
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_test123"},
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{Port: port, SessionID: "ses_test123", Directory: "/test"}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	if got != true {
		t.Errorf("IsAlive() with existing session = %v, want true", got)
	}
}

func TestOpenCodeManagerIsAliveBusyChildren(t *testing.T) {
	// Note: With orch-315, IsAlive only checks session existence.
	// Busy children are no longer considered for IsAlive (only for GetStatus).
	// Parent session must exist in session list to be considered alive.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Parent session not in list, only child
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_child"},
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{Port: port, SessionID: "ses_parent"}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	// Now expects false: parent session must exist to be alive
	if got != false {
		t.Errorf("IsAlive() with only busy children (no parent session) = %v, want false", got)
	}
}

func TestOpenCodeManagerIsAliveRecentActivity(t *testing.T) {
	// Note: With orch-315, IsAlive only checks session existence.
	// Recent activity alone is no longer sufficient - session must exist in list.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Session exists in list
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_active"},
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{Port: port, SessionID: "ses_active"}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	// Now expects true because session exists in list
	if got != true {
		t.Errorf("IsAlive() with session in list = %v, want true", got)
	}
}

func TestOpenCodeManagerIsAliveNoActivity(t *testing.T) {
	// Session not in list = not alive
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Empty session list - our session doesn't exist
			json.NewEncoder(w).Encode([]Session{})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{Port: port, SessionID: "ses_stale"}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	if got != false {
		t.Errorf("IsAlive() with session not in list = %v, want false", got)
	}
}

// Tests for orch-308: Session detection should work regardless of directory mismatch
// OpenCode scopes sessions by git root, not exact worktree path.
// These tests verify that sessions are found by ID alone, not by directory filter.

func TestOpenCodeManagerSessionExistsIgnoresDirectory(t *testing.T) {
	// Scenario: Session was created with directory "/repo" but daemon queries with "/repo/worktree"
	// The session should still be found because we query all sessions
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Return session list including our target session
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_target", Directory: "/repo"},
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	// Manager has worktree path, but session was created with different directory
	manager := &OpenCodeManager{
		Port:      port,
		SessionID: "ses_target",
		Directory: "/repo/.git-worktrees/issue-123/abc123_run",  // Worktree path
	}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	if got != true {
		t.Errorf("IsAlive() should find session regardless of directory mismatch, got %v", got)
	}
}

func TestOpenCodeManagerGetStatusIgnoresDirectory(t *testing.T) {
	// Verify GetStatus correctly detects idle/busy status despite directory mismatch
	tests := []struct {
		name          string
		sessionStatus SessionStatus
		wantStatus    model.Status
	}{
		{
			name:          "detects idle status (blocked) with directory mismatch",
			sessionStatus: SessionStatusIdle,
			wantStatus:    model.StatusBlocked,
		},
		{
			name:          "detects busy status (running) with directory mismatch",
			sessionStatus: SessionStatusBusy,
			wantStatus:    model.StatusRunning,
		},
		{
			name:          "detects retry status (blocked_api) with directory mismatch",
			sessionStatus: SessionStatusRetry,
			wantStatus:    model.StatusBlockedAPI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/session/status":
					// Verify no directory filter is applied
					dir := r.Header.Get("X-OpenCode-Directory")
					if dir != "" {
						t.Errorf("Expected no X-OpenCode-Directory header, got %q", dir)
					}
					json.NewEncoder(w).Encode(map[string]SessionStatus{
						"ses_mismatched": tt.sessionStatus,
					})
				}
			}))
			defer server.Close()

			port := extractPort(server.URL)
			manager := &OpenCodeManager{
				Port:      port,
				SessionID: "ses_mismatched",
				Directory: "/different/worktree/path",  // Different from session's actual directory
			}

			run := &model.Run{Status: model.StatusRunning}
			state := &RunState{}
			got := manager.GetStatus(run, "", state, false, false)
			if got != tt.wantStatus {
				t.Errorf("GetStatus() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestOpenCodeManagerHasActiveBusyChildrenIgnoresDirectory(t *testing.T) {
	// Note: With orch-315, IsAlive no longer checks for busy children.
	// This test now verifies that parent session must exist to be alive.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Only child session exists, not parent
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_busy_child"},
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{
		Port:      port,
		SessionID: "ses_parent",
		Directory: "/mismatched/worktree/path",
	}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	// Now expects false: parent session must exist in list to be alive
	if got != false {
		t.Errorf("IsAlive() without parent session in list = %v, want false", got)
	}
}

func TestOpenCodeManagerHasRecentActivityIgnoresDirectory(t *testing.T) {
	// Note: With orch-315, IsAlive no longer checks for recent activity.
	// This test now verifies session existence works regardless of directory.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			// Session exists in list
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_recent", Directory: "/different/path"},
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	manager := &OpenCodeManager{
		Port:      port,
		SessionID: "ses_recent",
		Directory: "/wrong/directory/path",
	}
	run := &model.Run{Agent: "opencode"}

	got := manager.IsAlive(run)
	if got != true {
		t.Errorf("IsAlive() should find session regardless of directory mismatch, got %v", got)
	}
}

// TestOpenCodeManagerDirectoryMismatchScenario simulates the exact bug from orch-308
// where daemon couldn't detect blocked status because worktree path didn't match OpenCode's project scope
func TestOpenCodeManagerDirectoryMismatchScenario(t *testing.T) {
	// This test simulates:
	// 1. Session created with git root directory
	// 2. Daemon queries with worktree path (different from git root)
	// 3. Session should still be found and status correctly detected as "blocked" when idle
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		switch r.URL.Path {
		case "/session":
			// Return session list for IsAlive check
			json.NewEncoder(w).Encode([]Session{
				{ID: "ses_orch305", Directory: "/Users/dev/repos/orch"},
			})
		case "/session/status":
			// Verify no directory filter on status check
			dir := r.Header.Get("X-OpenCode-Directory")
			if dir != "" {
				t.Errorf("Request to /session/status should not have X-OpenCode-Directory header, got %q", dir)
			}
			// Agent finished work, session is now idle
			json.NewEncoder(w).Encode(map[string]SessionStatus{
				"ses_orch305": SessionStatusIdle,
			})
		case "/session/ses_orch305":
			json.NewEncoder(w).Encode(Session{
				ID:        "ses_orch305",
				Directory: "/Users/dev/repos/orch",  // Git root (how OpenCode stores it)
			})
		}
	}))
	defer server.Close()

	port := extractPort(server.URL)
	
	// Daemon uses worktree path (different from session's stored directory)
	manager := &OpenCodeManager{
		Port:      port,
		SessionID: "ses_orch305",
		Directory: "/Users/dev/repos/orch/.git-worktrees/orch-305/5f7fe7_run",  // Worktree path
		RunRef:    "orch-305#20260119-002608",
	}

	// Test IsAlive - should find the session
	run := &model.Run{Agent: "opencode", Status: model.StatusRunning}
	alive := manager.IsAlive(run)
	if !alive {
		t.Error("IsAlive() should return true - session exists despite directory mismatch")
	}

	// Test GetStatus - should detect idle status as blocked
	state := &RunState{}
	status := manager.GetStatus(run, "", state, false, false)
	if status != model.StatusBlocked {
		t.Errorf("GetStatus() = %v, want %v (blocked when agent is idle)", status, model.StatusBlocked)
	}
}
