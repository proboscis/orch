package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	globalDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte("vault: /global\nagent: claude\n"), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: /repo\nagent: codex\npr_target_branch: develop\nno_pr: true\n"), 0644); err != nil {
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
	if cfg.Vault != "/repo" || cfg.Agent != "codex" || !cfg.NoPR || cfg.PRTargetBranch != "develop" {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	t.Setenv("ORCH_VAULT", "/env")
	t.Setenv("ORCH_AGENT", "gemini")
	cfgEnv, err := Load()
	if err != nil {
		t.Fatalf("Load env error: %v", err)
	}
	if cfgEnv.Vault != "/repo" || cfgEnv.Agent != "codex" {
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
	if cfgEnvOnly.Vault != "/env" || cfgEnvOnly.Agent != "gemini" {
		t.Fatalf("unexpected env-only config: %+v", cfgEnvOnly)
	}
}

func TestParentConfigPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "/env")
	t.Setenv("ORCH_AGENT", "gemini")
	t.Setenv("ORCH_WORKTREE_DIR", "/env-worktrees")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: /parent\nagent: claude\n"), 0644); err != nil {
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
	if cfg.Vault != "/parent" || cfg.Agent != "claude" {
		t.Fatalf("unexpected parent config: %+v", cfg)
	}
	if cfg.WorktreeDir != "/env-worktrees" {
		t.Fatalf("unexpected env worktree_dir: %q", cfg.WorktreeDir)
	}

	if err := os.MkdirAll(filepath.Join(child, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir child .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, ".orch", "config.yaml"), []byte("vault: /local\n"), 0644); err != nil {
		t.Fatalf("write child config: %v", err)
	}

	cfgLocal, err := Load()
	if err != nil {
		t.Fatalf("Load local error: %v", err)
	}
	if cfgLocal.Vault != "/local" || cfgLocal.Agent != "claude" {
		t.Fatalf("unexpected local config: %+v", cfgLocal)
	}
	if cfgLocal.WorktreeDir != "/env-worktrees" {
		t.Fatalf("unexpected env worktree_dir (local): %q", cfgLocal.WorktreeDir)
	}
}

func TestLoadVaultCaseInsensitive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	repoVault := filepath.Join(repo, "VAULT")
	if err := os.MkdirAll(repoVault, 0755); err != nil {
		t.Fatalf("mkdir VAULT: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("Vault: "+repoVault+"\n"), 0644); err != nil {
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
	if cfg.Vault != repoVault {
		t.Fatalf("Vault = %q, want %q", cfg.Vault, repoVault)
	}
}

func TestLoadDefaultVault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	globalDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	defaultVault := filepath.Join(home, "vault")
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte("default_vault: "+defaultVault+"\n"), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

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
	if cfg.Vault != defaultVault {
		t.Fatalf("Vault = %q, want %q", cfg.Vault, defaultVault)
	}
}

func TestRepoConfigWithoutVaultUsesGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	globalDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte("default_vault: /global\n"), 0644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

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
	if cfg.Vault != "/global" {
		t.Fatalf("Vault = %q, want /global", cfg.Vault)
	}
	if cfg.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", cfg.Agent)
	}
}

func TestRepoConfigDir(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: /repo\n"), 0644); err != nil {
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

	if got := ExpandPath("~/vault", ""); got != filepath.Join("/home/test", "vault") {
		t.Fatalf("ExpandPath home = %q", got)
	}
	if got := ExpandPath("relative/path", "/base"); got != filepath.Join("/base", "relative/path") {
		t.Fatalf("ExpandPath relative = %q", got)
	}
}

