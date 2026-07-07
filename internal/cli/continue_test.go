package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/orchapi"
)

func TestRestartFromCommandDoesNotExposePathSelectorFlags(t *testing.T) {
	cmd := newRestartFromCmd()

	if flag := cmd.Flags().Lookup("repo-root"); flag != nil {
		t.Fatalf("unexpected repo-root flag: %#v", flag)
	}
	if flag := cmd.Flags().Lookup("worktree-dir"); flag != nil {
		t.Fatalf("unexpected worktree-dir flag: %#v", flag)
	}
}

func TestRestartFromCodexProfileHelpMatchesInheritanceSemantics(t *testing.T) {
	cmd := newRestartFromCmd()
	flag := cmd.Flags().Lookup("codex-profile")
	if flag == nil {
		t.Fatal("expected --codex-profile flag")
	}
	if !strings.Contains(flag.Usage, "defaults to the prior run's profile, then codex.default_profile") {
		t.Fatalf("codex-profile help = %q, want source-run profile before config default", flag.Usage)
	}
}

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
