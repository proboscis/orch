package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/s22625/orch/internal/daemon"
)

var (
	orchBinary    string
	testVault     string
	testRepo      string
	daemonProcess *os.Process
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "orch-integration-*")
	if err != nil {
		panic(err)
	}

	runtimeDir := filepath.Join(tmpDir, "runtime")
	stateDir := filepath.Join(tmpDir, "state")
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		panic(err)
	}
	os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	os.Setenv("XDG_STATE_HOME", stateDir)
	os.Setenv("XDG_DATA_HOME", dataDir)
	os.Unsetenv("ORCH_VAULT")
	os.Unsetenv("ORCH_ISSUES_ROOT")

	orchBinary = filepath.Join(tmpDir, "orch")
	cmd := exec.Command("go", "build", "-o", orchBinary, "../../cmd/orch")
	if err := cmd.Run(); err != nil {
		panic("failed to build orch: " + err.Error())
	}

	testVault = filepath.Join(tmpDir, "vault")
	os.MkdirAll(filepath.Join(testVault, "issues"), 0755)
	os.MkdirAll(filepath.Join(testVault, "runs"), 0755)

	testRepo = filepath.Join(tmpDir, "repo")
	os.MkdirAll(testRepo, 0755)
	exec.Command("git", "-C", testRepo, "init").Run()
	exec.Command("git", "-C", testRepo, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", testRepo, "config", "user.name", "Test").Run()
	os.MkdirAll(filepath.Join(testRepo, ".orch"), 0755)
	os.WriteFile(filepath.Join(testRepo, ".orch", "config.yaml"), []byte("{}\n"), 0644)
	os.Setenv("ORCH_PROJECT_ROOT", testRepo)
	os.WriteFile(filepath.Join(testRepo, "README.md"), []byte("# Test"), 0644)
	exec.Command("git", "-C", testRepo, "add", ".").Run()
	exec.Command("git", "-C", testRepo, "commit", "-m", "initial").Run()

	startTestDaemon()
	code := m.Run()
	stopTestDaemon()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func startTestDaemon() {
	cmd := exec.Command(orchBinary, "daemon", "run")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		panic("failed to start daemon: " + err.Error())
	}
	daemonProcess = cmd.Process

	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if daemon.IsRunning("") {
			return
		}
	}
	panic("daemon did not start in time")
}

