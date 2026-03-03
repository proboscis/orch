package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hasGH() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

func skipIfNoGH(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCH_E2E_GITHUB") == "" {
		t.Skip("ORCH_E2E_GITHUB not set (GitHub E2E tests are opt-in)")
	}
	if !hasGH() {
		t.Skip("gh CLI not available or not authenticated")
	}
}

func buildOrch(t *testing.T, dir string) string {
	t.Helper()
	binary := filepath.Join(dir, "orch")
	cmd := exec.Command("go", "build", "-o", binary, "../../cmd/orch")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build orch: %v", err)
	}
	return binary
}

func setupTestVault(t *testing.T, dir string) string {
	t.Helper()
	shortDir, err := os.MkdirTemp("/tmp", "ov-")
	if err != nil {
		t.Fatalf("failed to create short temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(shortDir) })

	vault := shortDir
	os.MkdirAll(filepath.Join(vault, ".orch"), 0755)
	os.MkdirAll(filepath.Join(vault, "issues"), 0755)
	os.MkdirAll(filepath.Join(vault, "runs"), 0755)
	return vault
}

func setupTestRepo(t *testing.T, dir string) string {
	t.Helper()
	repo := filepath.Join(dir, "repo")
	os.MkdirAll(repo, 0755)
	exec.Command("git", "-C", repo, "init").Run()
	exec.Command("git", "-C", repo, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repo, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Test"), 0644)
	exec.Command("git", "-C", repo, "add", ".").Run()
	exec.Command("git", "-C", repo, "commit", "-m", "initial").Run()
	return repo
}

func writeGitHubConfig(t *testing.T, vault, owner, repo, labelFilter string) {
	t.Helper()
	config := fmt.Sprintf(`vault: .
agent: opencode
issues:
  backend: github
github:
  owner: %s
  repo: %s
  label_filter: %s
  poll_interval: 5
`, owner, repo, labelFilter)
	configPath := filepath.Join(vault, ".orch", "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

func startDaemon(t *testing.T, binary, vault, repo string) func() {
	t.Helper()
	cmd := exec.Command(binary, "--project-root", repo, "daemon")
	cmd.Dir = repo
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	waitForDaemonSync(t, vault, 10*time.Second)
	return func() {
		cmd.Process.Kill()
		cmd.Wait()
	}
}

func waitForDaemonSync(t *testing.T, vault string, timeout time.Duration) {
	t.Helper()
	logPath := filepath.Join(vault, ".orch", "daemon.log")
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil {
			content := string(data)
			if strings.Contains(content, "GitHub sync:") || strings.Contains(content, "initial GitHub sync") {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("warning: daemon sync not detected within timeout")
}

func createGitHubIssueViaGH(t *testing.T, owner, repo, title, body string, labels []string) int {
	t.Helper()
	args := []string{"issue", "create", "-R", owner + "/" + repo, "--title", title, "--body", body}
	for _, label := range labels {
		args = append(args, "--label", label)
	}
	cmd := exec.Command("gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gh issue create failed: %v\n%s", err, out)
	}
	url := strings.TrimSpace(string(out))
	var number int
	fmt.Sscanf(url[strings.LastIndex(url, "/")+1:], "%d", &number)
	return number
}

func closeGitHubIssueViaGH(t *testing.T, owner, repo string, number int) {
	t.Helper()
	cmd := exec.Command("gh", "issue", "close", fmt.Sprintf("%d", number), "-R", owner+"/"+repo)
	cmd.Run()
}

func TestGitHubBackend_ListIssues_Empty(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	writeGitHubConfig(t, vault, "proboscis", "orch", "orch-test-nonexistent-label-12345")

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	cmd := exec.Command(binary, "--project-root", repo, "issue", "list", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "No issues found") {
			return
		}
		t.Fatalf("issue list failed: %v\n%s", err, out)
	}

	var result struct {
		OK     bool          `json:"ok"`
		Issues []interface{} `json:"issues"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		if strings.Contains(string(out), "No issues found") {
			return
		}
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, out)
	}

	if len(result.Issues) != 0 {
		t.Errorf("expected 0 issues with nonexistent label filter, got %d", len(result.Issues))
	}
}

func TestGitHubBackend_CreateAndListIssue(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	testLabel := "orch-e2e-test"
	writeGitHubConfig(t, vault, "proboscis", "orch", testLabel)

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	testTitle := fmt.Sprintf("E2E Test Issue %d", time.Now().Unix())
	testBody := "This is an automated test issue created by orch E2E tests."

	createCmd := exec.Command(binary, "--project-root", repo, "issue", "create", "--title", testTitle, "--body", testBody, "--json")
	createOut, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue create failed: %v\n%s", err, createOut)
	}

	var createResult struct {
		OK      bool   `json:"ok"`
		IssueID string `json:"issue_id"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(createOut, &createResult); err != nil {
		t.Fatalf("failed to parse create JSON: %v\nOutput: %s", err, createOut)
	}

	if !createResult.OK {
		t.Fatalf("issue create not OK: %s", createOut)
	}
	if !strings.HasPrefix(createResult.IssueID, "gh-") {
		t.Errorf("issue ID should start with 'gh-', got: %s", createResult.IssueID)
	}
	if !strings.Contains(createResult.Path, "github.com") {
		t.Errorf("path should be a GitHub URL, got: %s", createResult.Path)
	}

	var issueNumber int
	fmt.Sscanf(createResult.IssueID, "gh-%d", &issueNumber)
	defer closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)

	time.Sleep(2 * time.Second)

	listCmd := exec.Command(binary, "--project-root", repo, "issue", "list", "--json")
	listOut, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue list failed: %v\n%s", err, listOut)
	}

	var listResult struct {
		OK     bool `json:"ok"`
		Issues []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(listOut, &listResult); err != nil {
		t.Fatalf("failed to parse list JSON: %v\nOutput: %s", err, listOut)
	}

	found := false
	for _, issue := range listResult.Issues {
		if issue.ID == createResult.IssueID {
			found = true
			if issue.Title != testTitle {
				t.Errorf("title mismatch: got %q, want %q", issue.Title, testTitle)
			}
			break
		}
	}
	if !found {
		t.Errorf("created issue %s not found in list", createResult.IssueID)
	}
}

func TestGitHubBackend_CreateIssue_StatusOpen(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	testLabel := "orch-e2e-test"
	writeGitHubConfig(t, vault, "proboscis", "orch", testLabel)

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	testTitle := fmt.Sprintf("E2E Status Test %d", time.Now().Unix())

	createCmd := exec.Command(binary, "--project-root", repo, "issue", "create", "--title", testTitle, "--body", "Testing open status", "--json")
	createOut, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue create failed: %v\n%s", err, createOut)
	}

	var createResult struct {
		OK      bool   `json:"ok"`
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(createOut, &createResult); err != nil {
		t.Fatalf("failed to parse create JSON: %v\nOutput: %s", err, createOut)
	}

	var issueNumber int
	fmt.Sscanf(createResult.IssueID, "gh-%d", &issueNumber)
	defer closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)

	showCmd := exec.Command(binary, "--project-root", repo, "issue", "show", createResult.IssueID, "--json")
	showOut, err := showCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue show failed: %v\n%s", err, showOut)
	}

	var showResult struct {
		OK    bool `json:"ok"`
		Issue struct {
			ID          string            `json:"id"`
			Status      string            `json:"status"`
			Frontmatter map[string]string `json:"frontmatter"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(showOut, &showResult); err != nil {
		t.Fatalf("failed to parse show JSON: %v\nOutput: %s", err, showOut)
	}

	if showResult.Issue.Status != "open" {
		t.Errorf("newly created issue should have status 'open', got %q (frontmatter.state=%s)",
			showResult.Issue.Status, showResult.Issue.Frontmatter["state"])
	}
}

func TestGitHubBackend_GetIssue(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	testLabel := "orch-e2e-test"
	writeGitHubConfig(t, vault, "proboscis", "orch", testLabel)

	issueNumber := createGitHubIssueViaGH(t, "proboscis", "orch",
		fmt.Sprintf("E2E Get Test %d", time.Now().Unix()),
		"Test issue for get operation",
		[]string{testLabel})
	defer closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	daemonLog, _ := os.ReadFile(filepath.Join(vault, ".orch", "daemon.log"))
	t.Logf("Daemon log after start:\n%s", string(daemonLog))

	showCmd := exec.Command(binary, "--project-root", repo, "issue", "show", fmt.Sprintf("gh-%d", issueNumber), "--json")
	showOut, err := showCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue show failed: %v\n%s", err, showOut)
	}

	var showResult struct {
		OK    bool `json:"ok"`
		Issue struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(showOut, &showResult); err != nil {
		t.Fatalf("failed to parse show JSON: %v\nOutput: %s", err, showOut)
	}

	if !showResult.OK {
		t.Fatalf("issue show not OK: %s", showOut)
	}
	if showResult.Issue.ID != fmt.Sprintf("gh-%d", issueNumber) {
		t.Errorf("issue ID mismatch: got %s, want gh-%d", showResult.Issue.ID, issueNumber)
	}
}

func TestGitHubBackend_CloseIssue(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	testLabel := "orch-e2e-test"
	writeGitHubConfig(t, vault, "proboscis", "orch", testLabel)

	issueNumber := createGitHubIssueViaGH(t, "proboscis", "orch",
		fmt.Sprintf("E2E Close Test %d", time.Now().Unix()),
		"Test issue for close operation",
		[]string{testLabel})

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	closeCmd := exec.Command(binary, "--project-root", repo, "issue", "close", fmt.Sprintf("gh-%d", issueNumber), "--json")
	closeOut, err := closeCmd.CombinedOutput()
	if err != nil {
		closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)
		t.Fatalf("issue close failed: %v\n%s", err, closeOut)
	}

	var closeResult struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(closeOut, &closeResult); err != nil {
		closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)
		t.Fatalf("failed to parse close JSON: %v\nOutput: %s", err, closeOut)
	}

	if !closeResult.OK {
		closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)
		t.Fatalf("issue close not OK: %s", closeOut)
	}

	verifyCmd := exec.Command("gh", "issue", "view", fmt.Sprintf("%d", issueNumber), "-R", "proboscis/orch", "--json", "state")
	verifyOut, _ := verifyCmd.CombinedOutput()
	if !strings.Contains(string(verifyOut), "CLOSED") {
		t.Errorf("issue should be closed on GitHub, got: %s", verifyOut)
	}
}

func TestGitHubBackend_SyncIssues(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	testLabel := "orch-e2e-test"
	writeGitHubConfig(t, vault, "proboscis", "orch", testLabel)

	issueNumber := createGitHubIssueViaGH(t, "proboscis", "orch",
		fmt.Sprintf("E2E Sync Test %d", time.Now().Unix()),
		"Test issue for sync operation",
		[]string{testLabel})
	defer closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	syncCmd := exec.Command(binary, "--project-root", repo, "issue", "sync", "--json")
	syncOut, err := syncCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue sync failed: %v\n%s", err, syncOut)
	}

	var syncResult struct {
		OK      bool `json:"ok"`
		Synced  int  `json:"synced"`
		Updated int  `json:"updated"`
	}
	if err := json.Unmarshal(syncOut, &syncResult); err != nil {
		t.Fatalf("failed to parse sync JSON: %v\nOutput: %s", err, syncOut)
	}

	if !syncResult.OK {
		t.Fatalf("issue sync not OK: %s", syncOut)
	}
}

func TestGitHubBackend_NoFallbackToLocal(t *testing.T) {
	skipIfNoGH(t)

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	localIssue := `---
type: issue
id: local-test-issue
title: Local Test Issue
status: open
---

# Local Test Issue
`
	os.WriteFile(filepath.Join(vault, "issues", "local-test-issue.md"), []byte(localIssue), 0644)

	writeGitHubConfig(t, vault, "proboscis", "orch", "orch-test-nonexistent-label-12345")

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	daemonLog, _ := os.ReadFile(filepath.Join(vault, ".orch", "daemon.log"))
	t.Logf("Daemon log:\n%s", string(daemonLog))

	listCmd := exec.Command(binary, "--project-root", repo, "issue", "list")
	listOut, _ := listCmd.CombinedOutput()

	if strings.Contains(string(listOut), "local-test-issue") {
		t.Errorf("GitHub backend should NOT fall back to local issues, but found 'local-test-issue' in output:\n%s", listOut)
	}
}

func TestGitHubBackend_RunWithGitHubIssue(t *testing.T) {
	skipIfNoGH(t)
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	tmpDir := t.TempDir()
	binary := buildOrch(t, tmpDir)
	vault := setupTestVault(t, tmpDir)
	repo := setupTestRepo(t, tmpDir)

	testLabel := "orch-e2e-test"
	config := fmt.Sprintf(`vault: .
agent: custom
issues:
  backend: github
github:
  owner: proboscis
  repo: orch
  label_filter: %s
  poll_interval: 5
`, testLabel)
	os.WriteFile(filepath.Join(vault, ".orch", "config.yaml"), []byte(config), 0644)

	issueNumber := createGitHubIssueViaGH(t, "proboscis", "orch",
		fmt.Sprintf("E2E Run Test %d", time.Now().Unix()),
		"Test issue for orch run operation",
		[]string{testLabel})
	defer closeGitHubIssueViaGH(t, "proboscis", "orch", issueNumber)

	stopDaemon := startDaemon(t, binary, vault, repo)
	defer stopDaemon()

	issueID := fmt.Sprintf("gh-%d", issueNumber)
	runCmd := exec.Command(binary, "--project-root", repo, "run", issueID,
		"--agent", "custom",
		"--agent-cmd", "echo 'test'; sleep 1",
		"--worktree-dir", filepath.Join(repo, ".git-worktrees"),
		"--repo-root", repo,
		"--json")
	runCmd.Dir = repo
	runOut, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("orch run failed: %v\n%s", err, runOut)
	}

	var runResult struct {
		OK          bool   `json:"ok"`
		IssueID     string `json:"issue_id"`
		RunID       string `json:"run_id"`
		SessionName string `json:"session_name"`
	}
	jsonStart := strings.Index(string(runOut), "{")
	if jsonStart == -1 {
		t.Fatalf("no JSON found in output: %s", runOut)
	}
	if err := json.Unmarshal(runOut[jsonStart:], &runResult); err != nil {
		t.Fatalf("failed to parse run JSON: %v\nOutput: %s", err, runOut)
	}

	if !runResult.OK {
		t.Fatalf("orch run not OK: %s", runOut)
	}
	if runResult.IssueID != issueID {
		t.Errorf("issue ID mismatch: got %s, want %s", runResult.IssueID, issueID)
	}

	if runResult.SessionName != "" {
		exec.Command("tmux", "kill-session", "-t", runResult.SessionName).Run()
	}
}
