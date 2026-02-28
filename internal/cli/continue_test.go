package cli

import (
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/orchapi"
)

func TestApplyContinueConfigDefaultsLocalFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	opts := &continueOptions{}
	cfg := &orchapi.Config{}

	applyContinueConfigDefaults(opts, cfg, false)

	want := filepath.Join(home, ".orch", "worktrees")
	if opts.WorktreeDir != want {
		t.Fatalf("WorktreeDir = %q, want %q", opts.WorktreeDir, want)
	}
}

func TestApplyContinueConfigDefaultsRemoteKeepsEmptyWhenUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	opts := &continueOptions{}
	cfg := &orchapi.Config{}

	applyContinueConfigDefaults(opts, cfg, true)

	if opts.WorktreeDir != "" {
		t.Fatalf("WorktreeDir remote unset = %q, want empty", opts.WorktreeDir)
	}
}

func TestApplyContinueConfigDefaultsRemoteUsesConfigAndRespectsExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &orchapi.Config{WorktreeDir: "/srv/worktrees"}

	opts := &continueOptions{}
	applyContinueConfigDefaults(opts, cfg, true)
	if opts.WorktreeDir != "/srv/worktrees" {
		t.Fatalf("WorktreeDir from config = %q, want /srv/worktrees", opts.WorktreeDir)
	}

	opts = &continueOptions{WorktreeDir: "", WorktreeSet: true}
	applyContinueConfigDefaults(opts, cfg, true)
	if opts.WorktreeDir != "" {
		t.Fatalf("explicit empty WorktreeDir = %q, want empty", opts.WorktreeDir)
	}
}
