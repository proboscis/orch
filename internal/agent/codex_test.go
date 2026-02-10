package agent

import (
	"strings"
	"testing"
)

func TestCodexLaunchCommand(t *testing.T) {
	adapter := &CodexAdapter{}

	tests := []struct {
		name string
		cfg  *LaunchConfig
		want string
	}{
		{
			name: "default args with escaped prompt",
			cfg:  &LaunchConfig{Prompt: "hello 'world'"},
			want: `codex --yolo 'hello '"'"'world'"'"''`,
		},
		{
			name: "injects model flag and strips provider prefix",
			cfg: &LaunchConfig{
				Model:  "openai/gpt-5.3-codex",
				Prompt: "do the thing",
			},
			want: "codex --yolo --model gpt-5.3-codex 'do the thing'",
		},
		{
			name: "respects extra args and bare model",
			cfg: &LaunchConfig{
				Model:     "gpt-5.3-codex",
				ExtraArgs: []string{"--approval-mode", "full-auto"},
				Prompt:    "ship it",
			},
			want: "codex --approval-mode full-auto --model gpt-5.3-codex 'ship it'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := adapter.LaunchCommand(tt.cfg)
			if err != nil {
				t.Fatalf("LaunchCommand error: %v", err)
			}
			if cmd != tt.want {
				t.Fatalf("command = %q, want %q", cmd, tt.want)
			}
		})
	}
}

func TestCodexLaunchCommandOmitsEmptyModelFlag(t *testing.T) {
	adapter := &CodexAdapter{}
	cmd, err := adapter.LaunchCommand(&LaunchConfig{Prompt: "hi"})
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	if strings.Contains(cmd, "--model") {
		t.Fatalf("command = %q, expected no --model flag", cmd)
	}
}

func TestFormatCodexModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: "gpt-5.3-codex", want: "gpt-5.3-codex"},
		{input: "openai/gpt-5.3-codex", want: "gpt-5.3-codex"},
		{input: "  openai/gpt-5  ", want: "gpt-5"},
	}

	for _, tt := range tests {
		if got := formatCodexModel(tt.input); got != tt.want {
			t.Fatalf("formatCodexModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
