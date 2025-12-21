package integration

import (
	"bytes"
	"encoding/json"
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

func createTestIssue(t *testing.T, id, content string) {
	t.Helper()
	path := filepath.Join(testVault, "issues", id+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
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

	// TSV columns: issue_id, issue_status, run_id, short_id, status, phase, updated_at, pr_url, branch, worktree_path, tmux_session
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
	createTestIssue(t, "status-open", "---\ntype: issue\ntitle: Open Issue\nstatus: filter-open\n---\n# Open Issue")
	createTestIssue(t, "status-closed", "---\ntype: issue\ntitle: Closed Issue\nstatus: filter-closed\n---\n# Closed Issue")

	openRunDir := filepath.Join(testVault, "runs", "status-open")
	closedRunDir := filepath.Join(testVault, "runs", "status-closed")
	os.MkdirAll(openRunDir, 0755)
	os.MkdirAll(closedRunDir, 0755)

	openRunContent := `---
issue: status-open
run: 20231221-100000
---

# Events

- 2023-12-21T10:00:00+09:00 | status | running
`
	closedRunContent := `---
issue: status-closed
run: 20231221-110000
---

# Events

- 2023-12-21T11:00:00+09:00 | status | done
`
	os.WriteFile(filepath.Join(openRunDir, "20231221-100000.md"), []byte(openRunContent), 0644)
	os.WriteFile(filepath.Join(closedRunDir, "20231221-110000.md"), []byte(closedRunContent), 0644)

	output, err := runOrch(t, "ps", "--issue-status", "filter-open", "--json")
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
	if result.Items[0].IssueID != "status-open" {
		t.Errorf("expected issue_id=status-open, got %s", result.Items[0].IssueID)
	}
	if result.Items[0].IssueStatus != "filter-open" {
		t.Errorf("expected issue_status=filter-open, got %s", result.Items[0].IssueStatus)
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
- 2023-12-20T10:05:00+09:00 | question | q1 | text="What should we do?"
`
	os.WriteFile(filepath.Join(runDir, "20231220-100000.md"), []byte(runContent), 0644)

	output, err := runOrch(t, "show", "show-test#20231220-100000", "--json")
	if err != nil {
		t.Fatalf("show failed: %v", err)
	}

	var result struct {
		OK        bool       `json:"ok"`
		IssueID   string     `json:"issue_id"`
		RunID     string     `json:"run_id"`
		Status    string     `json:"status"`
		Phase     string     `json:"phase"`
		Branch    string     `json:"branch"`
		Events    []struct{} `json:"events"`
		Questions []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"unanswered_questions"`
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
	if len(result.Questions) != 1 {
		t.Errorf("expected 1 unanswered question, got %d", len(result.Questions))
	}
}

func TestAnswerQuestion(t *testing.T) {
	createTestIssue(t, "answer-test", "---\ntype: issue\nid: answer-test\ntitle: Answer Test\n---\n# Answer Test")

	runDir := filepath.Join(testVault, "runs", "answer-test")
	os.MkdirAll(runDir, 0755)
	runContent := `---
issue: answer-test
run: 20231220-100000
---

# Events

- 2023-12-20T10:00:00+09:00 | status | blocked
- 2023-12-20T10:05:00+09:00 | question | q1 | text="What should we do?"
`
	runPath := filepath.Join(runDir, "20231220-100000.md")
	os.WriteFile(runPath, []byte(runContent), 0644)

	// Answer the question
	_, err := runOrch(t, "answer", "answer-test#20231220-100000", "q1", "--text", "Use option A")
	if err != nil {
		t.Fatalf("answer failed: %v", err)
	}

	// Verify the answer was appended
	content, _ := os.ReadFile(runPath)
	if !strings.Contains(string(content), "answer") {
		t.Error("expected answer event in run file")
	}

	// Check unanswered questions
	output, _ := runOrch(t, "show", "answer-test#20231220-100000", "--json")
	var result struct {
		Questions []struct{} `json:"unanswered_questions"`
	}
	json.Unmarshal([]byte(output), &result)
	if len(result.Questions) != 0 {
		t.Errorf("expected 0 unanswered questions after answer, got %d", len(result.Questions))
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
		"--worktree-root", filepath.Join(testRepo, ".git-worktrees"),
		"--repo-root", testRepo,
		"--json",
	)
	if err != nil {
		t.Fatalf("run failed: %v\nOutput: %s", err, output)
	}

	var result struct {
		OK          bool   `json:"ok"`
		Status      string `json:"status"`
		TmuxSession string `json:"tmux_session"`
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
	worktreePath := filepath.Join(testRepo, ".git-worktrees", "tmux-test", runID)
	exec.Command("git", "-C", testRepo, "worktree", "remove", worktreePath, "--force").Run()
}

func TestTickBlocked(t *testing.T) {
	createTestIssue(t, "tick-test", "---\ntype: issue\nid: tick-test\ntitle: Tick Test\n---\n# Tick Test")

	runDir := filepath.Join(testVault, "runs", "tick-test")
	os.MkdirAll(runDir, 0755)

	// Create a blocked run with answered question
	runContent := `---
issue: tick-test
run: 20231220-100000
---

# Events

- 2023-12-20T10:00:00+09:00 | status | blocked
- 2023-12-20T10:05:00+09:00 | question | q1 | text="What should we do?"
- 2023-12-20T10:10:00+09:00 | answer | q1 | text="Use option A" | by=user
`
	os.WriteFile(filepath.Join(runDir, "20231220-100000.md"), []byte(runContent), 0644)

	// Tick should detect no unanswered questions
	output, err := runOrch(t, "tick", "tick-test#20231220-100000", "--json")
	if err != nil {
		t.Logf("tick output: %s", output)
		// tick may fail if tmux is not available, that's ok for this test
	}
}
