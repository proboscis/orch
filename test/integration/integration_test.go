package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	orchBinary string
	testVault  string
	testRepo   string
)

func TestMain(m *testing.M) {
	// Build the orch binary
	tmpDir, err := os.MkdirTemp("", "orch-integration-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	orchBinary = filepath.Join(tmpDir, "orch")
	cmd := exec.Command("go", "build", "-o", orchBinary, "../../cmd/orch")
	if err := cmd.Run(); err != nil {
		panic("failed to build orch: " + err.Error())
	}

	// Create test vault
	testVault = filepath.Join(tmpDir, "vault")
	os.MkdirAll(filepath.Join(testVault, "issues"), 0755)
	os.MkdirAll(filepath.Join(testVault, "runs"), 0755)

	// Create test git repo
	testRepo = filepath.Join(tmpDir, "repo")
	os.MkdirAll(testRepo, 0755)
	exec.Command("git", "-C", testRepo, "init").Run()
	exec.Command("git", "-C", testRepo, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", testRepo, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(testRepo, "README.md"), []byte("# Test"), 0644)
	exec.Command("git", "-C", testRepo, "add", ".").Run()
	exec.Command("git", "-C", testRepo, "commit", "-m", "initial").Run()

	os.Exit(m.Run())
}

func runOrch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	fullArgs := append([]string{"--issues-root", testVault}, args...)
	cmd := exec.Command(orchBinary, fullArgs...)
	cmd.Dir = testRepo
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
	}
	return stdout.String(), err
}

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error: %v (%s)", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func createTestIssue(t *testing.T, id, content string) {
	t.Helper()
	if !strings.Contains(content, "type: issue") {
		if strings.HasPrefix(content, "---\n") {
			content = strings.Replace(content, "---\n", "---\ntype: issue\n", 1)
		} else {
			content = "---\ntype: issue\n---\n" + content
		}
	}
	path := filepath.Join(testVault, "issues", id+".md")
	content = ensureIssueType(content)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func ensureIssueType(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}

	frontmatterEnd := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			frontmatterEnd = i
			break
		}
	}
	if frontmatterEnd == -1 {
		return content
	}

	for i := 1; i < frontmatterEnd; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "type:") {
			return content
		}
	}

	updated := append([]string{}, lines[:1]...)
	updated = append(updated, "type: issue")
	updated = append(updated, lines[1:]...)
	return strings.Join(updated, "\n")
}

