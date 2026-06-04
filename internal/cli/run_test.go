package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
)

func applyPromptConfigDefaultsForTest(opts *runOptions) (*config.Config, error) {
	return applyPromptConfigDefaultsForTestWithRemote(opts, false)
}

func applyPromptConfigDefaultsForTestWithRemote(opts *runOptions, remoteMode bool) (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	orchCfg := configToOrchapi(cfg)

	presetName := opts.Preset
	if presetName == "" && cfg.DefaultPreset != "" {
		presetName = cfg.DefaultPreset
	}
	if presetName != "" {
		preset := findPreset(orchCfg.Presets, presetName)
		if preset == nil {
			return nil, fmt.Errorf("preset not found: %s", presetName)
		}
	}

	applyConfigDefaults(opts, orchCfg, remoteMode)
	return cfg, nil
}

func configToOrchapi(cfg *config.Config) *orchapi.Config {
	var presets []orchapi.Preset
	for _, p := range cfg.Presets {
		presets = append(presets, orchapi.Preset{
			Name:    p.Name,
			Backend: p.Backend,
			Model:   p.Model,
			Variant: p.Variant,
			Profile: p.Profile,
		})
	}
	for _, p := range cfg.OpenCodePresets {
		presets = append(presets, orchapi.Preset{
			Name:    p.Name,
			Backend: "opencode",
			Model:   p.Model,
			Variant: p.Variant,
		})
	}

	return &orchapi.Config{
		Agent:            cfg.Agent,
		Model:            cfg.Model,
		ModelVariant:     cfg.ModelVariant,
		WorktreeDir:      cfg.WorktreeDir,
		BaseBranch:       cfg.BaseBranch,
		PRTargetBranch:   cfg.PRTargetBranch,
		PromptTemplate:   cfg.PromptTemplate,
		Multiplexer:      cfg.Multiplexer,
		AgentMultiplexer: cfg.GetAgentMultiplexer(),
		NoPR:             cfg.NoPR,
		DefaultPreset:    cfg.DefaultPreset,
		Presets:          presets,
		OpenCode: orchapi.OpenCodeConfig{
			DefaultModel:   cfg.OpenCode.DefaultModel,
			DefaultVariant: cfg.OpenCode.DefaultVariant,
		},
	}
}

func TestBuildAgentPromptDefault(t *testing.T) {
	issue := &model.Issue{
		ID:    "orch-1",
		Title: "Title",
		Body:  "Body text",
	}

	prompt := buildAgentPrompt(issue, &promptOptions{})
	if !strings.Contains(prompt, issue.Body) {
		t.Fatalf("prompt missing body: %q", prompt)
	}
	if !strings.Contains(prompt, "create a pull request") {
		t.Fatalf("prompt missing PR instructions: %q", prompt)
	}
	if !strings.Contains(prompt, "create a pull request targeting `main`") {
		t.Fatalf("prompt missing PR target branch: %q", prompt)
	}
}

func TestRunCommandDoesNotExposePathSelectorFlags(t *testing.T) {
	cmd := newRunCmd()

	if flag := cmd.Flags().Lookup("repo-root"); flag != nil {
		t.Fatalf("unexpected repo-root flag: %#v", flag)
	}
	if flag := cmd.Flags().Lookup("worktree-dir"); flag != nil {
		t.Fatalf("unexpected worktree-dir flag: %#v", flag)
	}
}

func TestBuildAgentPromptWithBaseBranch(t *testing.T) {
	issue := &model.Issue{
		ID:    "orch-1",
		Title: "Title",
		Body:  "Body text",
	}

	prompt := buildAgentPrompt(issue, &promptOptions{BaseBranch: "develop"})
	if !strings.Contains(prompt, "create a pull request targeting `develop`") {
		t.Fatalf("prompt missing base branch in PR instructions: %q", prompt)
	}
}

func TestBuildAgentPromptNoPR(t *testing.T) {
	issue := &model.Issue{ID: "orch-2", Body: "Body"}
	prompt := buildAgentPrompt(issue, &promptOptions{NoPR: true})
	if strings.Contains(prompt, "create a pull request") {
		t.Fatalf("unexpected PR instructions: %q", prompt)
	}
}

func TestBuildAgentPromptTargetBranch(t *testing.T) {
	issue := &model.Issue{ID: "orch-3", Body: "Body"}
	prompt := buildAgentPrompt(issue, &promptOptions{PRTargetBranch: "develop"})
	if !strings.Contains(prompt, "create a pull request targeting `develop`") {
		t.Fatalf("prompt missing custom PR target branch: %q", prompt)
	}
}

