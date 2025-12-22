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

func TestCheckMergeConflict(t *testing.T) {
	repoDir := t.TempDir()

	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	currentBranch := runGit(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != "main" {
		runGit(t, repoDir, "branch", "-m", "main")
	}

	runGit(t, repoDir, "checkout", "-b", "feature/clean")
	cleanPath := filepath.Join(repoDir, "clean.txt")
	if err := os.WriteFile(cleanPath, []byte("clean"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "clean.txt")
	runGit(t, repoDir, "commit", "-m", "clean change")

	runGit(t, repoDir, "checkout", "main")
	runGit(t, repoDir, "checkout", "-b", "feature/conflict")
	if err := os.WriteFile(readmePath, []byte("conflict branch"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "conflict change")

	runGit(t, repoDir, "checkout", "main")
	if err := os.WriteFile(readmePath, []byte("main branch"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "main change")

	conflict, err := CheckMergeConflict(repoDir, "feature/conflict", "main")
	if err != nil {
		t.Fatalf("CheckMergeConflict conflict branch failed: %v", err)
	}
	if !conflict {
		t.Fatalf("expected conflict branch to have merge conflicts")
	}

	clean, err := CheckMergeConflict(repoDir, "feature/clean", "main")
	if err != nil {
		t.Fatalf("CheckMergeConflict clean branch failed: %v", err)
	}
	if clean {
		t.Fatalf("expected clean branch to merge without conflicts")
	}
}

func TestMergedBranchesForTargetFallbackToMaster(t *testing.T) {
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
	runGit(t, repoDir, "branch", "-M", "master")

	runGit(t, repoDir, "checkout", "-b", "feature/merged")
	if err := os.WriteFile(readmePath, []byte("test\nchange"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "feature")

	runGit(t, repoDir, "checkout", "master")
	runGit(t, repoDir, "merge", "--no-ff", "feature/merged", "-m", "merge feature")

	targetRef, merged, err := MergedBranchesForTarget(repoDir, "main")
	if err != nil {
		t.Fatalf("MergedBranchesForTarget failed: %v", err)
	}
	if targetRef != "master" {
		t.Fatalf("targetRef = %q, want %q", targetRef, "master")
	}
	if !merged["feature/merged"] {
		t.Fatalf("expected feature/merged to be merged into master")
	}
}
