package agent

import "testing"

func TestCodexLaunchCommand(t *testing.T) {
	adapter := &CodexAdapter{}

	tests := []struct {
		name string
		cfg  *LaunchConfig
		want string
	}{
		{
			name: "default yolo with escaped prompt",
			cfg:  &LaunchConfig{Prompt: "hello 'world'"},
			want: `codex --yolo 'hello '"'"'world'"'"''`,
		},
		{
			name: "model flag with provider prefix stripped",
			cfg: &LaunchConfig{
				Model:  "openai/gpt-5.3-codex",
				Prompt: "hello",
			},
			want: `codex --yolo --model gpt-5.3-codex 'hello'`,
		},
		{
			name: "model flag with already normalized model",
			cfg: &LaunchConfig{
				Model:  "gpt-5.3-codex",
				Prompt: "hello",
			},
			want: `codex --yolo --model gpt-5.3-codex 'hello'`,
		},
		{
			name: "no empty model flag when normalized model is empty",
			cfg: &LaunchConfig{
				Model:  "openai/",
				Prompt: "hello",
			},
			want: `codex --yolo 'hello'`,
		},
		{
			name: "model flag is added when extra args override defaults",
			cfg: &LaunchConfig{
				ExtraArgs: []string{"--full-auto"},
				Model:     "openai/gpt-5.3-codex",
				Prompt:    "hello",
			},
			want: `codex --full-auto --model gpt-5.3-codex 'hello'`,
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

func TestCodexModelName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "provider model format",
			input: "openai/gpt-5.3-codex",
			want:  "gpt-5.3-codex",
		},
		{
			name:  "already normalized",
			input: "gpt-5.3-codex",
			want:  "gpt-5.3-codex",
		},
		{
			name:  "empty model after provider prefix",
			input: "openai/",
			want:  "",
		},
		{
			name:  "model id containing slash after provider",
			input: "openrouter/openai/gpt-5.3-codex",
			want:  "openai/gpt-5.3-codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codexModelName(tt.input)
			if got != tt.want {
				t.Fatalf("codexModelName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCodexLaunchCommandResumeAppendsSubcommand(t *testing.T) {
	// ADR-0005 R5: codex revive resumes the recorded rollout. Root options
	// precede the subcommand (`codex [OPTIONS] <COMMAND> [ARGS]`), and the
	// resume id is the agent-native identity, never the tmux session name.
	adapter := &CodexAdapter{}
	cfg := &LaunchConfig{
		Resume:         true,
		AgentSessionID: "019f5a57-6d94-78e0-8f88-2db9a64a5be6",
		SessionName:    "run-x-y",
	}
	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := "codex --yolo resume 019f5a57-6d94-78e0-8f88-2db9a64a5be6"
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}

func TestCodexLaunchCommandResumeWithoutIdentityFailsFast(t *testing.T) {
	adapter := &CodexAdapter{}
	if _, err := adapter.LaunchCommand(&LaunchConfig{Resume: true}); err == nil {
		t.Fatal("codex resume without AgentSessionID must fail")
	}
}
