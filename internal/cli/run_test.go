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
	if !strings.Contains(prompt, "Target branch: main") {
		t.Fatalf("prompt missing PR target branch: %q", prompt)
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
	if !strings.Contains(prompt, "Target branch: develop") {
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
