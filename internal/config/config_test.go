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

func TestPresets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `vault: /repo
presets:
  - name: opus:high
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: high
  - name: gpt5.2-codex:xhigh
    backend: opencode
    model: openai/gpt-5.2-codex
    variant: xhigh
  - name: codex:default
    backend: codex
    model: gpt-5.2-codex
  - name: claude:fast
    backend: claude
    profile: fast-profile
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

	if len(cfg.Presets) != 4 {
		t.Fatalf("Presets length = %d, want 4", len(cfg.Presets))
	}

	preset := cfg.GetPreset("opus:high")
	if preset == nil {
		t.Fatalf("GetPreset(opus:high) returned nil")
	}
	if preset.Backend != "opencode" || preset.Model != "anthropic/claude-opus-4-5" || preset.Variant != "high" {
		t.Fatalf("unexpected preset: %+v", preset)
	}

	opts := cfg.ResolvePreset("claude:fast")
	if opts == nil {
		t.Fatalf("ResolvePreset(claude:fast) returned nil")
	}
	if opts.Agent != "claude" || opts.Profile != "fast-profile" {
		t.Fatalf("unexpected resolved preset: %+v", opts)
	}

	missingOpts := cfg.ResolvePreset("nonexistent")
	if missingOpts != nil {
		t.Fatalf("ResolvePreset(nonexistent) should return nil")
	}
}

func TestLegacyOpenCodePresetsBackwardCompat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `vault: /repo
opencode_presets:
  - name: opus:high
    model: anthropic/claude-opus-4-5
    variant: high
  - name: sonnet:max
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

	if len(cfg.OpenCodePresets) != 2 {
		t.Fatalf("OpenCodePresets length = %d, want 2", len(cfg.OpenCodePresets))
	}

	preset := cfg.GetPreset("opus:high")
	if preset == nil {
		t.Fatalf("GetPreset(opus:high) returned nil for legacy preset")
	}
	if preset.Backend != "opencode" || preset.Model != "anthropic/claude-opus-4-5" || preset.Variant != "high" {
		t.Fatalf("unexpected legacy preset converted: %+v", preset)
	}

	opts := cfg.ResolvePreset("sonnet:max")
	if opts == nil {
		t.Fatalf("ResolvePreset(sonnet:max) returned nil for legacy preset")
	}
	if opts.Agent != "opencode" || opts.Model != "anthropic/claude-sonnet-4-5" || opts.ModelVariant != "max" {
		t.Fatalf("unexpected resolved legacy preset: %+v", opts)
	}

	allPresets := cfg.GetAllPresets()
	if len(allPresets) != 2 {
		t.Fatalf("GetAllPresets length = %d, want 2", len(allPresets))
	}
}

func TestMixedPresetsAndLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_VAULT", "")
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configContent := `vault: /repo
presets:
  - name: gpt5:xhigh
    backend: opencode
    model: openai/gpt-5.2-codex
    variant: xhigh
opencode_presets:
  - name: opus:high
    model: anthropic/claude-opus-4-5
    variant: high
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

	allPresets := cfg.GetAllPresets()
	if len(allPresets) != 2 {
		t.Fatalf("GetAllPresets length = %d, want 2", len(allPresets))
	}

	gpt5 := cfg.GetPreset("gpt5:xhigh")
	if gpt5 == nil || gpt5.Model != "openai/gpt-5.2-codex" {
		t.Fatalf("GetPreset(gpt5:xhigh) failed: %+v", gpt5)
	}

	opus := cfg.GetPreset("opus:high")
	if opus == nil || opus.Model != "anthropic/claude-opus-4-5" {
		t.Fatalf("GetPreset(opus:high) failed: %+v", opus)
	}
}
