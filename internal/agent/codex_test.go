package agent

import "testing"

func TestCodexLaunchCommand(t *testing.T) {
	adapter := &CodexAdapter{}
	cfg := &LaunchConfig{Prompt: "hello 'world'"}
	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := `codex --yolo 'hello '"'"'world'"'"''`
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}
