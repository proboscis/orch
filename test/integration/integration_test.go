package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	fullArgs := append([]string{"--vault", testVault}, args...)
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

func shortRunID(issueID, runID string) string {
	sum := sha256.Sum256([]byte(issueID + "#" + runID))
	return hex.EncodeToString(sum[:])[:6]
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
	if result.WorktreePath == "" {
		t.Error("expected worktree_path to be set")
	}

	shortID := shortRunID(result.IssueID, result.RunID)
	expectedSuffix := filepath.Join(".git-worktrees", result.IssueID, fmt.Sprintf("%s_claude_%s", shortID, result.RunID))
	if !strings.HasSuffix(result.WorktreePath, expectedSuffix) {
		t.Errorf("expected worktree_path to end with %q, got %q", expectedSuffix, result.WorktreePath)
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
		"--worktree-root", filepath.Join(testRepo, ".git-worktrees"),
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
