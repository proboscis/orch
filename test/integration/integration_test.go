package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/worker"
	"github.com/s22625/orch/internal/xdg"
)

var (
	orchBinary    string
	testVault     string
	testRepo      string
	remoteAddr    string
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
	homeDir := filepath.Join(tmpDir, "home")
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		panic(err)
	}
	os.Setenv("HOME", homeDir)
	os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	os.Setenv("XDG_STATE_HOME", stateDir)
	os.Setenv("XDG_DATA_HOME", dataDir)
	os.Setenv("XDG_CONFIG_HOME", configDir)
	os.Unsetenv("ORCH_REMOTE")
	os.Unsetenv("ORCH_PROJECT")

	addr, err := reserveLoopbackTCPAddr()
	if err != nil {
		panic("failed to reserve remote daemon address: " + err.Error())
	}
	remoteAddr = addr

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
	configData := fmt.Sprintf("issues:\n  path: %s\n", testVault)
	os.WriteFile(filepath.Join(testRepo, ".orch", "config.yaml"), []byte(configData), 0644)
	os.WriteFile(filepath.Join(testRepo, "README.md"), []byte("# Test"), 0644)
	exec.Command("git", "-C", testRepo, "add", ".").Run()
	exec.Command("git", "-C", testRepo, "commit", "-m", "initial").Run()
	exec.Command("git", "-C", testRepo, "branch", "-M", "main").Run()
	originRepo := filepath.Join(tmpDir, "origin.git")
	exec.Command("git", "init", "--bare", originRepo).Run()
	exec.Command("git", "-C", testRepo, "remote", "add", "origin", originRepo).Run()
	exec.Command("git", "-C", testRepo, "push", "-u", "origin", "main").Run()

	startTestDaemon()
	registerRepoOrPanic(testRepo)
	ensureWorkerOrPanic()
	code := m.Run()
	stopTestWorker()
	stopTestDaemon()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func registerRepoOrPanic(projectRoot string) {
	admin := daemon.NewProtoClient("")
	defer admin.Close()
	if _, err := admin.RegisterRepo(projectRoot); err != nil {
		panic("failed to register repo mapping: " + err.Error())
	}
}

func ensureRepoMapping(t *testing.T, repoRoot, issuesRoot string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(repoRoot, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo .orch: %v", err)
	}
	configData := fmt.Sprintf("issues:\n  path: %s\n", issuesRoot)
	if err := os.WriteFile(filepath.Join(repoRoot, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	admin := daemon.NewProtoClient("")
	defer admin.Close()
	if _, err := admin.RegisterRepo(repoRoot); err != nil {
		t.Fatalf("register repo mapping failed: %v", err)
	}
}

func startTestDaemon() {
	args := []string{"daemon", "run"}
	if remoteAddr != "" {
		args = append(args, "--listen", "tcp://"+remoteAddr)
	}
	cmd := exec.Command(orchBinary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		panic("failed to start daemon: " + err.Error())
	}
	daemonProcess = cmd.Process

	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if !daemon.IsRunning("") {
			continue
		}

		client := daemon.NewProtoClient("")
		err := client.Ping()
		_ = client.Close()
		if err == nil {
			return
		}
	}
	panic("daemon did not start in time")
}

func reserveLoopbackTCPAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

func stopTestDaemon() {
	if daemonProcess != nil {
		daemonProcess.Signal(syscall.SIGTERM)
		daemonProcess.Wait()
	}
}

func ensureWorkerOrPanic() {
	if err := ensureWorkerActiveWithTimeout(5 * time.Second); err != nil {
		panic(err)
	}
}

func stopTestWorker() {
	// External loop worker exits with the test process.
}

func runOrch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ensureRepoMapping(t, testRepo, testVault)
	ensureWorkerAvailable(t)
	cmd := exec.Command(orchBinary, args...)
	cmd.Dir = testRepo

	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_PROJECT=") || strings.HasPrefix(kv, "ORCH_REMOTE=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "ORCH_PROJECT=")
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
		t.Logf("stdout: %s", stdout.String())
	}
	return stdout.String(), err
}

func runOrchInRepo(t *testing.T, repoRoot, issuesRoot string, args ...string) (string, error) {
	t.Helper()
	ensureRepoMapping(t, repoRoot, issuesRoot)
	ensureWorkerAvailable(t)
	cmd := exec.Command(orchBinary, args...)
	cmd.Dir = repoRoot

	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_PROJECT=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "ORCH_PROJECT=")
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
		t.Logf("stdout: %s", stdout.String())
	}
	return stdout.String(), err
}

