package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfigForCommandRejectsInvalidRepoConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT_ROOT", "")
	t.Setenv("ORCH_ISSUES_ROOT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("agent: invalid-agent\n"), 0o644); err != nil {
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

	origProjectRoot := globalOpts.ProjectRoot
	globalOpts.ProjectRoot = ""
	t.Cleanup(func() { globalOpts.ProjectRoot = origProjectRoot })

	err = validateConfigForCommand()
	if err == nil {
		t.Fatal("expected config validation error")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected invalid config error, got: %v", err)
	}
}

func TestValidateConfigForCommandUsesProjectRootFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT_ROOT", "")
	t.Setenv("ORCH_ISSUES_ROOT", "")

	validRepo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(validRepo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir valid .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(validRepo, ".orch", "config.yaml"), []byte("agent: opencode\n"), 0o644); err != nil {
		t.Fatalf("write valid config: %v", err)
	}

	invalidRepo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(invalidRepo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir invalid .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidRepo, ".orch", "config.yaml"), []byte("issues:\n  backend: nope\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(validRepo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	origProjectRoot := globalOpts.ProjectRoot
	globalOpts.ProjectRoot = invalidRepo
	t.Cleanup(func() { globalOpts.ProjectRoot = origProjectRoot })

	err = validateConfigForCommand()
	if err == nil {
		t.Fatal("expected config validation error from --project-root")
	}
	if !strings.Contains(err.Error(), "issues.backend must be one of") {
		t.Fatalf("expected issues backend validation error, got: %v", err)
	}
}
