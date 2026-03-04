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
	t.Setenv("ORCH_PROJECT", "")

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

	origProject := globalOpts.Project
	globalOpts.Project = ""
	t.Cleanup(func() { globalOpts.Project = origProject })

	err = validateConfigForCommand()
	if err == nil {
		t.Fatal("expected config validation error")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected invalid config error, got: %v", err)
	}
}

func TestValidateConfigForCommandIgnoresDeprecatedProjectRootField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT", "")

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
	if err != nil {
		t.Fatalf("expected validation to ignore deprecated project-root field, got: %v", err)
	}
}

func TestValidateConfigForCommandRejectsInvalidClientConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT", "")

	clientCfgDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(clientCfgDir, 0o755); err != nil {
		t.Fatalf("mkdir client config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clientCfgDir, "client.yaml"), []byte("remote:\n  oops: true\n"), 0o644); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	err := validateConfigForCommand()
	if err == nil {
		t.Fatal("expected client config validation error")
	}
	if !strings.Contains(err.Error(), "invalid client config") {
		t.Fatalf("expected invalid client config error, got: %v", err)
	}
}
