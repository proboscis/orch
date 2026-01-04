package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
)

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

func TestBuildAgentPromptCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "prompt.tmpl")
	if err := os.WriteFile(tmplPath, []byte("Issue: {{.IssueID}} - {{.Title}}"), 0644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	issue := &model.Issue{ID: "orch-3", Title: "Custom"}
	prompt := buildAgentPrompt(issue, &promptOptions{PromptTemplate: tmplPath})
	if strings.TrimSpace(prompt) != "Issue: orch-3 - Custom" {
		t.Fatalf("unexpected prompt: %q", prompt)
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
	if err := applyPromptConfigDefaults(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaults: %v", err)
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
	if err := applyPromptConfigDefaults(opts2); err != nil {
		t.Fatalf("applyPromptConfigDefaults explicit: %v", err)
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
	if err := applyPromptConfigDefaults(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaults: %v", err)
	}
	if opts.BaseBranch != "develop" {
		t.Fatalf("BaseBranch = %q, want %q", opts.BaseBranch, "develop")
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
	if err := applyPromptConfigDefaults(opts2); err != nil {
		t.Fatalf("applyPromptConfigDefaults explicit: %v", err)
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
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(""), 0644); err != nil {
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
	if err := applyPromptConfigDefaults(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaults: %v", err)
	}
	if opts.BaseBranch != "main" {
		t.Fatalf("BaseBranch fallback = %q, want %q", opts.BaseBranch, "main")
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

	t.Run("preset sets agent model and variant", func(t *testing.T) {
		opts := &runOptions{Preset: "opus:high"}
		if err := applyPromptConfigDefaults(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaults: %v", err)
		}
		if opts.Agent != "opencode" {
			t.Errorf("Agent = %q, want opencode", opts.Agent)
		}
		if opts.Model != "anthropic/claude-opus-4-5" {
			t.Errorf("Model = %q, want anthropic/claude-opus-4-5", opts.Model)
		}
		if opts.ModelVariant != "high" {
			t.Errorf("ModelVariant = %q, want high", opts.ModelVariant)
		}
	})

	t.Run("preset sets claude backend and profile", func(t *testing.T) {
		opts := &runOptions{Preset: "claude:myprofile"}
		if err := applyPromptConfigDefaults(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaults: %v", err)
		}
		if opts.Agent != "claude" {
			t.Errorf("Agent = %q, want claude", opts.Agent)
		}
		if opts.AgentProfile != "myprofile" {
			t.Errorf("AgentProfile = %q, want myprofile", opts.AgentProfile)
		}
	})

	t.Run("explicit flags override preset", func(t *testing.T) {
		opts := &runOptions{
			Preset:       "opus:high",
			Agent:        "codex",
			Model:        "explicit-model",
			ModelVariant: "explicit-variant",
		}
		if err := applyPromptConfigDefaults(opts); err != nil {
			t.Fatalf("applyPromptConfigDefaults: %v", err)
		}
		if opts.Agent != "codex" {
			t.Errorf("Agent = %q, want codex (explicit override)", opts.Agent)
		}
		if opts.Model != "explicit-model" {
			t.Errorf("Model = %q, want explicit-model (explicit override)", opts.Model)
		}
		if opts.ModelVariant != "explicit-variant" {
			t.Errorf("ModelVariant = %q, want explicit-variant (explicit override)", opts.ModelVariant)
		}
	})

	t.Run("nonexistent preset returns error", func(t *testing.T) {
		opts := &runOptions{Preset: "nonexistent"}
		err := applyPromptConfigDefaults(opts)
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
	if err := applyPromptConfigDefaults(opts); err != nil {
		t.Fatalf("applyPromptConfigDefaults: %v", err)
	}
	if opts.Agent != "opencode" {
		t.Errorf("Agent = %q, want opencode (legacy presets default to opencode)", opts.Agent)
	}
	if opts.Model != "anthropic/claude-sonnet-4-5" {
		t.Errorf("Model = %q, want anthropic/claude-sonnet-4-5", opts.Model)
	}
	if opts.ModelVariant != "max" {
		t.Errorf("ModelVariant = %q, want max", opts.ModelVariant)
	}
}
