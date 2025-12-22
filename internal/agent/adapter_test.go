package agent

import (
	"strings"
	"testing"
)

func TestParseAgentType(t *testing.T) {
	cases := []struct {
		input string
		want  AgentType
	}{
		{"claude", AgentClaude},
		{"codex", AgentCodex},
		{"gemini", AgentGemini},
		{"custom", AgentCustom},
	}
	for _, tc := range cases {
		got, err := ParseAgentType(tc.input)
		if err != nil {
			t.Fatalf("ParseAgentType(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("ParseAgentType(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	if _, err := ParseAgentType("unknown"); err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestLaunchConfigEnv(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	cfg := &LaunchConfig{
		IssueID:   "issue",
		RunID:     "run",
		RunPath:   "/vault/run.md",
		WorkDir:   "/work",
		Branch:    "branch",
		VaultPath: "/vault",
		Model:     "claude-3-5-sonnet-20241022",
	}

	env := cfg.Env()
	assertEnvContains(t, env, "ORCH_ISSUE_ID=issue")
	assertEnvContains(t, env, "ORCH_RUN_ID=run")
	assertEnvContains(t, env, "ORCH_RUN_PATH=/vault/run.md")
	assertEnvContains(t, env, "ORCH_WORKTREE_PATH=/work")
	assertEnvContains(t, env, "ORCH_BRANCH=branch")
	assertEnvContains(t, env, "ORCH_VAULT=/vault")
	assertEnvContains(t, env, "ORCH_MODEL=claude-3-5-sonnet-20241022")
	assertEnvContains(t, env, "HOME=/tmp/home")
}

func TestGetAdapter(t *testing.T) {
	cases := []AgentType{AgentClaude, AgentCodex, AgentGemini, AgentCustom}
	for _, typ := range cases {
		adapter, err := GetAdapter(typ)
		if err != nil {
			t.Fatalf("GetAdapter(%q) error: %v", typ, err)
		}
		if adapter.Type() != typ {
			t.Fatalf("GetAdapter(%q) = %q, want %q", typ, adapter.Type(), typ)
		}
	}

	if _, err := GetAdapter(AgentType("unknown")); err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestCustomAdapterLaunchCommand(t *testing.T) {
	adapter := &CustomAdapter{}
	if _, err := adapter.LaunchCommand(&LaunchConfig{}); err == nil {
		t.Fatal("expected error when custom command is missing")
	}

	cmd, err := adapter.LaunchCommand(&LaunchConfig{CustomCmd: "do-it"})
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	if cmd != "do-it" {
		t.Fatalf("command = %q, want %q", cmd, "do-it")
	}
}

func assertEnvContains(t *testing.T, env []string, want string) {
	t.Helper()
	for _, entry := range env {
		if entry == want {
			return
		}
	}
	t.Fatalf("env missing %q in %s", want, strings.Join(env, ", "))
}
