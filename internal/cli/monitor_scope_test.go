package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func TestResolveMonitorProjectScopeLocalDropsImplicitCWDProject(t *testing.T) {
	withRepoProjectScope(t)

	origProject := globalOpts.Project
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Project = ""
	globalOpts.Remote = ""
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Project = origProject
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	projectRoot, explicitProjectRoot, remote := resolveMonitorProjectScope()
	if remote {
		t.Fatal("expected local mode")
	}
	if explicitProjectRoot {
		t.Fatal("expected implicit cwd-derived project scope in local mode")
	}
	if projectRoot != "" {
		t.Fatalf("projectRoot = %q, want empty", projectRoot)
	}
}

func TestResolveMonitorProjectScopeRemoteDropsImplicitCWDProject(t *testing.T) {
	withRepoProjectScope(t)

	origProject := globalOpts.Project
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Project = ""
	globalOpts.Remote = "zeus:7777"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Project = origProject
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	projectRoot, explicitProjectRoot, remote := resolveMonitorProjectScope()
	if !remote {
		t.Fatal("expected remote mode")
	}
	if explicitProjectRoot {
		t.Fatal("expected non-explicit project scope")
	}
	if projectRoot != "" {
		t.Fatalf("projectRoot = %q, want empty", projectRoot)
	}
}

func TestResolveMonitorProjectScopeRemoteKeepsExplicitProject(t *testing.T) {
	repo := withRepoProjectScope(t)

	origProject := globalOpts.Project
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Project = "example-" + filepath.Base(repo)
	globalOpts.Remote = "zeus:7777"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Project = origProject
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	projectRoot, explicitProjectRoot, remote := resolveMonitorProjectScope()
	if !remote {
		t.Fatal("expected remote mode")
	}
	if !explicitProjectRoot {
		t.Fatal("expected explicit project scope")
	}
	if canonicalPath(projectRoot) != canonicalPath(repo) {
		t.Fatalf("projectRoot = %q, want %q", projectRoot, repo)
	}
}

func TestRunMonitorKillAllRequiresExplicitProjectScope(t *testing.T) {
	withRepoProjectScope(t)

	origProject := globalOpts.Project
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Project = ""
	globalOpts.Remote = ""
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Project = origProject
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	err := runMonitorKill("", &monitorKillOptions{All: true, Global: false})
	if err == nil {
		t.Fatal("expected explicit project scope error")
	}
	if !strings.Contains(err.Error(), "project scope required for --all without --global") {
		t.Fatalf("unexpected error: %v", err)
	}
}
