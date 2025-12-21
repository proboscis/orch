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

func TestGeminiReadyPattern(t *testing.T) {
	adapter := &GeminiAdapter{}
	pattern := adapter.ReadyPattern()
	if pattern == "" {
		t.Fatal("ReadyPattern() should not be empty for Gemini")
	}
	if pattern != "Type your message" {
		t.Fatalf("ReadyPattern() = %q, want %q", pattern, "Type your message")
	}
}
