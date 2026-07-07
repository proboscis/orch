package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/config"
)

func withNoProjectScope(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_PROJECT", "")

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
	t.Setenv("ORCH_PROJECT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte("agent: opencode\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origin := t.TempDir()
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init repo: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", repo, "config", "user.email", "test@example.com").CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", repo, "config", "user.name", "Test User").CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", repo, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "init", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	originURL := fmt.Sprintf("https://github.com/example/%s.git", filepath.Base(repo))
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", originURL).CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v (%s)", err, strings.TrimSpace(string(out)))
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
	if !strings.Contains(err.Error(), "project identity required") {
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
	if !strings.Contains(err.Error(), "project identity required") {
		t.Fatalf("expected project scope guidance, got: %v", err)
	}
}

func TestDefaultGetAPIWithOptionsRemoteAllowsGitDerivedProjectIdentity(t *testing.T) {
	withRepoProjectScope(t)

	origProject := globalOpts.Project
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Project = ""
	// Unreachable-but-resolvable address: identity resolution happens before
	// the dial, so passing it means the error (if any) is a reachability
	// error, never "project identity required". A live daemon must not be a
	// test dependency (the previous "zeus:7777" only worked on the author's
	// machine).
	globalOpts.Remote = "127.0.0.1:1"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Project = origProject
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	_, err := defaultGetAPIWithOptions(true)
	if err != nil && strings.Contains(err.Error(), "project identity required") {
		t.Fatalf("expected git-derived project identity to pass in remote mode, got: %v", err)
	}
}

func TestDefaultGetAPIWithOptionsRemoteAllowsExplicitProjectFlag(t *testing.T) {
	repo := withRepoProjectScope(t)

	origProject := globalOpts.Project
	origRemote := globalOpts.Remote
	origFlag := remoteFlagWasSet
	globalOpts.Project = "example-" + filepath.Base(repo)
	globalOpts.Remote = "127.0.0.1:1"
	remoteFlagWasSet = true
	t.Cleanup(func() {
		globalOpts.Project = origProject
		globalOpts.Remote = origRemote
		remoteFlagWasSet = origFlag
	})

	_, err := defaultGetAPIWithOptions(true)
	if err != nil && strings.Contains(err.Error(), "project identity required") {
		t.Fatalf("expected explicit project to pass in remote mode, got: %v", err)
	}
}

func TestNormalizeProjectIdentityInputRejectsFilesystemPath(t *testing.T) {
	_, err := normalizeProjectIdentityInput("/tmp/example-repo")
	if err == nil {
		t.Fatal("expected filesystem path to be rejected")
	}
	if !strings.Contains(err.Error(), "filesystem path") {
		t.Fatalf("expected filesystem path guidance, got: %v", err)
	}
}

func TestDefaultGetAPIWithOptionsLocalAllowsGitDerivedProjectIdentity(t *testing.T) {
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

	api, err := defaultGetAPIWithOptions(true)
	if err != nil {
		t.Fatalf("expected git-derived project identity to pass in local mode, got: %v", err)
	}
	if api == nil {
		t.Fatal("expected non-nil API client")
	}
}

func TestResolveExplicitProjectScopeAcceptsImplicitCWDScope(t *testing.T) {
	withRepoProjectScope(t)

	root, err := resolveExplicitProjectScope("", "--project")
	if err != nil {
		t.Fatalf("expected cwd repo scope to pass: %v", err)
	}
	if strings.TrimSpace(root) == "" {
		t.Fatal("expected non-empty resolved repo root")
	}
}

func TestResolveExplicitProjectScopeAcceptsScopeFlagValue(t *testing.T) {
	withNoProjectScope(t)

	root, err := resolveExplicitProjectScope("/tmp/example-repo", "--project")
	if err != nil {
		t.Fatalf("expected explicit scope value to pass: %v", err)
	}
	if root != "/tmp/example-repo" {
		t.Fatalf("root = %q, want %q", root, "/tmp/example-repo")
	}
}

func TestResolveExplicitProjectScopeRejectsOutsideRepo(t *testing.T) {
	withNoProjectScope(t)

	_, err := resolveExplicitProjectScope("", "--project")
	if err == nil {
		t.Fatal("expected scope error outside repo")
	}
	if !strings.Contains(err.Error(), "project scope required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