func TestBuildAgentPromptRespectsConfiguredTargetBranch(t *testing.T) {
	issue := &model.Issue{ID: "test-issue", Body: "Test body"}

	testCases := []struct {
		name         string
		targetBranch string
	}{
		{"develop branch", "develop"},
		{"release branch", "release/v1.0"},
		{"feature branch", "feature/my-feature"},
		{"custom branch", "my-custom-branch"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prompt := buildAgentPrompt(issue, &promptOptions{PRTargetBranch: tc.targetBranch})

			expected := fmt.Sprintf("create a pull request targeting `%s`", tc.targetBranch)
			if !strings.Contains(prompt, expected) {
				t.Errorf("prompt should contain %q but got: %q", expected, prompt)
			}

			if tc.targetBranch != "main" && strings.Contains(prompt, "targeting `main`") {
				t.Errorf("prompt should not contain hardcoded 'main' when target branch is %q", tc.targetBranch)
			}
		})
	}
}

func TestBuildAgentPromptCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "prompt.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Issue: {{.IssueID}} - {{.Title}}"), 0644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	issue := &model.Issue{ID: "orch-3", Title: "Custom"}
	prompt := buildAgentPrompt(issue, &promptOptions{PromptTemplate: tmplPath})
	if !strings.Contains(prompt, "Reference to the issue: orch-3") {
		t.Fatalf("expected fallback prompt to include issue reference, got: %q", prompt)
	}
}

func TestExecuteTemplateFallback(t *testing.T) {
	issue := &model.Issue{ID: "orch-4"}
	prompt := executeTemplate("{{", issue, &promptOptions{})
	if !strings.Contains(prompt, "You are working on issue: orch-4") {
		t.Fatalf("unexpected fallback prompt: %q", prompt)
	}
}

func TestApplyPromptConfigDefaults(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROMPT_TEMPLATE", "")
	t.Setenv("ORCH_NO_PR", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	tmplPath := filepath.Join(repo, "prompt.tmpl")
	if err := os.WriteFile(tmplPath, []byte("tmpl"), 0644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	configData := fmt.Sprintf("prompt_template: %s\npr_target_branch: develop\nno_pr: true\n", tmplPath)
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
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

	opts := &runOptions{}
	if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
	}
	if opts.PromptTemplate != tmplPath {
		t.Fatalf("PromptTemplate = %q, want %q", opts.PromptTemplate, tmplPath)
	}
	if !opts.NoPR {
		t.Fatalf("NoPR = false, want true")
	}
	if opts.PRTargetBranch != "develop" {
		t.Fatalf("PRTargetBranch = %q, want develop", opts.PRTargetBranch)
	}

	opts2 := &runOptions{PromptTemplate: "explicit", NoPR: true, PRTargetBranch: "release"}
	if _, err := applyPromptConfigDefaultsForTest(opts2); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTest explicit: %v", err)
	}
	if opts2.PromptTemplate != "explicit" {
		t.Fatalf("PromptTemplate override = %q", opts2.PromptTemplate)
	}
	if opts2.PRTargetBranch != "release" {
		t.Fatalf("PRTargetBranch override = %q", opts2.PRTargetBranch)
	}
	if !opts2.NoPR {
		t.Fatalf("NoPR override = false, want true")
	}
}

