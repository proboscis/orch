package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	globalDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte("issues:\n  path: /global\nagent: claude\n"), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: /repo\nagent: codex\npr_target_branch: develop\nno_pr: true\n"), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Issues.Path != "/repo" || cfg.Agent != "codex" || !cfg.NoPR || cfg.PRTargetBranch != "develop" {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	t.Setenv("ORCH_ISSUES_ROOT", "/env")
	t.Setenv("ORCH_AGENT", "gemini")
	cfgEnv, err := Load()
	if err != nil {
		t.Fatalf("Load env error: %v", err)
	}
	// Repo config takes precedence over env for issues.path
	if cfgEnv.Issues.Path != "/repo" || cfgEnv.Agent != "codex" {
		t.Fatalf("unexpected env config: %+v", cfgEnv)
	}

	other := t.TempDir()
	if err := os.Chdir(other); err != nil {
		t.Fatalf("chdir other: %v", err)
	}
	cfgEnvOnly, err := Load()
	if err != nil {
		t.Fatalf("Load env-only error: %v", err)
	}
	if cfgEnvOnly.Issues.Path != "/env" || cfgEnvOnly.Agent != "gemini" {
		t.Fatalf("unexpected env-only config: %+v", cfgEnvOnly)
	}
}

func TestParentConfigPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "/env")
	t.Setenv("ORCH_AGENT", "gemini")
	t.Setenv("ORCH_WORKTREE_DIR", "/env-worktrees")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: /parent\nagent: claude\n"), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	child := filepath.Join(repo, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(child); err != nil {
		t.Fatalf("chdir child: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Issues.Path != "/parent" || cfg.Agent != "claude" {
		t.Fatalf("unexpected parent config: %+v", cfg)
	}
	if cfg.WorktreeDir != "/env-worktrees" {
		t.Fatalf("unexpected env worktree_dir: %q", cfg.WorktreeDir)
	}

	if err := os.MkdirAll(filepath.Join(child, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir child .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, ".orch", "config.yaml"), []byte("issues:\n  path: /local\n"), 0644); err != nil {
		t.Fatalf("write child config: %v", err)
	}

	cfgLocal, err := Load()
	if err != nil {
		t.Fatalf("Load local error: %v", err)
	}
	if cfgLocal.Issues.Path != "/local" || cfgLocal.Agent != "claude" {
		t.Fatalf("unexpected local config: %+v", cfgLocal)
	}
	if cfgLocal.WorktreeDir != "/env-worktrees" {
		t.Fatalf("unexpected env worktree_dir (local): %q", cfgLocal.WorktreeDir)
	}
}

func TestLoadIssuesPathCaseInsensitive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	repoIssues := filepath.Join(repo, "ISSUES")
	if err := os.MkdirAll(repoIssues, 0755); err != nil {
		t.Fatalf("mkdir ISSUES: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: "+repoIssues+"\n"), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Issues.Path != repoIssues {
		t.Fatalf("Issues.Path = %q, want %q", cfg.Issues.Path, repoIssues)
	}
}

func TestLoadDefaultIssuesPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	// No config at all - should use default path
	repo := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// When no issues.path is configured, GetIssuesPath() returns XDG default
	issuesPath := cfg.GetIssuesPath()
	if issuesPath == "" {
		t.Fatalf("GetIssuesPath() should return a default path, got empty")
	}
	// Should be in ~/.local/share/orch/
	expectedPrefix := filepath.Join(home, ".local", "share", "orch")
	if len(issuesPath) < len(expectedPrefix) || issuesPath[:len(expectedPrefix)] != expectedPrefix {
		t.Fatalf("GetIssuesPath() = %q, want prefix %q", issuesPath, expectedPrefix)
	}
}

func TestRepoConfigWithoutIssuesPathUsesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("agent: codex\n"), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// Issues.Path should be empty in config (default will be computed by GetIssuesPath)
	if cfg.Issues.Path != "" {
		t.Fatalf("Issues.Path = %q, want empty (uses default)", cfg.Issues.Path)
	}
	if cfg.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", cfg.Agent)
	}
}

func TestLoadRejectsUnknownConfigKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: /repo\nunknown_key: true\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = Load()
	if err == nil {
		t.Fatal("expected unknown key validation error")
	}
	if !strings.Contains(err.Error(), "invalid config schema") {
		t.Fatalf("expected schema validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown_key") {
		t.Fatalf("expected unknown key name in error, got: %v", err)
	}
}

func TestLoadRejectsInvalidAgentValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: /repo\nagent: not-an-agent\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = Load()
	if err == nil {
		t.Fatal("expected invalid agent error")
	}
	if !strings.Contains(err.Error(), "agent must be one of") {
		t.Fatalf("expected invalid agent message, got: %v", err)
	}
}

func TestLoadRejectsInvalidIssuesBackendValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: /repo\n  backend: invalid\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = Load()
	if err == nil {
		t.Fatal("expected invalid issues backend error")
	}
	if !strings.Contains(err.Error(), "issues.backend must be one of") {
		t.Fatalf("expected invalid backend message, got: %v", err)
	}
}

func TestRepoConfigDir(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("agent: opencode\n"), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	dir := RepoConfigDir()
	got, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks dir: %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(repo, ".orch"))
	if err != nil {
		t.Fatalf("EvalSymlinks want: %v", err)
	}
	if got != want {
		t.Fatalf("RepoConfigDir = %q, want %q", got, want)
	}
}

func TestExpandPath(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	if got := ExpandPath("~/issues", ""); got != filepath.Join("/home/test", "issues") {
		t.Fatalf("ExpandPath home = %q", got)
	}
	if got := ExpandPath("relative/path", "/base"); got != filepath.Join("/base", "relative/path") {
		t.Fatalf("ExpandPath relative = %q", got)
	}
}

func TestRelativeIssuesPathResolution(t *testing.T) {
	// Clear environment variables that could interfere
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_WORKTREE_DIR", "")
	t.Setenv("ORCH_PROMPT_TEMPLATE", "")

	// Create a temp home directory to avoid loading user's global config
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a repo with relative issues path
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "ISSUES"), 0755); err != nil {
		t.Fatalf("mkdir ISSUES: %v", err)
	}

	// Test with ./ISSUES relative path
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: ./ISSUES\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Issues.Path should be resolved to absolute path
	expectedIssues, err := filepath.EvalSymlinks(filepath.Join(repo, "ISSUES"))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotIssues, err := filepath.EvalSymlinks(cfg.Issues.Path)
	if err != nil {
		t.Fatalf("EvalSymlinks issues: %v", err)
	}

	if gotIssues != expectedIssues {
		t.Fatalf("issues path not resolved correctly: got %q, want %q", gotIssues, expectedIssues)
	}
}

func TestLoadTargetsAndGetTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configBody := `targets:
  - name: mac
    host: user@mac
    repo: /Users/user/repos/project
  - name: linux
    host: dev@linux
    repo: /home/dev/project
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configBody), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if len(cfg.Targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(cfg.Targets))
	}

	mac := cfg.GetTarget("mac")
	if mac == nil {
		t.Fatalf("GetTarget(mac) = nil, want target")
	}
	if mac.Host != "user@mac" || mac.Repo != "/Users/user/repos/project" {
		t.Fatalf("GetTarget(mac) = %+v, unexpected values", mac)
	}

	if cfg.GetTarget("missing") != nil {
		t.Fatalf("GetTarget(missing) != nil")
	}
}

func TestOpenCodeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_OPENCODE_DEFAULT_MODEL", "")
	t.Setenv("ORCH_OPENCODE_DEFAULT_VARIANT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `issues:
  path: /repo
opencode:
  default_model: anthropic/claude-sonnet-4-5
  default_variant: max
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.OpenCode.DefaultModel != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("OpenCode.DefaultModel = %q, want anthropic/claude-sonnet-4-5", cfg.OpenCode.DefaultModel)
	}
	if cfg.OpenCode.DefaultVariant != "max" {
		t.Fatalf("OpenCode.DefaultVariant = %q, want max", cfg.OpenCode.DefaultVariant)
	}
}

func TestOpenCodeConfigEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_OPENCODE_DEFAULT_MODEL", "openai/gpt-5")
	t.Setenv("ORCH_OPENCODE_DEFAULT_VARIANT", "high")

	repo := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.OpenCode.DefaultModel != "openai/gpt-5" {
		t.Fatalf("OpenCode.DefaultModel = %q, want openai/gpt-5", cfg.OpenCode.DefaultModel)
	}
	if cfg.OpenCode.DefaultVariant != "high" {
		t.Fatalf("OpenCode.DefaultVariant = %q, want high", cfg.OpenCode.DefaultVariant)
	}
}

func TestCodexConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_CODEX_DEFAULT_MODEL", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `issues:
  path: /repo
codex:
  default_model: openai/gpt-5.3-codex
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Codex.DefaultModel != "openai/gpt-5.3-codex" {
		t.Fatalf("Codex.DefaultModel = %q, want openai/gpt-5.3-codex", cfg.Codex.DefaultModel)
	}
}

func TestCodexConfigEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_CODEX_DEFAULT_MODEL", "openai/gpt-5.3-codex")

	repo := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Codex.DefaultModel != "openai/gpt-5.3-codex" {
		t.Fatalf("Codex.DefaultModel = %q, want openai/gpt-5.3-codex", cfg.Codex.DefaultModel)
	}
}

