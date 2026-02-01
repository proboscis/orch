package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetWorktreeStatus_DirtyWithDiffStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogit-dirty-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test User")

	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial content\n"), 0644); err != nil {
		t.Fatalf("Failed to write initial file: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	runGit("checkout", "-b", "feature")

	newFile := filepath.Join(tmpDir, "uncommitted.txt")
	if err := os.WriteFile(newFile, []byte("line1\nline2\nline3\nline4\nline5\n"), 0644); err != nil {
		t.Fatalf("Failed to write uncommitted file: %v", err)
	}
	runGit("add", "uncommitted.txt")

	status := GetWorktreeStatus(tmpDir, "feature", "main")

	if status.State != BranchStateDirty {
		t.Errorf("Expected BranchStateDirty, got %v", status.State)
	}

	if status.DiffStats.Additions != 5 {
		t.Errorf("Expected 5 additions for uncommitted changes, got %d", status.DiffStats.Additions)
	}

	if status.DiffStats.Deletions != 0 {
		t.Errorf("Expected 0 deletions, got %d", status.DiffStats.Deletions)
	}

	if status.DiffStats.FilesChanged != 1 {
		t.Errorf("Expected 1 file changed, got %d", status.DiffStats.FilesChanged)
	}
}

func TestGetWorktreeStatus_DirtyWithBothCommittedAndUncommitted(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogit-mixed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test User")

	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial\n"), 0644); err != nil {
		t.Fatalf("Failed to write initial file: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	runGit("checkout", "-b", "feature")

	committedFile := filepath.Join(tmpDir, "committed.txt")
	if err := os.WriteFile(committedFile, []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatalf("Failed to write committed file: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "add committed file")

	uncommittedFile := filepath.Join(tmpDir, "uncommitted.txt")
	if err := os.WriteFile(uncommittedFile, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatalf("Failed to write uncommitted file: %v", err)
	}
	runGit("add", "uncommitted.txt")

	status := GetWorktreeStatus(tmpDir, "feature", "main")

	if status.State != BranchStateDirty {
		t.Errorf("Expected BranchStateDirty, got %v", status.State)
	}

	expectedAdditions := 5
	if status.DiffStats.Additions != expectedAdditions {
		t.Errorf("Expected %d additions (committed + uncommitted), got %d", expectedAdditions, status.DiffStats.Additions)
	}

	expectedFiles := 2
	if status.DiffStats.FilesChanged != expectedFiles {
		t.Errorf("Expected %d files changed, got %d", expectedFiles, status.DiffStats.FilesChanged)
	}
}
