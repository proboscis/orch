package agent

import "testing"

func TestGeminiLaunchCommand(t *testing.T) {
	adapter := &GeminiAdapter{}
	cfg := &LaunchConfig{Prompt: "hello 'world'"}
	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	// Gemini now launches without -p flag; prompt is sent via tmux send-keys
	want := `gemini --yolo`
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}

func TestGeminiPromptInjection(t *testing.T) {
	adapter := &GeminiAdapter{}
	if adapter.PromptInjection() != InjectionTmux {
		t.Fatalf("PromptInjection() = %v, want %v", adapter.PromptInjection(), InjectionTmux)
	}
}