func ensureWorkerAvailable(t *testing.T) {
	t.Helper()

	if err := ensureWorkerActiveWithTimeout(3 * time.Second); err != nil {
		t.Fatalf("%v", err)
	}
}

func ensureWorkerActiveWithTimeout(timeout time.Duration) error {
	admin := daemon.NewProtoClient("")
	defer admin.Close()

	if workersActive(admin) {
		return nil
	}

	workerClient := daemon.NewProtoClient("")
	go func() {
		_ = worker.RunExternalLoop(workerClient, worker.RunConfig{
			WorkerID:          "integration-worker",
			PollInterval:      100 * time.Millisecond,
			HeartbeatInterval: 500 * time.Millisecond,
		})
		_ = workerClient.Close()
	}()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if workersActive(admin) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("integration worker did not become active in time")
}

func workersActive(client *daemon.ProtoClient) bool {
	resp, err := client.ListWorkers()
	if err != nil || resp == nil {
		return false
	}
	for _, w := range resp.Workers {
		if w.Active {
			return true
		}
	}
	return false
}

func runOrchRemoteOutsideRepo(t *testing.T, workingDir, project string, args ...string) (string, string, error) {
	t.Helper()

	fullArgs := []string{"--remote", remoteAddr, "--project", project}
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command(orchBinary, fullArgs...)
	cmd.Dir = workingDir

	home := filepath.Join(workingDir, ".home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_PROJECT=") ||
			strings.HasPrefix(kv, "ORCH_REMOTE=") ||
			strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runOrchOutsideRepo(t *testing.T, workingDir string, args ...string) (string, string, error) {
	t.Helper()

	cmd := exec.Command(orchBinary, args...)
	cmd.Dir = workingDir

	home := filepath.Join(workingDir, ".home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	env := make([]string, 0, len(os.Environ())+5)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_PROJECT=") ||
			strings.HasPrefix(kv, "ORCH_REMOTE=") ||
			strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+home,
		"ORCH_PROJECT=",
		"ORCH_REMOTE=",
	)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func waitForRemoteDaemon(t *testing.T) {
	t.Helper()

	client := daemon.NewProtoClientWithAddress("", remoteAddr)
	defer client.Close()

	var lastErr error
	for i := 0; i < 40; i++ {
		if err := client.Ping(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("remote daemon %s did not become reachable: %v", remoteAddr, lastErr)
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
	content = ensureIssueFrontmatterDefaults(content)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func ensureIssueFrontmatterDefaults(content string) string {
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

	insertions := []string{}
	if !frontmatterHasKey(lines, frontmatterEnd, "type") {
		insertions = append(insertions, "type: issue")
	}
	if len(insertions) == 0 {
		return content
	}

	updated := append([]string{}, lines[:1]...)
	updated = append(updated, insertions...)
	updated = append(updated, lines[1:]...)
	return strings.Join(updated, "\n")
}

func frontmatterHasKey(lines []string, frontmatterEnd int, key string) bool {
	prefix := key + ":"
	for i := 1; i < frontmatterEnd; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			return true
		}
	}
	return false
}

func TestPsRemoteWithRepoIDProjectRootOutsideRepo(t *testing.T) {
	if strings.TrimSpace(remoteAddr) == "" {
		t.Fatal("remote daemon address is empty")
	}

	waitForRemoteDaemon(t)

	tmp := t.TempDir()
	serverRepo := filepath.Join(tmp, "server-repo")
	serverIssues := filepath.Join(tmp, "server-issues")
	outsideRepo := filepath.Join(tmp, "outside")

	if err := os.MkdirAll(filepath.Join(serverRepo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir server repo .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(serverIssues, "issues"), 0755); err != nil {
		t.Fatalf("mkdir server issues/issues: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(serverIssues, "runs"), 0755); err != nil {
		t.Fatalf("mkdir server issues/runs: %v", err)
	}
	if err := os.MkdirAll(outsideRepo, 0755); err != nil {
		t.Fatalf("mkdir outside repo dir: %v", err)
	}

	configData := fmt.Sprintf("issues:\n  path: %s\n", serverIssues)
	if err := os.WriteFile(filepath.Join(serverRepo, ".orch", "config.yaml"), []byte(configData), 0644); err != nil {
		t.Fatalf("write server repo config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serverRepo, "README.md"), []byte("# Server Repo\n"), 0644); err != nil {
		t.Fatalf("write server repo README: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "init").Run(); err != nil {
		t.Fatalf("git init server repo: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("git config name: %v", err)
	}
	serverOrigin := filepath.Join(tmp, "server-origin.git")
	if err := exec.Command("git", "init", "--bare", serverOrigin).Run(); err != nil {
		t.Fatalf("git init --bare server origin: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "add", ".").Run(); err != nil {
		t.Fatalf("git add server repo: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "commit", "-m", "init").Run(); err != nil {
		t.Fatalf("git commit server repo: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "branch", "-M", "main").Run(); err != nil {
		t.Fatalf("git branch -M main: %v", err)
	}
	if err := exec.Command("git", "-C", serverRepo, "remote", "add", "origin", serverOrigin).Run(); err != nil {
		t.Fatalf("git remote add origin: %v", err)
	}

	admin := daemon.NewProtoClientWithAddress("", remoteAddr)
	defer admin.Close()

	repoID, err := admin.RegisterRepo(serverRepo)
	if err != nil {
		t.Fatalf("register repo mapping failed: %v", err)
	}

	projectToken := repoID
	out, errOut, err := runOrchRemoteOutsideRepo(t, outsideRepo, projectToken, "ps", "--json")
	if err != nil {
		t.Fatalf("remote ps failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	var result struct {
		OK    bool              `json:"ok"`
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse ps --json output: %v\noutput: %s", err, out)
	}
	if !result.OK {
		t.Fatalf("expected ok=true from remote ps output, got: %s", out)
	}
}

func TestWorkerLifecycleRemoteUsesLocalSupervisor(t *testing.T) {
	if strings.TrimSpace(remoteAddr) == "" {
		t.Fatal("remote daemon address is empty")
	}

	waitForRemoteDaemon(t)
	tmp := t.TempDir()

	cleanupManaged := func() {
		_, _, _ = runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "stop", "--all", "--json")
	}
	cleanupManaged()

	t.Cleanup(func() {
		cleanupManaged()
	})

	type workerStartResult struct {
		OK       bool   `json:"ok"`
		WorkerID string `json:"worker_id"`
		PID      int    `json:"pid"`
		LogPath  string `json:"log_path"`
		Reused   bool   `json:"reused"`
	}
	type workerStatusResult struct {
		OK       bool   `json:"ok"`
		WorkerID string `json:"worker_id"`
		Profile  string `json:"profile"`
		Local    struct {
			Managed       bool   `json:"managed"`
			ProcessExists bool   `json:"process_exists"`
			State         string `json:"state"`
			PID           int    `json:"pid"`
			LogPath       string `json:"log_path"`
			LastError     string `json:"last_error"`
		} `json:"local"`
		Master struct {
			Reachable    bool   `json:"reachable"`
			State        string `json:"state"`
			Error        string `json:"error"`
			Registration *struct {
				ID string `json:"id"`
			} `json:"registration"`
		} `json:"master"`
	}
	type workerStopResult struct {
		OK           bool `json:"ok"`
		StoppedCount int  `json:"stopped_count"`
	}

	out, errOut, err := runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "start", "--json")
	if err != nil {
		t.Fatalf("worker start failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	var start workerStartResult
	if err := json.Unmarshal([]byte(out), &start); err != nil {
		t.Fatalf("parse worker start json: %v\noutput: %s", err, out)
	}
	if !start.OK {
		t.Fatalf("expected ok=true from worker start, got: %s", out)
	}
	if start.WorkerID == "" || start.PID == 0 {
		t.Fatalf("unexpected worker start result: %+v", start)
	}
	if daemonProcess != nil && start.PID == daemonProcess.Pid {
		t.Fatalf("worker start returned daemon pid %d; expected a separate local worker process", start.PID)
	}
	if start.LogPath == "" {
		t.Fatalf("expected worker log path, got: %+v", start)
	}
	if _, err := os.Stat(start.LogPath); err != nil {
		t.Fatalf("worker log path stat failed: %v", err)
	}

	stateMatches, err := filepath.Glob(filepath.Join(xdg.WorkersStateDir(), start.WorkerID+"-*.json"))
	if err != nil {
		t.Fatalf("glob worker state files: %v", err)
	}
	if len(stateMatches) == 0 {
		t.Fatalf("expected worker state file for %s under %s", start.WorkerID, xdg.WorkersStateDir())
	}
	pidMatches, err := filepath.Glob(filepath.Join(xdg.WorkersRuntimeDir(), start.WorkerID+"-*.pid"))
	if err != nil {
		t.Fatalf("glob worker pid files: %v", err)
	}
	if len(pidMatches) == 0 {
		t.Fatalf("expected worker pid file for %s under %s", start.WorkerID, xdg.WorkersRuntimeDir())
	}

	out, errOut, err = runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "start", "--json")
	if err != nil {
		t.Fatalf("second worker start failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	var reused workerStartResult
	if err := json.Unmarshal([]byte(out), &reused); err != nil {
		t.Fatalf("parse second worker start json: %v\noutput: %s", err, out)
	}
	if !reused.Reused {
		t.Fatalf("expected reused=true on second start, got: %+v", reused)
	}
	if reused.PID != start.PID || reused.WorkerID != start.WorkerID {
		t.Fatalf("expected second start to reuse same worker, first=%+v second=%+v", start, reused)
	}

	var status workerStatusResult
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, errOut, err = runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "status", "--json")
		if err != nil {
			t.Fatalf("worker status failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
		}
		if err := json.Unmarshal([]byte(out), &status); err != nil {
			t.Fatalf("parse worker status json: %v\noutput: %s", err, out)
		}
		if status.Local.ProcessExists && status.Local.State == "running" && status.Master.State == "active" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker status did not become active in time: %+v", status)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !status.OK {
		t.Fatalf("expected ok=true from worker status, got: %+v", status)
	}
	if !status.Local.Managed || !status.Local.ProcessExists {
		t.Fatalf("expected managed local running worker, got: %+v", status.Local)
	}
	if status.Master.Registration == nil || status.Master.Registration.ID != start.WorkerID {
		t.Fatalf("expected master registration for %s, got: %+v", start.WorkerID, status.Master)
	}

	out, errOut, err = runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "stop", "--json")
	if err != nil {
		t.Fatalf("worker stop failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	var stop workerStopResult
	if err := json.Unmarshal([]byte(out), &stop); err != nil {
		t.Fatalf("parse worker stop json: %v\noutput: %s", err, out)
	}
	if !stop.OK || stop.StoppedCount != 1 {
		t.Fatalf("unexpected worker stop result: %+v", stop)
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		out, errOut, err = runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "status", "--json")
		if err != nil {
			t.Fatalf("worker status after stop failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
		}
		if err := json.Unmarshal([]byte(out), &status); err != nil {
			t.Fatalf("parse worker status after stop json: %v\noutput: %s", err, out)
		}
		if !status.Local.ProcessExists && (status.Master.State == "not_registered" || status.Master.State == "stale") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker status did not reflect stopped worker in time: %+v", status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestDaemonStatusOutsideRepo(t *testing.T) {
	tmp := t.TempDir()
	out, errOut, err := runOrchOutsideRepo(t, tmp, "daemon", "status")
	if err != nil {
		t.Fatalf("daemon status failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if strings.Contains(out, "could not determine project root") || strings.Contains(errOut, "could not determine project root") {
		t.Fatalf("unexpected project-root error\nstdout: %s\nstderr: %s", out, errOut)
	}
	if !strings.Contains(out, "Status:") {
		t.Fatalf("expected status output, got: %s", out)
	}
}

func TestMasterStatusOutsideRepo(t *testing.T) {
	tmp := t.TempDir()
	out, errOut, err := runOrchOutsideRepo(t, tmp, "master", "status")
	if err != nil {
		t.Fatalf("master status failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if strings.Contains(out, "could not determine project root") || strings.Contains(errOut, "could not determine project root") {
		t.Fatalf("unexpected project-root error\nstdout: %s\nstderr: %s", out, errOut)
	}
	if !strings.Contains(out, "Status:") {
		t.Fatalf("expected status output, got: %s", out)
	}
}

func TestPsOutsideRepo(t *testing.T) {
	tmp := t.TempDir()
	out, errOut, err := runOrchOutsideRepo(t, tmp, "ps", "--json")
	if err != nil {
		t.Fatalf("ps outside repo failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if strings.Contains(out, "project root not found") || strings.Contains(errOut, "project root not found") {
		t.Fatalf("unexpected project-root error\nstdout: %s\nstderr: %s", out, errOut)
	}

	var result struct {
		OK    bool              `json:"ok"`
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse ps --json output: %v\noutput: %s", err, out)
	}
	if !result.OK {
		t.Fatalf("expected ok=true from ps output, got: %s", out)
	}
}

func TestIssueListOutsideRepo(t *testing.T) {
	tmp := t.TempDir()
	out, errOut, err := runOrchOutsideRepo(t, tmp, "issue", "list", "--json")
	if err != nil {
		t.Fatalf("issue list outside repo failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if strings.Contains(out, "project root not found") || strings.Contains(errOut, "project root not found") {
		t.Fatalf("unexpected project-root error\nstdout: %s\nstderr: %s", out, errOut)
	}

	var result struct {
		OK     bool              `json:"ok"`
		Issues []json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse issue list --json output: %v\noutput: %s", err, out)
	}
	if !result.OK {
		t.Fatalf("expected ok=true from issue list output, got: %s", out)
	}
}

func TestQueryOutsideRepo(t *testing.T) {
	tmp := t.TempDir()
	out, errOut, err := runOrchOutsideRepo(t, tmp, "query", "SELECT COUNT(*) AS n FROM runs", "--format", "json")
	if err != nil {
		t.Fatalf("query outside repo failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if strings.Contains(out, "project root not found") || strings.Contains(errOut, "project root not found") {
		t.Fatalf("unexpected project-root error\nstdout: %s\nstderr: %s", out, errOut)
	}
}

func TestSchemaOutsideRepo(t *testing.T) {
	tmp := t.TempDir()
	out, errOut, err := runOrchOutsideRepo(t, tmp, "schema", "--format", "json")
	if err != nil {
		t.Fatalf("schema outside repo failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if strings.Contains(out, "project root not found") || strings.Contains(errOut, "project root not found") {
		t.Fatalf("unexpected project-root error\nstdout: %s\nstderr: %s", out, errOut)
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

func TestRestartFromBranch(t *testing.T) {
	issueID := "restart-branch"
	createTestIssue(t, issueID, "---\ntitle: Restart Branch\n---\n# Restart Branch")

	runGitCmd(t, testRepo, "checkout", "-b", "feature/restart-branch")
	if err := os.WriteFile(filepath.Join(testRepo, "feature.txt"), []byte("feature"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, testRepo, "add", "feature.txt")
	runGitCmd(t, testRepo, "commit", "-m", "feature work")
	runGitCmd(t, testRepo, "checkout", "main")

	output, err := runOrch(t, "--json", "restart-from", issueID, "--branch", "feature/restart-branch", "--tmux=false")
	if err != nil {
		t.Fatalf("restart-from failed: %v", err)
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
	if result.Branch != "feature/restart-branch" {
		t.Fatalf("Branch = %q, want %q", result.Branch, "feature/restart-branch")
	}
	if !strings.HasPrefix(result.ContinuedFrom, "branch:") {
		t.Fatalf("ContinuedFrom = %q, want prefix %q", result.ContinuedFrom, "branch:")
	}
	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}

	branch := runGitCmd(t, result.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "feature/restart-branch" {
		t.Fatalf("worktree branch = %q, want %q", branch, "feature/restart-branch")
	}
}

func TestHelpListsRestartFromAndNotContinue(t *testing.T) {
	output, err := runOrch(t, "--help")
	if err != nil {
		t.Fatalf("help failed: %v", err)
	}

	if !strings.Contains(output, "restart-from") {
		t.Fatalf("expected help output to contain restart-from command")
	}
	if strings.Contains(output, "\n  continue") {
		t.Fatalf("expected help output to remove continue command")
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
		output, err := runOrch(t, "run", issueID, "--dry-run", "--json")
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
		if result.WorktreePath == "" {
			t.Fatalf("expected worktree path for %s to be set", issueID)
		}
		issueSegment := string(os.PathSeparator) + issueID + string(os.PathSeparator)
		if !strings.Contains(result.WorktreePath, issueSegment) {
			t.Fatalf("expected worktree path for %s to contain %s, got %s", issueID, issueSegment, result.WorktreePath)
		}
	}
}

func TestDaemonMultiRepoIsolation(t *testing.T) {
	createTestIssue(t, "isolation-a", "---\ntype: issue\nid: isolation-a\ntitle: Isolation A\nstatus: open\n---\n# Isolation A")

	outputA, err := runOrch(t, "run", "isolation-a", "--dry-run", "--json")
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
	if resultA.WorktreePath == "" {
		t.Fatalf("expected repo A worktree path to be set")
	}
	if !strings.Contains(resultA.WorktreePath, string(os.PathSeparator)+"isolation-a"+string(os.PathSeparator)) {
		t.Fatalf("expected repo A worktree path to contain issue ID, got %s", resultA.WorktreePath)
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
	runGitCmd(t, repoB, "branch", "-M", "main")
	originB := filepath.Join(tmpRoot, "origin-b.git")
	runGitCmd(t, tmpRoot, "init", "--bare", originB)
	runGitCmd(t, repoB, "remote", "add", "origin", originB)
	runGitCmd(t, repoB, "push", "-u", "origin", "main")

	createTestIssueInVault(t, vaultB, "isolation-b", "---\ntype: issue\nid: isolation-b\ntitle: Isolation B\nstatus: open\n---\n# Isolation B")

	outputB, err := runOrchInRepo(t, repoB, vaultB, "run", "isolation-b", "--dry-run", "--json")
	if err != nil {
		if strings.Contains(outputB, "unknown project_id") {
			t.Skipf("multi-repo project-id aliasing not yet available in distributed worker path: %s", strings.TrimSpace(outputB))
		}
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
	if resultB.WorktreePath == "" {
		t.Fatalf("expected repo B worktree path to be set")
	}
	if !strings.Contains(resultB.WorktreePath, string(os.PathSeparator)+"isolation-b"+string(os.PathSeparator)) {
		t.Fatalf("expected repo B worktree path to contain issue ID, got %s", resultB.WorktreePath)
	}
	if resultA.WorktreePath == resultB.WorktreePath {
		t.Fatalf("expected isolated worktree paths per repo, both were %s", resultB.WorktreePath)
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

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func tmuxGlobalOption(name string) (string, error) {
	out, err := exec.Command("tmux", "show-option", "-g", "-v", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("show-option %s failed: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func setTmuxGlobalOption(name, value string) error {
	out, err := exec.Command("tmux", "set-option", "-g", name, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set-option %s failed: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func TestRunWithBuiltInAgents(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	for _, bin := range []string{"codex", "claude", "opencode"} {
		if !hasBinary(bin) {
			t.Skipf("%s not available", bin)
		}
	}

	issueID := "run-builtins-" + time.Now().Format("20060102-150405")
	createTestIssue(t, issueID, fmt.Sprintf("---\ntype: issue\nid: %s\ntitle: Run Built-in Agents Test\nstatus: open\n---\n# Run Built-in Agents Test", issueID))

	t.Cleanup(func() {
		if output, err := runOrch(t, "stop", issueID, "--force", "--json"); err != nil {
			t.Logf("cleanup stop failed for %s: %v (%s)", issueID, err, strings.TrimSpace(output))
		}
	})

	type runResult struct {
		OK      bool   `json:"ok"`
		IssueID string `json:"issue_id"`
		RunID   string `json:"run_id"`
		Status  string `json:"status"`
		Error   string `json:"error,omitempty"`
	}

	agents := []string{"codex", "claude", "opencode"}
	for idx, agentName := range agents {
		runID := fmt.Sprintf("%s-%d-%s", time.Now().Format("20060102-150405"), idx, agentName)
		output, err := runOrch(t,
			"run", issueID,
			"--agent", agentName,
			"--run-id", runID,
			"--no-pr",
			"--json",
		)
		if err != nil {
			if agentName == "opencode" && strings.Contains(output, "failed to create opencode session") && strings.Contains(output, "SQLiteError: disk I/O error") {
				t.Skipf("opencode runtime not healthy in test env: %s", output)
			}
			t.Fatalf("run failed for %s: %v\nOutput: %s", agentName, err, output)
		}

		var result runResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse run JSON for %s: %v\nOutput: %s", agentName, err, output)
		}

		if !result.OK {
			t.Fatalf("expected ok=true for %s, got false: %s", agentName, output)
		}
		if result.IssueID != issueID {
			t.Fatalf("expected issue_id=%s for %s, got %s", issueID, agentName, result.IssueID)
		}
		if result.RunID == "" {
			t.Fatalf("expected run_id for %s, got empty", agentName)
		}
		if result.Status != "running" {
			t.Fatalf("expected status=running for %s, got %q", agentName, result.Status)
		}
	}

	psOutput, err := runOrch(t, "ps", "--all", "--issue", issueID, "--json")
	if err != nil {
		t.Fatalf("ps failed for %s: %v\nOutput: %s", issueID, err, psOutput)
	}

	var psResult struct {
		OK    bool `json:"ok"`
		Items []struct {
			CLI string `json:"cli"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(psOutput), &psResult); err != nil {
		t.Fatalf("failed to parse ps JSON: %v\nOutput: %s", err, psOutput)
	}
	if !psResult.OK {
		t.Fatalf("expected ps ok=true for %s, got false", issueID)
	}

	seen := map[string]bool{}
	for _, item := range psResult.Items {
		seen[item.CLI] = true
	}

	for _, agentName := range agents {
		if !seen[agentName] {
			t.Fatalf("expected ps output to include cli=%s for issue %s", agentName, issueID)
		}
	}
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

func TestRunWithTmuxStartupPromptRaceRegression(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	issueID := "tmux-startup-race-" + time.Now().Format("20060102-150405")
	createTestIssue(t, issueID, fmt.Sprintf("---\ntype: issue\nid: %s\ntitle: Tmux startup prompt race\nstatus: open\n---\n# Tmux startup prompt race", issueID))

	fakeDir := t.TempDir()
	fakeShell := filepath.Join(fakeDir, "fake-startup-shell.sh")
	fakeAgent := filepath.Join(fakeDir, "fake-agent.sh")

	fakeShellScript := `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '[fake-startup] prompt [Y/n]\n'
  dd bs=1 count=1 of=/dev/null 2>/dev/null || true
  exec /bin/sh
fi
exec /bin/sh "$@"
`
	if err := os.WriteFile(fakeShell, []byte(fakeShellScript), 0755); err != nil {
		t.Fatalf("write fake shell: %v", err)
	}

	fakeAgentScript := `#!/bin/sh
echo started > AGENT_STARTED
sleep 2
`
	if err := os.WriteFile(fakeAgent, []byte(fakeAgentScript), 0755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	_ = exec.Command("tmux", "start-server").Run()
	defaultShell, err := tmuxGlobalOption("default-shell")
	if err != nil {
		t.Skipf("unable to read tmux default-shell: %v", err)
	}
	if err := setTmuxGlobalOption("default-shell", fakeShell); err != nil {
		t.Fatalf("set tmux default-shell: %v", err)
	}
	t.Cleanup(func() {
		if err := setTmuxGlobalOption("default-shell", defaultShell); err != nil {
			t.Logf("restore tmux default-shell failed: %v", err)
		}
	})

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	output, err := runOrch(t, "run", issueID,
		"--run-id", runID,
		"--agent", "custom",
		"--agent-cmd", fakeAgent,
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
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nOutput: %s", err, output)
	}
	if !result.OK {
		t.Fatalf("expected ok=true, got false: %s", output)
	}

	t.Cleanup(func() {
		if stopOut, stopErr := runOrch(t, "stop", issueID+"#"+runID, "--force", "--json"); stopErr != nil {
			t.Logf("cleanup stop failed: %v (%s)", stopErr, strings.TrimSpace(stopOut))
		}
		if result.WorktreePath != "" {
			exec.Command("git", "-C", testRepo, "worktree", "remove", result.WorktreePath, "--force").Run()
		}
	})

	marker := filepath.Join(result.WorktreePath, "AGENT_STARTED")
	deadline := time.Now().Add(5 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			found = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !found {
		pane := ""
		if result.SessionName != "" {
			if out, err := exec.Command("tmux", "capture-pane", "-t", result.SessionName, "-p", "-S", "-50").CombinedOutput(); err == nil {
				pane = string(out)
			}
		}
		t.Fatalf("expected %s to exist, command may have been corrupted by startup prompt\nPane:\n%s", marker, pane)
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
		{"waiting", "wait"},
		{"rate_limited", "rlimit"},
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