func TestRelativeVaultPathResolution(t *testing.T) {
	// Clear environment variables that could interfere
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_WORKTREE_DIR", "")
	t.Setenv("ORCH_PROMPT_TEMPLATE", "")

	// Create a temp home directory to avoid loading user's global config
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a repo with relative vault path
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "VAULT"), 0755); err != nil {
		t.Fatalf("mkdir VAULT: %v", err)
	}

	// Test with ./VAULT relative path
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: ./VAULT\n"), 0644); err != nil {
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

	// Vault should be resolved to absolute path
	expectedVault, err := filepath.EvalSymlinks(filepath.Join(repo, "VAULT"))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotVault, err := filepath.EvalSymlinks(cfg.Vault)
	if err != nil {
		t.Fatalf("EvalSymlinks vault: %v", err)
	}

	if gotVault != expectedVault {
		t.Fatalf("vault path not resolved correctly: got %q, want %q", gotVault, expectedVault)
	}
}

func TestOpenCodeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_OPENCODE_DEFAULT_MODEL", "")
	t.Setenv("ORCH_OPENCODE_DEFAULT_VARIANT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `vault: /repo
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
	t.Setenv("ORCH_VAULT", "")
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

func TestControlAgentConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_CONTROL_AGENT", "")
	t.Setenv("ORCH_CONTROL_MODEL", "")
	t.Setenv("ORCH_CONTROL_MODEL_VARIANT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `vault: /repo
agent: claude
model: sonnet
model_variant: default
control_agent: opencode
control_model: opus
control_model_variant: high
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

	if cfg.GetControlAgent() != "opencode" {
		t.Fatalf("GetControlAgent = %q, want opencode", cfg.GetControlAgent())
	}
	if cfg.GetControlModel() != "opus" {
		t.Fatalf("GetControlModel = %q, want opus", cfg.GetControlModel())
	}
	if cfg.GetControlModelVariant() != "high" {
		t.Fatalf("GetControlModelVariant = %q, want high", cfg.GetControlModelVariant())
	}
}

func TestControlAgentConfigFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_CONTROL_AGENT", "")
	t.Setenv("ORCH_CONTROL_MODEL", "")
	t.Setenv("ORCH_CONTROL_MODEL_VARIANT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `vault: /repo
agent: claude
model: sonnet
model_variant: default
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

	if cfg.GetControlAgent() != "claude" {
		t.Fatalf("GetControlAgent fallback = %q, want claude", cfg.GetControlAgent())
	}
	if cfg.GetControlModel() != "sonnet" {
		t.Fatalf("GetControlModel fallback = %q, want sonnet", cfg.GetControlModel())
	}
	if cfg.GetControlModelVariant() != "default" {
		t.Fatalf("GetControlModelVariant fallback = %q, want default", cfg.GetControlModelVariant())
	}
}

func TestControlAgentConfigEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_CONTROL_AGENT", "gemini")
	t.Setenv("ORCH_CONTROL_MODEL", "gemini-pro")
	t.Setenv("ORCH_CONTROL_MODEL_VARIANT", "max")

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
	if cfg.GetControlAgent() != "gemini" {
		t.Fatalf("GetControlAgent = %q, want gemini", cfg.GetControlAgent())
	}
	if cfg.GetControlModel() != "gemini-pro" {
		t.Fatalf("GetControlModel = %q, want gemini-pro", cfg.GetControlModel())
	}
	if cfg.GetControlModelVariant() != "max" {
		t.Fatalf("GetControlModelVariant = %q, want max", cfg.GetControlModelVariant())
	}
}

func TestRelativePathFromSubdirectory(t *testing.T) {
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_WORKTREE_DIR", "")

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a repo with a subdirectory
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "VAULT"), 0755); err != nil {
		t.Fatalf("mkdir VAULT: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src", "subdir"), 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: ./VAULT\nworktree_dir: .git-worktrees\n"), 0644); err != nil {
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

	expectedVault, err := filepath.EvalSymlinks(filepath.Join(repo, "VAULT"))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	gotVault, err := filepath.EvalSymlinks(cfg.Vault)
	if err != nil {
		t.Fatalf("EvalSymlinks vault: %v", err)
	}

	if gotVault != expectedVault {
		t.Fatalf("vault not resolved relative to repo root: got %q, want %q", gotVault, expectedVault)
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
