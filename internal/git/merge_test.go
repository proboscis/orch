package git

import (
	"os"
	"path/filepath"
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

func TestGetBranchCommitTimes(t *testing.T) {
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

	runGit(t, repoDir, "checkout", "-b", "feature/times")
	if err := os.WriteFile(readmePath, []byte("test\nchange"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "feature")

	commitTimes, err := GetBranchCommitTimes(repoDir)
	if err != nil {
		t.Fatalf("GetBranchCommitTimes failed: %v", err)
	}
	if _, ok := commitTimes["main"]; !ok {
		t.Fatalf("expected main to have a commit time")
	}
	if _, ok := commitTimes["feature/times"]; !ok {
		t.Fatalf("expected feature/times to have a commit time")
	}
	if commitTimes["main"].IsZero() {
		t.Fatalf("expected main commit time to be set")
	}
	if commitTimes["feature/times"].IsZero() {
		t.Fatalf("expected feature/times commit time to be set")
	}
}
