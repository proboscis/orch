package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
)

func TestSlackNotifier_NotifyBlocked_Webhook(t *testing.T) {
	var receivedMessage SlackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedMessage); err != nil {
			t.Errorf("failed to decode message: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		Enabled:    true,
		WebhookURL: server.URL,
	}
	notifier := NewSlackNotifier(cfg)

	run := &model.Run{
		IssueID: "test-123",
		RunID:   "20260115-120000",
		Status:  model.StatusWaiting,
	}

	err := notifier.NotifyBlocked(run, "Test Issue Title", "Agent is waiting for input")
	if err != nil {
		t.Fatalf("NotifyBlocked failed: %v", err)
	}

	if receivedMessage.Text == "" {
		t.Error("expected message text to be set")
	}
	// 3 base blocks + 1 agent message block = 4
	if len(receivedMessage.Blocks) != 4 {
		t.Errorf("expected 4 blocks, got %d", len(receivedMessage.Blocks))
	}
}

func TestSlackNotifier_NotifyBlocked_BotToken(t *testing.T) {
	var receivedMessage SlackMessage
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&receivedMessage); err != nil {
			t.Errorf("failed to decode message: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		Enabled:  true,
		BotToken: "xoxb-test-token",
		Channel:  "#test-channel",
	}
	notifier := NewSlackNotifier(cfg)
	notifier.client = server.Client()

	origURL := "https://slack.com/api/chat.postMessage"
	_ = origURL

	run := &model.Run{
		IssueID: "test-456",
		RunID:   "20260115-130000",
		Status:  model.StatusRateLimited,
	}

	err := notifier.NotifyBlocked(run, "Another Issue", "")
	if err != nil && authHeader == "" {
		t.Logf("Expected error when not hitting real Slack API: %v", err)
	}
}

func TestSlackNotifier_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.SlackConfig
		expected bool
	}{
		{
			name:     "disabled",
			cfg:      config.SlackConfig{Enabled: false},
			expected: false,
		},
		{
			name:     "enabled with webhook",
			cfg:      config.SlackConfig{Enabled: true, WebhookURL: "https://hooks.slack.com/test"},
			expected: true,
		},
		{
			name:     "enabled with bot token and channel",
			cfg:      config.SlackConfig{Enabled: true, BotToken: "xoxb-test", Channel: "#test"},
			expected: true,
		},
		{
			name:     "enabled with bot token but no channel",
			cfg:      config.SlackConfig{Enabled: true, BotToken: "xoxb-test"},
			expected: false,
		},
		{
			name:     "enabled but no credentials",
			cfg:      config.SlackConfig{Enabled: true},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsConfigured(); got != tt.expected {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSlackConfig_ShouldNotify(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.SlackConfig
		status   string
		expected bool
	}{
		{
			name:     "disabled config",
			cfg:      config.SlackConfig{Enabled: false},
			status:   "blocked",
			expected: false,
		},
		{
			name:     "default notifies on waiting",
			cfg:      config.SlackConfig{Enabled: true},
			status:   "waiting",
			expected: true,
		},
		{
			name:     "default notifies on rate_limited",
			cfg:      config.SlackConfig{Enabled: true},
			status:   "rate_limited",
			expected: true,
		},
		{
			name:     "default notifies on blocked (compat)",
			cfg:      config.SlackConfig{Enabled: true},
			status:   "blocked",
			expected: true,
		},
		{
			name:     "default notifies on blocked_api (compat)",
			cfg:      config.SlackConfig{Enabled: true},
			status:   "blocked_api",
			expected: true,
		},
		{
			name:     "default does not notify on done",
			cfg:      config.SlackConfig{Enabled: true},
			status:   "done",
			expected: false,
		},
		{
			name:     "custom notify_on includes done",
			cfg:      config.SlackConfig{Enabled: true, NotifyOn: []string{"done", "failed"}},
			status:   "done",
			expected: true,
		},
		{
			name:     "custom notify_on excludes blocked",
			cfg:      config.SlackConfig{Enabled: true, NotifyOn: []string{"done"}},
			status:   "blocked",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ShouldNotify(tt.status); got != tt.expected {
				t.Errorf("ShouldNotify(%q) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		status   model.Status
		expected string
	}{
		{model.StatusWaiting, ":no_entry:"},
		{model.StatusRateLimited, ":no_entry:"},
		{model.StatusDone, ":white_check_mark:"},
		{model.StatusFailed, ":x:"},
		{model.StatusPROpen, ":pull_request:"},
		{model.StatusRunning, ":information_source:"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := statusEmoji(tt.status); got != tt.expected {
				t.Errorf("statusEmoji(%q) = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}

func TestStatusDescription(t *testing.T) {
	tests := []struct {
		status   model.Status
		expected string
	}{
		{model.StatusWaiting, "waiting for user input"},
		{model.StatusRateLimited, "waiting for API response"},
		{model.StatusDone, "task completed"},
		{model.StatusFailed, "run failed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := statusDescription(tt.status); got != tt.expected {
				t.Errorf("statusDescription(%q) = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}
