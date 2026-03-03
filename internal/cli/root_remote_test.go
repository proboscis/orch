package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/config"
)

func withNoProjectScope(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT_ROOT", "")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func withRepoProjectScope(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT_ROOT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("agent: opencode\n"), 0o644); err != nil {
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

	return repo
}

func TestResolveRemoteAddrPrecedence(t *testing.T) {
	clientCfg := &config.ClientConfig{
		Remote: config.ClientRemoteConfig{
			Default: "zeus",
			Hosts: map[string]config.ClientRemoteHost{
				"zeus":  {Addr: "zeus:7777"},
				"cloud": {Addr: "10.0.0.5:7777"},
			},
		},
	}

	tests := []struct {
		name        string
		flagValue   string
		flagChanged bool
		envValue    string
		want        string
	}{
		{
			name:        "explicit empty flag forces local",
			flagValue:   "",
			flagChanged: true,
			envValue:    "cloud",
			want:        "",
		},
		{
			name:        "flag alias resolves via hosts",
			flagValue:   "cloud",
			flagChanged: true,
			envValue:    "",
			want:        "10.0.0.5:7777",
		},
		{
			name:        "env alias resolves when flag not set",
			flagValue:   "",
			flagChanged: false,
			envValue:    "zeus",
			want:        "zeus:7777",
		},
		{
			name:        "client default applies when no flag or env",
			flagValue:   "",
			flagChanged: false,
			envValue:    "",
			want:        "zeus:7777",
		},
		{
			name:        "raw address passes through",
			flagValue:   "",
			flagChanged: false,
			envValue:    "host.example:9999",
			want:        "host.example:9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRemoteAddr(tt.flagValue, tt.flagChanged, tt.envValue, clientCfg)
			if got != tt.want {
				t.Fatalf("resolveRemoteAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultGetAPIWithOptionsRequiresProjectScopeLocal(t *testing.T) {
	withNoProjectScope(t)

	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Remote = ""
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	_, err := defaultGetAPIWithOptions(true)
	if err == nil {
		t.Fatal("expected project-scope error")
	}
	if !strings.Contains(err.Error(), "project scope required for this command") {
		t.Fatalf("expected project scope guidance, got: %v", err)
	}
}

func TestDefaultGetAPIWithOptionsRequiresProjectScopeRemote(t *testing.T) {
	withNoProjectScope(t)

	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Remote = "zeus:7777"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	_, err := defaultGetAPIWithOptions(true)
	if err == nil {
		t.Fatal("expected project-scope error")
	}
	if !strings.Contains(err.Error(), "project scope required for this command") {
		t.Fatalf("expected project scope guidance, got: %v", err)
	}
}

func TestDefaultGetAPIWithOptionsRemoteRequiresExplicitProjectScope(t *testing.T) {
	withRepoProjectScope(t)

	origProjectRoot := globalOpts.ProjectRoot
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.ProjectRoot = ""
	globalOpts.Remote = "zeus:7777"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.ProjectRoot = origProjectRoot
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	_, err := defaultGetAPIWithOptions(true)
	if err == nil {
		t.Fatal("expected explicit project scope error")
	}
	if !strings.Contains(err.Error(), "project scope required for remote command") {
		t.Fatalf("expected remote explicit project scope guidance, got: %v", err)
	}
}

func TestDefaultGetAPIWithOptionsRemoteAllowsExplicitProjectRootFlag(t *testing.T) {
	repo := withRepoProjectScope(t)

	origProjectRoot := globalOpts.ProjectRoot
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.ProjectRoot = repo
	globalOpts.Remote = "zeus:7777"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.ProjectRoot = origProjectRoot
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	api, err := defaultGetAPIWithOptions(true)
	if err != nil {
		t.Fatalf("expected explicit project root to pass in remote mode, got: %v", err)
	}
	if api == nil {
		t.Fatal("expected non-nil API client")
	}
}

func TestDefaultGetAPIWithOptionsLocalRequiresExplicitProjectScopeWhenRepoPresent(t *testing.T) {
	withRepoProjectScope(t)

	origProjectRoot := globalOpts.ProjectRoot
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.ProjectRoot = ""
	globalOpts.Remote = ""
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.ProjectRoot = origProjectRoot
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	_, err := defaultGetAPIWithOptions(true)
	if err == nil {
		t.Fatal("expected explicit project scope error")
	}
	if !strings.Contains(err.Error(), "set --project-root or ORCH_PROJECT_ROOT") {
		t.Fatalf("expected explicit scope guidance, got: %v", err)
	}
}

func TestResolveExplicitProjectScopeRejectsImplicitCWDScope(t *testing.T) {
	withRepoProjectScope(t)

	origProjectRoot := globalOpts.ProjectRoot
	globalOpts.ProjectRoot = ""
	t.Cleanup(func() {
		globalOpts.ProjectRoot = origProjectRoot
	})

	_, err := resolveExplicitProjectScope("", "--repo-root")
	if err == nil {
		t.Fatal("expected explicit scope error")
	}
	if !strings.Contains(err.Error(), "project scope required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveExplicitProjectScopeAcceptsScopeFlagValue(t *testing.T) {
	withNoProjectScope(t)

	root, err := resolveExplicitProjectScope("/tmp/example-repo", "--repo-root")
	if err != nil {
		t.Fatalf("expected explicit scope value to pass: %v", err)
	}
	if root != "/tmp/example-repo" {
		t.Fatalf("root = %q, want %q", root, "/tmp/example-repo")
	}
}

func TestResolveExplicitProjectScopeAcceptsGlobalProjectRoot(t *testing.T) {
	repo := withRepoProjectScope(t)

	origProjectRoot := globalOpts.ProjectRoot
	globalOpts.ProjectRoot = repo
	t.Cleanup(func() {
		globalOpts.ProjectRoot = origProjectRoot
	})

	root, err := resolveExplicitProjectScope("", "--repo-root")
	if err != nil {
		t.Fatalf("expected global project root to pass: %v", err)
	}
	if canonicalPath(root) != canonicalPath(repo) {
		t.Fatalf("root = %q, want %q", root, repo)
	}
}

func TestResolveExplicitProjectScopeAcceptsORCHProjectRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("agent: opencode\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT_ROOT", repo)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	origProjectRoot := globalOpts.ProjectRoot
	globalOpts.ProjectRoot = ""
	t.Cleanup(func() {
		globalOpts.ProjectRoot = origProjectRoot
	})

	root, err := resolveExplicitProjectScope("", "--repo-root")
	if err != nil {
		t.Fatalf("expected ORCH_PROJECT_ROOT to pass: %v", err)
	}
	if canonicalPath(root) != canonicalPath(repo) {
		t.Fatalf("root = %q, want %q", root, repo)
	}
}