func TestApplyConfigDefaultsBaseBranch(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_BASE_BRANCH", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_WORKTREE_DIR", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	// Test config with custom values
	configData := "base_branch: develop\nagent: codex\nworktree_dir: custom-worktrees\n"
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
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

	// Test: config values should be applied when flags are empty
	opts := &runOptions{}
	if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
	}
	// BaseBranch is intentionally NOT pre-filled by the CLI: the daemon owns
	// base-branch resolution (explicit flag > issue base_branch > config > "main")
	// so the per-issue value is not shadowed. It stays empty (unset) here.
	if opts.BaseBranch != "" {
		t.Fatalf("BaseBranch = %q, want %q (resolved by daemon, not CLI)", opts.BaseBranch, "")
	}
	if opts.Agent != "codex" {
		t.Fatalf("Agent = %q, want %q", opts.Agent, "codex")
	}
	// Compare paths after resolving symlinks (macOS /var -> /private/var)
	wantWorktreeDir := filepath.Join(repo, "custom-worktrees")
	gotWorktreeDir, _ := filepath.EvalSymlinks(opts.WorktreeDir)
	wantWorktreeDirResolved, _ := filepath.EvalSymlinks(wantWorktreeDir)
	if gotWorktreeDir != wantWorktreeDirResolved {
		t.Fatalf("WorktreeDir = %q, want %q", opts.WorktreeDir, wantWorktreeDir)
	}

	// Test: explicit flags should override config values
	opts2 := &runOptions{BaseBranch: "feature", Agent: "claude", WorktreeDir: "explicit-worktrees"}
	if _, err := applyPromptConfigDefaultsForTest(opts2); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTest explicit: %v", err)
	}
	if opts2.BaseBranch != "feature" {
		t.Fatalf("BaseBranch override = %q, want %q", opts2.BaseBranch, "feature")
	}
	if opts2.Agent != "claude" {
		t.Fatalf("Agent override = %q, want %q", opts2.Agent, "claude")
	}
	if opts2.WorktreeDir != "explicit-worktrees" {
		t.Fatalf("WorktreeDir override = %q, want %q", opts2.WorktreeDir, "explicit-worktrees")
	}
}

func TestApplyConfigDefaultsFallbacks(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_BASE_BRANCH", "")
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_WORKTREE_DIR", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	// Empty config - should use fallback defaults
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("{}\n"), 0644); err != nil {
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

	// Test: when config is empty, fallback defaults should be used
	opts := &runOptions{}
	if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
	}
	// BaseBranch stays empty at the CLI layer; the daemon applies the "main"
	// fallback (after the per-issue base_branch) so it is not resolved here.
	if opts.BaseBranch != "" {
		t.Fatalf("BaseBranch fallback = %q, want %q (resolved by daemon, not CLI)", opts.BaseBranch, "")
	}
	if opts.Agent != "claude" {
		t.Fatalf("Agent fallback = %q, want %q", opts.Agent, "claude")
	}
	// Default is now ~/.orch/worktrees
	wantWorktreeDir := filepath.Join(home, ".orch", "worktrees")
	if opts.WorktreeDir != wantWorktreeDir {
		t.Fatalf("WorktreeDir fallback = %q, want %q", opts.WorktreeDir, wantWorktreeDir)
	}
}

func TestApplyConfigDefaultsRemoteDoesNotForceLocalWorktreeDefault(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("{}\n"), 0644); err != nil {
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

	opts := &runOptions{}
	if _, err := applyPromptConfigDefaultsForTestWithRemote(opts, true); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTestWithRemote: %v", err)
	}
	if opts.WorktreeDir != "" {
		t.Fatalf("WorktreeDir remote fallback = %q, want empty", opts.WorktreeDir)
	}

	opts.WorktreeDir = ""
	opts.WorktreeSet = true
	if _, err := applyPromptConfigDefaultsForTestWithRemote(opts, true); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTestWithRemote explicit: %v", err)
	}
	if opts.WorktreeDir != "" {
		t.Fatalf("WorktreeDir explicit empty = %q, want empty", opts.WorktreeDir)
	}
}

