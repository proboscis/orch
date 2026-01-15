package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
)

type SlackNotifier struct {
	config *config.SlackConfig
	client *http.Client
}

func NewSlackNotifier(cfg *config.SlackConfig) *SlackNotifier {
	return &SlackNotifier{
		config: cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type SlackMessage struct {
	Text        string       `json:"text"`
	Blocks      []SlackBlock `json:"blocks,omitempty"`
	Channel     string       `json:"channel,omitempty"`
	UnfurlLinks bool         `json:"unfurl_links"`
}

type SlackBlock struct {
	Type string          `json:"type"`
	Text *SlackBlockText `json:"text,omitempty"`
}

type SlackBlockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *SlackNotifier) NotifyBlocked(run *model.Run, issueTitle string) error {
	if !s.config.IsConfigured() {
		return nil
	}

	emoji := statusEmoji(run.Status)
	statusText := statusDescription(run.Status)

	text := fmt.Sprintf("%s Run blocked: %s#%s", emoji, run.IssueID, run.ShortID())

	attachCmd := fmt.Sprintf("orch attach %s#%s", run.IssueID, run.RunID)

	blocks := []SlackBlock{
		{
			Type: "section",
			Text: &SlackBlockText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*%s Run blocked: %s#%s*", emoji, run.IssueID, run.ShortID()),
			},
		},
		{
			Type: "section",
			Text: &SlackBlockText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Issue:* %s\n*Status:* %s (%s)", issueTitle, run.Status, statusText),
			},
		},
		{
			Type: "section",
			Text: &SlackBlockText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Attach:* `%s`", attachCmd),
			},
		},
	}

	msg := SlackMessage{
		Text:        text,
		Blocks:      blocks,
		UnfurlLinks: false,
	}

	if s.config.BotToken != "" && s.config.Channel != "" {
		msg.Channel = s.config.Channel
		return s.sendBotMessage(msg)
	}

	return s.sendWebhookMessage(msg)
}

func (s *SlackNotifier) NotifyStatusChange(run *model.Run, issueTitle string, newStatus model.Status) error {
	if !s.config.IsConfigured() {
		return nil
	}

	emoji := statusEmoji(newStatus)
	text := fmt.Sprintf("%s Run %s: %s#%s - %s", emoji, newStatus, run.IssueID, run.ShortID(), issueTitle)

	msg := SlackMessage{
		Text:        text,
		UnfurlLinks: false,
	}

	if s.config.BotToken != "" && s.config.Channel != "" {
		msg.Channel = s.config.Channel
		return s.sendBotMessage(msg)
	}

	return s.sendWebhookMessage(msg)
}

func (s *SlackNotifier) sendWebhookMessage(msg SlackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	resp, err := s.client.Post(s.config.WebhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to send slack webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack webhook returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (s *SlackNotifier) sendBotMessage(msg SlackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	req, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.BotToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send slack message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode slack response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}

	return nil
}

func statusEmoji(status model.Status) string {
	switch status {
	case model.StatusBlocked, model.StatusBlockedAPI:
		return ":no_entry:"
	case model.StatusDone:
		return ":white_check_mark:"
	case model.StatusFailed:
		return ":x:"
	case model.StatusPROpen:
		return ":pull_request:"
	default:
		return ":information_source:"
	}
}

func statusDescription(status model.Status) string {
	switch status {
	case model.StatusBlocked:
		return "waiting for user input"
	case model.StatusBlockedAPI:
		return "waiting for API response"
	case model.StatusDone:
		return "task completed"
	case model.StatusFailed:
		return "run failed"
	case model.StatusPROpen:
		return "PR created, awaiting review"
	default:
		return string(status)
	}
}

// SendTest sends a test notification to verify Slack is configured correctly.
// Returns the channel name used (for success messages).
func (s *SlackNotifier) SendTest(message string) (string, error) {
	if message == "" {
		message = ":white_check_mark: orch Slack integration test successful!"
	}

	msg := SlackMessage{
		Text:        message,
		UnfurlLinks: false,
	}

	var channelName string
	if s.config.BotToken != "" && s.config.Channel != "" {
		msg.Channel = s.config.Channel
		channelName = s.config.Channel
		if err := s.sendBotMessage(msg); err != nil {
			return "", err
		}
	} else {
		channelName = "webhook"
		if err := s.sendWebhookMessage(msg); err != nil {
			return "", err
		}
	}

	return channelName, nil
}

// IsConfigured returns whether the notifier is properly configured.
func (s *SlackNotifier) IsConfigured() bool {
	return s.config.IsConfigured()
}
