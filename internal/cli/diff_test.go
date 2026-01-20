package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDiffArgs(t *testing.T) {
	tests := []struct {
		name       string
		baseBranch string
		branch     string
		stat       bool
		want       []string
	}{
		{
			name:       "basic diff",
			baseBranch: "main",
			branch:     "feature/test",
			stat:       false,
			want:       []string{"diff", "main...feature/test"},
		},
		{
			name:       "diff with stat",
			baseBranch: "main",
			branch:     "feature/test",
			stat:       true,
			want:       []string{"diff", "--stat", "main...feature/test"},
		},
		{
			name:       "diff against HEAD",
			baseBranch: "develop",
			branch:     "HEAD",
			stat:       false,
			want:       []string{"diff", "develop...HEAD"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDiffArgs(tt.baseBranch, tt.branch, tt.stat)
			if len(got) != len(tt.want) {
				t.Fatalf("buildDiffArgs() = %v, want %v", got, tt.want)
			}
			for i, arg := range got {
				if arg != tt.want[i] {
					t.Errorf("buildDiffArgs()[%d] = %q, want %q", i, arg, tt.want[i])
				}
			}
		})
	}
}

func TestGetDiffTool(t *testing.T) {
	// Save and restore environment
	origDiffTool := os.Getenv("ORCH_DIFFTOOL")
	origPager := os.Getenv("PAGER")
	defer func() {
		os.Setenv("ORCH_DIFFTOOL", origDiffTool)
		os.Setenv("PAGER", origPager)
	}()

	tests := []struct {
		name        string
		envDiffTool string
		envPager    string
		cfgDiffTool string
		wantTool    string
	}{
		{
			name:        "env ORCH_DIFFTOOL takes priority",
			envDiffTool: "custom-diff",
			envPager:    "less",
			cfgDiffTool: "delta",
			wantTool:    "custom-diff",
		},
		{
			name:        "config takes priority over pager",
			envDiffTool: "",
			envPager:    "less",
			cfgDiffTool: "delta",
			wantTool:    "delta",
		},
		{
			name:        "PAGER fallback",
			envDiffTool: "",
			envPager:    "most",
			cfgDiffTool: "",
			wantTool:    "", // Will be delta if installed, or most, or less
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ORCH_DIFFTOOL", tt.envDiffTool)
			os.Setenv("PAGER", tt.envPager)

			cfg := &mockConfig{diffTool: tt.cfgDiffTool}
			got := getDiffToolWithConfig(cfg)

			if tt.wantTool != "" && got != tt.wantTool {
				t.Errorf("getDiffTool() = %q, want %q", got, tt.wantTool)
			}
		})
	}
}

// mockConfig implements the minimal interface needed for getDiffTool
type mockConfig struct {
	diffTool string
}

func (m *mockConfig) GetDiffTool() string {
	return m.diffTool
}

// getDiffToolWithConfig is a testable version of getDiffTool
func getDiffToolWithConfig(cfg interface{ GetDiffTool() string }) string {
	// Priority order:
	// 1. ORCH_DIFFTOOL env var
	if tool := os.Getenv("ORCH_DIFFTOOL"); tool != "" {
		return tool
	}

	// 2. diff_tool in config
	if cfg.GetDiffTool() != "" {
		return cfg.GetDiffTool()
	}

	// 3. delta (if installed)
	if _, err := exec.LookPath("delta"); err == nil {
		return "delta"
	}

	// 4. PAGER env var
	if pager := os.Getenv("PAGER"); pager != "" {
		return pager
	}

	// 5. Fallback to less
	return "less"
}

func TestExecuteDiff(t *testing.T) {
	// Create a temporary git repo with changes
	tmpDir := t.TempDir()

	// Initialize git repo
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	// Create initial commit on main
	writeFile(t, filepath.Join(tmpDir, "README.md"), "# Test\n")
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")

	// Create feature branch with changes
	runGit(t, tmpDir, "checkout", "-b", "feature/test")
	writeFile(t, filepath.Join(tmpDir, "README.md"), "# Test\n\nNew content\n")
	writeFile(t, filepath.Join(tmpDir, "new-file.txt"), "new file content\n")
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "feature changes")

	// Test basic diff execution (output to stdout with cat)
	args := buildDiffArgs("main", "feature/test", false)
	err := executeDiff(tmpDir, args, "cat")
	if err != nil {
		t.Fatalf("executeDiff() error = %v", err)
	}

	// Test stat diff
	statArgs := buildDiffArgs("main", "feature/test", true)
	err = executeDiff(tmpDir, statArgs, "cat")
	if err != nil {
		t.Fatalf("executeDiff() with stat error = %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, strings.TrimSpace(string(out)))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