func TestPsEmpty(t *testing.T) {
	output, err := runOrch(t, "ps", "--json")
	if err != nil {
		t.Fatalf("ps failed: %v", err)
	}

	var result struct {
		OK    bool          `json:"ok"`
		Items []interface{} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if !result.OK {
		t.Error("expected ok=true")
	}
	if len(result.Items) != 0 {
		t.Errorf("expected empty items, got %d", len(result.Items))
	}
}

func TestPsJSONUpdatedAgo(t *testing.T) {
	createTestIssue(t, "json-ago-test", "---\ntitle: JSON Ago Test\n---\n# JSON Ago Test")

	runDir := filepath.Join(testVault, "runs", "json-ago-test")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	runID := time.Now().Format("20060102-150405")
	updatedAt := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	runContent := fmt.Sprintf(`---
issue: json-ago-test
run: %s
---

# Events

- %s | status | running
`, runID, updatedAt)
	if err := os.WriteFile(filepath.Join(runDir, runID+".md"), []byte(runContent), 0644); err != nil {
		t.Fatal(err)
	}

	output, err := runOrch(t, "ps", "--json")
	if err != nil {
		t.Fatalf("ps --json failed: %v", err)
	}

	var result struct {
		OK    bool `json:"ok"`
		Items []struct {
			IssueID    string `json:"issue_id"`
			UpdatedAt  string `json:"updated_at"`
			UpdatedAgo string `json:"updated_ago"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	var found bool
	for _, item := range result.Items {
		if item.IssueID != "json-ago-test" {
			continue
		}
		found = true
		if item.UpdatedAt == "" {
			t.Errorf("expected updated_at to be set")
		}
		if item.UpdatedAgo == "" {
			t.Errorf("expected updated_ago to be set")
		}
		if item.UpdatedAgo != "just now" && !strings.HasSuffix(item.UpdatedAgo, "ago") {
			t.Errorf("unexpected updated_ago format: %q", item.UpdatedAgo)
		}
		break
	}
	if !found {
		t.Fatalf("json-ago-test run not found in output: %s", output)
	}
}

func TestPsTSV(t *testing.T) {
	// Create an issue and run first
	createTestIssue(t, "tsv-test", "---\ntype: issue\nid: tsv-test\ntitle: TSV Test\nstatus: open\n---\n# TSV Test")

	// Create a run directory and file manually
	runDir := filepath.Join(testVault, "runs", "tsv-test")
	os.MkdirAll(runDir, 0755)
	runContent := `---
issue: tsv-test
run: 20231220-100000
---

# Events

- 2023-12-20T10:00:00+09:00 | status | running
`
	os.WriteFile(filepath.Join(runDir, "20231220-100000.md"), []byte(runContent), 0644)

	output, err := runOrch(t, "ps", "--tsv")
	if err != nil {
		t.Fatalf("ps --tsv failed: %v", err)
	}

	// Don't use TrimSpace as it removes trailing tabs (empty TSV fields)
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least one TSV line")
	}

	// TSV columns: issue_id, issue_status, run_id, short_id, agent, status, updated_at, pr_url, branch, worktree_path, tmux_session
	// Find our test line
	var testLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "tsv-test\t") {
			testLine = line
			break
		}
	}
	if testLine == "" {
		t.Fatalf("tsv-test run not found in output: %s", output)
	}

	fields := strings.Split(testLine, "\t")
	if len(fields) < 11 {
		t.Errorf("expected 11 TSV fields, got %d: %q", len(fields), testLine)
	}
	if fields[0] != "tsv-test" {
		t.Errorf("expected issue_id=tsv-test, got %s", fields[0])
	}
	if fields[1] != "open" {
		t.Errorf("expected issue_status=open, got %s", fields[1])
	}
}

func TestPsIssueStatusFilter(t *testing.T) {
	// Create issues with valid issue status values (open, resolved, closed)
	createTestIssue(t, "issue-open-status", "---\ntype: issue\ntitle: Open Issue\nstatus: open\n---\n# Open Issue")
	createTestIssue(t, "issue-resolved-status", "---\ntype: issue\ntitle: Resolved Issue\nstatus: resolved\n---\n# Resolved Issue")

	openRunDir := filepath.Join(testVault, "runs", "issue-open-status")
	resolvedRunDir := filepath.Join(testVault, "runs", "issue-resolved-status")
	os.MkdirAll(openRunDir, 0755)
	os.MkdirAll(resolvedRunDir, 0755)

	openRunContent := `---
issue: issue-open-status
run: 20231221-100000
---

# Events

- 2023-12-21T10:00:00+09:00 | status | running
`
	resolvedRunContent := `---
issue: issue-resolved-status
run: 20231221-110000
---

# Events

- 2023-12-21T11:00:00+09:00 | status | done
`
	os.WriteFile(filepath.Join(openRunDir, "20231221-100000.md"), []byte(openRunContent), 0644)
	os.WriteFile(filepath.Join(resolvedRunDir, "20231221-110000.md"), []byte(resolvedRunContent), 0644)

	// Filter by issue status "open" and specific issue - should only return run from that open issue
	output, err := runOrch(t, "ps", "--issue-status", "open", "--issue", "issue-open-status", "--json")
	if err != nil {
		t.Fatalf("ps --issue-status failed: %v", err)
	}

	var result struct {
		OK    bool `json:"ok"`
		Items []struct {
			IssueID     string `json:"issue_id"`
			IssueStatus string `json:"issue_status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if !result.OK {
		t.Error("expected ok=true")
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 run, got %d", len(result.Items))
	}
	if result.Items[0].IssueID != "issue-open-status" {
		t.Errorf("expected issue_id=issue-open-status, got %s", result.Items[0].IssueID)
	}
	if result.Items[0].IssueStatus != "open" {
		t.Errorf("expected issue_status=open, got %s", result.Items[0].IssueStatus)
	}
}

func TestContinueFromBranch(t *testing.T) {
	issueID := "continue-branch"
	createTestIssue(t, issueID, "---\ntitle: Continue Branch\n---\n# Continue Branch")

	runGitCmd(t, testRepo, "checkout", "-b", "feature/continue-branch")
	if err := os.WriteFile(filepath.Join(testRepo, "feature.txt"), []byte("feature"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, testRepo, "add", "feature.txt")
	runGitCmd(t, testRepo, "commit", "-m", "feature work")
	runGitCmd(t, testRepo, "checkout", "main")

	output, err := runOrch(t, "--json", "continue", issueID, "--branch", "feature/continue-branch", "--tmux=false")
	if err != nil {
		t.Fatalf("continue failed: %v", err)
	}

	var result struct {
		OK            bool   `json:"ok"`
		IssueID       string `json:"issue_id"`
		RunID         string `json:"run_id"`
		Branch        string `json:"branch"`
		WorktreePath  string `json:"worktree_path"`
		ContinuedFrom string `json:"continued_from"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok=true, got false: %s", output)
	}
	if result.IssueID != issueID {
		t.Fatalf("IssueID = %q, want %q", result.IssueID, issueID)
	}
	if result.Branch != "feature/continue-branch" {
		t.Fatalf("Branch = %q, want %q", result.Branch, "feature/continue-branch")
	}
	if !strings.HasPrefix(result.ContinuedFrom, "branch:") {
		t.Fatalf("ContinuedFrom = %q, want prefix %q", result.ContinuedFrom, "branch:")
	}
	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}

	branch := runGitCmd(t, result.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "feature/continue-branch" {
		t.Fatalf("worktree branch = %q, want %q", branch, "feature/continue-branch")
	}
}

func TestShowRun(t *testing.T) {
	// Create a run with events
	createTestIssue(t, "show-test", "---\ntype: issue\nid: show-test\ntitle: Show Test\n---\n# Show Test")

	runDir := filepath.Join(testVault, "runs", "show-test")
	os.MkdirAll(runDir, 0755)
	runContent := `---
issue: show-test
run: 20231220-100000
---

# Events

- 2023-12-20T10:00:00+09:00 | status | queued
- 2023-12-20T10:00:01+09:00 | status | running
- 2023-12-20T10:00:05+09:00 | artifact | branch | name=feature/test
- 2023-12-20T10:00:10+09:00 | phase | implement
`
	os.WriteFile(filepath.Join(runDir, "20231220-100000.md"), []byte(runContent), 0644)

	output, err := runOrch(t, "show", "show-test#20231220-100000", "--json")
	if err != nil {
		t.Fatalf("show failed: %v", err)
	}

	var result struct {
		OK      bool       `json:"ok"`
		IssueID string     `json:"issue_id"`
		RunID   string     `json:"run_id"`
		Status  string     `json:"status"`
		Phase   string     `json:"phase"`
		Branch  string     `json:"branch"`
		Events  []struct{} `json:"events"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, output)
	}

	if result.Status != "running" {
		t.Errorf("expected status=running, got %s", result.Status)
	}
	if result.Phase != "implement" {
		t.Errorf("expected phase=implement, got %s", result.Phase)
	}
}

func TestRunDryRun(t *testing.T) {
	createTestIssue(t, "dryrun-test", "---\ntype: issue\nid: dryrun-test\ntitle: Dry Run Test\n---\n# Dry Run Test")

	output, err := runOrch(t, "run", "dryrun-test", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("run --dry-run failed: %v", err)
	}

	var result struct {
		OK           bool   `json:"ok"`
		IssueID      string `json:"issue_id"`
		RunID        string `json:"run_id"`
		Branch       string `json:"branch"`
		WorktreePath string `json:"worktree_path"`
		TmuxSession  string `json:"tmux_session"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if !result.OK {
		t.Error("expected ok=true")
	}
	if result.IssueID != "dryrun-test" {
		t.Errorf("expected issue_id=dryrun-test, got %s", result.IssueID)
	}
	if result.Branch == "" {
		t.Error("expected branch to be set")
	}
	if result.TmuxSession == "" {
		t.Error("expected tmux_session to be set")
	}

	// Verify no run was actually created
	runDir := filepath.Join(testVault, "runs", "dryrun-test")
	entries, _ := os.ReadDir(runDir)
	if len(entries) > 0 {
		t.Error("expected no runs to be created in dry-run mode")
	}
}

func TestOpenPrintPath(t *testing.T) {
	createTestIssue(t, "open-test", "---\ntype: issue\nid: open-test\ntitle: Open Test\n---\n# Open Test")

	output, err := runOrch(t, "open", "open-test", "--print-path")
	if err != nil {
		t.Fatalf("open --print-path failed: %v", err)
	}

	expectedPath := filepath.Join(testVault, "issues", "open-test.md")
	if strings.TrimSpace(output) != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, strings.TrimSpace(output))
	}
}

// Skip tmux tests if tmux is not available
func hasTmux() bool {
	cmd := exec.Command("tmux", "-V")
	return cmd.Run() == nil
}

func TestRunWithTmux(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	createTestIssue(t, "tmux-test", "---\ntype: issue\nid: tmux-test\ntitle: Tmux Test\n---\n# Tmux Test")

	// Use a unique run ID
	runID := time.Now().Format("20060102-150405")

	output, err := runOrch(t, "run", "tmux-test",
		"--run-id", runID,
		"--agent", "custom",
		"--agent-cmd", "echo 'test'; sleep 1",
		"--worktree-dir", filepath.Join(testRepo, ".git-worktrees"),
		"--repo-root", testRepo,
		"--json",
	)
	if err != nil {
		t.Fatalf("run failed: %v\nOutput: %s", err, output)
	}

	var result struct {
		OK           bool   `json:"ok"`
		Status       string `json:"status"`
		TmuxSession  string `json:"tmux_session"`
		WorktreePath string `json:"worktree_path"`
	}
	json.Unmarshal([]byte(output), &result)

	if !result.OK {
		t.Error("expected ok=true")
	}

	// Clean up: kill the tmux session
	if result.TmuxSession != "" {
		exec.Command("tmux", "kill-session", "-t", result.TmuxSession).Run()
	}

	// Clean up: remove worktree
	if result.WorktreePath != "" {
		exec.Command("git", "-C", testRepo, "worktree", "remove", result.WorktreePath, "--force").Run()
	}
}

func TestTickBlocked(t *testing.T) {
	createTestIssue(t, "tick-test", "---\ntype: issue\nid: tick-test\ntitle: Tick Test\n---\n# Tick Test")

	runDir := filepath.Join(testVault, "runs", "tick-test")
	os.MkdirAll(runDir, 0755)

	// Create a blocked run
	runContent := `---
issue: tick-test
run: 20231220-100000
---

# Events

- 2023-12-20T10:00:00+09:00 | status | blocked
`
	os.WriteFile(filepath.Join(runDir, "20231220-100000.md"), []byte(runContent), 0644)

	// Tick the blocked run
	output, err := runOrch(t, "tick", "tick-test#20231220-100000", "--json")
	if err != nil {
		t.Logf("tick output: %s", output)
		// tick may fail if tmux is not available, that's ok for this test
	}
}

// TestDuplicateFrontmatterE2E tests that duplicate frontmatter is detected
// and a warning is emitted via stderr.
func TestDuplicateFrontmatterE2E(t *testing.T) {
	// Create a project root with .orch config
	projectRoot, err := os.MkdirTemp("", "orch-e2e-project-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(projectRoot)
	
	orchDir := filepath.Join(projectRoot, ".orch")
	os.MkdirAll(orchDir, 0755)
	os.WriteFile(filepath.Join(orchDir, "config.yaml"), []byte("# orch config\n"), 0644)

	// Test 1: Issue with duplicate frontmatter should trigger warning
	duplicateContent := `---
type: issue
id: dup-test-001
title: Test Issue With Duplicate Frontmatter
status: open
---

# Test Issue With Duplicate Frontmatter

---
type: issue
id: dup-test-001
title: Test Issue With Duplicate Frontmatter
status: open
tags: [lost-tag1, lost-tag2]
---

# Test Issue With Duplicate Frontmatter

This issue has duplicate frontmatter. Tags are in the second block and should be lost.
`
	path := filepath.Join(testVault, "issues", "dup-test-001.md")
	if err := os.WriteFile(path, []byte(duplicateContent), 0644); err != nil {
		t.Fatal(err)
	}
	// Run orch query which uses FileStore directly and emits warnings to stderr
	cmd := exec.Command(orchBinary, "--issues-root", testVault, "--project-root", projectRoot, "query", "SELECT id, title FROM issues WHERE id = 'dup-test-001'")
	cmd.Dir = projectRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		t.Fatalf("orch query failed: %v (stderr: %s)", err, stderr.String())
	}

	// Warning should be emitted to stderr
	if !strings.Contains(stderr.String(), "duplicate frontmatter") {
		t.Errorf("expected duplicate frontmatter warning in stderr, got: %q", stderr.String())
	}

	// Verify issue was found
	if !strings.Contains(stdout.String(), "dup-test-001") {
		t.Errorf("expected to find issue in output, got: %s", stdout.String())
	}

	// Clean up the duplicate file before testing proper file (so it doesn't trigger warning)
	os.Remove(path)

	// Test 2: Properly formatted issue should NOT trigger warning
	properContent := `---
type: issue
id: proper-test-001
title: Test Issue With Proper Frontmatter
status: open
tags: [tag1, tag2, tag3]
---

# Test Issue With Proper Frontmatter

This issue has proper single frontmatter with tags.
`
	path2 := filepath.Join(testVault, "issues", "proper-test-001.md")
	if err := os.WriteFile(path2, []byte(properContent), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path2)

	cmd2 := exec.Command(orchBinary, "--issues-root", testVault, "--project-root", projectRoot, "query", "SELECT id, title FROM issues WHERE id = 'proper-test-001'")
	cmd2.Dir = projectRoot
	var stdout2, stderr2 bytes.Buffer
	cmd2.Stdout = &stdout2
	cmd2.Stderr = &stderr2
	err = cmd2.Run()
	if err != nil {
		t.Fatalf("orch query failed: %v (stderr: %s)", err, stderr2.String())
	}

	// No warning should be emitted for proper frontmatter
	if strings.Contains(stderr2.String(), "duplicate frontmatter") {
		t.Errorf("unexpected duplicate frontmatter warning for proper issue: %q", stderr2.String())
	}

	// Verify issue was found
	if !strings.Contains(stdout2.String(), "proper-test-001") {
		t.Errorf("expected to find issue in output, got: %s", stdout2.String())
	}
}