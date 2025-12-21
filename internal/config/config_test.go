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
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("vault: /repo\nagent: codex\nno_pr: true\n"), 0644); err != nil {
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
	if cfg.Vault != "/repo" || cfg.Agent != "codex" || !cfg.NoPR {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	t.Setenv("ORCH_VAULT", "/env")
	t.Setenv("ORCH_AGENT", "gemini")
	cfgEnv, err := Load()
	if err != nil {
		t.Fatalf("Load env error: %v", err)
	}
	if cfgEnv.Vault != "/env" || cfgEnv.Agent != "gemini" {
		t.Fatalf("unexpected env config: %+v", cfgEnv)
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