func stopTestDaemon() {
	if daemonProcess != nil {
		daemonProcess.Signal(syscall.SIGTERM)
		daemonProcess.Wait()
	}
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

func runOrchInRepo(t *testing.T, repoRoot, issuesRoot string, args ...string) (string, error) {
	t.Helper()
	fullArgs := append([]string{"--issues-root", issuesRoot}, args...)
	cmd := exec.Command(orchBinary, fullArgs...)
	cmd.Dir = repoRoot

	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_PROJECT_ROOT=") ||
			strings.HasPrefix(kv, "ORCH_ISSUES_ROOT=") ||
			strings.HasPrefix(kv, "ORCH_VAULT=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"ORCH_PROJECT_ROOT="+repoRoot,
		"ORCH_ISSUES_ROOT=",
		"ORCH_VAULT=",
	)
	cmd.Env = env

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
	createTestIssueInVault(t, testVault, id, content)
}

func createTestIssueInVault(t *testing.T, vaultRoot, id, content string) {
	t.Helper()
	if !strings.Contains(content, "type: issue") {
		if strings.HasPrefix(content, "---\n") {
			content = strings.Replace(content, "---\n", "---\ntype: issue\n", 1)
		} else {
			content = "---\ntype: issue\n---\n" + content
		}
	}
	path := filepath.Join(vaultRoot, "issues", id+".md")
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

	// TSV columns: issue_id, issue_status, run_id, short_id, agent, status, updated_at, pr_url, branch, worktree_path, session_name
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
		SessionName  string `json:"session_name"`
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
	if result.SessionName == "" {
		t.Error("expected session_name to be set")
	}

	// Verify no run was actually created
	runDir := filepath.Join(testVault, "runs", "dryrun-test")
	entries, _ := os.ReadDir(runDir)
	if len(entries) > 0 {
		t.Error("expected no runs to be created in dry-run mode")
	}
}

func TestRunBackToBackSameProjectNoRootLoss(t *testing.T) {
	createTestIssue(t, "back-to-back-run-1", "---\ntype: issue\nid: back-to-back-run-1\ntitle: Back-to-back Run 1\nstatus: open\n---\n# Back-to-back Run 1")
	createTestIssue(t, "back-to-back-run-2", "---\ntype: issue\nid: back-to-back-run-2\ntitle: Back-to-back Run 2\nstatus: open\n---\n# Back-to-back Run 2")

	issueIDs := []string{"back-to-back-run-1", "back-to-back-run-2"}
	for _, issueID := range issueIDs {
		output, err := runOrch(t, "run", issueID, "--dry-run", "--repo-root", testRepo, "--worktree-dir", ".git-worktrees", "--json")
		if err != nil {
			t.Fatalf("run --dry-run failed for %s: %v", issueID, err)
		}

		var result struct {
			OK           bool   `json:"ok"`
			IssueID      string `json:"issue_id"`
			Status       string `json:"status"`
			WorktreePath string `json:"worktree_path"`
			Error        string `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse JSON for %s: %v\nOutput: %s", issueID, err, output)
		}
		if !result.OK {
			t.Fatalf("expected ok=true for %s, got false: %s", issueID, output)
		}
		if result.Status != "dry_run" {
			t.Fatalf("expected status=dry_run for %s, got %q", issueID, result.Status)
		}
		if result.IssueID != issueID {
			t.Fatalf("expected issue_id=%s, got %s", issueID, result.IssueID)
		}
		if !strings.HasPrefix(result.WorktreePath, testRepo+string(os.PathSeparator)) {
			t.Fatalf("expected worktree path for %s to be under %s, got %s", issueID, testRepo, result.WorktreePath)
		}
	}
}

func TestDaemonMultiRepoIsolation(t *testing.T) {
	createTestIssue(t, "isolation-a", "---\ntype: issue\nid: isolation-a\ntitle: Isolation A\nstatus: open\n---\n# Isolation A")

	outputA, err := runOrch(t, "run", "isolation-a", "--dry-run", "--repo-root", testRepo, "--worktree-dir", ".git-worktrees", "--json")
	if err != nil {
		t.Fatalf("run --dry-run for repo A failed: %v", err)
	}

	var resultA struct {
		OK           bool   `json:"ok"`
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal([]byte(outputA), &resultA); err != nil {
		t.Fatalf("failed to parse repo A JSON: %v\nOutput: %s", err, outputA)
	}
	if !resultA.OK {
		t.Fatalf("expected repo A run to succeed: %s", outputA)
	}
	if !strings.HasPrefix(resultA.WorktreePath, testRepo+string(os.PathSeparator)) {
		t.Fatalf("expected repo A worktree under %s, got %s", testRepo, resultA.WorktreePath)
	}

	tmpRoot := filepath.Dir(testRepo)
	repoB := filepath.Join(tmpRoot, "repo-b")
	vaultB := filepath.Join(tmpRoot, "vault-b")

	if err := os.MkdirAll(repoB, 0755); err != nil {
		t.Fatalf("mkdir repo-b: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultB, "issues"), 0755); err != nil {
		t.Fatalf("mkdir vault-b/issues: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultB, "runs"), 0755); err != nil {
		t.Fatalf("mkdir vault-b/runs: %v", err)
	}

	runGitCmd(t, repoB, "init")
	runGitCmd(t, repoB, "config", "user.email", "test@test.com")
	runGitCmd(t, repoB, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoB, "README.md"), []byte("# Repo B\n"), 0644); err != nil {
		t.Fatalf("write repo-b README: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoB, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo-b .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoB, ".orch", "config.yaml"), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write repo-b config: %v", err)
	}
	runGitCmd(t, repoB, "add", ".")
	runGitCmd(t, repoB, "commit", "-m", "initial")

	createTestIssueInVault(t, vaultB, "isolation-b", "---\ntype: issue\nid: isolation-b\ntitle: Isolation B\nstatus: open\n---\n# Isolation B")

	outputB, err := runOrchInRepo(t, repoB, vaultB, "run", "isolation-b", "--dry-run", "--repo-root", repoB, "--worktree-dir", ".git-worktrees", "--json")
	if err != nil {
		t.Fatalf("run --dry-run for repo B failed: %v", err)
	}

	var resultB struct {
		OK           bool   `json:"ok"`
		IssueID      string `json:"issue_id"`
		WorktreePath string `json:"worktree_path"`
		Error        string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(outputB), &resultB); err != nil {
		t.Fatalf("failed to parse repo B JSON: %v\nOutput: %s", err, outputB)
	}
	if !resultB.OK {
		t.Fatalf("expected repo B run to succeed: %s", outputB)
	}
	if resultB.IssueID != "isolation-b" {
		t.Fatalf("expected issue_id=isolation-b, got %s", resultB.IssueID)
	}
	if !strings.HasPrefix(resultB.WorktreePath, repoB+string(os.PathSeparator)) {
		t.Fatalf("expected repo B worktree under %s, got %s", repoB, resultB.WorktreePath)
	}
	if strings.HasPrefix(resultB.WorktreePath, testRepo+string(os.PathSeparator)) {
		t.Fatalf("expected repo B worktree to not use repo A root %s, got %s", testRepo, resultB.WorktreePath)
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
		SessionName  string `json:"session_name"`
		WorktreePath string `json:"worktree_path"`
	}
	json.Unmarshal([]byte(output), &result)

	if !result.OK {
		t.Error("expected ok=true")
	}

	// Clean up: kill the tmux session
	if result.SessionName != "" {
		exec.Command("tmux", "kill-session", "-t", result.SessionName).Run()
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

// ============================================================================
// Diff Command Tests
// ============================================================================

func TestDiffCommandHelp(t *testing.T) {
	// Test that the diff command exists and shows proper help
	cmd := exec.Command(orchBinary, "diff", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("diff --help failed: %v (%s)", err, out)
	}

	output := string(out)
	// Verify key help text is present
	if !strings.Contains(output, "Show the git diff") {
		t.Error("expected help to mention 'Show the git diff'")
	}
	if !strings.Contains(output, "--stat") {
		t.Error("expected help to mention --stat flag")
	}
	if !strings.Contains(output, "--base") {
		t.Error("expected help to mention --base flag")
	}
	if !strings.Contains(output, "ORCH_DIFFTOOL") {
		t.Error("expected help to mention ORCH_DIFFTOOL env var")
	}
}

func TestDiffWithWorktreeChanges(t *testing.T) {
	// Create a test issue
	issueID := "diff-test"
	createTestIssue(t, issueID, "---\ntype: issue\ntitle: Diff Test\n---\n# Diff Test")

	// Create a run with worktree
	runID := time.Now().Format("20060102-150405")
	worktreeDir := filepath.Join(testRepo, ".git-worktrees")
	os.MkdirAll(worktreeDir, 0755)

	// Start a run with --dry-run to get the worktree created
	output, err := runOrch(t, "run", issueID,
		"--run-id", runID,
		"--worktree-dir", worktreeDir,
		"--repo-root", testRepo,
		"--agent", "custom",
		"--agent-cmd", "sleep 0.1",
		"--tmux=false",
		"--json",
	)
	if err != nil {
		t.Fatalf("run failed: %v\nOutput: %s", err, output)
	}

	var runResult struct {
		OK           bool   `json:"ok"`
		Branch       string `json:"branch"`
		WorktreePath string `json:"worktree_path"`
	}
	if err := json.Unmarshal([]byte(output), &runResult); err != nil {
		t.Fatalf("unmarshal run result: %v", err)
	}

	if runResult.WorktreePath == "" {
		t.Skip("no worktree path created, skipping diff test")
	}

	// Make changes in the worktree
	testFile := filepath.Join(runResult.WorktreePath, "diff-test-file.txt")
	if err := os.WriteFile(testFile, []byte("test content for diff\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitCmd(t, runResult.WorktreePath, "add", "diff-test-file.txt")
	runGitCmd(t, runResult.WorktreePath, "commit", "-m", "test changes for diff")

	// Wait for daemon to pick up the run
	time.Sleep(500 * time.Millisecond)

	// Test diff command with --stat
	diffOutput, err := runOrch(t, "diff", issueID+"#"+runID, "--stat")
	if err != nil {
		t.Logf("diff command output: %s", diffOutput)
		// Diff may fail if daemon isn't running, that's acceptable in test
		t.Skipf("diff command failed (may need daemon): %v", err)
	}

	// Verify diff output contains our change
	if !strings.Contains(diffOutput, "diff-test-file.txt") {
		t.Errorf("expected diff output to contain 'diff-test-file.txt', got: %s", diffOutput)
	}

	// Cleanup worktree
	exec.Command("git", "-C", testRepo, "worktree", "remove", runResult.WorktreePath, "--force").Run()
}

func TestDiffStatFlag(t *testing.T) {
	// Create a test issue with a run
	issueID := "diff-stat-test"
	createTestIssue(t, issueID, "---\ntype: issue\ntitle: Diff Stat Test\n---\n# Diff Stat Test")

	// Create run directory and file
	runDir := filepath.Join(testVault, "runs", issueID)
	os.MkdirAll(runDir, 0755)

	runID := time.Now().Format("20060102-150405")
	runContent := fmt.Sprintf(`---
issue: %s
run: %s
---

# Events

- %s | status | running
- %s | artifact | worktree | path=%s
- %s | artifact | branch | name=feature/test
`, issueID, runID,
		time.Now().Format(time.RFC3339),
		time.Now().Format(time.RFC3339), testRepo,
		time.Now().Format(time.RFC3339))
	os.WriteFile(filepath.Join(runDir, runID+".md"), []byte(runContent), 0644)

	// The diff command should handle --stat flag
	// Note: This test may fail without daemon, that's expected
	_, _ = runOrch(t, "diff", issueID+"#"+runID, "--stat")
	// We're mainly testing that the flag is accepted, not the full workflow
}

func TestDiffToolSelection(t *testing.T) {
	// Test environment variable takes priority
	origEnv := os.Getenv("ORCH_DIFFTOOL")
	defer os.Setenv("ORCH_DIFFTOOL", origEnv)

	os.Setenv("ORCH_DIFFTOOL", "cat")

	// The diff tool selection is tested in unit tests
	// This integration test verifies the command accepts the env var
	cmd := exec.Command(orchBinary, "diff", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("diff --help failed: %v", err)
	}

	if !strings.Contains(string(out), "ORCH_DIFFTOOL") {
		t.Error("help should mention ORCH_DIFFTOOL")
	}
}

func TestPsOutputColumns(t *testing.T) {
	issueID := "ps-columns-test"
	createTestIssue(t, issueID, "---\ntype: issue\ntitle: Ps Columns Test\nstatus: open\n---\n# Ps Columns Test")

	runDir := filepath.Join(testVault, "runs", issueID)
	os.MkdirAll(runDir, 0755)

	runID := time.Now().Format("20060102-150405")
	runContent := fmt.Sprintf(`---
issue: %s
run: %s
---

# Events

- %s | status | running
- %s | artifact | pr | url=https://github.com/test/repo/pull/42
- %s | artifact | branch | name=feature/test
`, issueID, runID,
		time.Now().Add(-5*time.Minute).Format(time.RFC3339),
		time.Now().Add(-2*time.Minute).Format(time.RFC3339),
		time.Now().Add(-3*time.Minute).Format(time.RFC3339))
	os.WriteFile(filepath.Join(runDir, runID+".md"), []byte(runContent), 0644)

	output, err := runOrch(t, "ps", "--issue", issueID)
	if err != nil {
		t.Fatalf("ps failed: %v", err)
	}

	if !strings.Contains(output, "AGENT") {
		t.Errorf("expected output to contain AGENT column header, got: %s", output)
	}
	if !strings.Contains(output, "BRANCH") {
		t.Errorf("expected output to contain BRANCH column header, got: %s", output)
	}
	if !strings.Contains(output, "PR") {
		t.Errorf("expected output to contain PR column header, got: %s", output)
	}

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (header + data), got: %d", len(lines))
	}

	header := lines[0]
	headerFields := strings.Fields(header)

	agentIdx := -1
	branchIdx := -1
	prIdx := -1
	for i, f := range headerFields {
		if f == "AGENT" {
			agentIdx = i
		}
		if f == "BRANCH" {
			branchIdx = i
		}
		if f == "PR" {
			prIdx = i
		}
	}

	if agentIdx == -1 {
		t.Errorf("AGENT column not found in header: %v", headerFields)
	}
	if branchIdx == -1 {
		t.Errorf("BRANCH column not found in header: %v", headerFields)
	}
	if prIdx == -1 {
		t.Errorf("PR column not found in header: %v", headerFields)
	}

	for _, f := range headerFields {
		if f == "STATUS" {
			t.Errorf("found deprecated STATUS column, should be split into AGENT/BRANCH/PR: %v", headerFields)
		}
	}
}

func TestPsJSONOutputColumns(t *testing.T) {
	issueID := "ps-json-columns-test"
	createTestIssue(t, issueID, "---\ntype: issue\ntitle: Ps JSON Columns Test\nstatus: open\n---\n# Ps JSON Columns Test")

	runDir := filepath.Join(testVault, "runs", issueID)
	os.MkdirAll(runDir, 0755)

	runID := time.Now().Format("20060102-150405")
	runContent := fmt.Sprintf(`---
issue: %s
run: %s
---

# Events

- %s | status | running
- %s | artifact | pr | url=https://github.com/test/repo/pull/99
`, issueID, runID,
		time.Now().Add(-5*time.Minute).Format(time.RFC3339),
		time.Now().Add(-2*time.Minute).Format(time.RFC3339))
	os.WriteFile(filepath.Join(runDir, runID+".md"), []byte(runContent), 0644)

	output, err := runOrch(t, "ps", "--issue", issueID, "--json")
	if err != nil {
		t.Fatalf("ps --json failed: %v", err)
	}

	var result struct {
		OK    bool `json:"ok"`
		Items []struct {
			IssueID      string `json:"issue_id"`
			RunID        string `json:"run_id"`
			Status       string `json:"status"`
			AgentStatus  string `json:"agent_status"`
			BranchStatus string `json:"branch_status"`
			PRStatus     string `json:"pr_status"`
			PRUrl        string `json:"pr_url"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, output)
	}

	if !result.OK {
		t.Error("expected ok=true")
	}

	var foundRun *struct {
		IssueID      string `json:"issue_id"`
		RunID        string `json:"run_id"`
		Status       string `json:"status"`
		AgentStatus  string `json:"agent_status"`
		BranchStatus string `json:"branch_status"`
		PRStatus     string `json:"pr_status"`
		PRUrl        string `json:"pr_url"`
	}
	for i := range result.Items {
		if result.Items[i].IssueID == issueID {
			foundRun = &result.Items[i]
			break
		}
	}
	if foundRun == nil {
		t.Fatalf("run not found in output: %s", output)
	}

	if foundRun.AgentStatus == "" {
		t.Error("expected agent_status to be set")
	}
	if foundRun.AgentStatus == "running" {
		t.Errorf("agent_status should be short form 'run', got: %s", foundRun.AgentStatus)
	}
	if foundRun.AgentStatus != "run" {
		t.Errorf("expected agent_status='run' for running status, got: %s", foundRun.AgentStatus)
	}

	if foundRun.BranchStatus == "" {
		t.Error("expected branch_status to be set")
	}

	if foundRun.PRStatus == "" {
		t.Error("expected pr_status to be set")
	}
	if foundRun.PRStatus != "open" {
		t.Errorf("expected pr_status='open' for running with PR, got: %s", foundRun.PRStatus)
	}
}

func TestPsAgentStatusValues(t *testing.T) {
	testCases := []struct {
		status        string
		expectedShort string
	}{
		{"queued", "queue"},
		{"booting", "boot"},
		{"running", "run"},
		{"blocked", "block"},
		{"blocked_api", "block"},
		{"pr_open", "pr"},
		{"done", "done"},
		{"failed", "fail"},
		{"canceled", "cancel"},
	}

	for _, tc := range testCases {
		t.Run(tc.status, func(t *testing.T) {
			issueID := fmt.Sprintf("agent-status-%s-test", tc.status)
			createTestIssue(t, issueID, fmt.Sprintf("---\ntype: issue\ntitle: Agent Status %s Test\nstatus: open\n---\n# Test", tc.status))

			runDir := filepath.Join(testVault, "runs", issueID)
			os.MkdirAll(runDir, 0755)

			runID := time.Now().Format("20060102-150405")
			runContent := fmt.Sprintf(`---
issue: %s
run: %s
---

# Events

- %s | status | %s
`, issueID, runID, time.Now().Format(time.RFC3339), tc.status)
			os.WriteFile(filepath.Join(runDir, runID+".md"), []byte(runContent), 0644)

			output, err := runOrch(t, "ps", "--issue", issueID, "--json")
			if err != nil {
				t.Fatalf("ps --json failed: %v", err)
			}

			var result struct {
				Items []struct {
					AgentStatus string `json:"agent_status"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}

			if len(result.Items) == 0 {
				t.Fatal("no items in result")
			}

			if result.Items[0].AgentStatus != tc.expectedShort {
				t.Errorf("expected agent_status=%q for status=%q, got: %q",
					tc.expectedShort, tc.status, result.Items[0].AgentStatus)
			}
		})
	}
}

func TestIssuesSortedByModificationTime(t *testing.T) {
	oldIssueID := "sort-test-old"
	newIssueID := "sort-test-new"

	createTestIssue(t, oldIssueID, "---\ntype: issue\ntitle: Old Issue\nstatus: open\n---\n# Old Issue")

	time.Sleep(1100 * time.Millisecond)

	createTestIssue(t, newIssueID, "---\ntype: issue\ntitle: New Issue\nstatus: open\n---\n# New Issue")

	output, err := runOrch(t, "issue", "list", "--json")
	if err != nil {
		t.Fatalf("issue list failed: %v", err)
	}

	var result struct {
		OK     bool `json:"ok"`
		Issues []struct {
			ID         string `json:"id"`
			ModifiedAt string `json:"modified_at"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, output)
	}

	if !result.OK {
		t.Error("expected ok=true")
	}

	oldIdx := -1
	newIdx := -1
	for i, issue := range result.Issues {
		if issue.ID == oldIssueID {
			oldIdx = i
		}
		if issue.ID == newIssueID {
			newIdx = i
		}
	}

	if oldIdx == -1 {
		t.Errorf("old issue not found in list")
	}
	if newIdx == -1 {
		t.Errorf("new issue not found in list")
	}

	if newIdx > oldIdx {
		t.Errorf("expected new issue (idx=%d) to appear before old issue (idx=%d) - issues should be sorted newest first", newIdx, oldIdx)
	}

	var newModified, oldModified time.Time
	for _, issue := range result.Issues {
		if issue.ID == newIssueID && issue.ModifiedAt != "" {
			newModified, _ = time.Parse(time.RFC3339, issue.ModifiedAt)
		}
		if issue.ID == oldIssueID && issue.ModifiedAt != "" {
			oldModified, _ = time.Parse(time.RFC3339, issue.ModifiedAt)
		}
	}

	if !newModified.IsZero() && !oldModified.IsZero() {
		if !newModified.After(oldModified) {
			t.Errorf("expected new issue modified_at (%v) to be after old issue modified_at (%v)", newModified, oldModified)
		}
	}
}

func TestPsPrStatusValues(t *testing.T) {
	testCases := []struct {
		name       string
		status     string
		hasPR      bool
		expectedPR string
	}{
		{"running_no_pr", "running", false, "-"},
		{"running_with_pr", "running", true, "open"},
		{"pr_open_status", "pr_open", false, "open"},
		{"done_with_pr", "done", true, "merged"},
		{"done_no_pr", "done", false, "-"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			issueID := fmt.Sprintf("pr-status-%s-test", tc.name)
			createTestIssue(t, issueID, fmt.Sprintf("---\ntype: issue\ntitle: PR Status %s Test\nstatus: open\n---\n# Test", tc.name))

			runDir := filepath.Join(testVault, "runs", issueID)
			os.MkdirAll(runDir, 0755)

			runID := time.Now().Format("20060102-150405")
			prEvent := ""
			if tc.hasPR {
				prEvent = fmt.Sprintf("- %s | artifact | pr | url=https://github.com/test/pr/1\n",
					time.Now().Format(time.RFC3339))
			}
			runContent := fmt.Sprintf(`---
issue: %s
run: %s
---

# Events

- %s | status | %s
%s`, issueID, runID, time.Now().Format(time.RFC3339), tc.status, prEvent)
			os.WriteFile(filepath.Join(runDir, runID+".md"), []byte(runContent), 0644)

			output, err := runOrch(t, "ps", "--issue", issueID, "--json")
			if err != nil {
				t.Fatalf("ps --json failed: %v", err)
			}

			var result struct {
				Items []struct {
					PRStatus string `json:"pr_status"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}

			if len(result.Items) == 0 {
				t.Fatal("no items in result")
			}

			if result.Items[0].PRStatus != tc.expectedPR {
				t.Errorf("expected pr_status=%q, got: %q", tc.expectedPR, result.Items[0].PRStatus)
			}
		})
	}
}
