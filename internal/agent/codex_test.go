package agent

import "testing"

func TestCodexLaunchCommand(t *testing.T) {
	adapter := &CodexAdapter{}
	cfg := &LaunchConfig{Prompt: "hello 'world'", Model: "o1", Thinking: "xhigh"}
	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := `codex --yolo --model o1 -c model_reasoning_effort=xhigh 'hello '"'"'world'"'"''`
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}
