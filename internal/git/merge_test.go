package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetMergedBranches(t *testing.T) {
	repoDir := t.TempDir()

	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	currentBranch := runGit(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != "main" {
		runGit(t, repoDir, "branch", "-m", "main")
	}

	runGit(t, repoDir, "checkout", "-b", "feature/merged")
	if err := os.WriteFile(readmePath, []byte("test\nchange"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "feature")

	runGit(t, repoDir, "checkout", "main")
	runGit(t, repoDir, "merge", "--no-ff", "feature/merged", "-m", "merge feature")

	merged, err := GetMergedBranches(repoDir, "main")
	if err != nil {
		t.Fatalf("GetMergedBranches failed: %v", err)
	}
	if !merged["feature/merged"] {
		t.Fatalf("expected feature/merged to be merged into main")
	}
}

func runGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}
