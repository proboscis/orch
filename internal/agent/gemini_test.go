package agent

import "testing"

func TestGeminiLaunchCommand(t *testing.T) {
	adapter := &GeminiAdapter{}
	cfg := &LaunchConfig{Prompt: "hello 'world'", Model: "gemini-1.5-pro"}
	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := `gemini --yolo --model gemini-1.5-pro --prompt-interactive "hello 'world'"`
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}

func TestGeminiPromptInjection(t *testing.T) {
	adapter := &GeminiAdapter{}
	if adapter.PromptInjection() != InjectionArg {
		t.Fatalf("PromptInjection() = %v, want %v", adapter.PromptInjection(), InjectionArg)
	}
}

func TestGeminiReadyPattern(t *testing.T) {
	adapter := &GeminiAdapter{}
	pattern := adapter.ReadyPattern()
	if pattern != "" {
		t.Fatalf("ReadyPattern() = %q, want %q", pattern, "")
	}
}
