package agent

import "testing"

func TestDoubleQuote(t *testing.T) {
	input := "path\\with \"quotes\" $HOME and `tick"
	got := doubleQuote(input)
	want := "\"path\\\\with \\\"quotes\\\" \\$HOME and \\`tick\""
	if got != want {
		t.Fatalf("doubleQuote = %q, want %q", got, want)
	}
}

func TestClaudeLaunchCommand(t *testing.T) {
	adapter := &ClaudeAdapter{}
	cfg := &LaunchConfig{
		Prompt:      "hello",
		Profile:     "work",
		Model:       "sonnet",
		Resume:      true,
		SessionName: "session-1",
	}

	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := "claude --dangerously-skip-permissions --profile work --model sonnet --resume session-1 \"hello\""
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}
