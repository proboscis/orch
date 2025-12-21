package agent

import "testing"

func TestGeminiLaunchCommand(t *testing.T) {
	adapter := &GeminiAdapter{}
	cfg := &LaunchConfig{Prompt: "hello 'world'"}
	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := `gemini --yolo -p 'hello '"'"'world'"'"''`
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}
