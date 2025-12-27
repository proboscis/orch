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

	configData := fmt.Sprintf("prompt_template: %s\nno_pr: true\n", tmplPath)
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

	opts2 := &runOptions{PromptTemplate: "explicit", NoPR: true}
	if err := applyPromptConfigDefaults(opts2); err != nil {
		t.Fatalf("applyPromptConfigDefaults explicit: %v", err)
	}
	if opts2.PromptTemplate != "explicit" {
		t.Fatalf("PromptTemplate override = %q", opts2.PromptTemplate)
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