func TestRelativePathFromSubdirectory(t *testing.T) {
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_WORKTREE_DIR", "")

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a repo with a subdirectory
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "ISSUES"), 0755); err != nil {
		t.Fatalf("mkdir ISSUES: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src", "subdir"), 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("issues:\n  path: ./ISSUES\nworktree_dir: .git-worktrees\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// Run from a subdirectory - config should still resolve relative to repo root
	if err := os.Chdir(filepath.Join(repo, "src", "subdir")); err != nil {
		t.Fatalf("chdir to subdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	expectedIssues, err := filepath.EvalSymlinks(filepath.Join(repo, "ISSUES"))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotIssues, err := filepath.EvalSymlinks(cfg.Issues.Path)
	if err != nil {
		t.Fatalf("EvalSymlinks issues: %v", err)
	}

	if gotIssues != expectedIssues {
		t.Fatalf("issues not resolved relative to repo root: got %q, want %q", gotIssues, expectedIssues)
	}

	// WorktreeDir directory doesn't exist, so we can't use EvalSymlinks
	// But we need to handle the /private symlink on macOS
	// The easiest way is to check if the path ends correctly
	expectedSuffix := ".git-worktrees"
	if !filepath.IsAbs(cfg.WorktreeDir) {
		t.Fatalf("worktree_dir should be absolute: got %q", cfg.WorktreeDir)
	}
	if filepath.Base(cfg.WorktreeDir) != expectedSuffix {
		t.Fatalf("worktree_dir should end with %q: got %q", expectedSuffix, cfg.WorktreeDir)
	}
}

func TestVaultConfigErrorsWithHelpfulMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	// Use deprecated vault config
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: /old-path\nagent: codex\n"), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	_, err = Load()
	if err == nil {
		t.Fatal("expected error when using deprecated vault config")
	}
	// Check error message contains helpful migration info
	if !contains(err.Error(), "deprecated") || !contains(err.Error(), "issues.path") {
		t.Fatalf("error should mention deprecation and migration: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Preset tests

func TestPresetEffectiveBackend(t *testing.T) {
	tests := []struct {
		name   string
		preset Preset
		want   string
	}{
		{
			name:   "explicit opencode backend",
			preset: Preset{Name: "test", Backend: "opencode"},
			want:   "opencode",
		},
		{
			name:   "explicit claude backend",
			preset: Preset{Name: "test", Backend: "claude"},
			want:   "claude",
		},
		{
			name:   "empty backend defaults to opencode",
			preset: Preset{Name: "test", Backend: ""},
			want:   "opencode",
		},
		{
			name:   "codex backend",
			preset: Preset{Name: "test", Backend: "codex"},
			want:   "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.preset.EffectiveBackend(); got != tt.want {
				t.Errorf("EffectiveBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetPresetNewStyle(t *testing.T) {
	cfg := &Config{
		Presets: []Preset{
			{Name: "opus:high", Backend: "opencode", Model: "anthropic/claude-opus-4-5", Variant: "high"},
			{Name: "gpt5.2-codex:xhigh", Backend: "opencode", Model: "openai/gpt-5.2-codex", Variant: "xhigh"},
			{Name: "claude:default", Backend: "claude", Profile: "default"},
		},
	}

	// Test finding existing preset
	preset := cfg.GetPreset("opus:high")
	if preset == nil {
		t.Fatal("expected to find preset 'opus:high'")
	}
	if preset.Model != "anthropic/claude-opus-4-5" {
		t.Errorf("Model = %q, want anthropic/claude-opus-4-5", preset.Model)
	}
	if preset.Variant != "high" {
		t.Errorf("Variant = %q, want high", preset.Variant)
	}
	if preset.EffectiveBackend() != "opencode" {
		t.Errorf("Backend = %q, want opencode", preset.EffectiveBackend())
	}

	// Test finding claude preset
	claudePreset := cfg.GetPreset("claude:default")
	if claudePreset == nil {
		t.Fatal("expected to find preset 'claude:default'")
	}
	if claudePreset.EffectiveBackend() != "claude" {
		t.Errorf("Backend = %q, want claude", claudePreset.EffectiveBackend())
	}
	if claudePreset.Profile != "default" {
		t.Errorf("Profile = %q, want default", claudePreset.Profile)
	}

	// Test non-existent preset
	missing := cfg.GetPreset("nonexistent")
	if missing != nil {
		t.Errorf("expected nil for nonexistent preset, got %+v", missing)
	}
}

func TestGetPresetLegacyFallback(t *testing.T) {
	cfg := &Config{
		OpenCodePresets: []OpenCodePreset{
			{Name: "legacy:preset", Model: "anthropic/claude-sonnet-4-5", Variant: "max"},
		},
	}

	// Test finding legacy preset
	preset := cfg.GetPreset("legacy:preset")
	if preset == nil {
		t.Fatal("expected to find legacy preset")
	}
	if preset.Model != "anthropic/claude-sonnet-4-5" {
		t.Errorf("Model = %q, want anthropic/claude-sonnet-4-5", preset.Model)
	}
	if preset.Variant != "max" {
		t.Errorf("Variant = %q, want max", preset.Variant)
	}
	// Legacy presets should default to opencode backend
	if preset.EffectiveBackend() != "opencode" {
		t.Errorf("Backend = %q, want opencode", preset.EffectiveBackend())
	}
}

func TestGetPresetNewStyleTakesPrecedence(t *testing.T) {
	cfg := &Config{
		Presets: []Preset{
			{Name: "shared:name", Backend: "opencode", Model: "new-model", Variant: "new-variant"},
		},
		OpenCodePresets: []OpenCodePreset{
			{Name: "shared:name", Model: "legacy-model", Variant: "legacy-variant"},
		},
	}

	// New-style presets should take precedence
	preset := cfg.GetPreset("shared:name")
	if preset == nil {
		t.Fatal("expected to find preset")
	}
	if preset.Model != "new-model" {
		t.Errorf("Model = %q, want new-model (new-style should take precedence)", preset.Model)
	}
}

func TestGetAllPresets(t *testing.T) {
	cfg := &Config{
		Presets: []Preset{
			{Name: "new:a", Backend: "opencode", Model: "model-a"},
			{Name: "new:b", Backend: "claude", Profile: "default"},
		},
		OpenCodePresets: []OpenCodePreset{
			{Name: "legacy:c", Model: "model-c"},
			{Name: "new:a", Model: "should-be-ignored"}, // Duplicate - new-style should win
		},
	}

	allPresets := cfg.GetAllPresets()

	// Should have 3 unique presets
	if len(allPresets) != 3 {
		t.Errorf("expected 3 presets, got %d", len(allPresets))
	}

	// Verify the presets are sorted by name
	names := make([]string, len(allPresets))
	for i, p := range allPresets {
		names[i] = p.Name
	}
	if names[0] != "legacy:c" || names[1] != "new:a" || names[2] != "new:b" {
		t.Errorf("presets not sorted correctly: %v", names)
	}

	// Verify new:a uses new-style values (not legacy)
	for _, p := range allPresets {
		if p.Name == "new:a" {
			if p.Model != "model-a" {
				t.Errorf("new:a should use new-style model, got %q", p.Model)
			}
		}
	}
}

func TestGetPresetsForBackend(t *testing.T) {
	cfg := &Config{
		Presets: []Preset{
			{Name: "opencode:a", Backend: "opencode", Model: "model-a"},
			{Name: "opencode:b", Backend: "opencode", Model: "model-b"},
			{Name: "claude:a", Backend: "claude", Profile: "default"},
			{Name: "codex:a", Backend: "codex", Model: "codex-model"},
			{Name: "default-backend", Backend: "", Model: "default"}, // Empty backend -> opencode
		},
		OpenCodePresets: []OpenCodePreset{
			{Name: "legacy:a", Model: "legacy-model"}, // Legacy presets are opencode
		},
	}

	// Test opencode presets
	opencodePresets := cfg.GetPresetsForBackend("opencode")
	if len(opencodePresets) != 4 { // opencode:a, opencode:b, default-backend, legacy:a
		t.Errorf("expected 4 opencode presets, got %d", len(opencodePresets))
	}

	// Test claude presets
	claudePresets := cfg.GetPresetsForBackend("claude")
	if len(claudePresets) != 1 {
		t.Errorf("expected 1 claude preset, got %d", len(claudePresets))
	}
	if claudePresets[0].Name != "claude:a" {
		t.Errorf("expected claude:a, got %q", claudePresets[0].Name)
	}

	// Test codex presets
	codexPresets := cfg.GetPresetsForBackend("codex")
	if len(codexPresets) != 1 {
		t.Errorf("expected 1 codex preset, got %d", len(codexPresets))
	}

	// Test gemini presets (should be empty)
	geminiPresets := cfg.GetPresetsForBackend("gemini")
	if len(geminiPresets) != 0 {
		t.Errorf("expected 0 gemini presets, got %d", len(geminiPresets))
	}
}

func TestValidatePresets(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *Config
		wantWarnings int
	}{
		{
			name: "valid presets",
			cfg: &Config{
				Presets: []Preset{
					{Name: "valid:a", Backend: "opencode", Model: "model-a"},
					{Name: "valid:b", Backend: "claude", Profile: "default"},
				},
			},
			wantWarnings: 0,
		},
		{
			name: "invalid backend",
			cfg: &Config{
				Presets: []Preset{
					{Name: "invalid:backend", Backend: "invalid-backend", Model: "model"},
				},
			},
			wantWarnings: 1,
		},
		{
			name: "empty name",
			cfg: &Config{
				Presets: []Preset{
					{Name: "", Backend: "opencode", Model: "model"},
				},
			},
			wantWarnings: 1,
		},
		{
			name: "legacy presets without new presets triggers deprecation warning",
			cfg: &Config{
				OpenCodePresets: []OpenCodePreset{
					{Name: "legacy:preset", Model: "model"},
				},
			},
			wantWarnings: 1,
		},
		{
			name: "legacy presets with new presets does not trigger deprecation warning",
			cfg: &Config{
				Presets: []Preset{
					{Name: "new:preset", Backend: "opencode", Model: "model"},
				},
				OpenCodePresets: []OpenCodePreset{
					{Name: "legacy:preset", Model: "model"},
				},
			},
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.cfg.ValidatePresets()
			if len(warnings) != tt.wantWarnings {
				t.Errorf("ValidatePresets() returned %d warnings, want %d: %v", len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
}

func TestLoadPresetsFromConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configContent := `issues:
  path: /repo
presets:
  - name: opus:high
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: high
  - name: gpt5.2-codex:xhigh
    backend: opencode
    model: openai/gpt-5.2-codex
    variant: xhigh
  - name: claude:default
    backend: claude
    profile: default
opencode_presets:
  - name: legacy:preset
    model: anthropic/claude-sonnet-4-5
    variant: max
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Test new-style presets loaded
	if len(cfg.Presets) != 3 {
		t.Errorf("expected 3 new-style presets, got %d", len(cfg.Presets))
	}

	// Test legacy presets loaded
	if len(cfg.OpenCodePresets) != 1 {
		t.Errorf("expected 1 legacy preset, got %d", len(cfg.OpenCodePresets))
	}

	// Test GetPreset works correctly
	preset := cfg.GetPreset("opus:high")
	if preset == nil {
		t.Fatal("expected to find preset 'opus:high'")
	}
	if preset.Model != "anthropic/claude-opus-4-5" {
		t.Errorf("Model = %q, want anthropic/claude-opus-4-5", preset.Model)
	}
	if preset.EffectiveBackend() != "opencode" {
		t.Errorf("Backend = %q, want opencode", preset.EffectiveBackend())
	}

	// Test GetAllPresets returns all 4 presets
	allPresets := cfg.GetAllPresets()
	if len(allPresets) != 4 {
		t.Errorf("expected 4 total presets, got %d", len(allPresets))
	}
}

func TestDefaultPresetConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_DEFAULT_PRESET", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configContent := `issues:
  path: /repo
presets:
  - name: opus:high
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: high
default_preset: opus:high
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.DefaultPreset != "opus:high" {
		t.Errorf("DefaultPreset = %q, want opus:high", cfg.DefaultPreset)
	}
}

func TestDefaultPresetEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_DEFAULT_PRESET", "sonnet:max")

	repo := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.DefaultPreset != "sonnet:max" {
		t.Errorf("DefaultPreset = %q, want sonnet:max", cfg.DefaultPreset)
	}
}

func TestResolveModelAndVariantCodexDefaults(t *testing.T) {
	cfg := &Config{
		Model:        "global-model",
		ModelVariant: "global-variant",
		Codex: CodexConfig{
			DefaultModel: "openai/gpt-5.3-codex",
		},
		Presets: []Preset{
			{
				Name:    "preset-a",
				Model:   "preset-model",
				Variant: "preset-variant",
			},
		},
	}

	tests := []struct {
		name       string
		preset     string
		reqModel   string
		reqVariant string
		wantModel  string
		wantVar    string
	}{
		{
			name:       "explicit request wins",
			preset:     "preset-a",
			reqModel:   "request-model",
			reqVariant: "request-variant",
			wantModel:  "request-model",
			wantVar:    "request-variant",
		},
		{
			name:      "preset wins over defaults",
			preset:    "preset-a",
			wantModel: "preset-model",
			wantVar:   "preset-variant",
		},
		{
			name:      "codex default model used when no request or preset model",
			wantModel: "openai/gpt-5.3-codex",
			wantVar:   "global-variant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModel, gotVariant := cfg.ResolveModelAndVariant("codex", tt.preset, tt.reqModel, tt.reqVariant)
			if gotModel != tt.wantModel || gotVariant != tt.wantVar {
				t.Fatalf("ResolveModelAndVariant(codex) = (%q, %q), want (%q, %q)", gotModel, gotVariant, tt.wantModel, tt.wantVar)
			}
		})
	}
}

func TestResolveControlModelAndVariantCodexDefaults(t *testing.T) {
	cfg := &Config{
		Model:               "global-model",
		ModelVariant:        "global-variant",
		ControlModel:        "",
		ControlModelVariant: "",
		Codex: CodexConfig{
			DefaultModel: "openai/gpt-5.3-codex",
		},
	}

	gotModel, gotVariant := cfg.ResolveControlModelAndVariant("codex")
	if gotModel != "openai/gpt-5.3-codex" || gotVariant != "global-variant" {
		t.Fatalf("ResolveControlModelAndVariant(codex) = (%q, %q), want (%q, %q)", gotModel, gotVariant, "openai/gpt-5.3-codex", "global-variant")
	}

	cfg.ControlModel = "control-model"
	cfg.ControlModelVariant = "control-variant"
	gotModel, gotVariant = cfg.ResolveControlModelAndVariant("codex")
	if gotModel != "control-model" || gotVariant != "control-variant" {
		t.Fatalf("ResolveControlModelAndVariant(codex) override = (%q, %q), want (%q, %q)", gotModel, gotVariant, "control-model", "control-variant")
	}
}

func TestGetPromptTemplate(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *Config
		agent string
		want  string
	}{
		{
			name: "opencode with per-backend template",
			cfg: &Config{
				PromptTemplate: "global template",
				OpenCode:       OpenCodeConfig{PromptTemplate: "opencode template"},
			},
			agent: "opencode",
			want:  "opencode template",
		},
		{
			name: "claude with per-backend template",
			cfg: &Config{
				PromptTemplate: "global template",
				Claude:         ClaudeConfig{PromptTemplate: "claude template"},
			},
			agent: "claude",
			want:  "claude template",
		},
		{
			name: "codex with per-backend template",
			cfg: &Config{
				PromptTemplate: "global template",
				Codex:          CodexConfig{PromptTemplate: "codex template"},
			},
			agent: "codex",
			want:  "codex template",
		},
		{
			name: "gemini with per-backend template",
			cfg: &Config{
				PromptTemplate: "global template",
				Gemini:         GeminiConfig{PromptTemplate: "gemini template"},
			},
			agent: "gemini",
			want:  "gemini template",
		},
		{
			name: "opencode falls back to global template",
			cfg: &Config{
				PromptTemplate: "global template",
				OpenCode:       OpenCodeConfig{},
			},
			agent: "opencode",
			want:  "global template",
		},
		{
			name: "unknown agent falls back to global template",
			cfg: &Config{
				PromptTemplate: "global template",
			},
			agent: "custom",
			want:  "global template",
		},
		{
			name:  "empty config returns empty string",
			cfg:   &Config{},
			agent: "opencode",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetPromptTemplate(tt.agent)
			if got != tt.want {
				t.Errorf("GetPromptTemplate(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}

func TestLoadPerBackendPromptTemplates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_ISSUES_ROOT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_PROMPT_TEMPLATE", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configContent := `issues:
  path: /repo
prompt_template: "global {{issue}}"
opencode:
  default_model: anthropic/claude-opus-4-5
  prompt_template: "opencode {{issue}}"
claude:
  prompt_template: "claude {{issue}}"
codex:
  prompt_template: "codex {{issue}}"
gemini:
  prompt_template: "gemini {{issue}}"
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.PromptTemplate != "global {{issue}}" {
		t.Errorf("PromptTemplate = %q, want global {{issue}}", cfg.PromptTemplate)
	}
	if cfg.OpenCode.PromptTemplate != "opencode {{issue}}" {
		t.Errorf("OpenCode.PromptTemplate = %q, want opencode {{issue}}", cfg.OpenCode.PromptTemplate)
	}
	if cfg.Claude.PromptTemplate != "claude {{issue}}" {
		t.Errorf("Claude.PromptTemplate = %q, want claude {{issue}}", cfg.Claude.PromptTemplate)
	}
	if cfg.Codex.PromptTemplate != "codex {{issue}}" {
		t.Errorf("Codex.PromptTemplate = %q, want codex {{issue}}", cfg.Codex.PromptTemplate)
	}
	if cfg.Gemini.PromptTemplate != "gemini {{issue}}" {
		t.Errorf("Gemini.PromptTemplate = %q, want gemini {{issue}}", cfg.Gemini.PromptTemplate)
	}

	if got := cfg.GetPromptTemplate("opencode"); got != "opencode {{issue}}" {
		t.Errorf("GetPromptTemplate(opencode) = %q, want opencode {{issue}}", got)
	}
	if got := cfg.GetPromptTemplate("claude"); got != "claude {{issue}}" {
		t.Errorf("GetPromptTemplate(claude) = %q, want claude {{issue}}", got)
	}
	if got := cfg.GetPromptTemplate("custom"); got != "global {{issue}}" {
		t.Errorf("GetPromptTemplate(custom) = %q, want global {{issue}}", got)
	}
}

func TestGetIssuesPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name     string
		cfg      *Config
		wantPath string
	}{
		{
			name: "explicit path returns as-is",
			cfg: &Config{
				Issues: IssuesConfig{Path: "/explicit/path"},
			},
			wantPath: "/explicit/path",
		},
		{
			name: "tilde expansion",
			cfg: &Config{
				Issues: IssuesConfig{Path: "~/issues"},
			},
			wantPath: filepath.Join(home, "issues"),
		},
		{
			name:     "empty path returns default",
			cfg:      &Config{},
			wantPath: "", // Will check for prefix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetIssuesPath()
			if tt.wantPath == "" {
				// For default case, verify it uses XDG path
				expectedPrefix := filepath.Join(home, ".local", "share", "orch")
				if len(got) < len(expectedPrefix) || got[:len(expectedPrefix)] != expectedPrefix {
					t.Errorf("GetIssuesPath() = %q, want prefix %q", got, expectedPrefix)
				}
			} else if got != tt.wantPath {
				t.Errorf("GetIssuesPath() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}
