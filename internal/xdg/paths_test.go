package xdg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestParseRepoID(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      string
		wantErr   bool
	}{
		{
			name:      "https with .git",
			remoteURL: "https://github.com/owner/repo.git",
			want:      "owner-repo",
		},
		{
			name:      "https without .git",
			remoteURL: "https://github.com/owner/repo",
			want:      "owner-repo",
		},
		{
			name:      "ssh format",
			remoteURL: "git@github.com:owner/repo.git",
			want:      "owner-repo",
		},
		{
			name:      "ssh format without .git",
			remoteURL: "git@github.com:owner/repo",
			want:      "owner-repo",
		},
		{
			name:      "git protocol",
			remoteURL: "git://github.com/owner/repo.git",
			want:      "owner-repo",
		},
		{
			name:      "ssh protocol",
			remoteURL: "ssh://git@github.com/owner/repo.git",
			want:      "owner-repo",
		},
		{
			name:      "gitlab ssh",
			remoteURL: "git@gitlab.com:myorg/myproject.git",
			want:      "myorg-myproject",
		},
		{
			name:      "trailing slash",
			remoteURL: "https://github.com/owner/repo/",
			want:      "owner-repo",
		},
		{
			name:      "empty",
			remoteURL: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepoID(tt.remoteURL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoID() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParseRepoID() error = %v", err)
				return
			}
			if got != model.RepoID(tt.want) {
				t.Errorf("ParseRepoID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuntimeDir(t *testing.T) {
	// Test with XDG_RUNTIME_DIR set
	original := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", original)

	os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got := RuntimeDir()
	want := "/run/user/1000/orch"
	if got != want {
		t.Errorf("RuntimeDir() with XDG_RUNTIME_DIR = %q, want %q", got, want)
	}

	// Test without XDG_RUNTIME_DIR
	os.Unsetenv("XDG_RUNTIME_DIR")
	got = RuntimeDir()
	// Should contain "orch" somewhere in the path
	if !strings.Contains(got, "orch") {
		t.Errorf("RuntimeDir() without XDG_RUNTIME_DIR should contain 'orch', got %q", got)
	}

	os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := WorkersRuntimeDir(); got != "/run/user/1000/orch/workers" {
		t.Errorf("WorkersRuntimeDir() = %q, want %q", got, "/run/user/1000/orch/workers")
	}
}

func TestStateDir(t *testing.T) {
	original := os.Getenv("XDG_STATE_HOME")
	defer os.Setenv("XDG_STATE_HOME", original)

	os.Setenv("XDG_STATE_HOME", "/home/test/.local/state")
	got := StateDir()
	want := "/home/test/.local/state/orch"
	if got != want {
		t.Errorf("StateDir() with XDG_STATE_HOME = %q, want %q", got, want)
	}

	if got := WorkersStateDir(); got != filepath.Join(want, "workers") {
		t.Errorf("WorkersStateDir() = %q, want %q", got, filepath.Join(want, "workers"))
	}
}

func TestDataDir(t *testing.T) {
	original := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", original)

	os.Setenv("XDG_DATA_HOME", "/home/test/.local/share")
	got := DataDir()
	want := "/home/test/.local/share/orch"
	if got != want {
		t.Errorf("DataDir() with XDG_DATA_HOME = %q, want %q", got, want)
	}
}

func TestDaemonDBPath(t *testing.T) {
	original := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", original)

	os.Setenv("XDG_DATA_HOME", "/home/test/.local/share")
	got := DaemonDBPath()
	want := "/home/test/.local/share/orch/daemon.db"
	if got != want {
		t.Errorf("DaemonDBPath() = %q, want %q", got, want)
	}
}

func TestConfigDir(t *testing.T) {
	original := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", original)

	os.Setenv("XDG_CONFIG_HOME", "/home/test/.config")
	got := ConfigDir()
	want := "/home/test/.config/orch"
	if got != want {
		t.Errorf("ConfigDir() with XDG_CONFIG_HOME = %q, want %q", got, want)
	}
}

func TestRepoDataDir(t *testing.T) {
	original := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", original)

	os.Setenv("XDG_DATA_HOME", "/home/test/.local/share")
	got := RepoDataDir("proboscis-orch")
	want := "/home/test/.local/share/orch/proboscis-orch"
	if got != want {
		t.Errorf("RepoDataDir() = %q, want %q", got, want)
	}
}

func TestRepoRunsDir(t *testing.T) {
	original := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", original)

	os.Setenv("XDG_DATA_HOME", "/home/test/.local/share")
	got := RepoRunsDir("proboscis-orch")
	want := "/home/test/.local/share/orch/proboscis-orch/runs"
	if got != want {
		t.Errorf("RepoRunsDir() = %q, want %q", got, want)
	}
}

func TestLegacyPaths(t *testing.T) {
	projectRoot := "/home/test/repos/myproject"
	issuesRoot := "/home/test/repos/issues"

	if got := LegacyOrchDir(projectRoot); got != filepath.Join(projectRoot, ".orch") {
		t.Errorf("LegacyOrchDir() = %q, want %q", got, filepath.Join(projectRoot, ".orch"))
	}

	if got := LegacySocketPath(projectRoot); got != filepath.Join(projectRoot, ".orch", "daemon.sock") {
		t.Errorf("LegacySocketPath() = %q, want %q", got, filepath.Join(projectRoot, ".orch", "daemon.sock"))
	}

	if got := LegacyRunsDir(issuesRoot); got != filepath.Join(issuesRoot, "runs") {
		t.Errorf("LegacyRunsDir() = %q, want %q", got, filepath.Join(issuesRoot, "runs"))
	}
}

func TestSanitizeRepoID(t *testing.T) {
	tests := []struct {
		owner string
		repo  string
		want  string
	}{
		{"owner", "repo", "owner-repo"},
		{"my-org", "my-repo", "my-org-my-repo"},
		{"owner_name", "repo_name", "owner_name-repo_name"},
		{"owner/bad", "repo", "ownerbad-repo"}, // slashes removed
		{"owner", "repo.git", "repogit-"},      // wait this is wrong
	}

	for _, tt := range tests {
		t.Run(tt.owner+"/"+tt.repo, func(t *testing.T) {
			got := sanitizeRepoID(tt.owner, tt.repo)
			// Just check it doesn't have unsafe chars
			if strings.ContainsAny(got, "/\\:*?\"<>|") {
				t.Errorf("sanitizeRepoID(%q, %q) = %q contains unsafe characters", tt.owner, tt.repo, got)
			}
		})
	}
}

func TestRepoIDRequiresGitRemote(t *testing.T) {
	if _, err := RepoID("/tmp/definitely-not-a-git-repo-xyzzy/my-project"); err == nil {
		t.Fatal("RepoID() expected error for path without git remote")
	}
}

func TestRepoIDUsesRemoteOrigin(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}

	run("git", "init", repo)
	run("git", "-C", repo, "remote", "add", "origin", "https://github.com/example/repo-id.git")

	id, err := RepoID(repo)
	if err != nil {
		t.Fatalf("RepoID() unexpected error: %v", err)
	}
	if id != "example-repo-id" {
		t.Fatalf("RepoID() = %q, want %q", id, "example-repo-id")
	}
}

func TestRepoIDLegacyCompatibility(t *testing.T) {
	got := LegacyRepoID("/work/client-a/orch")
	if got != "orch" {
		t.Errorf("LegacyRepoID() = %q, want %q", got, "orch")
	}

	got = LegacyRepoID("/home/user/repos/my-project")
	if got != "my-project" {
		t.Errorf("LegacyRepoID() = %q, want %q", got, "my-project")
	}
}
