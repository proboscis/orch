package agent

import (
	"os"
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
		IssueID:    "issue",
		RunID:      "run",
		RunPath:    "/vault/run.md",
		WorkDir:    "/work",
		Branch:     "branch",
		IssuesRoot: "/vault",
	}

	env := cfg.Env()
	assertEnvContains(t, env, "ORCH_ISSUE_ID=issue")
	assertEnvContains(t, env, "ORCH_RUN_ID=run")
	assertEnvContains(t, env, "ORCH_RUN_PATH=/vault/run.md")
	assertEnvContains(t, env, "ORCH_WORKTREE_PATH=/work")
	assertEnvContains(t, env, "ORCH_BRANCH=branch")
	assertEnvContains(t, env, "HOME=/tmp/home")
}

func TestLaunchConfigEnvCodexHomeAbsolute(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	cfg := &LaunchConfig{
		IssueID:   "issue",
		RunID:     "run",
		CodexHome: "/opt/codex-company",
	}

	env := cfg.Env()
	assertEnvContains(t, env, "CODEX_HOME=/opt/codex-company")
}

func TestLaunchConfigEnvCodexHomeTildeExpansion(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	cfg := &LaunchConfig{
		IssueID:   "issue",
		RunID:     "run",
		CodexHome: "~/.codex-company",
	}

	env := cfg.Env()
	assertEnvContains(t, env, "CODEX_HOME=/tmp/home/.codex-company")
}

func TestLaunchConfigEnvCodexHomeBareTilde(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	cfg := &LaunchConfig{CodexHome: "~"}

	env := cfg.Env()
	assertEnvContains(t, env, "CODEX_HOME=/tmp/home")
}

func TestLaunchConfigEnvCodexHomeEmptyInjectsNothing(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	cfg := &LaunchConfig{IssueID: "issue", RunID: "run"} // CodexHome empty

	env := cfg.Env()
	for _, entry := range env {
		if strings.HasPrefix(entry, "CODEX_HOME=") {
			t.Fatalf("expected no CODEX_HOME entry, got %q", entry)
		}
	}
}

func TestLaunchConfigCodexHomeEnv(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	// Tilde expansion.
	got := (&LaunchConfig{CodexHome: "~/.codex-company"}).CodexHomeEnv()
	if len(got) != 1 || got[0] != "CODEX_HOME=/tmp/home/.codex-company" {
		t.Errorf("CodexHomeEnv(~/.codex-company) = %v, want [CODEX_HOME=/tmp/home/.codex-company]", got)
	}

	// Absolute path passes through.
	got = (&LaunchConfig{CodexHome: "/opt/codex"}).CodexHomeEnv()
	if len(got) != 1 || got[0] != "CODEX_HOME=/opt/codex" {
		t.Errorf("CodexHomeEnv(/opt/codex) = %v, want [CODEX_HOME=/opt/codex]", got)
	}

	// Empty injects nothing.
	if got := (&LaunchConfig{}).CodexHomeEnv(); len(got) != 0 {
		t.Errorf("CodexHomeEnv(empty) = %v, want empty", got)
	}

	// Env() and CodexHomeEnv() agree on the CODEX_HOME entry.
	full := (&LaunchConfig{CodexHome: "~/.codex-company"}).Env()
	assertEnvContains(t, full, "CODEX_HOME=/tmp/home/.codex-company")
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

// Fail-fast law (run-state-machine.md §5 L9, launch step agent_auth): a codex
// launch with an explicitly configured CODEX_HOME that cannot authenticate
// must fail at preflight instead of parking at the sign-in wizard.
func TestAuthPreflight(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")

	authed := t.TempDir()
	if err := os.WriteFile(authed+"/auth.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()

	if err := AuthPreflight(nil); err != nil {
		t.Fatalf("nil config must pass: %v", err)
	}
	if err := AuthPreflight(&LaunchConfig{Type: AgentClaude, CodexHome: empty}); err != nil {
		t.Fatalf("non-codex agent must pass: %v", err)
	}
	if err := AuthPreflight(&LaunchConfig{Type: AgentCodex}); err != nil {
		t.Fatalf("codex without configured CODEX_HOME must pass (agent default not second-guessed): %v", err)
	}
	if err := AuthPreflight(&LaunchConfig{Type: AgentCodex, CodexHome: authed}); err != nil {
		t.Fatalf("codex with auth.json must pass: %v", err)
	}

	err := AuthPreflight(&LaunchConfig{Type: AgentCodex, CodexHome: empty})
	if err == nil {
		t.Fatal("codex with empty CODEX_HOME must fail preflight")
	}
	if !strings.Contains(err.Error(), empty) || !strings.Contains(err.Error(), "auth.json") {
		t.Fatalf("preflight error must name the concrete path, got: %v", err)
	}

	// ~ expansion happens against the execution host's HOME.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(fakeHome+"/.codex/profiles/p", 0755); err != nil {
		t.Fatal(err)
	}
	if err := AuthPreflight(&LaunchConfig{Type: AgentCodex, CodexHome: "~/.codex/profiles/p"}); err == nil {
		t.Fatal("tilde CODEX_HOME without auth.json must fail preflight")
	}
	if err := os.WriteFile(fakeHome+"/.codex/profiles/p/auth.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AuthPreflight(&LaunchConfig{Type: AgentCodex, CodexHome: "~/.codex/profiles/p"}); err != nil {
		t.Fatalf("tilde CODEX_HOME with auth.json must pass: %v", err)
	}

	// An API key in the environment authenticates codex without auth.json.
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if err := AuthPreflight(&LaunchConfig{Type: AgentCodex, CodexHome: empty}); err != nil {
		t.Fatalf("API key in env must skip the auth.json requirement: %v", err)
	}
}