func TestApplyPresetFromConfig(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_MODEL", "")
	t.Setenv("ORCH_MODEL_VARIANT", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configData := `presets:
  - name: opus:high
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: high
  - name: gpt5:xhigh
    backend: opencode
    model: openai/gpt-5.2-codex
    variant: xhigh
  - name: claude:myprofile
    backend: claude
    profile: myprofile
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
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

	t.Run("preset sets agent and forwards preset name", func(t *testing.T) {
		opts := &runOptions{Preset: "opus:high"}
		if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
		}
		if opts.Agent != "opencode" {
			t.Errorf("Agent = %q, want opencode", opts.Agent)
		}
		// Model and variant resolution is handled by the daemon, not CLI.
		// CLI just forwards the preset name; daemon resolves model/variant.
		if opts.Preset != "opus:high" {
			t.Errorf("Preset = %q, want opus:high", opts.Preset)
		}
	})

	t.Run("preset sets claude backend and profile", func(t *testing.T) {
		opts := &runOptions{Preset: "claude:myprofile"}
		if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
		}
		if opts.Agent != "claude" {
			t.Errorf("Agent = %q, want claude", opts.Agent)
		}
		if opts.AgentProfile != "myprofile" {
			t.Errorf("AgentProfile = %q, want myprofile", opts.AgentProfile)
		}
	})

	t.Run("explicit agent flag overrides preset", func(t *testing.T) {
		opts := &runOptions{
			Preset:       "opus:high",
			Agent:        "codex",
			Model:        "explicit-model",
			ModelVariant: "explicit-variant",
		}
		if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
		}
		if opts.Agent != "codex" {
			t.Errorf("Agent = %q, want codex (explicit override)", opts.Agent)
		}
		// Model/variant flags are forwarded as-is to daemon for resolution
		if opts.Model != "explicit-model" {
			t.Errorf("Model = %q, want explicit-model (passthrough)", opts.Model)
		}
		if opts.ModelVariant != "explicit-variant" {
			t.Errorf("ModelVariant = %q, want explicit-variant (passthrough)", opts.ModelVariant)
		}
	})

	t.Run("nonexistent preset returns error", func(t *testing.T) {
		opts := &runOptions{Preset: "nonexistent"}
		_, err := applyPromptConfigDefaultsForTest(opts)
		if err == nil {
			t.Fatal("expected error for nonexistent preset")
		}
		if !strings.Contains(err.Error(), "preset not found") {
			t.Errorf("error = %q, want to contain 'preset not found'", err.Error())
		}
	})
}

func TestApplyPresetLegacyOpenCodePresets(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_MODEL", "")
	t.Setenv("ORCH_MODEL_VARIANT", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configData := `opencode_presets:
  - name: legacy:preset
    model: anthropic/claude-sonnet-4-5
    variant: max
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
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

	opts := &runOptions{Preset: "legacy:preset"}
	if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
	}
	if opts.Agent != "opencode" {
		t.Errorf("Agent = %q, want opencode (legacy presets default to opencode)", opts.Agent)
	}
	// Model/variant resolution is handled by daemon; CLI just forwards preset name
	if opts.Preset != "legacy:preset" {
		t.Errorf("Preset = %q, want legacy:preset", opts.Preset)
	}
}

func TestApplyDefaultPreset(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_MODEL", "")
	t.Setenv("ORCH_MODEL_VARIANT", "")
	t.Setenv("ORCH_DEFAULT_PRESET", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configData := `presets:
  - name: opus:high
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: high
  - name: sonnet:max
    backend: opencode
    model: anthropic/claude-sonnet-4-5
    variant: max
default_preset: opus:high
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
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

	t.Run("default_preset applied when no --preset flag", func(t *testing.T) {
		opts := &runOptions{}
		if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
		}
		if opts.Agent != "opencode" {
			t.Errorf("Agent = %q, want opencode", opts.Agent)
		}
		// CLI forwards preset name to daemon; daemon resolves model/variant
		if opts.Preset != "opus:high" {
			t.Errorf("Preset = %q, want opus:high", opts.Preset)
		}
	})

	t.Run("explicit --preset overrides default_preset", func(t *testing.T) {
		opts := &runOptions{Preset: "sonnet:max"}
		if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
		}
		if opts.Agent != "opencode" {
			t.Errorf("Agent = %q, want opencode", opts.Agent)
		}
		if opts.Preset != "sonnet:max" {
			t.Errorf("Preset = %q, want sonnet:max", opts.Preset)
		}
	})

	t.Run("explicit flags override default_preset values", func(t *testing.T) {
		opts := &runOptions{
			Agent:        "codex",
			Model:        "explicit-model",
			ModelVariant: "explicit-variant",
		}
		if _, err := applyPromptConfigDefaultsForTest(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaultsForTest: %v", err)
		}
		if opts.Agent != "codex" {
			t.Errorf("Agent = %q, want codex (explicit override)", opts.Agent)
		}
		// Model/variant flags are forwarded as-is to daemon
		if opts.Model != "explicit-model" {
			t.Errorf("Model = %q, want explicit-model (passthrough)", opts.Model)
		}
		if opts.ModelVariant != "explicit-variant" {
			t.Errorf("ModelVariant = %q, want explicit-variant (passthrough)", opts.ModelVariant)
		}
	})
}

func TestApplyDefaultPresetNotFound(t *testing.T) {
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ORCH_AGENT", "")
	t.Setenv("ORCH_DEFAULT_PRESET", "")

	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	configData := `default_preset: nonexistent
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
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

	opts := &runOptions{}
	_, err = applyPromptConfigDefaultsForTest(opts)
	if err == nil {
		t.Fatal("expected error for nonexistent default_preset")
	}
	if !strings.Contains(err.Error(), "preset not found") {
		t.Errorf("error = %q, want to contain 'preset not found'", err.Error())
	}
}
