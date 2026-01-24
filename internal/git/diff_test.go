package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseDiffNumstat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantAdd int
		wantDel int
	}{
		{
			name:    "empty",
			input:   "",
			wantAdd: 0,
			wantDel: 0,
		},
		{
			name:    "single file",
			input:   "10\t5\tfile.go\n",
			wantAdd: 10,
			wantDel: 5,
		},
		{
			name:    "multiple files",
			input:   "10\t5\tfile1.go\n20\t3\tfile2.go\n",
			wantAdd: 30,
			wantDel: 8,
		},
		{
			name:    "binary file",
			input:   "-\t-\timage.png\n5\t2\tfile.go\n",
			wantAdd: 5,
			wantDel: 2,
		},
		{
			name:    "only additions",
			input:   "100\t0\tnew_file.go\n",
			wantAdd: 100,
			wantDel: 0,
		},
		{
			name:    "only deletions",
			input:   "0\t50\tdeleted_content.go\n",
			wantAdd: 0,
			wantDel: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := parseDiffNumstat(tt.input)
			if stats.Additions != tt.wantAdd {
				t.Errorf("Additions = %d, want %d", stats.Additions, tt.wantAdd)
			}
			if stats.Deletions != tt.wantDel {
				t.Errorf("Deletions = %d, want %d", stats.Deletions, tt.wantDel)
			}
		})
	}
}

func TestGetDiffStats_EmptyInputs(t *testing.T) {
	stats := GetDiffStats("", "", "")
	if stats.Additions != 0 || stats.Deletions != 0 {
		t.Errorf("Expected zero stats for empty inputs, got +%d -%d", stats.Additions, stats.Deletions)
	}

	stats = GetDiffStats("/some/path", "", "main")
	if stats.Additions != 0 || stats.Deletions != 0 {
		t.Errorf("Expected zero stats for empty branch, got +%d -%d", stats.Additions, stats.Deletions)
	}
}

func TestGetDiffStats_NonexistentPath(t *testing.T) {
	stats := GetDiffStats("/nonexistent/path/that/does/not/exist", "feature", "main")
	if stats.Additions != 0 || stats.Deletions != 0 {
		t.Errorf("Expected zero stats for nonexistent path, got +%d -%d", stats.Additions, stats.Deletions)
	}
}

func TestGetDiffStats_RealRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-diff-test-*")
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

	newFile := filepath.Join(tmpDir, "new.txt")
	if err := os.WriteFile(newFile, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	runGit("add", ".")
	runGit("commit", "-m", "add new file")

	stats := getDiffStatsInternal(tmpDir, "main", "feature")
	t.Logf("Got stats: +%d -%d", stats.Additions, stats.Deletions)

	if stats.Additions != 3 {
		t.Errorf("Expected 3 additions, got %d", stats.Additions)
	}
	if stats.Deletions != 0 {
		t.Errorf("Expected 0 deletions, got %d", stats.Deletions)
	}
}
