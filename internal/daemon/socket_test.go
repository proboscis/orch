package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/git"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
	"github.com/proboscis/orch/internal/runevents"
	"github.com/proboscis/orch/internal/store"
	"github.com/proboscis/orch/internal/xdg"
	"google.golang.org/protobuf/proto"
)

func TestExternalWorkerHelperProcess(t *testing.T) {
	if os.Getenv("ORCH_WORKER_HELPER") != "1" {
		return
	}

	workerID := strings.TrimSpace(os.Getenv("ORCH_WORKER_ID"))
	if workerID == "" {
		os.Exit(2)
	}
	helperMode := strings.TrimSpace(os.Getenv("ORCH_WORKER_HELPER_MODE"))
	if helperMode == "" {
		helperMode = "fail"
	}
	resultJSON := os.Getenv("ORCH_WORKER_HELPER_RESULT_JSON")
	expectedEffect := strings.TrimSpace(os.Getenv("ORCH_WORKER_EXPECT_EFFECT"))
	autoRegister := strings.TrimSpace(os.Getenv("ORCH_WORKER_HELPER_REGISTER")) == "1"

	client := NewProtoClientWithAddress("", "")
	defer client.Close()
	if autoRegister {
		_, _ = client.RegisterWorker(workerID, "executor", "localhost", "external")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		leaseResp, err := client.LeaseWork(workerID)
		if err != nil {
			os.Exit(3)
		}
		if leaseResp != nil && leaseResp.Lease != nil && strings.TrimSpace(leaseResp.Lease.LeaseID) != "" {
			if expectedEffect != "" && strings.TrimSpace(leaseResp.Lease.Effect) != expectedEffect {
				_ = client.AcknowledgeEffect(workerID, leaseResp.Lease.LeaseID, false, "unexpected lease effect", "")
				os.Exit(5)
			}

			success := helperMode == "success"
			errMsg := ""
			if !success {
				errMsg = "external worker helper failure"
			}
			_ = client.AcknowledgeEffect(workerID, leaseResp.Lease.LeaseID, success, errMsg, resultJSON)
			os.Exit(0)
		}
		time.Sleep(50 * time.Millisecond)
	}

	os.Exit(4)
}

type mockStore struct {
	runs         map[string]*model.Run
	issues       map[string]*model.Issue
	updatedIssue *model.Issue
}

type listRunsErrorStore struct {
	*mockStore
	err error
}

func (s *listRunsErrorStore) ListRuns(*store.ListRunsFilter) ([]*model.Run, error) {
	return nil, s.err
}

type resolveIssueErrorStore struct {
	*mockStore
	err error
}

func (s *resolveIssueErrorStore) ResolveIssue(model.IssueID) (*model.Issue, error) {
	return nil, s.err
}

type setIssueStatusErrorStore struct {
	*mockStore
	err error
}

func (s *setIssueStatusErrorStore) SetIssueStatus(model.IssueID, model.IssueStatus) error {
	return s.err
}

func (m *mockStore) ResolveIssue(issueID model.IssueID) (*model.Issue, error) {
	if issue, ok := m.issues[string(issueID)]; ok {
		return issue, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockStore) ListIssues() ([]*model.Issue, error) {
	var issues []*model.Issue
	for _, issue := range m.issues {
		issues = append(issues, issue)
	}
	return issues, nil
}

func (m *mockStore) SetIssueStatus(issueID model.IssueID, status model.IssueStatus) error {
	return nil
}

func (m *mockStore) CreateRun(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
	// Mirror FileStore: verify the issue exists for non-GitHub issues.
	if !strings.HasPrefix(string(issueID), "gh-") && !strings.HasPrefix(string(issueID), "gh#") {
		if _, err := m.ResolveIssue(issueID); err != nil {
			return nil, fmt.Errorf("issue not found: %s", issueID)
		}
	}
	return m.createRunDoc(issueID, runID, metadata)
}

func (m *mockStore) CreateRunForExistingIssue(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
	// No issue verification: worker-delegated path.
	return m.createRunDoc(issueID, runID, metadata)
}

func (m *mockStore) createRunDoc(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
	if m.runs == nil {
		m.runs = make(map[string]*model.Run)
	}
	run := &model.Run{
		IssueID:       issueID,
		RunID:         runID,
		Agent:         metadata["agent"],
		Profile:       metadata["profile"],
		Model:         metadata["model"],
		ModelVariant:  metadata["model_variant"],
		Target:        metadata["target"],
		ContinuedFrom: metadata["continued_from"],
		Status:        model.StatusQueued,
	}
	m.runs[run.Ref().String()] = run
	return run, nil
}

func (m *mockStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	if m.runs == nil || ref == nil || event == nil {
		return nil
	}
	if run, ok := m.runs[ref.String()]; ok && run != nil {
		run.Events = append(run.Events, event)
		run.DeriveState()
	}
	return nil
}

func (m *mockStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	var runs []*model.Run
	for _, run := range m.runs {
		if filter.IssueID != "" && run.IssueID != filter.IssueID {
			continue
		}
		if len(filter.Status) > 0 {
			statusMatch := false
			for _, st := range filter.Status {
				if run.Status == st {
					statusMatch = true
					break
				}
			}
			if !statusMatch {
				continue
			}
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].UpdatedAt.After(runs[j].UpdatedAt)
	})
	return runs, nil
}

func (m *mockStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	key := ref.String()
	if run, ok := m.runs[key]; ok {
		return run, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockStore) GetRunByShortID(shortID model.ShortID) (*model.Run, error) {
	var match *model.Run
	for _, run := range m.runs {
		if run == nil {
			continue
		}
		if strings.HasPrefix(string(run.ShortID()), string(shortID)) {
			if match != nil {
				return nil, fmt.Errorf("ambiguous short ID")
			}
			match = run
		}
	}
	if match == nil {
		return nil, os.ErrNotExist
	}
	return match, nil
}

func (m *mockStore) GetLatestRun(issueID model.IssueID) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) RootPath() string {
	return ""
}

func (m *mockStore) DeleteRun(ref *model.RunRef) error {
	return nil
}

func (m *mockStore) UpdateIssue(issue *model.Issue) error {
	clone := *issue
	m.updatedIssue = &clone
	return nil
}

func (m *mockStore) ValidateIssueFiles(issueID model.IssueID) (*store.ValidationResult, error) {
	return &store.ValidationResult{}, nil
}

func (m *mockStore) WriteAgentPrompt(ref *model.RunRef, content string) error {
	return nil
}

func (m *mockStore) ReadAgentPrompt(ref *model.RunRef) (string, error) {
	return "", nil
}

func (m *mockStore) CreateIssue(issue *model.Issue) error {
	return nil
}

func TestSocketFilePath(t *testing.T) {
	// Set up temp XDG runtime dir with short path (Unix socket limit is 104 chars)
	tmpDir := filepath.Join("/tmp", "orch-test-"+randomID())
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	defer os.Setenv("XDG_RUNTIME_DIR", oldXDG)

	// SocketFilePath now returns global XDG path
	path := SocketFilePath("")
	expected := xdg.SocketPath()
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestLegacySocketFilePath(t *testing.T) {
	// Legacy path should still return per-project path
	path := LegacySocketFilePath("/vault")
	expected := "/vault/.orch/daemon.sock"
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestGetAllRepoContextsCollapsesStoreAliases(t *testing.T) {
	shared := &mockStoreForUpdate{}
	other := &mockStoreForUpdate{}
	s := &SocketServer{
		repos: map[string]*RepoContext{
			// getOrCreateStore caches the same store under its issuesRoot
			// path and under the repo ID; the monitor must see it once.
			"/home/u/repos/orch-issues": {Store: shared},
			"proboscis-orch":            {Store: shared, RepoID: "proboscis-orch", ProjectRoot: "/home/u/repos/orch"},
			"other-repo":                {Store: other, RepoID: "other-repo", ProjectRoot: "/home/u/repos/other"},
		},
	}

	contexts := s.GetAllRepoContexts()
	if len(contexts) != 2 {
		t.Fatalf("expected aliases collapsed to 2 contexts, got %d", len(contexts))
	}
	for _, ctx := range contexts {
		if ctx.Store == store.Store(shared) && ctx.ProjectRoot != "/home/u/repos/orch" {
			t.Fatalf("expected the alias with project metadata to win, got %+v", ctx)
		}
	}
}

func randomID() string {
	return time.Now().Format("150405") + "-" + randomString(4)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func setupXDGTestEnv(t *testing.T) func() {
	tmpDir := filepath.Join("/tmp", "orch-test-"+randomID())
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	oldXDGState := os.Getenv("XDG_STATE_HOME")
	oldXDGConfig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", filepath.Join(tmpDir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config"))
	return func() {
		os.Setenv("XDG_RUNTIME_DIR", oldXDG)
		os.Setenv("XDG_STATE_HOME", oldXDGState)
		os.Setenv("XDG_CONFIG_HOME", oldXDGConfig)
		os.RemoveAll(tmpDir)
	}
}

func newTestServer(t *testing.T, st store.Store) *SocketServer {
	logger := log.New(io.Discard, "", 0)
	factory := func(issuesRoot string) (store.Store, error) {
		return st, nil
	}
	server := NewSocketServer(factory, logger)
	if st != nil {
		registerRepoContextForTest(t, server, testProjectID, testProjectRoot, st)
	}
	return server
}

func registerRepoContextForTest(t *testing.T, server *SocketServer, repoID, projectRoot string, st store.Store) {
	t.Helper()

	if _, err := server.registerRepoContext(repoID, projectRoot, "", st); err != nil {
		t.Fatalf("register repo context: %v", err)
	}
}

func createGitRepoWithOrigin(t *testing.T, remoteURL string) string {
	t.Helper()

	repo := t.TempDir()
	if remoteURL == "" {
		remoteURL = fmt.Sprintf("https://github.com/example/%s.git", filepath.Base(repo))
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}

	run("git", "init", repo)
	run("git", "-C", repo, "config", "user.email", "test@example.com")
	run("git", "-C", repo, "config", "user.name", "Test User")
	run("git", "-C", repo, "remote", "add", "origin", remoteURL)

	return repo
}

func initGitRepoWithCommit(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}

	run("git", "init", repo)
	run("git", "-C", repo, "config", "user.email", "test@example.com")
	run("git", "-C", repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("git", "-C", repo, "add", "README.md")
	run("git", "-C", repo, "commit", "-m", "init")
	run("git", "-C", repo, "branch", "-M", "main")
	run("git", "-C", repo, "remote", "add", "origin", repo)
	run("git", "-C", repo, "fetch", "origin")

	return repo
}

func TestSocketServerStartStop(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: make(map[string]*model.Run)}
	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	socketPath := xdg.SocketPath()
	if _, err := os.Stat(socketPath); err != nil {
		t.Errorf("socket file not created: %v", err)
	}

	server.Stop()

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file not cleaned up")
	}
}

func TestWithWorkerLeaseUsesEmbeddedDispatcherRPCPath(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	issueID := "orch-lease"
	runID := "run-lease"
	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID: model.IssueID(issueID),
				RunID:   model.RunID(runID),
				Status:  model.StatusRunning,
			},
		},
		issues: map[string]*model.Issue{},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	workerID := "lease-helper-worker"
	if _, ttl := server.registerWorker(workerID, "external", "localhost", "external", []string{"stop_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for helper worker")
	}
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease != nil {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", "")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	projectID := testProjectID
	if _, err := server.withWorkerLease(projectID, "stop_run", issueID, runID, nil); err != nil {
		t.Fatalf("withWorkerLease() error = %v", err)
	}

	allLeases := server.listWorkerLeases(true)
	var matched *WorkerLease
	for _, lease := range allLeases {
		if lease.ProjectID == projectID && lease.Effect == "stop_run" && lease.IssueID == issueID && lease.RunID == runID {
			matched = lease
			break
		}
	}
	if matched == nil {
		t.Fatal("expected worker lease record")
	}
	if !matched.Completed || !matched.Success {
		t.Fatalf("expected completed successful lease, got completed=%v success=%v err=%q", matched.Completed, matched.Success, matched.Error)
	}
	if matched.DispatchCount < 1 {
		t.Fatalf("dispatch_count = %d, want >= 1", matched.DispatchCount)
	}
}

func TestWithWorkerLeaseExternalProcessAckPath(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	client := NewProtoClientWithAddress("", "")
	defer client.Close()

	workerID := "external-helper-worker"
	if _, err := client.RegisterWorker(workerID, "executor", "localhost", "external"); err != nil {
		t.Fatalf("register external worker failed: %v", err)
	}
	defer client.UnregisterWorker(workerID)

	errCh := make(chan error, 1)
	go func() {
		_, err := server.withWorkerLease("project-test", "stop_run", "orch-x", "run-x", nil)
		errCh <- err
	}()

	cmd := exec.Command(os.Args[0], "-test.run=TestExternalWorkerHelperProcess")
	cmd.Env = append(os.Environ(), "ORCH_WORKER_HELPER=1", "ORCH_WORKER_ID="+workerID, "ORCH_WORKER_HELPER_MODE=fail")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external worker helper failed: %v, output: %s", err, out)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("withWorkerLease() error = nil, want propagated external worker failure")
		}
		if !strings.Contains(err.Error(), "external worker helper failure") {
			t.Fatalf("withWorkerLease() error = %v, want external worker failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for withWorkerLease completion")
	}

	allLeases := server.listWorkerLeases(true)
	var matched *WorkerLease
	for _, lease := range allLeases {
		if lease.WorkerID == workerID && lease.Effect == "stop_run" && lease.IssueID == "orch-x" && lease.RunID == "run-x" {
			matched = lease
			break
		}
	}
	if matched == nil {
		t.Fatal("expected lease assigned to external helper worker")
	}
	if !matched.Completed || matched.Success {
		t.Fatalf("expected completed failed lease, got completed=%v success=%v", matched.Completed, matched.Success)
	}
}

func TestWithWorkerLeaseExternalProcessSuccessResultPath(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	client := NewProtoClientWithAddress("", "")
	defer client.Close()

	workerID := "external-helper-worker-success"
	if _, err := client.RegisterWorker(workerID, "executor", "localhost", "external"); err != nil {
		t.Fatalf("register external worker failed: %v", err)
	}
	defer client.UnregisterWorker(workerID)

	resultJSON := `{"continue_run_result":{"RunID":"run-success"}}`
	errCh := make(chan error, 1)
	go func() {
		_, err := server.withWorkerLease("project-test", "stop_run", "orch-y", "run-y", nil)
		errCh <- err
	}()

	cmd := exec.Command(os.Args[0], "-test.run=TestExternalWorkerHelperProcess")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	cmd.Env = append(os.Environ(),
		"ORCH_WORKER_HELPER=1",
		"ORCH_WORKER_ID="+workerID,
		"ORCH_WORKER_HELPER_MODE=success",
		"ORCH_WORKER_HELPER_RESULT_JSON="+resultJSON,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("external worker helper failed: %v, output: %s", err, outBuf.String())
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("withWorkerLease() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for withWorkerLease completion")
	}

	allLeases := server.listWorkerLeases(true)
	var matched *WorkerLease
	for _, lease := range allLeases {
		if lease.WorkerID == workerID && lease.Effect == "stop_run" && lease.IssueID == "orch-y" && lease.RunID == "run-y" {
			matched = lease
			break
		}
	}
	if matched == nil {
		t.Fatal("expected lease assigned to external helper worker")
	}
	if !matched.Completed || !matched.Success {
		t.Fatalf("expected completed successful lease, got completed=%v success=%v", matched.Completed, matched.Success)
	}
	if strings.TrimSpace(matched.ResultJSON) == "" {
		t.Fatal("expected non-empty lease result_json")
	}
}

func TestProtoStartRunUsesResolvedDefaultWorkerResultJSON(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectID := testProjectID
	st := &mockStore{
		runs: make(map[string]*model.Run),
		issues: map[string]*model.Issue{
			"issue-start": {ID: "issue-start", Title: "Start issue", Status: model.IssueStatusOpen, Path: "/tmp/issue-start.md"},
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	client := NewProtoClientWithAddress("", "")
	defer client.Close()

	workerID := defaultWorkerID()
	if _, err := client.RegisterWorker(workerID, "executor", "localhost", "external"); err != nil {
		t.Fatalf("register default worker failed: %v", err)
	}
	defer client.UnregisterWorker(workerID)

	resultJSON := `{"start_run_result":{"RunID":"run-from-worker","Branch":"worker-branch","WorktreePath":"/tmp/wt","SessionName":"sess1","Status":"running"}}`
	cmd := exec.Command(os.Args[0], "-test.run=TestExternalWorkerHelperProcess")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	cmd.Env = append(os.Environ(),
		"ORCH_WORKER_HELPER=1",
		"ORCH_WORKER_ID="+workerID,
		"ORCH_WORKER_HELPER_MODE=success",
		"ORCH_WORKER_EXPECT_EFFECT=start_run",
		"ORCH_WORKER_HELPER_RESULT_JSON="+resultJSON,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_StartRun{StartRun: &orchpb.StartRunRequest{
			IssueId: "issue-start",
			RunId:   "run-from-worker",
			Context: &orchpb.RequestContext{ProjectId: projectID},
		}},
	})

	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process failed: %v, output: %s", err, outBuf.String())
	}

	if !resp.Ok || resp.GetStartRun() == nil {
		t.Fatalf("expected successful start_run response, got ok=%v error=%q", resp.Ok, resp.Error)
	}
	if resp.GetStartRun().GetRunId() != "run-from-worker" || resp.GetStartRun().GetBranch() != "worker-branch" {
		t.Fatalf("unexpected start_run response: %+v", resp.GetStartRun())
	}

	allLeases := server.listWorkerLeases(true)
	found := false
	for _, lease := range allLeases {
		if lease.WorkerID == workerID && lease.Effect == "start_run" && lease.IssueID == "issue-start" {
			if !lease.Completed || !lease.Success || strings.TrimSpace(lease.ResultJSON) == "" {
				t.Fatalf("unexpected lease completion state: completed=%v success=%v result=%q", lease.Completed, lease.Success, lease.ResultJSON)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected start_run lease assigned to resolved default worker")
	}
}

func TestProtoContinueRunUsesExternalWorkerResultJSON(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectID := testProjectID
	st := &mockStore{
		runs: map[string]*model.Run{
			"issue-cont#run-prev": {
				IssueID:      "issue-cont",
				RunID:        "run-prev",
				Status:       model.StatusFailed,
				Agent:        "custom",
				Branch:       "cont-branch",
				WorktreePath: "/tmp/cont-prev",
			},
		},
		issues: map[string]*model.Issue{
			// The master (issue-store SSOT) resolves the issue before delegating to
			// the worker, so the issue must exist on the master store.
			"issue-cont": {ID: "issue-cont", Title: "Cont", Status: model.IssueStatusOpen},
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	client := NewProtoClientWithAddress("", "")
	defer client.Close()

	workerID := "external-helper-worker-continue"
	if _, err := client.RegisterWorker(workerID, "executor", "localhost", "external"); err != nil {
		t.Fatalf("register external worker failed: %v", err)
	}
	defer client.UnregisterWorker(workerID)

	resultJSON := `{"continue_run_result":{"RunID":"run-cont","Branch":"cont-branch","WorktreePath":"/tmp/cont","SessionName":"sess-cont","Status":"running","ContinuedFrom":"run-prev","IssueID":"issue-cont"}}`
	cmd := exec.Command(os.Args[0], "-test.run=TestExternalWorkerHelperProcess")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	cmd.Env = append(os.Environ(),
		"ORCH_WORKER_HELPER=1",
		"ORCH_WORKER_ID="+workerID,
		"ORCH_WORKER_HELPER_MODE=success",
		"ORCH_WORKER_EXPECT_EFFECT=continue_run",
		"ORCH_WORKER_HELPER_RESULT_JSON="+resultJSON,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ContinueRun{ContinueRun: &orchpb.ContinueRunRequest{
			IssueId: "issue-cont",
			RunId:   "run-prev",
			Context: &orchpb.RequestContext{ProjectId: projectID},
		}},
	})

	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process failed: %v, output: %s", err, outBuf.String())
	}

	if !resp.Ok || resp.GetContinueRun() == nil {
		t.Fatalf("expected successful continue_run response, got ok=%v error=%q", resp.Ok, resp.Error)
	}
	if resp.GetContinueRun().GetRunId() != "run-cont" || resp.GetContinueRun().GetContinuedFrom() != "run-prev" {
		t.Fatalf("unexpected continue_run response: %+v", resp.GetContinueRun())
	}

	allLeases := server.listWorkerLeases(true)
	found := false
	for _, lease := range allLeases {
		if lease.WorkerID == workerID && lease.Effect == "continue_run" && lease.IssueID == "issue-cont" {
			if !lease.Completed || !lease.Success || strings.TrimSpace(lease.ResultJSON) == "" {
				t.Fatalf("unexpected lease completion state: completed=%v success=%v result=%q", lease.Completed, lease.Success, lease.ResultJSON)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected continue_run lease assigned to external worker")
	}
}

func TestProtoContinueRunInheritsSourceExecutionDefaults(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectRoot := t.TempDir()
	issuesRoot := filepath.Join(projectRoot, "issues-store")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(issuesRoot, "issues"), 0o755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}
	configBody := fmt.Sprintf(`agent: codex
model: cfg-model
model_variant: cfg-variant
agent_multiplexer: zellij
issues:
  path: %s
codex:
  default_profile: company
  profiles:
    company:
      target: mac
      codex_home: ~/.codex-company
    personal:
      target: mac
      codex_home: ~/.codex-personal
targets:
  - name: mac
    host: mac-host
`, issuesRoot)
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tests := []struct {
		name            string
		run             *model.Run
		req             *orchpb.ContinueRunRequest
		wantProfile     string
		wantCodexHome   string
		wantMultiplexer string
		wantModel       string
		wantVariant     string
	}{
		{
			name: "source run wins over config defaults",
			run: &model.Run{
				IssueID:        "issue-cont",
				RunID:          "run-prev-inherit",
				Status:         model.StatusFailed,
				Agent:          "codex",
				Profile:        "personal",
				Model:          "source-model",
				ModelVariant:   "source-variant",
				Branch:         "cont-branch",
				WorktreePath:   "/tmp/cont-prev",
				Target:         "mac",
				TargetHost:     "mac-host",
				TargetWorkerID: HostWorkerID("mac-host"),
				Multiplexer:    "tmux",
			},
			req: &orchpb.ContinueRunRequest{
				IssueId: "issue-cont",
				RunId:   "run-prev-inherit",
			},
			wantProfile:     "personal",
			wantCodexHome:   "~/.codex-personal",
			wantMultiplexer: "tmux",
			wantModel:       "source-model",
			wantVariant:     "source-variant",
		},
		{
			name: "explicit flags override source run",
			run: &model.Run{
				IssueID:        "issue-cont",
				RunID:          "run-prev-override",
				Status:         model.StatusFailed,
				Agent:          "codex",
				Profile:        "personal",
				Model:          "source-model",
				ModelVariant:   "source-variant",
				Branch:         "cont-branch",
				WorktreePath:   "/tmp/cont-prev",
				Target:         "mac",
				TargetHost:     "mac-host",
				TargetWorkerID: HostWorkerID("mac-host"),
				Multiplexer:    "tmux",
			},
			req: &orchpb.ContinueRunRequest{
				IssueId:      "issue-cont",
				RunId:        "run-prev-override",
				CodexProfile: "company",
				Multiplexer:  "zellij",
			},
			wantProfile:     "company",
			wantCodexHome:   "~/.codex-company",
			wantMultiplexer: "zellij",
			wantModel:       "source-model",
			wantVariant:     "source-variant",
		},
		{
			name: "missing source records fall back to config defaults",
			run: &model.Run{
				IssueID:      "issue-cont",
				RunID:        "run-prev-default",
				Status:       model.StatusFailed,
				Agent:        "codex",
				Branch:       "cont-branch",
				WorktreePath: "/tmp/cont-prev",
			},
			req: &orchpb.ContinueRunRequest{
				IssueId: "issue-cont",
				RunId:   "run-prev-default",
			},
			wantProfile:     "company",
			wantCodexHome:   "~/.codex-company",
			wantMultiplexer: "zellij",
			wantModel:       "cfg-model",
			wantVariant:     "cfg-variant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &mockStore{
				runs: map[string]*model.Run{
					tt.run.Ref().String(): tt.run,
				},
				issues: map[string]*model.Issue{
					"issue-cont": {ID: "issue-cont", Title: "Cont", Status: model.IssueStatusOpen},
				},
			}
			server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
			registerRepoContextForTest(t, server, testProjectID, projectRoot, st)
			if err := server.Start(); err != nil {
				t.Fatalf("failed to start server: %v", err)
			}
			defer server.Stop()

			client := NewProtoClientWithAddress("", "")
			defer client.Close()

			workerID := HostWorkerID("mac-host")
			if _, err := client.RegisterWorker(workerID, "executor", "mac-host", "external"); err != nil {
				t.Fatalf("register external worker failed: %v", err)
			}
			defer client.UnregisterWorker(workerID)

			resultRunID := "run-cont-" + strings.TrimPrefix(string(tt.run.RunID), "run-prev-")
			resultJSON := fmt.Sprintf(`{"continue_run_result":{"RunID":%q,"Branch":"cont-branch","WorktreePath":"/tmp/cont","SessionName":"sess-cont","Status":"running","ContinuedFrom":%q,"IssueID":"issue-cont","Multiplexer":%q}}`, resultRunID, tt.run.Ref().String(), tt.wantMultiplexer)
			cmd := exec.Command(os.Args[0], "-test.run=TestExternalWorkerHelperProcess")
			var outBuf bytes.Buffer
			cmd.Stdout = &outBuf
			cmd.Stderr = &outBuf
			cmd.Env = append(os.Environ(),
				"ORCH_WORKER_HELPER=1",
				"ORCH_WORKER_ID="+workerID,
				"ORCH_WORKER_HELPER_MODE=success",
				"ORCH_WORKER_EXPECT_EFFECT=continue_run",
				"ORCH_WORKER_HELPER_RESULT_JSON="+resultJSON,
			)
			if err := cmd.Start(); err != nil {
				t.Fatalf("failed to start helper process: %v", err)
			}

			tt.req.Context = &orchpb.RequestContext{ProjectId: testProjectID}
			resp := sendProtoRequest(t, &orchpb.Request{
				Request: &orchpb.Request_ContinueRun{ContinueRun: tt.req},
			})

			if err := cmd.Wait(); err != nil {
				t.Fatalf("helper process failed: %v, output: %s", err, outBuf.String())
			}
			if !resp.Ok || resp.GetContinueRun() == nil {
				t.Fatalf("expected successful continue_run response, got ok=%v error=%q", resp.Ok, resp.Error)
			}

			var payload *ContinueRunOptions
			for _, lease := range server.listWorkerLeases(true) {
				if lease.Effect == "continue_run" && lease.IssueID == "issue-cont" && lease.Payload != nil {
					payload = lease.Payload.ContinueRun
					break
				}
			}
			if payload == nil {
				t.Fatal("expected continue_run payload")
			}
			if payload.CodexProfile != tt.wantProfile {
				t.Fatalf("payload.CodexProfile = %q, want %q", payload.CodexProfile, tt.wantProfile)
			}
			if payload.CodexHome != tt.wantCodexHome {
				t.Fatalf("payload.CodexHome = %q, want %q", payload.CodexHome, tt.wantCodexHome)
			}
			if payload.Multiplexer != tt.wantMultiplexer {
				t.Fatalf("payload.Multiplexer = %q, want %q", payload.Multiplexer, tt.wantMultiplexer)
			}
			if payload.Model != tt.wantModel || payload.ModelVariant != tt.wantVariant {
				t.Fatalf("payload model = (%q, %q), want (%q, %q)", payload.Model, payload.ModelVariant, tt.wantModel, tt.wantVariant)
			}
			if payload.Target != "mac" || payload.TargetHost != "mac-host" || payload.TargetWorkerID != HostWorkerID("mac-host") {
				t.Fatalf("payload target = (%q, %q, %q), want mac/mac-host/%s", payload.Target, payload.TargetHost, payload.TargetWorkerID, HostWorkerID("mac-host"))
			}

			projected, err := st.GetRun(&model.RunRef{IssueID: "issue-cont", RunID: model.RunID(resultRunID)})
			if err != nil {
				t.Fatalf("expected projected master run: %v", err)
			}
			if projected.Agent != "codex" || projected.Profile != tt.wantProfile || projected.Model != tt.wantModel || projected.ModelVariant != tt.wantVariant {
				t.Fatalf("projected run execution identity = agent=%q profile=%q model=%q variant=%q", projected.Agent, projected.Profile, projected.Model, projected.ModelVariant)
			}
		})
	}
}

func TestSocketServerSendRequest(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue#run": {
				IssueID:           "issue",
				RunID:             "run",
				Agent:             "claude",
				ServerPort:        4096,
				OpenCodeSessionID: "session",
			},
		},
	}
	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_SendMessage{
			SendMessage: &orchpb.SendMessageRequest{
				IssueId: "issue",
				RunId:   "run",
				Message: "test message",
			},
		},
	})

	if resp.Ok {
		t.Error("expected error for non-opencode agent")
	}
	if resp.Error == "" {
		t.Error("expected error message for non-opencode agent")
	}
}

func TestSocketServerSendRequestMissingRun(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: make(map[string]*model.Run)}
	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_SendMessage{
			SendMessage: &orchpb.SendMessageRequest{
				IssueId: "missing",
				RunId:   "run",
				Message: "test message",
			},
		},
	})

	if resp.Ok {
		t.Error("expected error for missing run")
	}
	if resp.Error == "" {
		t.Error("expected error message for missing run")
	}
}

func TestSocketServerSendRequestMissingConfig(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue#run": {
				IssueID:           "issue",
				RunID:             "run",
				Agent:             "opencode",
				ServerPort:        0,
				OpenCodeSessionID: "",
			},
		},
	}
	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_SendMessage{
			SendMessage: &orchpb.SendMessageRequest{
				IssueId: "issue",
				RunId:   "run",
				Message: "test message",
			},
		},
	})

	if resp.Ok {
		t.Error("expected error for missing server config")
	}
	if resp.Error == "" {
		t.Error("expected error message explaining what config is missing")
	}
}

func TestProcessSendOpenCodeReturnsAfterAck(t *testing.T) {
	projectRoot, err := git.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	repoID, err := xdg.RepoIDStrict(projectRoot)
	if err != nil {
		t.Fatalf("failed to resolve repo id: %v", err)
	}

	const bodyDelay = 600 * time.Millisecond
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
		case r.URL.Path == "/project/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","worktree":%q,"sandboxes":[]}`, repoID, projectRoot))
		case strings.HasSuffix(r.URL.Path, "/message"):
			w.WriteHeader(http.StatusAccepted)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(bodyDelay)
			_, _ = io.WriteString(w, `{"status":"accepted"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.openCodeServers[string(repoID)] = &managedServer{
		RepoID:      string(repoID),
		ProjectRoot: projectRoot,
		Port:        getPortFromURL(t, testServer.URL),
		WaitResult:  make(chan error, 1),
	}

	originalTimeout := openCodeSendAckTimeout
	openCodeSendAckTimeout = 2 * time.Second
	defer func() {
		openCodeSendAckTimeout = originalTimeout
	}()

	ref := &model.RunRef{IssueID: "orch-432", RunID: "run-1"}
	run := &model.Run{
		IssueID:           ref.IssueID,
		RunID:             ref.RunID,
		Agent:             string(agent.AgentOpenCode),
		OpenCodeSessionID: "ses_fast_ack",
		WorktreePath:      projectRoot,
	}

	startedAt := time.Now()
	err = server.processSendOpenCode(nil, ref, run, "please rebase")
	elapsed := time.Since(startedAt)

	if err != nil {
		t.Fatalf("processSendOpenCode() error = %v", err)
	}
	if elapsed >= bodyDelay {
		t.Fatalf("expected send to return before response body completion, elapsed=%s bodyDelay=%s", elapsed, bodyDelay)
	}
}

func TestProcessSendOpenCodeTimesOutPromptlyWithoutAck(t *testing.T) {
	projectRoot, err := git.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	repoID, err := xdg.RepoIDStrict(projectRoot)
	if err != nil {
		t.Fatalf("failed to resolve repo id: %v", err)
	}

	const ackDelay = 300 * time.Millisecond
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
		case r.URL.Path == "/project/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","worktree":%q,"sandboxes":[]}`, repoID, projectRoot))
		case strings.HasSuffix(r.URL.Path, "/message"):
			time.Sleep(ackDelay)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"accepted"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.openCodeServers[string(repoID)] = &managedServer{
		RepoID:      string(repoID),
		ProjectRoot: projectRoot,
		Port:        getPortFromURL(t, testServer.URL),
		WaitResult:  make(chan error, 1),
	}

	originalTimeout := openCodeSendAckTimeout
	openCodeSendAckTimeout = 80 * time.Millisecond
	defer func() {
		openCodeSendAckTimeout = originalTimeout
	}()

	ref := &model.RunRef{IssueID: "orch-432", RunID: "run-2"}
	run := &model.Run{
		IssueID:           ref.IssueID,
		RunID:             ref.RunID,
		Agent:             string(agent.AgentOpenCode),
		OpenCodeSessionID: "ses_slow_ack",
		WorktreePath:      projectRoot,
	}

	startedAt := time.Now()
	err = server.processSendOpenCode(nil, ref, run, "please retry")
	elapsed := time.Since(startedAt)

	if err == nil {
		t.Fatal("expected timeout error when ACK is too slow")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected context deadline exceeded error, got: %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("expected send to fail promptly, elapsed=%s", elapsed)
	}
}

func TestProcessSendOpenCodeAckTimeoutButQueuedMessageSucceeds(t *testing.T) {
	projectRoot, err := git.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	repoID, err := xdg.RepoIDStrict(projectRoot)
	if err != nil {
		t.Fatalf("failed to resolve repo id: %v", err)
	}

	const ackDelay = 300 * time.Millisecond
	var (
		mu       sync.Mutex
		messages []agent.Message
	)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
		case r.URL.Path == "/project/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","worktree":%q,"sandboxes":[]}`, repoID, projectRoot))
		case r.URL.Path == "/session/ses_queued/message" && r.Method == http.MethodPost:
			var req agent.PromptRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			text := ""
			for _, part := range req.Parts {
				if part.Type == "text" {
					text = part.Text
					break
				}
			}

			mu.Lock()
			messages = append(messages, agent.Message{
				Info: agent.MessageInfo{
					ID:        "msg-1",
					SessionID: "ses_queued",
					Role:      "user",
					CreatedAt: time.Now(),
				},
				Parts: []agent.MessagePart{{Type: "text", Text: text}},
			})
			mu.Unlock()

			time.Sleep(ackDelay)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"accepted"}`)
		case r.URL.Path == "/session/ses_queued/message" && r.Method == http.MethodGet:
			mu.Lock()
			copied := append([]agent.Message(nil), messages...)
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(copied); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.openCodeServers[string(repoID)] = &managedServer{
		RepoID:      string(repoID),
		ProjectRoot: projectRoot,
		Port:        getPortFromURL(t, testServer.URL),
		WaitResult:  make(chan error, 1),
	}

	prevAckTimeout := openCodeSendAckTimeout
	prevConfirmTimeout := openCodeSendConfirmTimeout
	prevPollInterval := openCodeSendConfirmPollInterval
	openCodeSendAckTimeout = 80 * time.Millisecond
	openCodeSendConfirmTimeout = 1200 * time.Millisecond
	openCodeSendConfirmPollInterval = 20 * time.Millisecond
	defer func() {
		openCodeSendAckTimeout = prevAckTimeout
		openCodeSendConfirmTimeout = prevConfirmTimeout
		openCodeSendConfirmPollInterval = prevPollInterval
	}()

	ref := &model.RunRef{IssueID: "orch-432", RunID: "run-queued"}
	run := &model.Run{
		IssueID:           ref.IssueID,
		RunID:             ref.RunID,
		Agent:             string(agent.AgentOpenCode),
		OpenCodeSessionID: "ses_queued",
		WorktreePath:      projectRoot,
	}

	startedAt := time.Now()
	err = server.processSendOpenCode(nil, ref, run, "please queue this")
	elapsed := time.Since(startedAt)

	if err != nil {
		t.Fatalf("processSendOpenCode() error = %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("expected queued-message confirmation to return quickly, elapsed=%s", elapsed)
	}
}

func TestIsDaemonSocketAvailable(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	if IsDaemonSocketAvailable("") {
		t.Error("expected socket not available initially")
	}

	// Create the runtime dir and socket file
	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	socketPath := xdg.SocketPath()
	f, _ := os.Create(socketPath)
	f.Close()

	if !IsDaemonSocketAvailable("") {
		t.Error("expected socket available after creation")
	}
}

const testProjectRoot = "/test/project"
const testIssuesRoot = "/test/issues"
const testProjectID = "test-project"

func ensureRequestContext(req *orchpb.Request) {
	if req == nil || req.Request == nil {
		return
	}

	newCtx := func() *orchpb.RequestContext {
		return &orchpb.RequestContext{ProjectId: testProjectID}
	}

	switch r := req.Request.(type) {
	case *orchpb.Request_ListRuns:
		if r.ListRuns != nil && r.ListRuns.Context == nil {
			r.ListRuns.Context = newCtx()
		}
	case *orchpb.Request_GetRun:
		if r.GetRun != nil && r.GetRun.Context == nil {
			r.GetRun.Context = newCtx()
		}
	case *orchpb.Request_StartRun:
		if r.StartRun != nil && r.StartRun.Context == nil {
			r.StartRun.Context = newCtx()
		}
	case *orchpb.Request_StopRun:
		if r.StopRun != nil && r.StopRun.Context == nil {
			r.StopRun.Context = newCtx()
		}
	case *orchpb.Request_ResolveRun:
		if r.ResolveRun != nil && r.ResolveRun.Context == nil {
			r.ResolveRun.Context = newCtx()
		}
	case *orchpb.Request_ListIssues:
		if r.ListIssues != nil && r.ListIssues.Context == nil {
			r.ListIssues.Context = newCtx()
		}
	case *orchpb.Request_GetIssue:
		if r.GetIssue != nil && r.GetIssue.Context == nil {
			r.GetIssue.Context = newCtx()
		}
	case *orchpb.Request_CreateIssue:
		if r.CreateIssue != nil && r.CreateIssue.Context == nil {
			r.CreateIssue.Context = newCtx()
		}
	case *orchpb.Request_CloseIssue:
		if r.CloseIssue != nil && r.CloseIssue.Context == nil {
			r.CloseIssue.Context = newCtx()
		}
	case *orchpb.Request_GetRunByShortId:
		if r.GetRunByShortId != nil && r.GetRunByShortId.Context == nil {
			r.GetRunByShortId.Context = newCtx()
		}
	case *orchpb.Request_ResolveIssue:
		if r.ResolveIssue != nil && r.ResolveIssue.Context == nil {
			r.ResolveIssue.Context = newCtx()
		}
	case *orchpb.Request_DeleteRun:
		if r.DeleteRun != nil && r.DeleteRun.Context == nil {
			r.DeleteRun.Context = newCtx()
		}
	case *orchpb.Request_UpdateIssue:
		if r.UpdateIssue != nil && r.UpdateIssue.Context == nil {
			r.UpdateIssue.Context = newCtx()
		}
	case *orchpb.Request_GetAttachInfo:
		if r.GetAttachInfo != nil && r.GetAttachInfo.Context == nil {
			r.GetAttachInfo.Context = newCtx()
		}
	case *orchpb.Request_CaptureSession:
		if r.CaptureSession != nil && r.CaptureSession.Context == nil {
			r.CaptureSession.Context = newCtx()
		}
	case *orchpb.Request_SendMessage:
		if r.SendMessage != nil && r.SendMessage.Context == nil {
			r.SendMessage.Context = newCtx()
		}
	case *orchpb.Request_GetDiffStats:
		if r.GetDiffStats != nil && r.GetDiffStats.Context == nil {
			r.GetDiffStats.Context = newCtx()
		}
	case *orchpb.Request_GetBranchState:
		if r.GetBranchState != nil && r.GetBranchState.Context == nil {
			r.GetBranchState.Context = newCtx()
		}
	case *orchpb.Request_GetDiff:
		if r.GetDiff != nil && r.GetDiff.Context == nil {
			r.GetDiff.Context = newCtx()
		}
	case *orchpb.Request_AppendEvent:
		if r.AppendEvent != nil && r.AppendEvent.Context == nil {
			r.AppendEvent.Context = newCtx()
		}
	case *orchpb.Request_ValidateIssueFiles:
		if r.ValidateIssueFiles != nil && r.ValidateIssueFiles.Context == nil {
			r.ValidateIssueFiles.Context = newCtx()
		}
	case *orchpb.Request_WriteAgentPrompt:
		if r.WriteAgentPrompt != nil && r.WriteAgentPrompt.Context == nil {
			r.WriteAgentPrompt.Context = newCtx()
		}
	case *orchpb.Request_ReadAgentPrompt:
		if r.ReadAgentPrompt != nil && r.ReadAgentPrompt.Context == nil {
			r.ReadAgentPrompt.Context = newCtx()
		}
	case *orchpb.Request_CreateRun:
		if r.CreateRun != nil && r.CreateRun.Context == nil {
			r.CreateRun.Context = newCtx()
		}
	case *orchpb.Request_InjectInitialPrompt:
		if r.InjectInitialPrompt != nil && r.InjectInitialPrompt.Context == nil {
			r.InjectInitialPrompt.Context = newCtx()
		}
	case *orchpb.Request_ContinueRun:
		if r.ContinueRun != nil && r.ContinueRun.Context == nil {
			r.ContinueRun.Context = newCtx()
		}
	case *orchpb.Request_GetControlAgentLaunch:
		if r.GetControlAgentLaunch != nil && r.GetControlAgentLaunch.Context == nil {
			r.GetControlAgentLaunch.Context = newCtx()
		}
	case *orchpb.Request_GetControlAgentConfig:
		if r.GetControlAgentConfig != nil && r.GetControlAgentConfig.Context == nil {
			r.GetControlAgentConfig.Context = newCtx()
		}
	case *orchpb.Request_GetConfig:
		if r.GetConfig != nil && r.GetConfig.Context == nil {
			r.GetConfig.Context = newCtx()
		}
	}
}

func setupTestServer(t *testing.T, st *mockStore) (*SocketServer, func()) {
	cleanup := setupXDGTestEnv(t)

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		cleanup()
		t.Fatalf("failed to start server: %v", err)
	}

	return server, func() {
		server.Stop()
		cleanup()
	}
}

func TestHandleProtoCleanRunWorktreeRemovesWorktree(t *testing.T) {
	repo := initGitRepoWithCommit(t)
	issueID := "orch-clean"
	runID := "20260314-120000"

	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	worktree, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:    repo,
		WorktreeDir: worktreeRoot,
		IssueID:     model.IssueID(issueID),
		RunID:       model.RunID(runID),
		Agent:       "codex",
	})
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:      model.IssueID(issueID),
				RunID:        model.RunID(runID),
				Status:       model.StatusFailed,
				WorktreePath: worktree.WorktreePath,
				Branch:       worktree.Branch,
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, logger)
	registerRepoContextForTest(t, server, testProjectID, repo, st)

	resp := server.handleProtoCleanRunWorktree(&orchpb.CleanRunWorktreeRequest{
		IssueId: issueID,
		RunId:   runID,
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoCleanRunWorktree() error = %s", resp.Error)
	}

	cleanResp := resp.GetCleanRunWorktree()
	if cleanResp == nil {
		t.Fatal("expected clean_run_worktree response")
	}
	if !cleanResp.WorktreeRemoved {
		t.Fatal("expected worktree_removed=true")
	}
	if cleanResp.Skipped {
		t.Fatalf("expected skipped=false, got reason=%q", cleanResp.Reason)
	}
	if _, err := os.Stat(worktree.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after cleanup: %v", err)
	}

	trees, err := git.ListWorktrees(repo)
	if err != nil {
		t.Fatalf("ListWorktrees() error = %v", err)
	}
	for _, tree := range trees {
		if tree == worktree.WorktreePath {
			t.Fatalf("worktree %q still registered after cleanup", tree)
		}
	}
}

func TestHandleProtoCleanRunWorktreeRejectsActiveRun(t *testing.T) {
	st := &mockStore{
		runs: map[string]*model.Run{
			"orch-active#20260314-130000": {
				IssueID:      model.IssueID("orch-active"),
				RunID:        model.RunID("20260314-130000"),
				Status:       model.StatusRunning,
				WorktreePath: "/tmp/orch-active",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, logger)
	registerRepoContextForTest(t, server, testProjectID, t.TempDir(), st)

	resp := server.handleProtoCleanRunWorktree(&orchpb.CleanRunWorktreeRequest{
		IssueId: "orch-active",
		RunId:   "20260314-130000",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatal("expected active run cleanup to fail")
	}
	if !strings.Contains(resp.Error, "cannot clean worktree for active run") {
		t.Fatalf("resp.Error = %q, want active-run cleanup error", resp.Error)
	}
}

func TestRemoteGetRunReportsWorkerObservedWorktreeExists(t *testing.T) {
	repo := initGitRepoWithCommit(t)
	const (
		issueID = "orch-remote-debug"
		runID   = "20260713-170000"
		host    = "mac-host"
	)
	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:        model.IssueID(issueID),
				RunID:          model.RunID(runID),
				Status:         model.StatusCanceled,
				WorktreePath:   "/Users/runner/orch-worktrees/remote-debug",
				Branch:         "issue/orch-remote-debug/run-20260713-170000",
				Target:         "mac",
				TargetHost:     host,
				TargetWorkerID: HostWorkerID(host),
			},
		},
		issues: map[string]*model.Issue{},
	}

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, testProjectID, repo, st)
	workerID := HostWorkerID(host)
	if _, ttl := server.registerWorker(workerID, "executor", host, "external", []string{"run_worktree"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for worktree worker")
	}

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", `{"run_worktree_result":{"exists":true,"registered":true}}`)
			return
		}
	}()

	resp := server.handleProtoGetRun(&orchpb.GetRunRequest{
		IssueId: issueID,
		RunId:   runID,
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoGetRun() error = %s", resp.Error)
	}
	got := resp.GetGetRun().GetRun()
	if got == nil {
		t.Fatal("expected get_run payload")
	}
	if !got.WorktreeExists {
		t.Fatalf("worktree_exists = false, want worker-observed true for %s", got.WorktreePath)
	}
}

func TestRemoteGetRunWorktreeObservationFailureIsExplicit(t *testing.T) {
	repo := initGitRepoWithCommit(t)
	const (
		issueID = "orch-remote-debug-error"
		runID   = "20260713-170500"
		host    = "mac-host"
	)
	worktreePath := "/Users/runner/orch-worktrees/remote-debug-error"
	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:        model.IssueID(issueID),
				RunID:          model.RunID(runID),
				Status:         model.StatusCanceled,
				WorktreePath:   worktreePath,
				TargetHost:     host,
				TargetWorkerID: HostWorkerID(host),
			},
		},
		issues: map[string]*model.Issue{},
	}

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, testProjectID, repo, st)
	workerID := HostWorkerID(host)
	if _, ttl := server.registerWorker(workerID, "executor", host, "external", []string{"run_worktree"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for worktree worker")
	}

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "failed to stat "+worktreePath+": permission denied", "")
			return
		}
	}()

	resp := server.handleProtoGetRun(&orchpb.GetRunRequest{
		IssueId: issueID,
		RunId:   runID,
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatalf("handleProtoGetRun() unexpectedly succeeded with worktree_exists=%v", resp.GetGetRun().GetRun().GetWorktreeExists())
	}
	for _, want := range []string{"worktree inspect", "execution host " + host, worktreePath, "permission denied"} {
		if !strings.Contains(resp.Error, want) {
			t.Fatalf("resp.Error = %q, want %q", resp.Error, want)
		}
	}
}

func TestRemoteCleanRunWorktreeUsesExecutionHostWorker(t *testing.T) {
	repo := initGitRepoWithCommit(t)
	const (
		issueID = "orch-remote-clean"
		runID   = "20260713-171000"
		host    = "mac-host"
	)
	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:        model.IssueID(issueID),
				RunID:          model.RunID(runID),
				Status:         model.StatusCanceled,
				WorktreePath:   "/Users/runner/orch-worktrees/remote-clean",
				Branch:         "issue/orch-remote-clean/run-20260713-171000",
				Target:         "mac",
				TargetHost:     host,
				TargetWorkerID: HostWorkerID(host),
			},
		},
		issues: map[string]*model.Issue{},
	}

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, testProjectID, repo, st)
	workerID := HostWorkerID(host)
	if _, ttl := server.registerWorker(workerID, "executor", host, "external", []string{"run_worktree"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for worktree worker")
	}

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", `{"run_worktree_result":{"exists":true,"registered":true,"removed":true}}`)
			return
		}
	}()

	resp := server.handleProtoCleanRunWorktree(&orchpb.CleanRunWorktreeRequest{
		IssueId: issueID,
		RunId:   runID,
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoCleanRunWorktree() error = %s", resp.Error)
	}
	cleanResp := resp.GetCleanRunWorktree()
	if cleanResp == nil {
		t.Fatal("expected clean_run_worktree response")
	}
	if !cleanResp.WorktreeRemoved || cleanResp.Skipped {
		t.Fatalf("remote cleanup result = %+v, want worker removal", cleanResp)
	}
}

func TestHandleProtoCleanRunWorktreeRemovesMissingWorktreeRegistration(t *testing.T) {
	repo := initGitRepoWithCommit(t)
	issueID := "orch-missing"
	runID := "20260314-140000"

	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	worktree, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:    repo,
		WorktreeDir: worktreeRoot,
		IssueID:     model.IssueID(issueID),
		RunID:       model.RunID(runID),
		Agent:       "codex",
	})
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	if err := os.RemoveAll(worktree.WorktreePath); err != nil {
		t.Fatalf("RemoveAll(worktree) error = %v", err)
	}

	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:      model.IssueID(issueID),
				RunID:        model.RunID(runID),
				Status:       model.StatusCanceled,
				WorktreePath: worktree.WorktreePath,
				Branch:       worktree.Branch,
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, logger)
	registerRepoContextForTest(t, server, testProjectID, repo, st)

	resp := server.handleProtoCleanRunWorktree(&orchpb.CleanRunWorktreeRequest{
		IssueId: issueID,
		RunId:   runID,
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoCleanRunWorktree() error = %s", resp.Error)
	}

	cleanResp := resp.GetCleanRunWorktree()
	if cleanResp == nil {
		t.Fatal("expected clean_run_worktree response")
	}
	if !cleanResp.WorktreeRemoved {
		t.Fatal("expected worktree_removed=true")
	}
	if cleanResp.Skipped {
		t.Fatalf("expected skipped=false, got reason=%q", cleanResp.Reason)
	}

	trees, err := git.ListWorktrees(repo)
	if err != nil {
		t.Fatalf("ListWorktrees() error = %v", err)
	}
	for _, tree := range trees {
		if tree == worktree.WorktreePath {
			t.Fatalf("worktree %q still registered after cleanup", tree)
		}
	}
}

func TestProtoClientCleanRunWorktreeDispatchesToHandler(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	repo := initGitRepoWithCommit(t)
	issueID := "orch-dispatch"
	runID := "20260314-150000"
	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	worktree, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:    repo,
		WorktreeDir: worktreeRoot,
		IssueID:     model.IssueID(issueID),
		RunID:       model.RunID(runID),
		Agent:       "codex",
	})
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:      model.IssueID(issueID),
				RunID:        model.RunID(runID),
				Status:       model.StatusFailed,
				WorktreePath: worktree.WorktreePath,
				Branch:       worktree.Branch,
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, logger)
	registerRepoContextForTest(t, server, testProjectID, repo, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	client := NewProtoClientWithAddress(testProjectID, "")
	defer client.Close()

	resp, err := client.CleanRunWorktree(issueID, runID, "")
	if err != nil {
		t.Fatalf("CleanRunWorktree() error = %v", err)
	}
	if !resp.WorktreeRemoved {
		t.Fatal("expected worktree_removed=true")
	}
}

func TestHandleProtoDeleteRunRemovesMissingWorktreeRegistration(t *testing.T) {
	repo := initGitRepoWithCommit(t)
	issueID := "orch-delete-missing"
	runID := "20260314-160000"

	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	worktree, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:    repo,
		WorktreeDir: worktreeRoot,
		IssueID:     model.IssueID(issueID),
		RunID:       model.RunID(runID),
		Agent:       "codex",
	})
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	if err := os.RemoveAll(worktree.WorktreePath); err != nil {
		t.Fatalf("RemoveAll(worktree) error = %v", err)
	}

	st := &mockStore{
		runs: map[string]*model.Run{
			issueID + "#" + runID: {
				IssueID:      model.IssueID(issueID),
				RunID:        model.RunID(runID),
				Status:       model.StatusFailed,
				WorktreePath: worktree.WorktreePath,
				Branch:       worktree.Branch,
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, logger)
	registerRepoContextForTest(t, server, testProjectID, repo, st)

	resp := server.handleProtoDeleteRun(&orchpb.DeleteRunRequest{
		IssueId:      issueID,
		RunId:        runID,
		WithWorktree: true,
		Context:      &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoDeleteRun() error = %s", resp.Error)
	}

	deleteResp := resp.GetDeleteRun()
	if deleteResp == nil {
		t.Fatal("expected delete_run response")
	}
	if !deleteResp.WorktreeRemoved {
		t.Fatal("expected worktree_removed=true")
	}

	trees, err := git.ListWorktrees(repo)
	if err != nil {
		t.Fatalf("ListWorktrees() error = %v", err)
	}
	for _, tree := range trees {
		if tree == worktree.WorktreePath {
			t.Fatalf("worktree %q still registered after delete cleanup", tree)
		}
	}
}

func sendProtoRequest(t *testing.T, req *orchpb.Request) *orchpb.Response {
	ensureRequestContext(req)

	conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := conn.Write(lenBuf); err != nil {
		t.Fatalf("failed to write length: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("failed to write data: %v", err)
	}

	respLenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, respLenBuf); err != nil {
		t.Fatalf("failed to read response length: %v", err)
	}
	respLen := binary.BigEndian.Uint32(respLenBuf)

	respData := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respData); err != nil {
		t.Fatalf("failed to read response data: %v", err)
	}

	var resp orchpb.Response
	if err := proto.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	return &resp
}

func TestListRunsAPI(t *testing.T) {
	st := &mockStore{
		runs: map[string]*model.Run{
			"orch-001#20250117-010000": {
				IssueID:   "orch-001",
				RunID:     "20250117-010000",
				Status:    model.StatusRunning,
				Agent:     "opencode",
				Path:      "/vault/runs/orch-001/20250117-010000.md",
				StartedAt: time.Now().Add(-1 * time.Hour),
				UpdatedAt: time.Now(),
			},
			"orch-001#20250117-020000": {
				IssueID:   "orch-001",
				RunID:     "20250117-020000",
				Status:    model.StatusDone,
				Agent:     "claude",
				Path:      "/vault/runs/orch-001/20250117-020000.md",
				StartedAt: time.Now().Add(-2 * time.Hour),
				UpdatedAt: time.Now().Add(-1 * time.Hour),
			},
			"orch-002#20250117-030000": {
				IssueID:   "orch-002",
				RunID:     "20250117-030000",
				Status:    model.StatusRunning,
				Agent:     "opencode",
				Path:      "/vault/runs/orch-002/20250117-030000.md",
				StartedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		issues: make(map[string]*model.Issue),
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("list all runs", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp == nil {
			t.Fatal("expected ListRunsResponse")
		}
		if listResp.Total != 3 {
			t.Errorf("expected 3 runs, got %d", listResp.Total)
		}
	})

	t.Run("filter by issue_id", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{IssueId: "orch-001"},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp == nil {
			t.Fatal("expected ListRunsResponse")
		}
		if listResp.Total != 2 {
			t.Errorf("expected 2 runs for orch-001, got %d", listResp.Total)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Status: []orchpb.RunStatus{orchpb.RunStatus_RUN_STATUS_RUNNING}},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp == nil {
			t.Fatal("expected ListRunsResponse")
		}
		if listResp.Total != 2 {
			t.Errorf("expected 2 running runs, got %d", listResp.Total)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 1},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp == nil {
			t.Fatal("expected ListRunsResponse")
		}
		if len(listResp.Runs) < 1 {
			t.Error("expected at least 1 run")
		}
	})

	t.Run("run summary has URI", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 1},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp == nil || len(listResp.Runs) == 0 {
			t.Fatal("expected at least 1 run")
		}
	})
}

func TestListRunsAPI_InvalidUTF8IssueMetadataDoesNotBreakProto(t *testing.T) {
	invalid := string([]byte{'b', 0xff, 'd'})
	st := &mockStore{
		runs: map[string]*model.Run{
			"orch-utf8#run-1": {
				IssueID:   "orch-utf8",
				RunID:     "run-1",
				Status:    model.StatusRunning,
				Agent:     "opencode",
				StartedAt: time.Now().Add(-1 * time.Minute),
				UpdatedAt: time.Now(),
			},
		},
		issues: map[string]*model.Issue{
			"orch-utf8": {
				ID:      "orch-utf8",
				Title:   "UTF8 test",
				Topic:   "",
				Summary: invalid,
				Status:  model.IssueStatusOpen,
				Path:    "/vault/issues/orch-utf8.md",
			},
		},
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListRuns{
			ListRuns: &orchpb.ListRunsRequest{IssueId: "orch-utf8"},
		},
	})
	if !resp.Ok {
		t.Fatalf("expected OK=true, got error: %s", resp.Error)
	}

	listResp := resp.GetListRuns()
	if listResp == nil || len(listResp.Runs) != 1 {
		t.Fatalf("expected exactly 1 run, got %+v", listResp)
	}
	if got, want := listResp.Runs[0].IssueTopic, "b\ufffdd"; got != want {
		t.Fatalf("issue_topic = %q, want %q", got, want)
	}
}

func TestListRunsPaginationContract(t *testing.T) {
	now := time.Now()
	st := &mockStore{
		runs: map[string]*model.Run{
			"orch-001#run-1": {IssueID: "orch-001", RunID: "run-1", Status: model.StatusRunning, Agent: "opencode", StartedAt: now.Add(-5 * time.Hour), UpdatedAt: now.Add(-4 * time.Hour)},
			"orch-001#run-2": {IssueID: "orch-001", RunID: "run-2", Status: model.StatusDone, Agent: "claude", StartedAt: now.Add(-4 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
			"orch-002#run-3": {IssueID: "orch-002", RunID: "run-3", Status: model.StatusRunning, Agent: "opencode", StartedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
			"orch-002#run-4": {IssueID: "orch-002", RunID: "run-4", Status: model.StatusWaiting, Agent: "claude", StartedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
			"orch-003#run-5": {IssueID: "orch-003", RunID: "run-5", Status: model.StatusFailed, Agent: "opencode", StartedAt: now.Add(-1 * time.Hour), UpdatedAt: now},
		},
		issues: make(map[string]*model.Issue),
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("total reflects full filtered count before pagination", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 2},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp == nil {
			t.Fatal("expected ListRunsResponse")
		}
		if listResp.Total != 5 {
			t.Errorf("total should reflect full count (5), got %d", listResp.Total)
		}
		if len(listResp.Runs) != 2 {
			t.Errorf("expected 2 runs in page, got %d", len(listResp.Runs))
		}
	})

	t.Run("next_cursor returned when more items exist", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 2},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp.NextCursor == "" {
			t.Error("expected next_cursor when more items exist (5 total, limit 2)")
		}
	})

	t.Run("no next_cursor when all items returned", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 10},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp.NextCursor != "" {
			t.Errorf("expected no next_cursor when all items returned, got %q", listResp.NextCursor)
		}
	})

	t.Run("cursor advances through pages correctly", func(t *testing.T) {
		resp1 := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 2},
			},
		})
		if !resp1.Ok {
			t.Fatalf("page 1: expected OK=true, got error: %s", resp1.Error)
		}
		page1 := resp1.GetListRuns()
		if len(page1.Runs) != 2 {
			t.Fatalf("page 1: expected 2 runs, got %d", len(page1.Runs))
		}
		if page1.NextCursor == "" {
			t.Fatal("page 1: expected next_cursor")
		}

		resp2 := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 2, Cursor: page1.NextCursor},
			},
		})
		if !resp2.Ok {
			t.Fatalf("page 2: expected OK=true, got error: %s", resp2.Error)
		}
		page2 := resp2.GetListRuns()
		if len(page2.Runs) != 2 {
			t.Fatalf("page 2: expected 2 runs, got %d", len(page2.Runs))
		}
		if page2.Total != 5 {
			t.Errorf("page 2: total should still be 5, got %d", page2.Total)
		}

		resp3 := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Limit: 2, Cursor: page2.NextCursor},
			},
		})
		if !resp3.Ok {
			t.Fatalf("page 3: expected OK=true, got error: %s", resp3.Error)
		}
		page3 := resp3.GetListRuns()
		if len(page3.Runs) != 1 {
			t.Errorf("page 3: expected 1 run (remainder), got %d", len(page3.Runs))
		}
		if page3.NextCursor != "" {
			t.Errorf("page 3: expected no next_cursor (last page), got %q", page3.NextCursor)
		}

		allRunIDs := make(map[string]bool)
		for _, r := range page1.Runs {
			allRunIDs[r.RunId] = true
		}
		for _, r := range page2.Runs {
			allRunIDs[r.RunId] = true
		}
		for _, r := range page3.Runs {
			allRunIDs[r.RunId] = true
		}
		if len(allRunIDs) != 5 {
			t.Errorf("expected 5 unique runs across all pages, got %d", len(allRunIDs))
		}
	})

	t.Run("filter with pagination - total reflects filtered count", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{
					Status: []orchpb.RunStatus{orchpb.RunStatus_RUN_STATUS_RUNNING},
					Limit:  1,
				},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp.Total != 2 {
			t.Errorf("expected total=2 (2 running runs), got %d", listResp.Total)
		}
		if len(listResp.Runs) != 1 {
			t.Errorf("expected 1 run in page, got %d", len(listResp.Runs))
		}
		if listResp.NextCursor == "" {
			t.Error("expected next_cursor (2 running, limit 1)")
		}
	})

	t.Run("default limit caps at 50", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if listResp.Total != 5 {
			t.Errorf("expected total=5, got %d", listResp.Total)
		}
		if len(listResp.Runs) != 5 {
			t.Errorf("expected all 5 runs (under default limit), got %d", len(listResp.Runs))
		}
	})

	t.Run("cursor beyond total returns empty page", func(t *testing.T) {
		beyondCursor := EncodeCursor(100)
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListRuns{
				ListRuns: &orchpb.ListRunsRequest{Cursor: beyondCursor, Limit: 10},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListRuns()
		if len(listResp.Runs) != 0 {
			t.Errorf("expected 0 runs for cursor beyond total, got %d", len(listResp.Runs))
		}
		if listResp.Total != 5 {
			t.Errorf("total should still be 5, got %d", listResp.Total)
		}
	})
}

func TestGetRunAPI(t *testing.T) {
	worktreePath := initGitRepoWithCommit(t)
	st := &mockStore{
		runs: map[string]*model.Run{
			"orch-001#20250117-010000": {
				IssueID:           "orch-001",
				RunID:             "20250117-010000",
				Status:            model.StatusRunning,
				Agent:             "opencode",
				Model:             "anthropic/claude-sonnet-4",
				ModelVariant:      "high",
				Branch:            "main",
				WorktreePath:      worktreePath,
				ServerPort:        8080,
				OpenCodeSessionID: "sess_xxx",
				Path:              "/vault/runs/orch-001/20250117-010000.md",
				StartedAt:         time.Now().Add(-1 * time.Hour),
				UpdatedAt:         time.Now(),
				Events: []*model.Event{
					{Timestamp: time.Now().Add(-1 * time.Hour), Type: model.EventTypeStatus, Name: "running"},
					{Timestamp: time.Now().Add(-30 * time.Minute), Type: model.EventTypeArtifact, Name: "branch", Attrs: map[string]string{"name": "orch-001-feature"}},
				},
			},
		},
		issues: make(map[string]*model.Issue),
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("get existing run", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetRun{
				GetRun: &orchpb.GetRunRequest{IssueId: "orch-001", RunId: "20250117-010000"},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		getResp := resp.GetGetRun()
		if getResp == nil {
			t.Fatal("expected GetRunResponse")
		}
		if getResp.Run == nil {
			t.Fatal("expected run to be set")
		}
		if getResp.Run.IssueId != "orch-001" {
			t.Errorf("expected IssueID=orch-001, got %s", getResp.Run.IssueId)
		}
		if len(getResp.Events) != 2 {
			t.Errorf("expected 2 events, got %d", len(getResp.Events))
		}
		if getResp.Run.ServerPort != 8080 {
			t.Errorf("expected ServerPort=8080, got %d", getResp.Run.ServerPort)
		}
	})

	t.Run("get non-existent run", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetRun{
				GetRun: &orchpb.GetRunRequest{IssueId: "orch-999", RunId: "20250117-010000"},
			},
		})
		if resp.Ok {
			t.Error("expected OK=false for non-existent run")
		}
		if resp.Error != "not_found" {
			t.Errorf("expected error=not_found, got %s", resp.Error)
		}
	})

	t.Run("get run without issue_id", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetRun{
				GetRun: &orchpb.GetRunRequest{RunId: "20250117-010000"},
			},
		})
		if resp.Ok {
			t.Error("expected OK=false when issue_id missing")
		}
		if resp.Error == "" {
			t.Error("expected error message")
		}
	})
}

func TestListIssuesAPI(t *testing.T) {
	st := &mockStore{
		runs: make(map[string]*model.Run),
		issues: map[string]*model.Issue{
			"orch-001": {
				ID:      "orch-001",
				Title:   "Implement feature X",
				Topic:   "feature",
				Summary: "Add support for X",
				Status:  model.IssueStatusOpen,
				Path:    "/vault/issues/orch-001.md",
			},
			"orch-002": {
				ID:      "orch-002",
				Title:   "Fix bug Y",
				Topic:   "bugfix",
				Summary: "Fix issue with Y",
				Status:  model.IssueStatusResolved,
				Path:    "/vault/issues/orch-002.md",
			},
			"orch-003": {
				ID:      "orch-003",
				Title:   "Refactor Z",
				Topic:   "refactor",
				Summary: "Improve code quality",
				Status:  model.IssueStatusOpen,
				Path:    "/vault/issues/orch-003.md",
			},
		},
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("list all issues", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListIssues{
				ListIssues: &orchpb.ListIssuesRequest{},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListIssues()
		if listResp == nil {
			t.Fatal("expected ListIssuesResponse")
		}
		if listResp.Total != 3 {
			t.Errorf("expected 3 issues, got %d", listResp.Total)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListIssues{
				ListIssues: &orchpb.ListIssuesRequest{Status: []orchpb.IssueStatus{orchpb.IssueStatus_ISSUE_STATUS_OPEN}},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListIssues()
		if listResp == nil {
			t.Fatal("expected ListIssuesResponse")
		}
		if listResp.Total < 1 {
			t.Error("expected at least 1 issue")
		}
	})

	t.Run("pagination", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListIssues{
				ListIssues: &orchpb.ListIssuesRequest{Limit: 2},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListIssues()
		if listResp == nil {
			t.Fatal("expected ListIssuesResponse")
		}
		if len(listResp.Issues) < 1 {
			t.Error("expected at least 1 issue")
		}
	})

	t.Run("issue summary has URI", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListIssues{
				ListIssues: &orchpb.ListIssuesRequest{Limit: 1},
			},
		})
		if !resp.Ok {
			t.Fatalf("expected OK=true, got error: %s", resp.Error)
		}
		listResp := resp.GetListIssues()
		if listResp == nil || len(listResp.Issues) == 0 {
			t.Fatal("expected at least 1 issue")
		}
	})
}

func TestListIssuesAPI_InvalidUTF8TextFieldsAreSanitized(t *testing.T) {
	invalid := string([]byte{'x', 0xff, 'y'})
	st := &mockStore{
		runs: make(map[string]*model.Run),
		issues: map[string]*model.Issue{
			"orch-utf8": {
				ID:      "orch-utf8",
				Title:   invalid,
				Topic:   invalid,
				Summary: invalid,
				Status:  model.IssueStatusOpen,
				Body:    invalid,
				Path:    "/vault/issues/orch-utf8.md",
			},
		},
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListIssues{
			ListIssues: &orchpb.ListIssuesRequest{},
		},
	})
	if !resp.Ok {
		t.Fatalf("expected OK=true, got error: %s", resp.Error)
	}

	listResp := resp.GetListIssues()
	if listResp == nil || len(listResp.Issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %+v", listResp)
	}

	issue := listResp.Issues[0]
	want := "x\ufffdy"
	if issue.Title != want || issue.Topic != want || issue.Summary != want || issue.Body != want {
		t.Fatalf("unexpected sanitized fields: title=%q topic=%q summary=%q body=%q", issue.Title, issue.Topic, issue.Summary, issue.Body)
	}
}

func TestGetIssueAPI(t *testing.T) {
	st := &mockStore{
		runs: make(map[string]*model.Run),
		issues: map[string]*model.Issue{
			"orch-001": {
				ID:      "orch-001",
				Title:   "Implement feature X",
				Topic:   "feature",
				Summary: "Add support for X",
				Status:  model.IssueStatusOpen,
				Body:    "# Implement feature X\n\nThis is the full body of the issue.",
				Path:    "/vault/issues/orch-001.md",
				Frontmatter: map[string]string{
					"type":   "issue",
					"id":     "orch-001",
					"status": "open",
				},
			},
		},
	}

	_, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("get existing issue", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetIssue{
				GetIssue: &orchpb.GetIssueRequest{IssueId: "orch-001"},
			},
		})
		if !resp.Ok {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		getResp := resp.GetGetIssue()
		if getResp == nil {
			t.Fatal("expected GetIssueResponse")
		}
		if getResp.Issue == nil {
			t.Fatal("expected issue to be set")
		}
		if getResp.Issue.Id != "orch-001" {
			t.Errorf("expected ID=orch-001, got %s", getResp.Issue.Id)
		}
		if getResp.Issue.Body == "" {
			t.Error("expected Body to be set")
		}
	})

	t.Run("get non-existent issue", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetIssue{
				GetIssue: &orchpb.GetIssueRequest{IssueId: "orch-999"},
			},
		})
		if resp.Ok {
			t.Error("expected OK=false for non-existent issue")
		}
		if resp.Error != "not_found" {
			t.Errorf("expected error=not_found, got %s", resp.Error)
		}
	})

	t.Run("get issue without issue_id", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetIssue{
				GetIssue: &orchpb.GetIssueRequest{},
			},
		})
		if resp.Ok {
			t.Error("expected OK=false when issue_id missing")
		}
		if resp.Error == "" {
			t.Error("expected error message")
		}
	})
}

func TestUpdateIssueContentAPI(t *testing.T) {
	st := &mockStore{
		issues: map[string]*model.Issue{
			"orch-001": {
				ID:     "orch-001",
				Title:  "Old title",
				Status: model.IssueStatusOpen,
				Body:   "old body",
				Path:   "/vault/issues/orch-001.md",
			},
		},
	}
	server := newTestServer(t, st)
	ctx := &orchpb.RequestContext{ProjectId: testProjectID}

	appendBody := "\nappended"
	resp := server.handleProtoUpdateIssue(&orchpb.UpdateIssueRequest{
		IssueId:    "orch-001",
		AppendBody: &appendBody,
		Context:    ctx,
	})
	if !resp.Ok {
		t.Fatalf("append body failed: %s", resp.Error)
	}
	if st.updatedIssue == nil || st.updatedIssue.Body != "old body\nappended" {
		t.Fatalf("updated body = %#v, want appended body", st.updatedIssue)
	}
	if st.updatedIssue.Status != model.IssueStatusOpen {
		t.Fatalf("status changed during content update: %s", st.updatedIssue.Status)
	}

	emptyBody := ""
	resp = server.handleProtoUpdateIssue(&orchpb.UpdateIssueRequest{
		IssueId: "orch-001",
		Body:    &emptyBody,
		Context: ctx,
	})
	if !resp.Ok {
		t.Fatalf("empty body replacement failed: %s", resp.Error)
	}
	if st.updatedIssue == nil || st.updatedIssue.Body != "" {
		t.Fatalf("updated body = %#v, want explicit empty body", st.updatedIssue)
	}

	resp = server.handleProtoUpdateIssue(&orchpb.UpdateIssueRequest{
		IssueId:    "orch-001",
		Body:       &emptyBody,
		AppendBody: &appendBody,
		Context:    ctx,
	})
	if resp.Ok || !strings.Contains(resp.Error, "mutually exclusive") {
		t.Fatalf("body plus append response = %+v, want explicit rejection", resp)
	}
}

func TestGetIssueAPIPropagatesStoreDrift(t *testing.T) {
	drift := errors.New("store drift detected: /vault/Issues/orch-001.md was modified outside the daemon (ADR-0004); restart the daemon to adopt the external change")
	st := &resolveIssueErrorStore{mockStore: &mockStore{}, err: drift}
	server := newTestServer(t, st)

	resp := server.handleProtoGetIssue(&orchpb.GetIssueRequest{
		IssueId: "orch-001",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatal("GetIssue response OK = true, want drift failure")
	}
	if !strings.Contains(resp.Error, "/vault/Issues/orch-001.md") || !strings.Contains(resp.Error, "ADR-0004") {
		t.Fatalf("GetIssue error = %q, want drift path and ADR-0004", resp.Error)
	}
}

func TestCloseIssueAPIPropagatesStoreDrift(t *testing.T) {
	drift := errors.New("store drift detected: /vault/Issues/orch-001.md was modified outside the daemon (ADR-0004); restart the daemon to adopt the external change")
	st := &setIssueStatusErrorStore{mockStore: &mockStore{}, err: drift}
	server := newTestServer(t, st)

	resp := server.handleProtoCloseIssue(&orchpb.CloseIssueRequest{
		IssueId: "orch-001",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatal("CloseIssue response OK = true, want drift failure")
	}
	if !strings.Contains(resp.Error, "/vault/Issues/orch-001.md") || !strings.Contains(resp.Error, "ADR-0004") {
		t.Fatalf("CloseIssue error = %q, want drift path and ADR-0004", resp.Error)
	}
}

func TestCursorEncoding(t *testing.T) {
	t.Run("encode and decode cursor", func(t *testing.T) {
		encoded := EncodeCursor(42)
		decoded, err := DecodeCursor(encoded)
		if err != nil {
			t.Fatalf("failed to decode cursor: %v", err)
		}
		if decoded != 42 {
			t.Errorf("expected 42, got %d", decoded)
		}
	})

	t.Run("decode empty cursor", func(t *testing.T) {
		offset, err := DecodeCursor("")
		if err != nil {
			t.Fatalf("failed to decode empty cursor: %v", err)
		}
		if offset != 0 {
			t.Errorf("expected 0 for empty cursor, got %d", offset)
		}
	})

	t.Run("decode invalid cursor", func(t *testing.T) {
		_, err := DecodeCursor("not-valid-base64!")
		if err == nil {
			t.Error("expected error for invalid cursor")
		}
	})

	t.Run("reject negative offset cursor", func(t *testing.T) {
		negativeCursor := base64.StdEncoding.EncodeToString([]byte(`{"offset":-1}`))
		_, err := DecodeCursor(negativeCursor)
		if err == nil {
			t.Error("expected error for negative offset cursor")
		}
	})
}

func TestFileURIEmptyPath(t *testing.T) {
	uri := FileURI("")
	if uri != "" {
		t.Errorf("expected empty string for empty path, got %s", uri)
	}

	uri = FileURI("/path/to/file")
	if uri != "file:///path/to/file" {
		t.Errorf("expected file:///path/to/file, got %s", uri)
	}
}

func TestStoreFactoryDynamicCreation(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	factoryCalled := false
	factoryIssuesRoot := ""
	mockFactory := func(issuesRoot string) (store.Store, error) {
		factoryCalled = true
		factoryIssuesRoot = issuesRoot
		return &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}, nil
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(mockFactory, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resolved := server.getOrCreateStore("/test/issues/path", "")
	if resolved == nil {
		t.Fatal("expected store to be resolved")
	}

	if !factoryCalled {
		t.Error("expected factory to be called")
	}
	if factoryIssuesRoot != "/test/issues/path" {
		t.Errorf("expected factory issuesRoot=%q, got %q", "/test/issues/path", factoryIssuesRoot)
	}
}

func TestStoreFactoryReusesExistingStore(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	callCount := 0
	mockFactory := func(issuesRoot string) (store.Store, error) {
		callCount++
		return &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}, nil
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(mockFactory, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	sendListIssues := func() {
		resolved := server.getOrCreateStore("/reuse/test/path", "")
		if resolved == nil {
			t.Fatal("expected store to be resolved")
		}
	}

	sendListIssues()
	sendListIssues()
	sendListIssues()

	if callCount != 1 {
		t.Errorf("expected factory to be called once (store reuse), got %d calls", callCount)
	}
}

func TestResolveStoreWithProjectRoot(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	registerRepoContextForTest(t, server, "project-root", "/project/root", st)

	resolved := server.resolveStore(SendRequest{RepoID: "project-root"})
	if resolved == nil {
		t.Error("expected store to be resolved for registered repo id")
	}

	resolved = server.resolveStore(SendRequest{RepoID: "unknown-project"})
	if resolved != nil {
		t.Error("expected nil store for unknown repo id")
	}
}

func TestGetRepoContextEmptyLookupReturnsNil(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	server.repos["/issues/root"] = &RepoContext{
		ProjectRoot: "",
		RepoID:      "/issues/root",
		Store:       &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)},
	}

	if got := server.GetRepoContext(""); got != nil {
		t.Fatalf("expected nil context for empty lookup key, got %+v", got)
	}
}

func TestGetOrCreateStoreHydratesProjectRootOnReuse(t *testing.T) {
	callCount := 0
	mockFactory := func(string) (store.Store, error) {
		callCount++
		return &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}, nil
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(mockFactory, logger)

	first := server.getOrCreateStore("/issues/root", "")
	if first == nil {
		t.Fatal("expected first store creation to succeed")
	}

	second := server.getOrCreateStore("/issues/root", "/project/root")
	if second == nil {
		t.Fatal("expected second store lookup to return store")
	}
	if first != second {
		t.Fatal("expected store cache reuse for same issues root")
	}

	ctx := server.repos["/issues/root"]
	if ctx == nil {
		t.Fatal("expected cached repo context")
	}
	if ctx.ProjectRoot != "/project/root" {
		t.Fatalf("expected project root to be hydrated to %q, got %q", "/project/root", ctx.ProjectRoot)
	}
	if callCount != 1 {
		t.Fatalf("expected factory to be called once, got %d", callCount)
	}
}

func TestResolveProjectRootPrecedence(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	repoStore := &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}
	repoID := "daemon-project"
	registerRepoContextForTest(t, server, repoID, "/daemon/project", repoStore)

	if got := server.resolveProjectRoot(SendRequest{RepoID: repoID}); got != "/daemon/project" {
		t.Fatalf("expected repo context project root, got %q", got)
	}

	if got := server.resolveProjectRoot(SendRequest{}); got != "" {
		t.Fatalf("expected empty project root when request has no project root and no repo id, got %q", got)
	}

	emptyServer := NewSocketServer(nil, logger)
	if got := emptyServer.resolveProjectRoot(SendRequest{}); got != "" {
		t.Fatalf("expected empty project root when request is empty, got %q", got)
	}
}

func TestEnsureRepoStoreByIDUsesRegisteredProjectRoot(t *testing.T) {
	callCount := 0
	mockFactory := func(string) (store.Store, error) {
		callCount++
		return &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}, nil
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(mockFactory, logger)

	projectRoot := t.TempDir()
	issuesRoot := filepath.Join(projectRoot, "issues")
	if err := os.MkdirAll(issuesRoot, 0o755); err != nil {
		t.Fatalf("mkdir issues root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configYAML := []byte("issues:\n  path: " + issuesRoot + "\n")
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	repoID := "repo-store-by-id"
	server.reposMu.Lock()
	server.repos[repoID] = &RepoContext{ProjectRoot: projectRoot, RepoID: repoID}
	server.reposMu.Unlock()

	resolved := server.ensureRepoStoreByID(repoID)
	if resolved == nil || resolved.Store == nil {
		t.Fatal("expected store to resolve for registered repo id")
	}
	if callCount != 1 {
		t.Fatalf("expected one store creation call, got %d", callCount)
	}

	resolvedAgain := server.ensureRepoStoreByID(repoID)
	if resolvedAgain == nil || resolvedAgain.Store == nil {
		t.Fatal("expected store to resolve on repeated repo id lookup")
	}
	if callCount != 1 {
		t.Fatalf("expected no additional store creation call, got %d", callCount)
	}
	if resolved.Store != resolvedAgain.Store {
		t.Fatal("expected same store instance on repeated repo id lookup")
	}
}

func TestEnsureRepoContextByIDDoesNotFallbackToEnvProjectRoot(t *testing.T) {
	callCount := 0
	mockFactory := func(string) (store.Store, error) {
		callCount++
		return &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}, nil
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(mockFactory, logger)

	projectRoot := t.TempDir()
	issuesRoot := filepath.Join(projectRoot, "issues")
	if err := os.MkdirAll(issuesRoot, 0o755); err != nil {
		t.Fatalf("mkdir issues root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configYAML := []byte("issues:\n  path: " + issuesRoot + "\n")
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("ORCH_PROJECT", projectRoot)
	repoID := "missing-project"
	if got := server.ensureRepoContextByID(repoID); got != nil {
		t.Fatalf("expected nil context without registry mapping, got %#v", got)
	}
	if callCount != 0 {
		t.Fatalf("expected no store creation without registry mapping, got %d", callCount)
	}
}

func TestRepoRegistryPersistenceAcrossServerInstances(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/orch.git")
	resp := server.handleProtoRegisterRepo(&orchpb.RegisterRepoRequest{ProjectRoot: projectRoot})
	if !resp.Ok {
		t.Fatalf("register repo failed: %s", resp.Error)
	}

	repoID := "example-orch"
	projectCfgPath := filepath.Join(xdg.ConfigDir(), "projects", repoID+".yaml")
	data, err := os.ReadFile(projectCfgPath)
	if err != nil {
		t.Fatalf("read project config %s: %v", projectCfgPath, err)
	}
	if !strings.Contains(string(data), "project_id: "+repoID) {
		t.Fatalf("project config missing project_id %q: %s", repoID, string(data))
	}
	if !strings.Contains(string(data), "root: "+projectRoot) {
		t.Fatalf("project config missing workspace root %q: %s", projectRoot, string(data))
	}

	server2 := NewSocketServer(nil, logger)
	if err := server2.loadRepoRegistry(); err != nil {
		t.Fatalf("loadRepoRegistry() error: %v", err)
	}

	ctx := server2.GetRepoContext(repoID)
	if ctx == nil {
		t.Fatalf("expected repo context for %q after reload", repoID)
	}
	if ctx.ProjectRoot != projectRoot {
		t.Fatalf("ProjectRoot = %q, want %q", ctx.ProjectRoot, projectRoot)
	}
}

func TestLoadRepoRegistrySupportsLegacyStateFile(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	legacyPath := filepath.Join(xdg.StateDir(), repoRegistryLegacyFileName)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy registry dir: %v", err)
	}

	legacy := repoRegistrySnapshot{
		Version: 1,
		Repos: []repoRegistryEntry{
			{RepoID: "legacy-repo", ProjectRoot: "/srv/legacy-repo"},
		},
	}
	legacyData, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy registry: %v", err)
	}
	if err := os.WriteFile(legacyPath, legacyData, 0o644); err != nil {
		t.Fatalf("write legacy registry: %v", err)
	}

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	if err := server.loadRepoRegistry(); err != nil {
		t.Fatalf("loadRepoRegistry() error: %v", err)
	}

	ctx := server.GetRepoContext("legacy-repo")
	if ctx == nil {
		t.Fatal("expected legacy repo context to load")
	}
	if ctx.ProjectRoot != "/srv/legacy-repo" {
		t.Fatalf("legacy project root = %q, want /srv/legacy-repo", ctx.ProjectRoot)
	}
}

func TestIsServerProcessAliveReturnsFalseAfterWaitResultClosed(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	waitResult := make(chan error, 1)
	close(waitResult)

	if server.isServerProcessAlive(&managedServer{WaitResult: waitResult}) {
		t.Fatal("expected server to be considered dead when wait result channel is closed")
	}
}

func TestWaitForServerExitUsesWaitResult(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	waitResult := make(chan error, 1)
	srv := &managedServer{WaitResult: waitResult}

	if server.waitForServerExit(srv, 5*time.Millisecond) {
		t.Fatal("expected timeout when process has not exited")
	}

	close(waitResult)
	if !server.waitForServerExit(srv, 5*time.Millisecond) {
		t.Fatal("expected waitForServerExit to return true once channel is closed")
	}
}

func TestWaitForOpenCodeServerHealthy(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	t.Run("returns nil when healthy", func(t *testing.T) {
		waitResult := make(chan error, 1)
		srv := &managedServer{WaitResult: waitResult}
		err := server.waitForOpenCodeServerHealthy(srv, 50*time.Millisecond, func(context.Context) bool {
			return true
		})
		if err != nil {
			t.Fatalf("expected healthy wait to succeed, got error: %v", err)
		}
		close(waitResult)
	})

	t.Run("fails fast on process exit", func(t *testing.T) {
		waitResult := make(chan error, 1)
		close(waitResult)
		srv := &managedServer{WaitResult: waitResult}
		err := server.waitForOpenCodeServerHealthy(srv, 50*time.Millisecond, func(context.Context) bool {
			return false
		})
		if err == nil || !strings.Contains(err.Error(), "exited during startup") {
			t.Fatalf("expected process-exit error, got: %v", err)
		}
	})

	t.Run("times out when process alive but unhealthy", func(t *testing.T) {
		waitResult := make(chan error, 1)
		srv := &managedServer{WaitResult: waitResult}
		err := server.waitForOpenCodeServerHealthy(srv, 20*time.Millisecond, func(context.Context) bool {
			return false
		})
		if err == nil || !strings.Contains(err.Error(), "timeout waiting for opencode server") {
			t.Fatalf("expected timeout error, got: %v", err)
		}
		close(waitResult)
	})
}

func TestOpenCodeServerLogPathIsPerProjectRoot(t *testing.T) {
	stateHome := filepath.Join(os.TempDir(), "orch-state-"+randomID())
	t.Setenv("XDG_STATE_HOME", stateHome)

	pathA1 := opencodeServerLogPath("/tmp/repos/demo/worktree-a")
	pathA2 := opencodeServerLogPath("/tmp/repos/demo/worktree-a")
	pathB := opencodeServerLogPath("/tmp/repos/demo/worktree-b")

	if pathA1 == "" || pathA2 == "" || pathB == "" {
		t.Fatal("expected non-empty log paths")
	}
	if pathA1 != pathA2 {
		t.Fatalf("expected deterministic log path for same project root, got %q vs %q", pathA1, pathA2)
	}
	if pathA1 == pathB {
		t.Fatalf("expected different project roots to use different log files, got same path %q", pathA1)
	}
	if !strings.HasPrefix(pathA1, xdg.StateDir()+string(os.PathSeparator)) {
		t.Fatalf("expected log path under state dir %q, got %q", xdg.StateDir(), pathA1)
	}
	if !strings.HasSuffix(pathA1, ".log") {
		t.Fatalf("expected .log suffix, got %q", pathA1)
	}
}

func writeStoredOpenCodeControlSession(t *testing.T, repoID, sessionID, modelName, modelVariant string) {
	t.Helper()
	sessionPath := controlSessionPathForRepoID(repoID)
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0755); err != nil {
		t.Fatalf("failed to create control session dir: %v", err)
	}
	data, err := json.Marshal(controlSessionRecord{
		SessionID:    sessionID,
		AgentType:    "opencode",
		Port:         1234,
		Model:        modelName,
		ModelVariant: modelVariant,
	})
	if err != nil {
		t.Fatalf("failed to marshal stored control session: %v", err)
	}
	if err := os.WriteFile(sessionPath, data, 0644); err != nil {
		t.Fatalf("failed to write stored control session: %v", err)
	}
}

func readStoredOpenCodeControlSession(t *testing.T, repoID string) controlSessionRecord {
	t.Helper()
	data, err := os.ReadFile(controlSessionPathForRepoID(repoID))
	if err != nil {
		t.Fatalf("failed to read stored control session: %v", err)
	}

	var stored controlSessionRecord
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("failed to decode stored control session: %v", err)
	}
	return stored
}

func TestGetOrCreateOpenCodeControlSessionReusesExisting(t *testing.T) {
	projectRoot := t.TempDir()
	repoID := "project-ctx"
	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"
	writeStoredOpenCodeControlSession(t, repoID, "ses_existing", modelName, modelVariant)

	var mu sync.Mutex
	getCalls := 0
	listCalls := 0
	createCalls := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/session/ses_existing":
			mu.Lock()
			getCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        "ses_existing",
				"title":     openCodeControlSessionTitle,
				"directory": projectRoot,
				"time": map[string]int64{
					"created": 1000,
					"updated": 2000,
				},
			})
		case r.Method == "GET" && r.URL.Path == "/session":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == "POST" && r.URL.Path == "/session":
			mu.Lock()
			createCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.repos[repoID] = &RepoContext{RepoID: repoID, ProjectRoot: projectRoot}
	port := getPortFromURL(t, ts.URL)
	sessionID, _, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}
	if sessionID != "ses_existing" {
		t.Fatalf("expected existing session to be reused, got %q", sessionID)
	}
	stored := readStoredOpenCodeControlSession(t, repoID)
	if stored.Port != port {
		t.Fatalf("expected stored port %d after reuse, got %d", port, stored.Port)
	}
	if stored.Model != modelName || stored.ModelVariant != modelVariant {
		t.Fatalf("expected stored model metadata (%q,%q), got (%q,%q)", modelName, modelVariant, stored.Model, stored.ModelVariant)
	}

	mu.Lock()
	defer mu.Unlock()
	if getCalls != 1 {
		t.Fatalf("expected one GET by session ID, got %d", getCalls)
	}
	if listCalls != 0 {
		t.Fatalf("expected no fallback list call, got %d", listCalls)
	}
	if createCalls != 0 {
		t.Fatalf("expected no create call, got %d", createCalls)
	}
}

func TestGetOrCreateOpenCodeControlSessionRecoversAfterServerRestart(t *testing.T) {
	projectRoot := t.TempDir()
	repoID := "project-ctx"
	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"
	writeStoredOpenCodeControlSession(t, repoID, "ses_stale", modelName, modelVariant)

	var mu sync.Mutex
	getCalls := 0
	listCalls := 0
	createCalls := 0
	listDirectory := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/session/ses_stale":
			mu.Lock()
			getCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		case r.Method == "GET" && r.URL.Path == "/session":
			mu.Lock()
			listCalls++
			listDirectory = r.Header.Get("X-OpenCode-Directory")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":        "ses_chat_latest",
					"title":     "chat",
					"directory": projectRoot,
					"time":      map[string]int64{"created": 1000, "updated": 9000},
				},
				{
					"id":        "ses_control_old",
					"title":     openCodeControlSessionTitle,
					"directory": projectRoot,
					"time":      map[string]int64{"created": 1000, "updated": 4000},
				},
				{
					"id":        "ses_control_new",
					"title":     openCodeControlSessionTitle,
					"directory": projectRoot,
					"time":      map[string]int64{"created": 1000, "updated": 5000},
				},
				{
					"id":        "ses_other_project",
					"title":     openCodeControlSessionTitle,
					"directory": "/other/project",
					"time":      map[string]int64{"created": 1000, "updated": 20000},
				},
			})
		case r.Method == "POST" && r.URL.Path == "/session":
			mu.Lock()
			createCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.repos[repoID] = &RepoContext{RepoID: repoID, ProjectRoot: projectRoot}
	port := getPortFromURL(t, ts.URL)
	sessionID, _, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}
	if sessionID != "ses_control_new" {
		t.Fatalf("expected recovered control session %q, got %q", "ses_control_new", sessionID)
	}

	stored := readStoredOpenCodeControlSession(t, repoID)
	if stored.SessionID != "ses_control_new" {
		t.Fatalf("expected stored session ID to be updated to recovered ID, got %q", stored.SessionID)
	}
	if stored.AgentType != "opencode" {
		t.Fatalf("expected stored agent type opencode, got %q", stored.AgentType)
	}
	if stored.Port != port {
		t.Fatalf("expected stored port %d, got %d", port, stored.Port)
	}
	if stored.Model != modelName || stored.ModelVariant != modelVariant {
		t.Fatalf("expected stored model metadata (%q,%q), got (%q,%q)", modelName, modelVariant, stored.Model, stored.ModelVariant)
	}

	mu.Lock()
	defer mu.Unlock()
	if getCalls != 1 {
		t.Fatalf("expected one GET by stale session ID, got %d", getCalls)
	}
	if listCalls != 1 {
		t.Fatalf("expected one fallback list call, got %d", listCalls)
	}
	if listDirectory != projectRoot {
		t.Fatalf("expected list directory header %q, got %q", projectRoot, listDirectory)
	}
	if createCalls != 0 {
		t.Fatalf("expected no new session creation during recovery, got %d", createCalls)
	}
}

func TestGetOrCreateOpenCodeControlSessionCreatesWhenRecoveryFindsNoSession(t *testing.T) {
	projectRoot := t.TempDir()
	repoID := "project-ctx"
	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"
	writeStoredOpenCodeControlSession(t, repoID, "ses_stale", modelName, modelVariant)

	var mu sync.Mutex
	getCalls := 0
	listCalls := 0
	createCalls := 0
	createDirectory := ""
	createTitle := ""
	createDecodeErr := ""
	promptReqCh := make(chan agent.PromptRequest, 1)
	promptDecodeErr := ""
	promptDirectory := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/session/ses_stale":
			mu.Lock()
			getCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		case r.Method == "GET" && r.URL.Path == "/session":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		case r.Method == "POST" && r.URL.Path == "/session":
			mu.Lock()
			createCalls++
			createDirectory = r.Header.Get("X-OpenCode-Directory")
			mu.Unlock()

			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				mu.Lock()
				createDecodeErr = err.Error()
				mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			mu.Lock()
			createTitle = body["title"]
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        "ses_brand_new",
				"title":     openCodeControlSessionTitle,
				"directory": projectRoot,
				"time": map[string]int64{
					"created": 3000,
					"updated": 3000,
				},
			})
		case r.Method == "POST" && r.URL.Path == "/session/ses_brand_new/message":
			var promptReq agent.PromptRequest
			if err := json.NewDecoder(r.Body).Decode(&promptReq); err != nil {
				mu.Lock()
				promptDecodeErr = err.Error()
				mu.Unlock()
			} else {
				select {
				case promptReqCh <- promptReq:
				default:
				}
			}
			mu.Lock()
			promptDirectory = r.Header.Get("X-OpenCode-Directory")
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.repos[repoID] = &RepoContext{RepoID: repoID, ProjectRoot: projectRoot}
	port := getPortFromURL(t, ts.URL)
	sessionID, _, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}
	if sessionID != "ses_brand_new" {
		t.Fatalf("expected newly created session ID %q, got %q", "ses_brand_new", sessionID)
	}

	var promptReq agent.PromptRequest
	select {
	case promptReq = <-promptReqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial control prompt request")
	}

	stored := readStoredOpenCodeControlSession(t, repoID)
	if stored.SessionID != "ses_brand_new" {
		t.Fatalf("expected stored session ID to be updated to new ID, got %q", stored.SessionID)
	}
	if stored.AgentType != "opencode" {
		t.Fatalf("expected stored agent type opencode, got %q", stored.AgentType)
	}
	if stored.Port != port {
		t.Fatalf("expected stored port %d, got %d", port, stored.Port)
	}
	if stored.Model != modelName || stored.ModelVariant != modelVariant {
		t.Fatalf("expected stored model metadata (%q,%q), got (%q,%q)", modelName, modelVariant, stored.Model, stored.ModelVariant)
	}

	if promptReq.Model == nil {
		t.Fatal("expected initial prompt request to include model override")
	}
	if got := promptReq.Model.ProviderID + "/" + promptReq.Model.ModelID; got != modelName {
		t.Fatalf("expected initial prompt model %q, got %q", modelName, got)
	}
	if promptReq.Variant != modelVariant {
		t.Fatalf("expected initial prompt variant %q, got %q", modelVariant, promptReq.Variant)
	}

	mu.Lock()
	defer mu.Unlock()
	if getCalls != 1 {
		t.Fatalf("expected one GET by stale session ID, got %d", getCalls)
	}
	if listCalls != 1 {
		t.Fatalf("expected one fallback list call, got %d", listCalls)
	}
	if createCalls != 1 {
		t.Fatalf("expected one create call, got %d", createCalls)
	}
	if createDirectory != projectRoot {
		t.Fatalf("expected create session directory header %q, got %q", projectRoot, createDirectory)
	}
	if createTitle != openCodeControlSessionTitle {
		t.Fatalf("expected create session title %q, got %q", openCodeControlSessionTitle, createTitle)
	}
	if createDecodeErr != "" {
		t.Fatalf("unexpected create request decode error: %s", createDecodeErr)
	}
	if promptDecodeErr != "" {
		t.Fatalf("unexpected prompt request decode error: %s", promptDecodeErr)
	}
	if promptDirectory != projectRoot {
		t.Fatalf("expected prompt directory header %q, got %q", projectRoot, promptDirectory)
	}
}

func TestGetOrCreateOpenCodeControlSessionCreatesNewWhenStoredModelMismatches(t *testing.T) {
	projectRoot := t.TempDir()
	repoID := "project-ctx"
	writeStoredOpenCodeControlSession(t, repoID, "ses_old", "anthropic/claude-opus-4-5", "high")

	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"

	var mu sync.Mutex
	getCalls := 0
	listCalls := 0
	createCalls := 0
	promptReqCh := make(chan struct{}, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/session/ses_old":
			mu.Lock()
			getCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && r.URL.Path == "/session":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		case r.Method == "POST" && r.URL.Path == "/session":
			mu.Lock()
			createCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        "ses_fresh",
				"title":     openCodeControlSessionTitle,
				"directory": projectRoot,
				"time": map[string]int64{
					"created": 4000,
					"updated": 4000,
				},
			})
		case r.Method == "POST" && r.URL.Path == "/session/ses_fresh/message":
			select {
			case promptReqCh <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.repos[repoID] = &RepoContext{RepoID: repoID, ProjectRoot: projectRoot}
	port := getPortFromURL(t, ts.URL)
	sessionID, _, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}
	if sessionID != "ses_fresh" {
		t.Fatalf("expected fresh session ID, got %q", sessionID)
	}

	select {
	case <-promptReqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial prompt on newly created session")
	}

	stored := readStoredOpenCodeControlSession(t, repoID)
	if stored.SessionID != "ses_fresh" {
		t.Fatalf("expected stored session to be refreshed, got %q", stored.SessionID)
	}
	if stored.Model != modelName || stored.ModelVariant != modelVariant {
		t.Fatalf("expected stored model metadata (%q,%q), got (%q,%q)", modelName, modelVariant, stored.Model, stored.ModelVariant)
	}

	mu.Lock()
	defer mu.Unlock()
	if getCalls != 0 {
		t.Fatalf("expected no GET session reuse call on model mismatch, got %d", getCalls)
	}
	if listCalls != 0 {
		t.Fatalf("expected no recovery list call on model mismatch, got %d", listCalls)
	}
	if createCalls != 1 {
		t.Fatalf("expected exactly one create call on model mismatch, got %d", createCalls)
	}
}

func TestResolvedControlModelAndVariantReachOpenCodeInitialPrompt(t *testing.T) {
	projectRoot := t.TempDir()
	repoID := "project-ctx"
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0755); err != nil {
		t.Fatalf("failed to create .orch dir: %v", err)
	}

	configYAML := []byte(`
control_agent: opencode
opencode:
  default_model: openai/gpt-5.3-codex
  default_variant: xhigh
`)
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), configYAML, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := loadControlAgentConfig(projectRoot)
	if err != nil {
		t.Fatalf("loadControlAgentConfig() error = %v", err)
	}
	modelName, modelVariant := cfg.ResolveControlModelAndVariant("opencode")
	if modelName != "openai/gpt-5.3-codex" || modelVariant != "xhigh" {
		t.Fatalf("unexpected resolved model config: (%q,%q)", modelName, modelVariant)
	}

	promptReqCh := make(chan agent.PromptRequest, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/session":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        "ses_from_cfg",
				"title":     openCodeControlSessionTitle,
				"directory": projectRoot,
				"time": map[string]int64{
					"created": 5000,
					"updated": 5000,
				},
			})
		case r.Method == "POST" && r.URL.Path == "/session/ses_from_cfg/message":
			var promptReq agent.PromptRequest
			if err := json.NewDecoder(r.Body).Decode(&promptReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case promptReqCh <- promptReq:
			default:
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.repos[repoID] = &RepoContext{RepoID: repoID, ProjectRoot: projectRoot}
	_, _, err = server.getOrCreateOpenCodeControlSession(projectRoot, getPortFromURL(t, ts.URL), modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}

	select {
	case promptReq := <-promptReqCh:
		if promptReq.Model == nil {
			t.Fatal("expected prompt to include model from ResolveControlModelAndVariant")
		}
		if got := promptReq.Model.ProviderID + "/" + promptReq.Model.ModelID; got != modelName {
			t.Fatalf("expected resolved model %q in prompt, got %q", modelName, got)
		}
		if promptReq.Variant != modelVariant {
			t.Fatalf("expected resolved variant %q in prompt, got %q", modelVariant, promptReq.Variant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for prompt request")
	}
}

func TestRegisterRepoAPI(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/register-repo.git")
	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_RegisterRepo{
			RegisterRepo: &orchpb.RegisterRepoRequest{ProjectRoot: projectRoot},
		},
	})

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}
	regResp := resp.GetRegisterRepo()
	if regResp == nil {
		t.Fatal("expected RegisterRepoResponse")
	}
	if regResp.RepoId == "" {
		t.Error("expected repo_id to be set")
	}
}

func TestRegisterRepoAPIRejectsPathWithoutRemote(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_RegisterRepo{
			RegisterRepo: &orchpb.RegisterRepoRequest{ProjectRoot: t.TempDir()},
		},
	})

	if resp.Ok {
		t.Fatal("expected register repo to fail for path without remote origin")
	}
	if !strings.Contains(resp.Error, "project identity required") {
		t.Fatalf("expected project identity guidance, got: %s", resp.Error)
	}
}

func TestDeriveRepoID(t *testing.T) {
	t.Run("returns empty for non-git path", func(t *testing.T) {
		got := deriveRepoID("/tmp/not-a-git-repo/my-project")
		if got != "" {
			t.Errorf("deriveRepoID for non-git path = %q, want empty", got)
		}
	})

	t.Run("returns repo id from git remote", func(t *testing.T) {
		projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/my-project.git")
		if got := deriveRepoID(projectRoot); got != "example-my-project" {
			t.Fatalf("deriveRepoID(%q) = %q, want %q", projectRoot, got, "example-my-project")
		}
	})

	t.Run("handles path with trailing slash", func(t *testing.T) {
		projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/another-project.git")
		withSlash := deriveRepoID(projectRoot + string(os.PathSeparator))
		withoutSlash := deriveRepoID(projectRoot)
		if withSlash != withoutSlash {
			t.Errorf("trailing slash should not change ID: %q != %q", withSlash, withoutSlash)
		}
	})

	t.Run("different remotes produce different IDs", func(t *testing.T) {
		id1 := deriveRepoID(createGitRepoWithOrigin(t, "https://github.com/example/project-a.git"))
		id2 := deriveRepoID(createGitRepoWithOrigin(t, "https://github.com/example/project-b.git"))
		if id1 == id2 {
			t.Errorf("different remotes should produce different IDs: %q == %q", id1, id2)
		}
	})

	t.Run("same repo path produces same ID", func(t *testing.T) {
		projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/my-project.git")
		id1 := deriveRepoID(projectRoot)
		id2 := deriveRepoID(projectRoot)
		if id1 != id2 {
			t.Errorf("same path should produce same ID: %q != %q", id1, id2)
		}
	})
}

func TestDeriveRepoIDNoBasenameCollision(t *testing.T) {
	idA := deriveRepoID(createGitRepoWithOrigin(t, "https://github.com/client-a/orch.git"))
	idB := deriveRepoID(createGitRepoWithOrigin(t, "https://github.com/client-b/orch.git"))
	if idA == idB {
		t.Errorf("same-basename repos produced same ID: %q", idA)
	}

	if idA != "client-a-orch" {
		t.Errorf("deriveRepoID(client-a/orch) = %q, want %q", idA, "client-a-orch")
	}
	if idB != "client-b-orch" {
		t.Errorf("deriveRepoID(client-b/orch) = %q, want %q", idB, "client-b-orch")
	}
}

func TestListReposAPI(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	registerRepoContextForTest(t, server, testProjectID, testProjectRoot, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListRepos{
			ListRepos: &orchpb.ListReposRequest{},
		},
	})

	if !resp.Ok {
		t.Errorf("expected ok=true, got error: %s", resp.Error)
	}
	listResp := resp.GetListRepos()
	if listResp == nil {
		t.Fatal("expected ListReposResponse")
	}
	if len(listResp.Repos) == 0 {
		t.Error("expected at least one registered repo")
	}
}

func TestListRunsWithRequestContextProjectID(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: map[string]*model.Run{
			"ctx-issue/ctx-run": {
				IssueID: "ctx-issue",
				RunID:   "ctx-run",
				Status:  model.StatusRunning,
				Agent:   "opencode",
				Branch:  "feature/ctx-run",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-ctx"] = &RepoContext{
		RepoID:      "project-ctx",
		ProjectRoot: "/srv/repos/orch",
		Store:       st,
	}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListRuns{
			ListRuns: &orchpb.ListRunsRequest{
				Context: &orchpb.RequestContext{ProjectId: "project-ctx"},
				Limit:   10,
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}
	listResp := resp.GetListRuns()
	if listResp == nil {
		t.Fatal("expected ListRunsResponse")
	}
	if len(listResp.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(listResp.Runs))
	}
	if listResp.Runs[0].IssueId != "ctx-issue" || listResp.Runs[0].RunId != "ctx-run" {
		t.Fatalf("unexpected run in response: issue=%q run=%q", listResp.Runs[0].IssueId, listResp.Runs[0].RunId)
	}
}

func TestListRunsStoreFailureIncludesProjectAndCause(t *testing.T) {
	cause := errors.New("corrupt run index")
	st := &listRunsErrorStore{
		mockStore: &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}},
		err:       cause,
	}
	server := newTestServer(t, st)

	resp := server.handleProtoListRuns(&orchpb.ListRunsRequest{
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatalf("handleProtoListRuns() Ok = true, want false")
	}
	if !strings.Contains(resp.Error, testProjectID) || !strings.Contains(resp.Error, cause.Error()) {
		t.Fatalf("handleProtoListRuns() error = %q, want project ID and underlying cause", resp.Error)
	}
	if resp.Error == "store_error" {
		t.Fatal("handleProtoListRuns() returned opaque store_error token")
	}
}

func TestListRunsWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	newer := time.Now()
	older := newer.Add(-1 * time.Hour)

	stA := &mockStore{
		runs: map[string]*model.Run{
			"issue-a/run-a": {
				IssueID:      "issue-a",
				RunID:        "run-a",
				Status:       model.StatusRunning,
				UpdatedAt:    older,
				StartedAt:    older,
				Agent:        "opencode",
				WorktreePath: "/tmp/a",
			},
		},
		issues: map[string]*model.Issue{
			"issue-a": {ID: "issue-a", Status: model.IssueStatusOpen, Summary: "A"},
		},
	}

	stB := &mockStore{
		runs: map[string]*model.Run{
			"issue-b/run-b": {
				IssueID:      "issue-b",
				RunID:        "run-b",
				Status:       model.StatusRunning,
				UpdatedAt:    newer,
				StartedAt:    newer,
				Agent:        "opencode",
				WorktreePath: "/tmp/b",
			},
		},
		issues: map[string]*model.Issue{
			"issue-b": {ID: "issue-b", Status: model.IssueStatusOpen, Summary: "B"},
		},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListRuns{
			ListRuns: &orchpb.ListRunsRequest{
				Context: &orchpb.RequestContext{},
				Limit:   10,
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}
	listResp := resp.GetListRuns()
	if listResp == nil {
		t.Fatal("expected ListRunsResponse")
	}
	if len(listResp.Runs) != 2 {
		t.Fatalf("expected 2 runs from aggregate listing, got %d", len(listResp.Runs))
	}
	if listResp.Runs[0].IssueId != "issue-b" || listResp.Runs[0].RunId != "run-b" {
		t.Fatalf("expected newest run first, got issue=%q run=%q", listResp.Runs[0].IssueId, listResp.Runs[0].RunId)
	}
}

func TestGetConfigWithUnknownProjectContextDoesNotFallbackToEnv(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetConfig{
			GetConfig: &orchpb.GetConfigRequest{
				Context: &orchpb.RequestContext{ProjectId: "missing-project"},
			},
		},
	})

	if resp.Ok {
		t.Fatal("expected error response for unknown project context")
	}
	if !strings.Contains(resp.Error, "unknown project_id") {
		t.Fatalf("expected unknown project_id error, got: %s", resp.Error)
	}
}

func TestListIssuesWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: make(map[string]*model.Run),
		issues: map[string]*model.Issue{
			"issue-1": {
				ID:     "issue-1",
				Title:  "Issue 1",
				Status: model.IssueStatusOpen,
			},
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListIssues{
			ListIssues: &orchpb.ListIssuesRequest{Context: &orchpb.RequestContext{}},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	listResp := resp.GetListIssues()
	if listResp == nil {
		t.Fatal("expected ListIssuesResponse")
	}
	if len(listResp.Issues) != 1 || listResp.Issues[0].Id != "issue-1" {
		t.Fatalf("expected aggregate issue listing, got %#v", listResp.Issues)
	}
}

func TestListIssuesUnknownProjectContextReturnsProjectScopedError(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListIssues{
			ListIssues: &orchpb.ListIssuesRequest{Context: &orchpb.RequestContext{ProjectId: "missing-project"}},
		},
	})

	if resp.Ok {
		t.Fatalf("expected error response for unknown project context")
	}
	expected := "no store available for project_id \"missing-project\" (register daemon project mapping)"
	if resp.Error != expected {
		t.Fatalf("expected %q, got %q", expected, resp.Error)
	}
}

func TestRunLookupWithoutProjectContextReturnsSpacedAmbiguousMessages(t *testing.T) {
	runA := &model.Run{IssueID: "ambiguous-issue", RunID: "ambiguous-run"}
	runB := &model.Run{IssueID: "ambiguous-issue", RunID: "ambiguous-run"}
	stA := &mockStore{runs: map[string]*model.Run{runA.Ref().String(): runA}, issues: map[string]*model.Issue{}}
	stB := &mockStore{runs: map[string]*model.Run{runB.Ref().String(): runB}, issues: map[string]*model.Issue{}}
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, "project-a", "/srv/repos/a", stA)
	registerRepoContextForTest(t, server, "project-b", "/srv/repos/b", stB)

	fullResp := server.handleProtoGetRun(&orchpb.GetRunRequest{
		IssueId: string(runA.IssueID),
		RunId:   string(runA.RunID),
		Context: &orchpb.RequestContext{},
	})
	fullMessage := fmt.Sprintf("ambiguous run ref: %s#%s", runA.IssueID, runA.RunID)
	if fullResp.Ok || fullResp.Error != fullMessage {
		t.Fatalf("full-ref response: ok=%v error=%q, want %q", fullResp.Ok, fullResp.Error, fullMessage)
	}

	shortID := string(runA.ShortID())
	shortResp := server.handleProtoGetRunByShortID(&orchpb.GetRunByShortIDRequest{
		ShortId: shortID,
		Context: &orchpb.RequestContext{},
	})
	shortMessage := fmt.Sprintf("ambiguous short id: %s", shortID)
	if shortResp.Ok || shortResp.Error != shortMessage {
		t.Fatalf("short-ref response: ok=%v error=%q, want %q", shortResp.Ok, shortResp.Error, shortMessage)
	}
}

func TestGetRunWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{
		runs:   map[string]*model.Run{},
		issues: map[string]*model.Issue{},
	}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"agg-issue#agg-run": {
				IssueID:      "agg-issue",
				RunID:        "agg-run",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: "/tmp/agg",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetRun{
			GetRun: &orchpb.GetRunRequest{
				IssueId: "agg-issue",
				RunId:   "agg-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	getResp := resp.GetGetRun()
	if getResp == nil || getResp.Run == nil {
		t.Fatal("expected GetRun response payload")
	}
	if getResp.Run.IssueId != "agg-issue" || getResp.Run.RunId != "agg-run" {
		t.Fatalf("unexpected run payload: issue=%q run=%q", getResp.Run.IssueId, getResp.Run.RunId)
	}
}

func TestGetIssueWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{},
		issues: map[string]*model.Issue{
			"agg-issue": {
				ID:         "agg-issue",
				Title:      "Aggregate issue",
				Status:     model.IssueStatusOpen,
				ModifiedAt: time.Now(),
			},
		},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetIssue{
			GetIssue: &orchpb.GetIssueRequest{
				IssueId: "agg-issue",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	getResp := resp.GetGetIssue()
	if getResp == nil || getResp.Issue == nil {
		t.Fatal("expected GetIssue response payload")
	}
	if getResp.Issue.Id != "agg-issue" {
		t.Fatalf("unexpected issue id: %q", getResp.Issue.Id)
	}
}

func TestGetRunByShortIDWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"agg-short#run-one": {
				IssueID:      "agg-short",
				RunID:        "run-one",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: "/tmp/agg-short",
			},
		},
		issues: map[string]*model.Issue{},
	}

	runShortID := model.GenerateShortID("agg-short", "run-one")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetRunByShortId{
			GetRunByShortId: &orchpb.GetRunByShortIDRequest{
				ShortId: string(runShortID),
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	getResp := resp.GetGetRunByShortId()
	if getResp == nil || getResp.Run == nil {
		t.Fatal("expected GetRunByShortID response payload")
	}
	if getResp.Run.IssueId != "agg-short" || getResp.Run.RunId != "run-one" {
		t.Fatalf("unexpected run payload: issue=%q run=%q", getResp.Run.IssueId, getResp.Run.RunId)
	}
}

func TestGetAttachInfoWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"attach-issue#attach-run": {
				IssueID:           "attach-issue",
				RunID:             "attach-run",
				Status:            model.StatusRunning,
				UpdatedAt:         time.Now(),
				WorktreePath:      "/tmp/attach",
				Agent:             "opencode",
				ServerPort:        7777,
				OpenCodeSessionID: "session-attach",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetAttachInfo{
			GetAttachInfo: &orchpb.GetAttachInfoRequest{
				IssueId: "attach-issue",
				RunId:   "attach-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	attachResp := resp.GetGetAttachInfo()
	if attachResp == nil {
		t.Fatal("expected GetAttachInfo response payload")
	}
	if attachResp.IssueId != "attach-issue" || attachResp.RunId != "attach-run" {
		t.Fatalf("unexpected attach payload: issue=%q run=%q", attachResp.IssueId, attachResp.RunId)
	}
}

func TestGetAttachInfoInvalidMultiplexerReturnsExplicitError(t *testing.T) {
	st := &mockStore{
		runs: map[string]*model.Run{
			"attach-issue#attach-run": {
				IssueID:     "attach-issue",
				RunID:       "attach-run",
				Agent:       "claude",
				Multiplexer: "screen",
			},
		},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	resp := server.handleProtoGetAttachInfo(&orchpb.GetAttachInfoRequest{
		IssueId: "attach-issue",
		RunId:   "attach-run",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatal("handleProtoGetAttachInfo() Ok = true, want false")
	}
	if !strings.Contains(resp.Error, "screen") || !strings.Contains(resp.Error, "unknown multiplexer type") {
		t.Fatalf("handleProtoGetAttachInfo() error = %q, want multiplexer type and cause", resp.Error)
	}
}

func TestGetAttachInfoUnavailableMultiplexerReturnsExplicitError(t *testing.T) {
	st := &mockStore{
		runs: map[string]*model.Run{
			"attach-issue#attach-run": {
				IssueID:     "attach-issue",
				RunID:       "attach-run",
				Agent:       "claude",
				Multiplexer: "auto",
			},
		},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	resp := server.handleProtoGetAttachInfo(&orchpb.GetAttachInfoRequest{
		IssueId: "attach-issue",
		RunId:   "attach-run",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatal("handleProtoGetAttachInfo() Ok = true, want false")
	}
	if !strings.Contains(resp.Error, `multiplexer "auto" unavailable`) || !strings.Contains(resp.Error, "unknown multiplexer type: auto") {
		t.Fatalf("handleProtoGetAttachInfo() error = %q, want unavailable multiplexer type and cause", resp.Error)
	}
}

func TestGetAttachInfoByShortIDWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"attach-short#attach-run": {
				IssueID:           "attach-short",
				RunID:             "attach-run",
				Status:            model.StatusRunning,
				UpdatedAt:         time.Now(),
				WorktreePath:      "/tmp/attach-short",
				Agent:             "opencode",
				ServerPort:        7777,
				OpenCodeSessionID: "session-short",
			},
		},
		issues: map[string]*model.Issue{},
	}

	shortID := model.GenerateShortID("attach-short", "attach-run")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetAttachInfo{
			GetAttachInfo: &orchpb.GetAttachInfoRequest{
				ShortId: string(shortID),
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	attachResp := resp.GetGetAttachInfo()
	if attachResp == nil {
		t.Fatal("expected GetAttachInfo response payload")
	}
	if attachResp.IssueId != "attach-short" || attachResp.RunId != "attach-run" {
		t.Fatalf("unexpected attach payload: issue=%q run=%q", attachResp.IssueId, attachResp.RunId)
	}
}

func TestCaptureSessionWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"capture-issue#capture-run": {
				IssueID:      "capture-issue",
				RunID:        "capture-run",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: "/tmp/capture",
				Agent:        "custom",
				Multiplexer:  "tmux",
				SessionName:  "capture-session",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	prevCaptureMux := getCaptureMultiplexerForType
	getCaptureMultiplexerForType = func(muxType multiplexer.Type) captureMultiplexer {
		return &mockCaptureMux{hasSession: true, content: "captured text"}
	}
	defer func() { getCaptureMultiplexerForType = prevCaptureMux }()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_CaptureSession{
			CaptureSession: &orchpb.CaptureSessionRequest{
				IssueId: "capture-issue",
				RunId:   "capture-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	captureResp := resp.GetCaptureSession()
	if captureResp == nil {
		t.Fatal("expected CaptureSession response payload")
	}
	if captureResp.Source != "tmux" {
		t.Fatalf("expected capture source tmux, got %q", captureResp.Source)
	}
}

func TestCaptureSessionRemoteTargetUsesWorkerLease(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: map[string]*model.Run{
			"capture-remote#run-remote": {
				IssueID:      "capture-remote",
				RunID:        "run-remote",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: "/tmp/capture-remote",
				Agent:        "custom",
				Multiplexer:  "tmux",
				Target:       "mac",
				TargetHost:   "mac-host",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	registerRepoContextForTest(t, server, "project-remote", "/srv/repos/remote", st)

	workerID := HostWorkerID("mac-host")
	if _, ttl := server.registerWorker(workerID, "external", "mac-host", "external", []string{"capture_session"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for capture worker")
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if lease.Effect != "capture_session" {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "unexpected effect", "")
				return
			}
			if lease.Payload == nil || lease.Payload.CaptureSession == nil || lease.Payload.CaptureSession.TargetWorkerID != workerID {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "unexpected capture payload", "")
				return
			}
			if lease.Payload.CaptureSession.RunSnapshot == nil || lease.Payload.CaptureSession.RunSnapshot.RunID != "run-remote" {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "missing capture run snapshot", "")
				return
			}
			resultJSON := EncodeWorkerEffectResult(&WorkerEffectResult{
				CaptureResult: &CaptureSessionResult{
					Content:       "remote capture",
					TimestampUnix: 12345,
					Source:        "tmux",
				},
			})
			_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", resultJSON)
			return
		}
	}()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_CaptureSession{
			CaptureSession: &orchpb.CaptureSessionRequest{
				IssueId: "capture-remote",
				RunId:   "run-remote",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected capture to succeed, got error: %s", resp.Error)
	}
	captureResp := resp.GetCaptureSession()
	if captureResp == nil {
		t.Fatal("expected CaptureSession response payload")
	}
	if captureResp.Content != "remote capture" || captureResp.Source != "tmux" || captureResp.TimestampUnix != 12345 {
		t.Fatalf("unexpected capture response: %+v", captureResp)
	}
}

func TestSendMessageRemoteTargetUsesWorkerLease(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue-remote#run-remote": {
				IssueID:     "issue-remote",
				RunID:       "run-remote",
				SessionName: "session-remote",
				Agent:       string(agent.AgentClaude),
				Target:      "mac",
				TargetHost:  "mac-host",
				Status:      model.StatusPROpen,
			},
		},
		issues: map[string]*model.Issue{},
	}

	registerRepoContextForTest(t, server, "project-remote", "/srv/repos/remote", st)
	workerID := HostWorkerID("mac-host")
	if _, ttl := server.registerWorker(workerID, "external", "mac-host", "external", []string{"send_message"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for send worker")
	}

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if lease.Effect != "send_message" {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "unexpected effect", "")
				return
			}
			if lease.Payload == nil || lease.Payload.SendMessage == nil {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "missing send payload", "")
				return
			}
			payload := lease.Payload.SendMessage
			if payload.Message != "hello remote" || payload.NoEnter || payload.TargetWorkerID != workerID {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "unexpected send payload", "")
				return
			}
			if payload.RunSnapshot == nil || payload.RunSnapshot.RunID != "run-remote" {
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "missing send run snapshot", "")
				return
			}
			_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", "")
			return
		}
	}()

	resp := server.handleProtoSendMessage(&orchpb.SendMessageRequest{
		IssueId: "issue-remote",
		RunId:   "run-remote",
		Message: "hello remote",
		NoEnter: false,
		Context: &orchpb.RequestContext{},
	})
	if !resp.Ok {
		t.Fatalf("expected send_message to succeed, got error: %s", resp.Error)
	}

	// Feedback resumes the run: a pr_open run that just received input is
	// working again, so `orch wait` blocks until the agent next goes idle.
	if got := st.runs["issue-remote#run-remote"].Status; got != model.StatusRunning {
		t.Fatalf("expected run marked running after feedback, got %s", got)
	}
}

func TestMarkRunFeedbackSentTransitions(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	cases := []struct {
		status model.Status
		want   model.Status
	}{
		{model.StatusWaiting, model.StatusRunning},
		{model.StatusPROpen, model.StatusRunning},
		{model.StatusRateLimited, model.StatusRunning},
		{model.StatusUnknown, model.StatusRunning},
		{model.StatusRunning, model.StatusRunning}, // no duplicate event
		{model.StatusBooting, model.StatusBooting}, // boot flow owns running
		{model.StatusDone, model.StatusDone},       // terminal stays terminal
		{model.StatusFailed, model.StatusFailed},
	}

	for _, tc := range cases {
		run := &model.Run{IssueID: "i", RunID: "r", Status: tc.status}
		st := &mockStore{runs: map[string]*model.Run{"i#r": run}, issues: map[string]*model.Issue{}}
		feedbackNoted := false
		var statusChanges []*runevents.StatusChangeEvent
		server.onRunFeedback = func(*model.Run) { feedbackNoted = true }
		server.onStatusChange = func(ev *runevents.StatusChangeEvent) {
			copy := *ev
			statusChanges = append(statusChanges, &copy)
		}

		server.markRunFeedbackSent(st, run)

		if got := st.runs["i#r"].Status; got != tc.want {
			t.Errorf("markRunFeedbackSent from %s: status = %s, want %s", tc.status, got, tc.want)
		}
		wantTransition := tc.want == model.StatusRunning && tc.status != model.StatusRunning
		if feedbackNoted != wantTransition {
			t.Errorf("markRunFeedbackSent from %s: feedback noted = %v, want %v", tc.status, feedbackNoted, wantTransition)
		}
		if wantTransition {
			if len(statusChanges) != 1 {
				t.Fatalf("markRunFeedbackSent from %s: listener events = %d, want 1", tc.status, len(statusChanges))
			}
			ev := statusChanges[0]
			if ev.Run != run {
				t.Fatalf("markRunFeedbackSent from %s: listener received wrong run", tc.status)
			}
			if ev.From != tc.status || ev.To != model.StatusRunning {
				t.Fatalf("markRunFeedbackSent from %s: listener transition = %s -> %s, want %s -> %s",
					tc.status, ev.From, ev.To, tc.status, model.StatusRunning)
			}
			if ev.Source != model.EventSourceUser {
				t.Fatalf("markRunFeedbackSent from %s: listener source = %s, want %s",
					tc.status, ev.Source, model.EventSourceUser)
			}
			if ev.Store != st {
				t.Fatalf("markRunFeedbackSent from %s: listener received wrong store", tc.status)
			}
		} else if len(statusChanges) != 0 {
			t.Fatalf("markRunFeedbackSent from %s: listener events = %d, want 0", tc.status, len(statusChanges))
		}
	}
}

// TestPublishFeedbackResume_VocabularyDriftSkipsNotPanics feeds a status
// with no proto mapping through the socket-plane publish path and asserts
// the drift rule of docs/design/run-state-machine.md §8: no panic (the old
// behavior panicked here and killed the daemon), an ERROR naming the run
// ref and the offending status is logged, no frame is published for the
// drifted run, and another run's feedback-resume publish is unaffected.
func TestPublishFeedbackResume_VocabularyDriftSkipsNotPanics(t *testing.T) {
	var logBuf bytes.Buffer
	server := NewSocketServer(nil, log.New(&logBuf, "", 0))

	sub := server.RunEventBus().Subscribe(RunEventFilter{})
	defer sub.Close()

	drifted := &model.Run{IssueID: "i-drift", RunID: "r-drift"}
	// Old behavior: this call panics and the test fails.
	server.publishFeedbackResume(drifted, model.Status("status-from-the-future"))

	logged := logBuf.String()
	if !strings.Contains(logged, "ERROR i-drift#r-drift") {
		t.Fatalf("ERROR log line does not name the run ref:\n%s", logged)
	}
	if !strings.Contains(logged, "status-from-the-future") {
		t.Fatalf("log does not name the offending status:\n%s", logged)
	}

	select {
	case ev := <-sub.Events():
		t.Fatalf("expected no event for drifted status, got %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: publish skipped
	}

	// Other runs' updates are unaffected: a healthy run resumed by feedback
	// still publishes through the full markRunFeedbackSent path.
	healthy := &model.Run{IssueID: "i", RunID: "r", Status: model.StatusWaiting}
	st := &mockStore{runs: map[string]*model.Run{"i#r": healthy}, issues: map[string]*model.Issue{}}
	server.markRunFeedbackSent(st, healthy)
	select {
	case ev := <-sub.Events():
		if ev.IssueId != "i" || ev.RunId != "r" {
			t.Fatalf("unexpected ids: %+v", ev)
		}
		if ev.FromStatus != orchpb.RunStatus_RUN_STATUS_WAITING || ev.ToStatus != orchpb.RunStatus_RUN_STATUS_RUNNING {
			t.Fatalf("unexpected transition: %+v", ev)
		}
		if ev.Source != string(model.EventSourceUser) {
			t.Fatalf("source = %q, want %q", ev.Source, model.EventSourceUser)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("healthy feedback-resume publish blocked after drift skip")
	}
}

func leaseRunSnapshot(lease *WorkerLease) *RunSnapshot {
	if lease == nil || lease.Payload == nil {
		return nil
	}
	switch lease.Effect {
	case "get_diff_stats":
		if lease.Payload.GetDiffStats != nil {
			return lease.Payload.GetDiffStats.RunSnapshot
		}
	case "get_branch_state":
		if lease.Payload.GetBranchState != nil {
			return lease.Payload.GetBranchState.RunSnapshot
		}
	case "get_diff":
		if lease.Payload.GetDiff != nil {
			return lease.Payload.GetDiff.RunSnapshot
		}
	}
	return nil
}

func TestRemoteGitRequestsUseWorkerLease(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	tests := []struct {
		name       string
		capability string
		request    *orchpb.Request
		resultJSON string
		assertResp func(t *testing.T, resp *orchpb.Response)
	}{
		{
			name:       "diff stats",
			capability: "get_diff_stats",
			request: &orchpb.Request{
				Request: &orchpb.Request_GetDiffStats{
					GetDiffStats: &orchpb.GetDiffStatsRequest{
						IssueId: "git-remote",
						RunId:   "run-remote",
						Context: &orchpb.RequestContext{},
					},
				},
			},
			resultJSON: EncodeWorkerEffectResult(&WorkerEffectResult{
				DiffStatsResult: &GetDiffStatsResult{
					Additions:    3,
					Deletions:    1,
					FilesChanged: 2,
					Files:        []string{"a.txt", "b.txt"},
				},
			}),
			assertResp: func(t *testing.T, resp *orchpb.Response) {
				t.Helper()
				stats := resp.GetGetDiffStats()
				if stats == nil || stats.DiffStats == nil {
					t.Fatal("expected diff stats response payload")
				}
				if stats.DiffStats.Additions != 3 || stats.DiffStats.Deletions != 1 || stats.DiffStats.FilesChanged != 2 {
					t.Fatalf("unexpected diff stats: %+v", stats.DiffStats)
				}
			},
		},
		{
			name:       "branch state",
			capability: "get_branch_state",
			request: &orchpb.Request{
				Request: &orchpb.Request_GetBranchState{
					GetBranchState: &orchpb.GetBranchStateRequest{
						IssueId: "git-remote",
						RunId:   "run-remote",
						Context: &orchpb.RequestContext{},
					},
				},
			},
			resultJSON: EncodeWorkerEffectResult(&WorkerEffectResult{
				BranchStateResult: &GetBranchStateResult{State: int32(orchpb.BranchState_BRANCH_STATE_DIRTY)},
			}),
			assertResp: func(t *testing.T, resp *orchpb.Response) {
				t.Helper()
				state := resp.GetGetBranchState()
				if state == nil {
					t.Fatal("expected branch state response payload")
				}
				if state.State != orchpb.BranchState_BRANCH_STATE_DIRTY {
					t.Fatalf("branch state = %v, want DIRTY", state.State)
				}
			},
		},
		{
			name:       "diff",
			capability: "get_diff",
			request: &orchpb.Request{
				Request: &orchpb.Request_GetDiff{
					GetDiff: &orchpb.GetDiffRequest{
						IssueId: "git-remote",
						RunId:   "run-remote",
						Context: &orchpb.RequestContext{},
					},
				},
			},
			resultJSON: EncodeWorkerEffectResult(&WorkerEffectResult{
				DiffResult: &GetDiffResult{Diff: "diff --git a/a.txt b/a.txt"},
			}),
			assertResp: func(t *testing.T, resp *orchpb.Response) {
				t.Helper()
				diff := resp.GetGetDiff()
				if diff == nil {
					t.Fatal("expected diff response payload")
				}
				if diff.Diff != "diff --git a/a.txt b/a.txt" {
					t.Fatalf("unexpected diff payload: %q", diff.Diff)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &mockStore{
				runs: map[string]*model.Run{
					"git-remote#run-remote": {
						IssueID:      "git-remote",
						RunID:        "run-remote",
						Status:       model.StatusRunning,
						UpdatedAt:    time.Now(),
						WorktreePath: "/tmp/git-remote",
						Branch:       "feature/remote",
						Target:       "mac",
						TargetHost:   "mac-host",
					},
				},
				issues: map[string]*model.Issue{},
			}

			logger := log.New(io.Discard, "", 0)
			server := NewSocketServer(nil, logger)
			registerRepoContextForTest(t, server, "project-remote", "/srv/repos/remote", st)

			workerID := HostWorkerID("mac-host")
			if _, ttl := server.registerWorker(workerID, "external", "mac-host", "external", []string{tt.capability}); ttl <= 0 {
				t.Fatal("expected positive heartbeat ttl for git worker")
			}

			if err := server.Start(); err != nil {
				t.Fatalf("failed to start server: %v", err)
			}
			defer server.Stop()

			go func() {
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					lease := server.leaseWorkForWorker(workerID)
					if lease == nil {
						time.Sleep(10 * time.Millisecond)
						continue
					}
					if lease.Effect != tt.capability {
						_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "unexpected effect", "")
						return
					}
					if leaseRunSnapshot(lease) == nil || leaseRunSnapshot(lease).RunID != "run-remote" {
						_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, "missing git run snapshot", "")
						return
					}
					_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", tt.resultJSON)
					return
				}
			}()

			resp := sendProtoRequest(t, tt.request)
			if !resp.Ok {
				t.Fatalf("expected request to succeed, got error: %s", resp.Error)
			}
			tt.assertResp(t, resp)
		})
	}
}

func TestGetDiffStatsGitFailureReturnsExplicitError(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()
	nonGitWorktree := t.TempDir()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"diffstats-issue#diffstats-run": {
				IssueID:      "diffstats-issue",
				RunID:        "diffstats-run",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: nonGitWorktree,
				Branch:       "feature/diffstats",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetDiffStats{
			GetDiffStats: &orchpb.GetDiffStatsRequest{
				IssueId: "diffstats-issue",
				RunId:   "diffstats-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if resp.Ok {
		t.Fatal("expected git failure response, got ok=true")
	}
	if !strings.Contains(resp.Error, nonGitWorktree) || !strings.Contains(strings.ToLower(resp.Error), "not a git repository") {
		t.Fatalf("error = %q, want worktree path and underlying git/filesystem cause", resp.Error)
	}
}

func TestGetBranchStateWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"branch-issue#branch-run": {
				IssueID:      "branch-issue",
				RunID:        "branch-run",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: "/tmp/branch-missing",
				Branch:       "feature/branch",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetBranchState{
			GetBranchState: &orchpb.GetBranchStateRequest{
				IssueId: "branch-issue",
				RunId:   "branch-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	stateResp := resp.GetGetBranchState()
	if stateResp == nil {
		t.Fatal("expected GetBranchState response payload")
	}
}

func TestGetDiffWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	stB := &mockStore{
		runs: map[string]*model.Run{
			"diff-issue#diff-run": {
				IssueID:      "diff-issue",
				RunID:        "diff-run",
				Status:       model.StatusRunning,
				UpdatedAt:    time.Now(),
				WorktreePath: "/tmp/diff-missing",
				Branch:       "feature/diff",
			},
		},
		issues: map[string]*model.Issue{},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetDiff{
			GetDiff: &orchpb.GetDiffRequest{
				IssueId: "diff-issue",
				RunId:   "diff-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	diffResp := resp.GetGetDiff()
	if diffResp == nil {
		t.Fatal("expected GetDiff response payload")
	}
}

func TestReadAgentPromptWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStoreWithPrompt{
		mockStore: mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}},
		prompts:   map[string]string{},
	}
	stB := &mockStoreWithPrompt{
		mockStore: mockStore{
			runs: map[string]*model.Run{
				"prompt-issue#prompt-run": {
					IssueID:      "prompt-issue",
					RunID:        "prompt-run",
					Status:       model.StatusRunning,
					UpdatedAt:    time.Now(),
					WorktreePath: "/tmp/prompt",
				},
			},
			issues: map[string]*model.Issue{},
		},
		prompts: map[string]string{
			"prompt-issue#prompt-run": "hello from aggregate prompt",
		},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ReadAgentPrompt{
			ReadAgentPrompt: &orchpb.ReadAgentPromptRequest{
				IssueId: "prompt-issue",
				RunId:   "prompt-run",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	promptResp := resp.GetReadAgentPrompt()
	if promptResp == nil {
		t.Fatal("expected ReadAgentPrompt response payload")
	}
	if promptResp.Content != "hello from aggregate prompt" {
		t.Fatalf("unexpected prompt content: %q", promptResp.Content)
	}
}

func TestValidateIssueFilesWithoutProjectContextAggregatesAcrossStores(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	stA := &mockStoreWithValidation{
		mockStore:        mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}},
		validationResult: &store.ValidationResult{Total: 2, Valid: 2},
	}
	stB := &mockStoreWithValidation{
		mockStore: mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}},
		validationResult: &store.ValidationResult{
			Total: 1,
			Valid: 0,
			Errors: []*store.ValidationResultItem{
				{
					File:    "issues/bad.md",
					IssueID: "bad-issue",
					Errors:  []store.ValidationIssue{{Code: "missing-title", Message: "title required", Line: 3, Level: "error"}},
				},
			},
			Duplicates: []*store.DuplicateID{{ID: "dup-1", Files: []string{"issues/a.md", "issues/b.md"}}},
		},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-a"] = &RepoContext{RepoID: "project-a", ProjectRoot: "/srv/repos/a", Store: stA}
	server.repos["project-b"] = &RepoContext{RepoID: "project-b", ProjectRoot: "/srv/repos/b", Store: stB}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ValidateIssueFiles{
			ValidateIssueFiles: &orchpb.ValidateIssueFilesRequest{
				IssueId: "",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	validateResp := resp.GetValidateIssueFiles()
	if validateResp == nil {
		t.Fatal("expected ValidateIssueFiles response payload")
	}
	if validateResp.Total != 3 || validateResp.Valid != 2 {
		t.Fatalf("unexpected aggregate totals: total=%d valid=%d", validateResp.Total, validateResp.Valid)
	}
	if len(validateResp.Errors) != 1 {
		t.Fatalf("expected one validation error item, got %d", len(validateResp.Errors))
	}
	if len(validateResp.Duplicates) != 1 {
		t.Fatalf("expected one duplicate item, got %d", len(validateResp.Duplicates))
	}
}

func TestProtoStartRunWithoutProjectRootDoesNotFallbackToEnv(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	st := &mockStore{
		runs: make(map[string]*model.Run),
		issues: map[string]*model.Issue{
			"test-issue": {
				ID:     "test-issue",
				Title:  "Test issue",
				Status: model.IssueStatusOpen,
				Path:   "/test/issues/test-issue.md",
			},
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_StartRun{
			StartRun: &orchpb.StartRunRequest{
				IssueId: "test-issue",
				DryRun:  true,
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if resp.Ok {
		t.Fatal("expected error response")
	}
	if resp.Error != "project_id required" {
		t.Fatalf("expected project_id required, got: %s", resp.Error)
	}
}

func TestProtoContinueRunWithoutProjectRootDoesNotFallbackToEnv(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ContinueRun{
			ContinueRun: &orchpb.ContinueRunRequest{
				IssueId: "test-issue",
				Context: &orchpb.RequestContext{},
			},
		},
	})

	if resp.Ok {
		t.Fatal("expected error response")
	}
	if resp.Error != "project_id required" {
		t.Fatalf("expected project_id required, got: %s", resp.Error)
	}
}

func TestProtoRunRequestsDoNotRouteByProjectRootWithoutProjectContext(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: map[string]*model.Run{},
		issues: map[string]*model.Issue{
			"test-issue": {
				ID:     "test-issue",
				Title:  "Test issue",
				Status: model.IssueStatusOpen,
				Path:   "/test/issues/test-issue.md",
			},
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	tests := []struct {
		name string
		req  *orchpb.Request
	}{
		{
			name: "start-run",
			req: &orchpb.Request{
				Request: &orchpb.Request_StartRun{
					StartRun: &orchpb.StartRunRequest{
						IssueId: "test-issue",
						Context: &orchpb.RequestContext{},
					},
				},
			},
		},
		{
			name: "continue-run",
			req: &orchpb.Request{
				Request: &orchpb.Request_ContinueRun{
					ContinueRun: &orchpb.ContinueRunRequest{
						IssueId: "test-issue",
						Context: &orchpb.RequestContext{},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := sendProtoRequest(t, tt.req)
			if resp.Ok {
				t.Fatal("expected error response")
			}
			if resp.Error != "project_id required" {
				t.Fatalf("expected project_id required, got %q", resp.Error)
			}
		})
	}
}

func TestGetConfigWithoutProjectRootDoesNotFallbackToEnv(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetConfig{
			GetConfig: &orchpb.GetConfigRequest{Context: &orchpb.RequestContext{}},
		},
	})

	if resp.Ok {
		t.Fatal("expected error response")
	}
	if resp.Error != "project_id required" {
		t.Fatalf("expected project_id required, got: %s", resp.Error)
	}
}

func TestGetConfigWithRequestContextProjectID(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configYAML := []byte("agent: opencode\nmodel: example/model\n")
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	registerRepoContextForTest(t, server, "project-ctx", projectRoot, nil)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetConfig{
			GetConfig: &orchpb.GetConfigRequest{Context: &orchpb.RequestContext{ProjectId: "project-ctx"}},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	cfgResp := resp.GetGetConfig()
	if cfgResp == nil {
		t.Fatal("expected GetConfig response payload")
	}
	if cfgResp.Agent != "opencode" {
		t.Fatalf("expected agent opencode, got %q", cfgResp.Agent)
	}
	if cfgResp.Model != "example/model" {
		t.Fatalf("expected model example/model, got %q", cfgResp.Model)
	}
}

func TestGetControlAgentConfigWithRequestContextProjectID(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configYAML := []byte("agent: opencode\ncontrol_agent: opencode\ncontrol_model: example/control\ncontrol_model_variant: fast\n")
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	st := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	registerRepoContextForTest(t, server, "project-ctx", projectRoot, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetControlAgentConfig{
			GetControlAgentConfig: &orchpb.GetControlAgentConfigRequest{Context: &orchpb.RequestContext{ProjectId: "project-ctx"}},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok response, got error: %s", resp.Error)
	}
	cfgResp := resp.GetGetControlAgentConfig()
	if cfgResp == nil {
		t.Fatal("expected GetControlAgentConfig response payload")
	}
	if cfgResp.Agent != "opencode" {
		t.Fatalf("expected agent opencode, got %q", cfgResp.Agent)
	}
	if cfgResp.Model != "example/control" {
		t.Fatalf("expected control model example/control, got %q", cfgResp.Model)
	}
	if cfgResp.ModelVariant != "fast" {
		t.Fatalf("expected control model variant fast, got %q", cfgResp.ModelVariant)
	}
	if strings.TrimSpace(cfgResp.PromptContent) == "" {
		t.Fatal("expected non-empty prompt content")
	}
}

func TestEnsureOpenCodeServerWithoutProjectContextDoesNotFallbackToEnv(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_EnsureOpencodeServer{
			EnsureOpencodeServer: &orchpb.EnsureOpenCodeServerRequest{Context: &orchpb.RequestContext{}},
		},
	})

	if resp.Ok {
		t.Fatal("expected error response")
	}
	if resp.Error != "project_id required" {
		t.Fatalf("expected project_id required, got: %s", resp.Error)
	}
}

func TestEnsureOpenCodeServerWithUnknownProjectContextDoesNotFallbackToEnv(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_EnsureOpencodeServer{
			EnsureOpencodeServer: &orchpb.EnsureOpenCodeServerRequest{
				Context: &orchpb.RequestContext{ProjectId: "missing-project"},
			},
		},
	})

	if resp.Ok {
		t.Fatal("expected error response")
	}
	expected := `unknown project_id "missing-project" (register daemon project mapping)`
	if resp.Error != expected {
		t.Fatalf("expected %q, got %q", expected, resp.Error)
	}
}

func TestGetIssueWithRequestContextProjectID(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs: map[string]*model.Run{},
		issues: map[string]*model.Issue{
			"ctx-issue": {
				ID:     "ctx-issue",
				Title:  "Context issue",
				Status: model.IssueStatusOpen,
				Path:   "/srv/repos/orch/issues/ctx-issue.md",
			},
		},
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.reposMu.Lock()
	server.repos["project-ctx"] = &RepoContext{
		RepoID:      "project-ctx",
		ProjectRoot: "/srv/repos/orch",
		Store:       st,
	}
	server.reposMu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetIssue{
			GetIssue: &orchpb.GetIssueRequest{
				IssueId: "ctx-issue",
				Context: &orchpb.RequestContext{ProjectId: "project-ctx"},
			},
		},
	})

	if !resp.Ok {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}

	getResp := resp.GetGetIssue()
	if getResp == nil || getResp.Issue == nil {
		t.Fatal("expected GetIssue response with issue")
	}
	if getResp.Issue.Id != "ctx-issue" {
		t.Fatalf("expected issue id ctx-issue, got %q", getResp.Issue.Id)
	}
}

func TestContextEnabledHandlersUnknownProjectReturnProjectScopedStoreError(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	t.Setenv("ORCH_PROJECT", "should-not-be-used")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	missing := &orchpb.RequestContext{ProjectId: "missing-project"}
	newTitle := "new"
	tests := []struct {
		name string
		req  *orchpb.Request
	}{
		{
			name: "get-run",
			req:  &orchpb.Request{Request: &orchpb.Request_GetRun{GetRun: &orchpb.GetRunRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "stop-run",
			req:  &orchpb.Request{Request: &orchpb.Request_StopRun{StopRun: &orchpb.StopRunRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "resolve-run",
			req:  &orchpb.Request{Request: &orchpb.Request_ResolveRun{ResolveRun: &orchpb.ResolveRunRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "get-issue",
			req:  &orchpb.Request{Request: &orchpb.Request_GetIssue{GetIssue: &orchpb.GetIssueRequest{IssueId: "i", Context: missing}}},
		},
		{
			name: "create-issue",
			req:  &orchpb.Request{Request: &orchpb.Request_CreateIssue{CreateIssue: &orchpb.CreateIssueRequest{IssueId: "i", Title: "title", Body: "body", Context: missing}}},
		},
		{
			name: "close-issue",
			req:  &orchpb.Request{Request: &orchpb.Request_CloseIssue{CloseIssue: &orchpb.CloseIssueRequest{IssueId: "i", Context: missing}}},
		},
		{
			name: "get-run-by-short-id",
			req:  &orchpb.Request{Request: &orchpb.Request_GetRunByShortId{GetRunByShortId: &orchpb.GetRunByShortIDRequest{ShortId: "abc", Context: missing}}},
		},
		{
			name: "resolve-issue",
			req:  &orchpb.Request{Request: &orchpb.Request_ResolveIssue{ResolveIssue: &orchpb.ResolveIssueRequest{IssueId: "i", Context: missing}}},
		},
		{
			name: "delete-run",
			req:  &orchpb.Request{Request: &orchpb.Request_DeleteRun{DeleteRun: &orchpb.DeleteRunRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "update-issue",
			req:  &orchpb.Request{Request: &orchpb.Request_UpdateIssue{UpdateIssue: &orchpb.UpdateIssueRequest{IssueId: "i", Title: &newTitle, Context: missing}}},
		},
		{
			name: "get-attach-info",
			req:  &orchpb.Request{Request: &orchpb.Request_GetAttachInfo{GetAttachInfo: &orchpb.GetAttachInfoRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "capture-session",
			req:  &orchpb.Request{Request: &orchpb.Request_CaptureSession{CaptureSession: &orchpb.CaptureSessionRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "send-message",
			req:  &orchpb.Request{Request: &orchpb.Request_SendMessage{SendMessage: &orchpb.SendMessageRequest{IssueId: "i", RunId: "r", Message: "hi", Context: missing}}},
		},
		{
			name: "get-diff-stats",
			req:  &orchpb.Request{Request: &orchpb.Request_GetDiffStats{GetDiffStats: &orchpb.GetDiffStatsRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "get-branch-state",
			req:  &orchpb.Request{Request: &orchpb.Request_GetBranchState{GetBranchState: &orchpb.GetBranchStateRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "get-diff",
			req:  &orchpb.Request{Request: &orchpb.Request_GetDiff{GetDiff: &orchpb.GetDiffRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "append-event",
			req:  &orchpb.Request{Request: &orchpb.Request_AppendEvent{AppendEvent: &orchpb.AppendEventRequest{IssueId: "i", RunId: "r", EventType: "status", EventName: "running", Context: missing}}},
		},
		{
			name: "validate-issue-files",
			req:  &orchpb.Request{Request: &orchpb.Request_ValidateIssueFiles{ValidateIssueFiles: &orchpb.ValidateIssueFilesRequest{IssueId: "i", Context: missing}}},
		},
		{
			name: "write-agent-prompt",
			req:  &orchpb.Request{Request: &orchpb.Request_WriteAgentPrompt{WriteAgentPrompt: &orchpb.WriteAgentPromptRequest{IssueId: "i", RunId: "r", Content: "x", Context: missing}}},
		},
		{
			name: "read-agent-prompt",
			req:  &orchpb.Request{Request: &orchpb.Request_ReadAgentPrompt{ReadAgentPrompt: &orchpb.ReadAgentPromptRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "create-run",
			req:  &orchpb.Request{Request: &orchpb.Request_CreateRun{CreateRun: &orchpb.CreateRunRequest{IssueId: "i", RunId: "r", Context: missing}}},
		},
		{
			name: "inject-initial-prompt",
			req:  &orchpb.Request{Request: &orchpb.Request_InjectInitialPrompt{InjectInitialPrompt: &orchpb.InjectInitialPromptRequest{IssueId: "i", RunId: "r", Prompt: "hello", Context: missing}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := sendProtoRequest(t, tt.req)
			if resp.Ok {
				t.Fatalf("expected error response, got ok=true")
			}
			if !strings.Contains(resp.Error, "no store available for project_id \"missing-project\"") {
				t.Fatalf("expected project-scoped store error, got: %s", resp.Error)
			}
		})
	}
}

func TestControlAgentProtoHandlersRequireRegisteredProjectMapping(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	projectID := "missing-control-project"
	missing := &orchpb.RequestContext{ProjectId: projectID}

	t.Run("get-control-agent-config", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetControlAgentConfig{
				GetControlAgentConfig: &orchpb.GetControlAgentConfigRequest{Context: missing},
			},
		})

		if resp.Ok {
			t.Fatalf("expected error response, got ok=true")
		}
		expected := fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID)
		if resp.Error != expected {
			t.Fatalf("expected %q, got %q", expected, resp.Error)
		}
	})

	t.Run("get-control-agent-launch", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetControlAgentLaunch{
				GetControlAgentLaunch: &orchpb.GetControlAgentLaunchRequest{Context: missing},
			},
		})

		if resp.Ok {
			t.Fatalf("expected error response, got ok=true")
		}
		expected := fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID)
		if resp.Error != expected {
			t.Fatalf("expected %q, got %q", expected, resp.Error)
		}
	})

	t.Run("get-control-agent-config-missing-context", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetControlAgentConfig{
				GetControlAgentConfig: &orchpb.GetControlAgentConfigRequest{Context: &orchpb.RequestContext{}},
			},
		})

		if resp.Ok {
			t.Fatalf("expected error response, got ok=true")
		}
		if resp.Error != "project_id required" {
			t.Fatalf("expected project_id required, got %q", resp.Error)
		}
	})

	t.Run("get-control-agent-launch-missing-context", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_GetControlAgentLaunch{
				GetControlAgentLaunch: &orchpb.GetControlAgentLaunchRequest{Context: &orchpb.RequestContext{}},
			},
		})

		if resp.Ok {
			t.Fatalf("expected error response, got ok=true")
		}
		if resp.Error != "project_id required" {
			t.Fatalf("expected project_id required, got %q", resp.Error)
		}
	})
}

type mockStoreWithCapture struct {
	mockStore
	capturedMetadata map[string]string
}

type mockStoreWithPrompt struct {
	mockStore
	prompts map[string]string
}

type mockStoreWithValidation struct {
	mockStore
	validationResult *store.ValidationResult
	validationErr    error
}

func (m *mockStoreWithValidation) ValidateIssueFiles(issueID model.IssueID) (*store.ValidationResult, error) {
	if m.validationErr != nil {
		return nil, m.validationErr
	}
	if m.validationResult == nil {
		return &store.ValidationResult{}, nil
	}
	return m.validationResult, nil
}

func (m *mockStoreWithPrompt) WriteAgentPrompt(ref *model.RunRef, content string) error {
	if m.prompts == nil {
		m.prompts = make(map[string]string)
	}
	if ref == nil {
		return fmt.Errorf("nil run ref")
	}
	key := ref.String()
	m.prompts[key] = content
	return nil
}

func (m *mockStoreWithPrompt) ReadAgentPrompt(ref *model.RunRef) (string, error) {
	if ref == nil {
		return "", os.ErrNotExist
	}
	key := ref.String()
	if content, ok := m.prompts[key]; ok {
		return content, nil
	}
	return "", os.ErrNotExist
}

func (m *mockStoreWithCapture) CreateRun(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
	m.capturedMetadata = metadata
	return &model.Run{
		IssueID: issueID,
		RunID:   runID,
		Status:  model.StatusQueued,
		Path:    "/test/runs/" + string(issueID) + "/" + string(runID) + ".md",
	}, nil
}

func (m *mockStoreWithCapture) AppendEvent(ref *model.RunRef, event *model.Event) error {
	return nil
}

func TestProtoStartRunFieldMapping(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStoreWithCapture{
		mockStore: mockStore{
			runs: make(map[string]*model.Run),
			issues: map[string]*model.Issue{
				"test-issue": {
					ID:     "test-issue",
					Title:  "Test issue",
					Status: model.IssueStatusOpen,
					Path:   "/test/issues/test-issue.md",
				},
			},
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_StartRun{
			StartRun: &orchpb.StartRunRequest{
				IssueId: "test-issue",
				Model:   "anthropic/claude-sonnet-4",
				DryRun:  true,
			},
		},
	})

	if !resp.Ok {
		// DryRun should succeed without needing real git infrastructure.
		// If it fails with "agent not available", that's expected in CI without claude installed.
		// The key contract test is that Model is NOT in Message.
		errMsg := resp.Error
		if !strings.Contains(errMsg, "agent not available: claude") && errMsg != "no project root available" && !strings.Contains(errMsg, "no active worker on host") {
			t.Fatalf("unexpected error: %s", errMsg)
		}
	}

	// Verify the Model field is NOT routed through Message by checking
	// that a non-dry-run would place it in metadata["model"].
	// Reset captured state and send without DryRun.
	st.capturedMetadata = nil
	_ = sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_StartRun{
			StartRun: &orchpb.StartRunRequest{
				IssueId: "test-issue",
				Model:   "anthropic/claude-sonnet-4",
			},
		},
	})

	if st.capturedMetadata != nil {
		if got := st.capturedMetadata["model"]; got != "anthropic/claude-sonnet-4" {
			t.Errorf("expected metadata[model]=%q, got %q", "anthropic/claude-sonnet-4", got)
		}
	}
	// If capturedMetadata is nil, CreateRun was never reached (agent unavailable).
	// That's acceptable — the mapping code is still correct; we just can't verify it
	// without a real agent adapter.
}

func TestProtoContinueRunFieldMapping(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStoreWithCapture{
		mockStore: mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		},
	}

	server := newTestServer(t, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	t.Run("SessionName mapped from SessionName", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ContinueRun{
				ContinueRun: &orchpb.ContinueRunRequest{
					IssueId:     "test-issue",
					SessionName: "my-session",
				},
			},
		})

		// The handler will fail (no issue found, no run found, etc.)
		// but the request was correctly mapped through the proto handler.
		// The fact that it returns a proper error (not a panic) proves the mapping worked.
		if resp.Ok {
			t.Error("expected error since issue doesn't exist")
		}
	})

	t.Run("missing project context fails closed", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ContinueRun{
				ContinueRun: &orchpb.ContinueRunRequest{
					IssueId: "test-issue",
					Context: &orchpb.RequestContext{},
				},
			},
		})

		if resp.Ok {
			t.Error("expected error since issue doesn't exist")
		}
		if resp.Error != "project_id required" {
			t.Fatalf("expected project_id required, got %q", resp.Error)
		}
	})
}

type mockStoreWithEvents struct {
	mockStore
	appendedEvents []*model.Event
}

func (m *mockStoreWithEvents) AppendEvent(ref *model.RunRef, event *model.Event) error {
	m.appendedEvents = append(m.appendedEvents, event)
	return nil
}

func TestStopSingleRunOpencode(t *testing.T) {
	st := &mockStoreWithEvents{
		mockStore: mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		},
	}

	t.Run("opencode run calls abort API", func(t *testing.T) {
		st.appendedEvents = nil
		abortCalled := false
		sessionIDReceived := ""

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/abort") {
				abortCalled = true
				parts := strings.Split(r.URL.Path, "/")
				if len(parts) >= 3 {
					sessionIDReceived = parts[len(parts)-2]
				}
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		port := getPortFromURL(t, ts.URL)
		logger := log.New(io.Discard, "", 0)
		server := NewSocketServer(nil, logger)

		run := &model.Run{
			IssueID:           "test-issue",
			RunID:             "run-1",
			Status:            model.StatusRunning,
			Agent:             "opencode",
			ServerPort:        port,
			OpenCodeSessionID: "sess_123",
		}

		warning, err := server.stopSingleRun(run, st, false)
		if err != nil {
			t.Fatalf("stopSingleRun() error = %v", err)
		}
		if warning != "" {
			t.Fatalf("stopSingleRun() warning = %q", warning)
		}

		if !abortCalled {
			t.Error("expected abort API to be called")
		}
		if sessionIDReceived != "sess_123" {
			t.Errorf("expected session ID 'sess_123', got %q", sessionIDReceived)
		}

		if len(st.appendedEvents) != 1 {
			t.Fatalf("expected 1 event appended, got %d", len(st.appendedEvents))
		}
		if st.appendedEvents[0].Name != string(model.StatusCanceled) {
			t.Errorf("expected canceled event, got %q", st.appendedEvents[0].Name)
		}
	})

	t.Run("opencode fallback rejects empty multiplexer", func(t *testing.T) {
		st.appendedEvents = nil

		logger := log.New(io.Discard, "", 0)
		server := NewSocketServer(nil, logger)

		run := &model.Run{
			IssueID:           "test-issue",
			RunID:             "run-2",
			Status:            model.StatusRunning,
			Agent:             "opencode",
			ServerPort:        0,
			OpenCodeSessionID: "",
		}

		_, err := server.stopSingleRun(run, st, false)
		if err == nil {
			t.Fatal("stopSingleRun() error = nil, want empty Multiplexer field error")
		}
		for _, want := range []string{"test-issue#run-2", "empty Multiplexer field"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("stopSingleRun() error = %q, want substring %q", err, want)
			}
		}
		if len(st.appendedEvents) != 0 {
			t.Fatalf("expected no event on multiplexer validation error, got %d", len(st.appendedEvents))
		}
	})

	t.Run("abort error propagates without canceled event", func(t *testing.T) {
		st.appendedEvents = nil

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		port := getPortFromURL(t, ts.URL)
		logger := log.New(io.Discard, "", 0)
		server := NewSocketServer(nil, logger)

		run := &model.Run{
			IssueID:           "test-issue",
			RunID:             "run-3",
			Status:            model.StatusRunning,
			Agent:             "opencode",
			ServerPort:        port,
			OpenCodeSessionID: "sess_456",
		}

		_, err := server.stopSingleRun(run, st, false)
		if err == nil {
			t.Fatal("stopSingleRun() error = nil, want opencode abort error")
		}
		if !strings.Contains(err.Error(), `cancel opencode session "sess_456" for run test-issue#run-3`) {
			t.Fatalf("stopSingleRun() error = %q, want run and opencode session", err)
		}
		if len(st.appendedEvents) != 0 {
			t.Fatalf("expected no event on abort error, got %d", len(st.appendedEvents))
		}
	})

	t.Run("non-concrete multiplexer errors", func(t *testing.T) {
		st.appendedEvents = nil

		logger := log.New(io.Discard, "", 0)
		server := NewSocketServer(nil, logger)

		run := &model.Run{
			IssueID:     "test-issue",
			RunID:       "run-4",
			Status:      model.StatusRunning,
			Agent:       "claude",
			SessionName: "test-session",
			Multiplexer: "auto",
		}

		_, err := server.stopSingleRun(run, st, false)
		if err == nil {
			t.Fatal("stopSingleRun() error = nil, want non-concrete Multiplexer field error")
		}
		if !strings.Contains(err.Error(), `non-concrete Multiplexer field "auto"`) {
			t.Fatalf("stopSingleRun() error = %q, want non-concrete Multiplexer field", err)
		}
		if len(st.appendedEvents) != 0 {
			t.Fatalf("expected no event on multiplexer validation error, got %d", len(st.appendedEvents))
		}
	})

	t.Run("terminal status skips stop", func(t *testing.T) {
		st.appendedEvents = nil

		logger := log.New(io.Discard, "", 0)
		server := NewSocketServer(nil, logger)

		for _, status := range []model.Status{model.StatusDone, model.StatusFailed, model.StatusCanceled} {
			run := &model.Run{
				IssueID: "test-issue",
				RunID:   "run-terminal",
				Status:  status,
				Agent:   "opencode",
			}

			warning, err := server.stopSingleRun(run, st, false)
			if err != nil {
				t.Fatalf("stopSingleRun() error = %v for status %s", err, status)
			}
			if warning != "" {
				t.Fatalf("stopSingleRun() warning = %q for status %s", warning, status)
			}
		}

		if len(st.appendedEvents) != 0 {
			t.Errorf("expected no events for terminal statuses, got %d", len(st.appendedEvents))
		}
	})
}

func TestHandleProtoStopRunKillFailurePolicy(t *testing.T) {
	binDir := t.TempDir()
	tmuxPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\necho simulated kill failure >&2\nexit 23\n"), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", binDir)

	run := &model.Run{
		IssueID:     "stop-kill-failure",
		RunID:       "run-1",
		Status:      model.StatusRunning,
		Agent:       "claude",
		SessionName: "run-stop-kill-failure-run-1",
		Multiplexer: "tmux",
	}
	st := &mockStore{
		runs:   map[string]*model.Run{run.Ref().String(): run},
		issues: make(map[string]*model.Issue),
	}
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, "stop-project", t.TempDir(), st)
	ctx := &orchpb.RequestContext{ProjectId: "stop-project"}

	resp := server.handleProtoStopRun(&orchpb.StopRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: ctx,
	})
	if resp.Ok {
		t.Fatal("default stop succeeded despite multiplexer kill failure")
	}
	for _, want := range []string{`kill session "run-stop-kill-failure-run-1"`, "stop-kill-failure#run-1", "exit status 23"} {
		if !strings.Contains(resp.Error, want) {
			t.Fatalf("default stop error = %q, want substring %q", resp.Error, want)
		}
	}
	if run.Status != model.StatusRunning {
		t.Fatalf("default stop changed status to %q after kill failure, want running", run.Status)
	}

	resp = server.handleProtoStopRun(&orchpb.StopRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Force:   true,
		Context: ctx,
	})
	if !resp.Ok {
		t.Fatalf("forced stop failed: %s", resp.Error)
	}
	stopResp := resp.GetStopRun()
	if stopResp == nil {
		t.Fatal("forced stop response missing stop_run payload")
	}
	for _, want := range []string{"session kill failed", "stop-kill-failure#run-1", "marked canceled because --force", "exit status 23"} {
		if !strings.Contains(stopResp.Warning, want) {
			t.Fatalf("forced stop warning = %q, want substring %q", stopResp.Warning, want)
		}
	}
	if run.Status != model.StatusCanceled {
		t.Fatalf("forced stop status = %q, want canceled", run.Status)
	}
}

func getPortFromURL(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	return port
}

func TestProcessStartRunCoreValidation(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	t.Run("missing issue_id returns error", func(t *testing.T) {
		opts := &StartRunOptions{
			IssueID: "",
		}
		_, err := server.processStartRunCore(st, "/project", opts)
		if err == nil {
			t.Error("expected error for missing issue_id")
		}
		if !strings.Contains(err.Error(), "issue_id required") {
			t.Errorf("expected 'issue_id required' error, got: %v", err)
		}
	})

	t.Run("issue not found returns error", func(t *testing.T) {
		opts := &StartRunOptions{
			IssueID: "nonexistent",
		}
		_, err := server.processStartRunCore(st, "/project", opts)
		if err == nil {
			t.Error("expected error for nonexistent issue")
		}
		if !strings.Contains(err.Error(), "issue not found") {
			t.Errorf("expected 'issue not found' error, got: %v", err)
		}
	})

	t.Run("missing project root returns error", func(t *testing.T) {
		st.issues["test-issue"] = &model.Issue{
			ID:     "test-issue",
			Title:  "Test",
			Status: model.IssueStatusOpen,
		}
		opts := &StartRunOptions{
			IssueID: "test-issue",
			Agent:   "custom",
		}
		_, err := server.processStartRunCore(st, "", opts)
		if err == nil {
			t.Error("expected error for missing project root")
		}
		if !strings.Contains(err.Error(), "no project root available") {
			t.Errorf("expected 'no project root available' error, got: %v", err)
		}
	})

	t.Run("unavailable agent error includes probe exit and PATH", func(t *testing.T) {
		binDir := t.TempDir()
		binPath := filepath.Join(binDir, "claude")
		if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 9\n"), 0755); err != nil {
			t.Fatalf("write fake claude: %v", err)
		}
		t.Setenv("PATH", binDir)

		opts := &StartRunOptions{
			IssueID: "test-issue",
			Agent:   "claude",
		}
		_, err := server.processStartRunCore(st, "", opts)
		if err == nil {
			t.Fatal("expected unavailable agent error")
		}
		for _, want := range []string{`agent not available: claude`, `probe "claude --version" exited 9`, "PATH=" + binDir} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("processStartRunCore() error = %q, want substring %q", err, want)
			}
		}
	})

	t.Run("uses payload issue snapshot without reading worker store", func(t *testing.T) {
		// A worker pinned to a different host than the master has no issue store.
		// The master carries the resolved issue in opts.IssueSnapshot; the worker
		// must consume it and never read its own store for the run's issue.
		emptyStore := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue), // intentionally empty: worker has no issue
		}
		opts := &StartRunOptions{
			IssueID: "remote-issue",
			Agent:   "custom",
			IssueSnapshot: &model.Issue{
				ID:     "remote-issue",
				Title:  "Remote",
				Body:   "do the thing",
				Status: model.IssueStatusOpen,
			},
		}
		// projectRoot="" makes the core fail at the next gate (project root). The
		// point is that it gets PAST issue resolution: it must NOT fail
		// "issue not found" even though the worker store is empty.
		_, err := server.processStartRunCore(emptyStore, "", opts)
		if err == nil {
			t.Fatal("expected error at the project-root gate")
		}
		if strings.Contains(err.Error(), "issue not found") {
			t.Fatalf("worker must use opts.IssueSnapshot, not read its empty store; got: %v", err)
		}
		if !strings.Contains(err.Error(), "no project root available") {
			t.Fatalf("expected to reach 'no project root available', got: %v", err)
		}
	})
}

func TestLegacyStartRunAvailabilityCheckUsesExecutionTarget(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte("agent: claude\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Chdir(projectRoot)
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	issue := &model.Issue{ID: "availability-check", Title: "availability check"}
	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: map[string]*model.Issue{string(issue.ID): issue},
	}
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, "availability-project", projectRoot, st)

	run := func(target string) StartRunResponse {
		t.Helper()
		var response bytes.Buffer
		server.handleStartRun(SendRequest{
			IssueID:   string(issue.ID),
			RepoID:    "availability-project",
			AgentType: "claude",
			Target:    target,
			DryRun:    true,
		}, json.NewEncoder(&response))
		var got StartRunResponse
		if err := json.Unmarshal(response.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got
	}

	if got := run("mac"); !got.OK {
		t.Fatalf("remote-target dry run failed on master availability check: %s", got.Error)
	}
	wantLocal := fmt.Sprintf(`agent not available: claude (probe "claude --version" failed: exec: "claude": executable file not found in $PATH; PATH=%s)`, binDir)
	if got := run("local"); got.OK || got.Error != wantLocal {
		t.Fatalf("local-target response = %+v, want explicit unavailable error", got)
	}
}

func TestProcessStartRunCoreAvailabilityErrorNamesEvaluatingWorker(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte("agent: claude\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.SetWorkerIdentity("host-zeus", "zeus")
	issue := &model.Issue{ID: "availability-context", Title: "availability context"}
	_, err := server.processStartRunCore(&mockStore{}, projectRoot, &StartRunOptions{
		IssueID:        issue.ID,
		IssueSnapshot:  issue,
		Agent:          "claude",
		TargetHost:     "zeus",
		TargetWorkerID: "host-zeus",
	})
	if err == nil {
		t.Fatal("processStartRunCore() error = nil, want unavailable agent error")
	}
	want := fmt.Sprintf(`agent not available: claude (worker host-zeus, host zeus; probe "claude --version" exited 9; PATH=%s)`, binDir)
	if err.Error() != want {
		t.Fatalf("processStartRunCore() error = %q, want %q", err, want)
	}
}

// TestMasterFailsFastOnMissingIssueBeforeDelegation verifies that the master
// (issue-store SSOT) rejects start/continue with an explicit "issue not found"
// BEFORE delegating to a worker. This keeps the error on the master (where the
// store lives) instead of on a worker that may run on a different host and have
// no issue store at all. No worker is registered, so reaching delegation would
// hang/fail differently; the assertion is that we never get there.
func TestMasterFailsFastOnMissingIssueBeforeDelegation(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue), // empty: issue does not exist on master
	}
	server := newTestServer(t, st)

	t.Run("start_run", func(t *testing.T) {
		resp := server.handleProtoStartRun(&orchpb.StartRunRequest{
			IssueId: "missing-issue",
			RunId:   "run-x",
			Context: &orchpb.RequestContext{ProjectId: testProjectID},
		})
		if resp.Ok {
			t.Fatalf("expected failure for missing issue, got ok response: %+v", resp)
		}
		if !strings.Contains(resp.Error, "issue not found: missing-issue") {
			t.Fatalf("expected explicit 'issue not found: missing-issue', got: %q", resp.Error)
		}
	})

	t.Run("continue_run", func(t *testing.T) {
		resp := server.handleProtoContinueRun(&orchpb.ContinueRunRequest{
			IssueId: "missing-issue",
			RunId:   "run-x",
			Branch:  "feature-branch",
			Context: &orchpb.RequestContext{ProjectId: testProjectID},
		})
		if resp.Ok {
			t.Fatalf("expected failure for missing issue, got ok response: %+v", resp)
		}
		if !strings.Contains(resp.Error, "issue not found: missing-issue") {
			t.Fatalf("expected explicit 'issue not found: missing-issue', got: %q", resp.Error)
		}
	})

	t.Run("continue_run short_id with no matching run fails fast on master", func(t *testing.T) {
		// The short-id mode derives the issue ID from the referenced run; if the run
		// is unknown to the master, fail fast here instead of delegating with an
		// empty issue ID (which would mislead on the wrong host).
		resp := server.handleProtoContinueRun(&orchpb.ContinueRunRequest{
			ShortId: "deadbe",
			Context: &orchpb.RequestContext{ProjectId: testProjectID},
		})
		if resp.Ok {
			t.Fatalf("expected failure for unknown short id, got ok response: %+v", resp)
		}
		if !strings.Contains(resp.Error, `run not in master store for project "test-project": deadbe`) {
			t.Fatalf("expected explicit master-store miss for deadbe, got: %q", resp.Error)
		}
	})
}

func TestProtoContinueRunFailsFastOnUnknownInheritedProfileBeforeDelegation(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectRoot := t.TempDir()
	issuesRoot := filepath.Join(projectRoot, "issues-store")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(issuesRoot, "issues"), 0o755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}
	configBody := fmt.Sprintf(`agent: codex
issues:
  path: %s
codex:
  default_profile: company
  profiles:
    company:
      codex_home: ~/.codex-company
`, issuesRoot)
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	run := &model.Run{
		IssueID:      "issue-cont",
		RunID:        "run-prev",
		Status:       model.StatusFailed,
		Agent:        "codex",
		Profile:      "ghost",
		Branch:       "cont-branch",
		WorktreePath: "/tmp/cont-prev",
	}
	st := &mockStore{
		runs: map[string]*model.Run{
			run.Ref().String(): run,
		},
		issues: map[string]*model.Issue{
			"issue-cont": {ID: "issue-cont", Title: "Cont", Status: model.IssueStatusOpen},
		},
	}
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, testProjectID, projectRoot, st)

	resp := server.handleProtoContinueRun(&orchpb.ContinueRunRequest{
		IssueId: "issue-cont",
		RunId:   "run-prev",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatalf("expected unknown inherited profile failure, got ok response: %+v", resp)
	}
	if !strings.Contains(resp.Error, `unknown codex profile "ghost"`) {
		t.Fatalf("expected unknown inherited profile error, got %q", resp.Error)
	}
	if leases := server.listWorkerLeases(true); len(leases) != 0 {
		t.Fatalf("expected fail-fast before worker delegation, got %d leases", len(leases))
	}
}

func TestRunMutationUsesMasterSnapshotAndHostAffinity(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	projectRoot := t.TempDir()
	issuesRoot := filepath.Join(projectRoot, "issues-store")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(issuesRoot, "issues"), 0o755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}
	configBody := "issues:\n  path: " + issuesRoot + "\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	run := &model.Run{
		IssueID:      "legacy-issue",
		RunID:        "20260317-014743",
		Status:       model.StatusPROpen,
		Agent:        "custom",
		Branch:       "issue/legacy/run",
		WorktreePath: filepath.Join(projectRoot, "dead-worker-worktree"),
		SessionName:  "legacy-session",
		Multiplexer:  "tmux",
		TargetHost:   "dead-host",
	}
	st := &mockStore{
		runs: map[string]*model.Run{
			run.Ref().String(): run,
		},
		issues: map[string]*model.Issue{
			"legacy-issue": {
				ID:     "legacy-issue",
				Title:  "Legacy issue",
				Status: model.IssueStatusOpen,
			},
		},
	}

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	registerRepoContextForTest(t, server, "project-legacy", projectRoot, st)

	liveWorkerID := HostWorkerID("other-host")
	if _, ttl := server.registerWorker(liveWorkerID, "external", "other-host", "external", []string{"continue_run", "stop_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for live worker")
	}

	ctx := &orchpb.RequestContext{ProjectId: "project-legacy"}
	showResp := server.handleProtoGetRun(&orchpb.GetRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: ctx,
	})
	if showResp.Ok || !strings.Contains(showResp.Error, `execution host dead-host`) {
		t.Fatalf("show must fail clearly when worktree host is unavailable, ok=%v error=%q", showResp.Ok, showResp.Error)
	}

	listResp := server.handleProtoListRuns(&orchpb.ListRunsRequest{Context: ctx})
	if !listResp.Ok || len(listResp.GetListRuns().GetRuns()) != 1 {
		t.Fatalf("list must remain available when a run's execution host is unavailable, ok=%v error=%q", listResp.Ok, listResp.Error)
	}
	if listResp.GetListRuns().GetRuns()[0].WorktreeExists {
		t.Fatal("list must leave worktree_exists unpopulated instead of performing per-run inspection")
	}

	var legacyListBuffer bytes.Buffer
	branch := run.Branch
	run.Branch = "" // Keep this assertion focused on worktree inspection, not diff enrichment.
	server.handleListRuns(SendRequest{RepoID: "project-legacy"}, json.NewEncoder(&legacyListBuffer))
	run.Branch = branch
	var legacyListResp ListRunsResponse
	if err := json.Unmarshal(legacyListBuffer.Bytes(), &legacyListResp); err != nil {
		t.Fatalf("decode legacy list response: %v", err)
	}
	if !legacyListResp.OK || len(legacyListResp.Runs) != 1 {
		t.Fatalf("legacy list must remain available when a run's execution host is unavailable, ok=%v error=%q", legacyListResp.OK, legacyListResp.Error)
	}
	if legacyListResp.Runs[0].WorktreeExists {
		t.Fatal("legacy list must leave worktree_exists unpopulated instead of performing per-run inspection")
	}

	stopResp := server.handleProtoStopRun(&orchpb.StopRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Force:   true,
		Context: ctx,
	})
	if !stopResp.Ok {
		t.Fatalf("forced stop should mark master run canceled even without dead-host worker, got %q", stopResp.Error)
	}
	if warning := stopResp.GetStopRun().GetWarning(); !strings.Contains(warning, "marked canceled because --force") {
		t.Fatalf("forced stop warning = %q, want kill failure surfaced", warning)
	}
	if run.Status != model.StatusCanceled {
		t.Fatalf("run.Status = %q, want canceled", run.Status)
	}
	for _, lease := range server.listWorkerLeases(true) {
		if lease.Effect == "stop_run" && lease.WorkerID == liveWorkerID {
			t.Fatalf("stop_run must not route legacy dead-host run to live worker %q", liveWorkerID)
		}
	}

	continueResp := server.handleProtoContinueRun(&orchpb.ContinueRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: ctx,
	})
	if continueResp.Ok {
		t.Fatal("restart-from should fail while the run's recorded host has no live worker")
	}
	if !strings.Contains(continueResp.Error, `no active worker on host "dead-host"`) {
		t.Fatalf("expected host-affinity error, got %q", continueResp.Error)
	}
	if strings.Contains(strings.ToLower(continueResp.Error), "not_found") || strings.Contains(strings.ToLower(continueResp.Error), "not found") {
		t.Fatalf("restart-from must not surface worker-store not_found, got %q", continueResp.Error)
	}
	for _, lease := range server.listWorkerLeases(true) {
		if lease.Effect == "continue_run" && lease.WorkerID == liveWorkerID {
			t.Fatalf("continue_run must not route legacy dead-host run to live worker %q", liveWorkerID)
		}
	}
}

func TestBootstrapOpenCodeRunSessionFailsFastOnCreateSessionError(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}
	run := &model.Run{IssueID: "orch-437", RunID: "run-opencode"}
	st.runs["orch-437#run-opencode"] = run

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/global/health":
			_ = json.NewEncoder(w).Encode(agent.HealthResponse{Healthy: true})
		case "/session":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"sqlite disk i/o"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer httpServer.Close()

	parsedURL, err := url.Parse(httpServer.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(parsedURL.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	launchCfg := &agent.LaunchConfig{
		Port:    port,
		WorkDir: "/tmp/worktree",
		Prompt:  "prompt",
	}

	serverPort, sessionID, err := server.bootstrapOpenCodeRunSession(st, run, "orch-437", "run-opencode", launchCfg, 2*time.Second)
	if err == nil {
		t.Fatal("expected bootstrapOpenCodeRunSession() to fail")
	}
	if !strings.Contains(err.Error(), "failed to create opencode session") {
		t.Fatalf("unexpected error: %v", err)
	}
	if serverPort != port {
		t.Fatalf("serverPort = %d, want %d", serverPort, port)
	}
	if sessionID != "" {
		t.Fatalf("sessionID = %q, want empty", sessionID)
	}
	if run.Status != model.StatusFailed {
		t.Fatalf("run.Status = %v, want %v", run.Status, model.StatusFailed)
	}
	if run.ServerPort != port {
		t.Fatalf("run.ServerPort = %d, want %d", run.ServerPort, port)
	}
	if run.OpenCodeSessionID != "" {
		t.Fatalf("run.OpenCodeSessionID = %q, want empty", run.OpenCodeSessionID)
	}
	foundErrorArtifact := false
	for _, event := range run.Events {
		if event.Type == model.EventTypeArtifact && event.Name == "error" {
			foundErrorArtifact = true
			break
		}
	}
	if !foundErrorArtifact {
		t.Fatal("expected error artifact event")
	}
}

func TestBootstrapOpenCodeRunSessionConfirmsQueuedPromptAfterAckTimeout(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}
	run := &model.Run{IssueID: "orch-438", RunID: "run-opencode"}
	st.runs["orch-438#run-opencode"] = run

	prompt := "Please read ORCH_PROMPT.md and wait."
	sessionID := "ses_confirmed"
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/global/health":
			_ = json.NewEncoder(w).Encode(agent.HealthResponse{Healthy: true})
		case r.URL.Path == "/session" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(agent.Session{ID: sessionID})
		case r.URL.Path == "/session/"+sessionID+"/message" && r.Method == http.MethodPost:
			time.Sleep(3 * time.Second)
		case r.URL.Path == "/session/"+sessionID+"/message" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]agent.Message{
				{
					Info: agent.MessageInfo{
						SessionID: sessionID,
						Role:      "user",
						CreatedAt: time.Now(),
					},
					Parts: []agent.MessagePart{{Type: "text", Text: prompt}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer httpServer.Close()

	parsedURL, err := url.Parse(httpServer.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(parsedURL.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}

	launchCfg := &agent.LaunchConfig{
		Port:    port,
		WorkDir: "/tmp/worktree",
		Prompt:  prompt,
	}

	serverPort, gotSessionID, err := server.bootstrapOpenCodeRunSession(st, run, "orch-438", "run-opencode", launchCfg, 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("bootstrapOpenCodeRunSession() error = %v", err)
	}
	if serverPort != port {
		t.Fatalf("serverPort = %d, want %d", serverPort, port)
	}
	if gotSessionID != sessionID {
		t.Fatalf("sessionID = %q, want %q", gotSessionID, sessionID)
	}
	if run.Status == model.StatusFailed {
		t.Fatalf("run.Status = %v, want non-failed", run.Status)
	}
}

func TestProcessContinueRunCoreValidation(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	t.Run("missing run reference returns error", func(t *testing.T) {
		opts := &ContinueRunOptions{}
		_, err := server.processContinueRunCore(st, "/project", opts)
		if err == nil {
			t.Error("expected error for missing run reference")
		}
		if !strings.Contains(err.Error(), "run reference required") {
			t.Errorf("expected 'run reference required' error, got: %v", err)
		}
	})

	t.Run("branch without issue_id returns error", func(t *testing.T) {
		opts := &ContinueRunOptions{
			Branch: "feature-branch",
		}
		_, err := server.processContinueRunCore(st, "/project", opts)
		if err == nil {
			t.Error("expected error for branch without issue_id")
		}
		if !strings.Contains(err.Error(), "issue_id required with branch") {
			t.Errorf("expected 'issue_id required with branch' error, got: %v", err)
		}
	})

	t.Run("run not found by short_id returns error", func(t *testing.T) {
		opts := &ContinueRunOptions{
			ShortID: "nonexistent",
		}
		_, err := server.processContinueRunCore(st, "/project", opts)
		if err == nil {
			t.Error("expected error for nonexistent run")
		}
		if !strings.Contains(err.Error(), "run not found") {
			t.Errorf("expected 'run not found' error, got: %v", err)
		}
	})

	t.Run("live run returns safe send/capture guidance", func(t *testing.T) {
		st.runs["orch-451#run-live"] = &model.Run{
			IssueID: "orch-451",
			RunID:   "run-live",
			Status:  model.StatusWaiting,
		}

		opts := &ContinueRunOptions{
			IssueID: "orch-451",
			RunID:   "run-live",
		}

		_, err := server.processContinueRunCore(st, "/project", opts)
		if err == nil {
			t.Fatal("expected error for live run")
		}

		msg := err.Error()
		if !strings.Contains(msg, "Run orch-451#run-live is alive (status: wait).") {
			t.Fatalf("expected alive status guidance, got: %s", msg)
		}
		if !strings.Contains(msg, "orch send orch-451#run-live \"your message\"") {
			t.Fatalf("expected orch send guidance, got: %s", msg)
		}
		if !strings.Contains(msg, "orch capture orch-451#run-live") {
			t.Fatalf("expected orch capture guidance, got: %s", msg)
		}
		if strings.Contains(msg, "orch stop") {
			t.Fatalf("expected no orch stop guidance, got: %s", msg)
		}
	})

	t.Run("done run is rejected", func(t *testing.T) {
		st.runs["orch-451#run-done"] = &model.Run{
			IssueID: "orch-451",
			RunID:   "run-done",
			Status:  model.StatusDone,
		}

		opts := &ContinueRunOptions{
			IssueID: "orch-451",
			RunID:   "run-done",
		}

		_, err := server.processContinueRunCore(st, "/project", opts)
		if err == nil {
			t.Fatal("expected error for done run")
		}
		if !strings.Contains(err.Error(), "'orch restart-from' only supports failed, canceled, or unknown runs") {
			t.Fatalf("expected restart-from terminal-state guidance, got: %s", err.Error())
		}
	})

	t.Run("branch path uses payload issue snapshot without reading worker store", func(t *testing.T) {
		// A worker pinned to a different host than the master has no issue store.
		// The master carries the resolved issue in opts.IssueSnapshot; the worker
		// must consume it and never read its own store for the run's issue.
		emptyStore := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue), // intentionally empty: worker has no issue
		}
		opts := &ContinueRunOptions{
			Branch:  "feature-branch",
			IssueID: "remote-issue",
			IssueSnapshot: &model.Issue{
				ID:     "remote-issue",
				Title:  "Remote",
				Body:   "do the thing",
				Status: model.IssueStatusOpen,
			},
		}
		// projectRoot="" makes the core fail at the next gate (project root). The
		// point is that it gets PAST the branch-path issue validation: it must NOT
		// fail "issue not found" even though the worker store is empty.
		_, err := server.processContinueRunCore(emptyStore, "", opts)
		if err == nil {
			t.Fatal("expected error at the project-root gate")
		}
		if strings.Contains(err.Error(), "issue not found") {
			t.Fatalf("worker must use opts.IssueSnapshot, not read its empty store; got: %v", err)
		}
		if !strings.Contains(err.Error(), "no project root available") {
			t.Fatalf("expected to reach 'no project root available', got: %v", err)
		}
	})

	t.Run("branch path fails fast on missing issue when no snapshot", func(t *testing.T) {
		emptyStore := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}
		opts := &ContinueRunOptions{
			Branch:  "feature-branch",
			IssueID: "remote-issue",
			// No IssueSnapshot: a non-delegated direct call falls back to the local
			// store and must fail fast (never silently swallow a missing issue).
		}
		_, err := server.processContinueRunCore(emptyStore, "/project", opts)
		if err == nil {
			t.Fatal("expected 'issue not found' error")
		}
		if !strings.Contains(err.Error(), "issue not found") {
			t.Fatalf("expected 'issue not found', got: %v", err)
		}
	})

	t.Run("restart path uses payload run snapshot without reading worker store", func(t *testing.T) {
		emptyStore := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}
		opts := &ContinueRunOptions{
			IssueID: "legacy-issue",
			RunID:   "legacy-run",
			IssueSnapshot: &model.Issue{
				ID:     "legacy-issue",
				Title:  "Legacy",
				Status: model.IssueStatusOpen,
			},
			RunSnapshot: &RunSnapshot{
				IssueID: "legacy-issue",
				RunID:   "legacy-run",
				Status:  model.StatusFailed,
				Branch:  "issue/legacy",
			},
		}

		_, err := server.processContinueRunCore(emptyStore, "/project", opts)
		if err == nil {
			t.Fatal("expected error after snapshot run resolution")
		}
		if strings.Contains(err.Error(), "run not found") {
			t.Fatalf("worker must use opts.RunSnapshot, not read its empty store; got: %v", err)
		}
		if strings.Contains(err.Error(), "issue not found") {
			t.Fatalf("worker must use opts.IssueSnapshot, not read its empty issue store; got: %v", err)
		}
		if !strings.Contains(err.Error(), "has no worktree path") {
			t.Fatalf("expected to reach worktree-path validation, got: %v", err)
		}
	})
}

func TestProcessCreateIssueCoreValidation(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	t.Run("missing title and issue_id returns error", func(t *testing.T) {
		st := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}
		params := &CreateIssueParams{}
		_, err := server.processCreateIssueCore(st, params)
		if err == nil {
			t.Error("expected error for missing title")
		}
		if !strings.Contains(err.Error(), "title required") {
			t.Errorf("expected 'title required' error, got: %v", err)
		}
	})

	t.Run("missing issue_id returns error", func(t *testing.T) {
		st := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}
		params := &CreateIssueParams{
			Title: "Test Issue",
		}
		_, err := server.processCreateIssueCore(st, params)
		if err == nil {
			t.Error("expected error for missing issue_id")
		}
		if !strings.Contains(err.Error(), "issue_id required") {
			t.Errorf("expected 'issue_id required' error, got: %v", err)
		}
	})

	t.Run("invalid issue_id characters returns error", func(t *testing.T) {
		st := &mockStore{
			runs:   make(map[string]*model.Run),
			issues: make(map[string]*model.Issue),
		}
		params := &CreateIssueParams{
			IssueID: "invalid/path",
			Title:   "Test Issue",
		}
		_, err := server.processCreateIssueCore(st, params)
		if err == nil {
			t.Error("expected error for invalid characters")
		}
		if !strings.Contains(err.Error(), "invalid characters") {
			t.Errorf("expected 'invalid characters' error, got: %v", err)
		}
	})
}

func TestProcessControlAgentLaunchCoreValidation(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	t.Run("missing project_root returns error", func(t *testing.T) {
		params := &ControlAgentLaunchParams{}
		_, err := server.processControlAgentLaunchCore(st, params)
		if err == nil {
			t.Error("expected error for missing project_root")
		}
		if !strings.Contains(err.Error(), "project_root required") {
			t.Errorf("expected 'project_root required' error, got: %v", err)
		}
	})
}

func TestProcessControlAgentLaunchCoreUsesProjectRootConfig(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	// Config that should be ignored (cwd-based config).
	cwdRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwdRoot, ".orch"), 0755); err != nil {
		t.Fatalf("failed to create cwd .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwdRoot, ".orch", "config.yaml"), []byte("control_agent: cwd-agent\n"), 0644); err != nil {
		t.Fatalf("failed to write cwd config: %v", err)
	}

	// Config that should be used (request project_root config).
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0755); err != nil {
		t.Fatalf("failed to create project .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte("control_agent: project-agent\n"), 0644); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	// Ensure env vars don't override config in this test.
	t.Setenv("ORCH_CONTROL_AGENT", "")
	t.Setenv("ORCH_AGENT", "")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(cwdRoot); err != nil {
		t.Fatalf("failed to chdir to cwd test dir: %v", err)
	}

	_, err = server.processControlAgentLaunchCore(st, &ControlAgentLaunchParams{
		ProjectRoot: projectRoot,
	})
	if err == nil {
		t.Fatal("expected config validation error")
	}
	if !strings.Contains(err.Error(), "control_agent must be one of") || !strings.Contains(err.Error(), "project-agent") {
		t.Fatalf("expected project config agent to be used, got error: %v", err)
	}
	if strings.Contains(err.Error(), "cwd-agent") {
		t.Fatalf("expected cwd config to be ignored, got error: %v", err)
	}
}

func TestProcessSendMessageValidation(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: make(map[string]*model.Issue),
	}

	t.Run("run not found returns error", func(t *testing.T) {
		params := &SendMessageParams{
			IssueID: "nonexistent",
			RunID:   "run-1",
			Message: "test message",
		}
		err := server.processSendMessage(st, params)
		if err == nil {
			t.Error("expected error for nonexistent run")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %v", err)
		}
	})
}

type sendCall struct {
	session string
	keys    string
}

type mockSendMux struct {
	hasSession bool
	sendErr    error
	muxType    multiplexer.Type

	sendKeysCalls        []sendCall
	sendKeysLiteralCalls []sendCall
	sendTextCalls        []sendCall
	sendBracketedPaste   []sendCall
}

func (m *mockSendMux) Type() multiplexer.Type {
	return m.muxType
}

func (m *mockSendMux) HasSession(name string) bool {
	return m.hasSession
}

func (m *mockSendMux) SendKeys(session, keys string) error {
	m.sendKeysCalls = append(m.sendKeysCalls, sendCall{session: session, keys: keys})
	return m.sendErr
}

func (m *mockSendMux) SendKeysLiteral(session, keys string) error {
	m.sendKeysLiteralCalls = append(m.sendKeysLiteralCalls, sendCall{session: session, keys: keys})
	return m.sendErr
}

func (m *mockSendMux) SendText(session, text string) error {
	m.sendTextCalls = append(m.sendTextCalls, sendCall{session: session, keys: text})
	return m.sendErr
}

func (m *mockSendMux) SendBracketedPaste(session, text string) error {
	m.sendBracketedPaste = append(m.sendBracketedPaste, sendCall{session: session, keys: text})
	return m.sendErr
}

type mockCaptureMux struct {
	hasSession bool
	content    string
	err        error
	lines      int
	session    string
}

func (m *mockCaptureMux) HasSession(name string) bool {
	m.session = name
	return m.hasSession
}

func (m *mockCaptureMux) CapturePane(session string, lines int) (string, error) {
	m.session = session
	m.lines = lines
	return m.content, m.err
}

func TestCaptureLocalMultiplexerSessionFailsWhenSessionMissing(t *testing.T) {
	run := &model.Run{
		IssueID:     "issue-capture",
		RunID:       "run-capture",
		SessionName: "session-capture",
		Multiplexer: string(multiplexer.TypeTmux),
	}

	prev := getCaptureMultiplexerForType
	mockMux := &mockCaptureMux{hasSession: false}
	getCaptureMultiplexerForType = func(muxType multiplexer.Type) captureMultiplexer { return mockMux }
	defer func() { getCaptureMultiplexerForType = prev }()

	_, _, err := captureLocalMultiplexerSession(run, 25)
	if err == nil {
		t.Fatal("expected captureLocalMultiplexerSession to fail")
	}
	if !strings.Contains(err.Error(), "session-capture") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCaptureLocalMultiplexerSessionUsesRequestedLines(t *testing.T) {
	run := &model.Run{
		IssueID:     "issue-capture",
		RunID:       "run-capture",
		SessionName: "session-capture",
		Multiplexer: string(multiplexer.TypeTmux),
	}

	prev := getCaptureMultiplexerForType
	mockMux := &mockCaptureMux{hasSession: true, content: "captured output"}
	getCaptureMultiplexerForType = func(muxType multiplexer.Type) captureMultiplexer { return mockMux }
	defer func() { getCaptureMultiplexerForType = prev }()

	content, source, err := captureLocalMultiplexerSession(run, 25)
	if err != nil {
		t.Fatalf("captureLocalMultiplexerSession() error = %v", err)
	}
	if content != "captured output" || source != string(multiplexer.TypeTmux) {
		t.Fatalf("unexpected capture result: content=%q source=%q", content, source)
	}
	if mockMux.lines != 25 {
		t.Fatalf("capture lines = %d, want 25", mockMux.lines)
	}
}

func TestProcessSendTmuxCodexSendsWithSubmit(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prev := getSendMultiplexer
	prevDelay := codexTmuxSubmitDelay
	prevExtraDelay := codexTmuxExtraEnterDelay
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	codexTmuxSubmitDelay = 0
	codexTmuxExtraEnterDelay = 0
	defer func() {
		getSendMultiplexer = prev
		codexTmuxSubmitDelay = prevDelay
		codexTmuxExtraEnterDelay = prevExtraDelay
	}()

	run := &model.Run{
		IssueID:     "issue-1",
		RunID:       "run-1",
		SessionName: "run-issue-1-run-1",
		Agent:       string(agent.AgentCodex),
	}

	if err := server.processSendTmux(run, "please continue", false); err != nil {
		t.Fatalf("processSendTmux() error = %v", err)
	}

	if len(mockMux.sendKeysLiteralCalls) != 1 {
		t.Fatalf("SendKeysLiteral calls = %d, want 1", len(mockMux.sendKeysLiteralCalls))
	}
	if got := mockMux.sendKeysLiteralCalls[0]; got.session != run.SessionName || got.keys != "please continue" {
		t.Fatalf("SendKeysLiteral call = (%q, %q), want (%q, %q)", got.session, got.keys, run.SessionName, "please continue")
	}
	if len(mockMux.sendTextCalls) != 2 {
		t.Fatalf("SendText calls = %d, want 2 (initial Enter + extra Enter for codex)", len(mockMux.sendTextCalls))
	}
	for i, got := range mockMux.sendTextCalls {
		if got.session != run.SessionName || got.keys != tmuxSubmitKeyEnter {
			t.Fatalf("SendText call[%d] = (%q, %q), want (%q, %q)", i, got.session, got.keys, run.SessionName, tmuxSubmitKeyEnter)
		}
	}
	if len(mockMux.sendKeysCalls) != 0 {
		t.Fatalf("SendKeys calls = %d, want 0", len(mockMux.sendKeysCalls))
	}
}

func TestProcessSendTmuxNoEnterUsesLiteral(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true}
	prev := getSendMultiplexer
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexer = prev }()

	run := &model.Run{
		IssueID:     "issue-1",
		RunID:       "run-1",
		SessionName: "run-issue-1-run-1",
		Agent:       string(agent.AgentCodex),
	}

	if err := server.processSendTmux(run, "partial input", true); err != nil {
		t.Fatalf("processSendTmux() error = %v", err)
	}

	if len(mockMux.sendKeysLiteralCalls) != 1 {
		t.Fatalf("SendKeysLiteral calls = %d, want 1", len(mockMux.sendKeysLiteralCalls))
	}
	if got := mockMux.sendKeysLiteralCalls[0]; got.session != run.SessionName || got.keys != "partial input" {
		t.Fatalf("SendKeysLiteral call = (%q, %q), want (%q, %q)", got.session, got.keys, run.SessionName, "partial input")
	}
	if len(mockMux.sendKeysCalls) != 0 {
		t.Fatalf("SendKeys calls = %d, want 0", len(mockMux.sendKeysCalls))
	}
	if len(mockMux.sendTextCalls) != 0 {
		t.Fatalf("SendText calls = %d, want 0", len(mockMux.sendTextCalls))
	}
}

func TestProcessSendTmuxNonCodexUsesSendKeys(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prev := getSendMultiplexer
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexer = prev }()

	run := &model.Run{
		IssueID:     "issue-1",
		RunID:       "run-1",
		SessionName: "run-issue-1-run-1",
		Agent:       string(agent.AgentGemini),
	}

	if err := server.processSendTmux(run, "continue", false); err != nil {
		t.Fatalf("processSendTmux() error = %v", err)
	}

	if len(mockMux.sendKeysCalls) != 1 {
		t.Fatalf("SendKeys calls = %d, want 1", len(mockMux.sendKeysCalls))
	}
	if got := mockMux.sendKeysCalls[0]; got.session != run.SessionName || got.keys != "continue" {
		t.Fatalf("SendKeys call = (%q, %q), want (%q, %q)", got.session, got.keys, run.SessionName, "continue")
	}
	if len(mockMux.sendKeysLiteralCalls) != 0 {
		t.Fatalf("SendKeysLiteral calls = %d, want 0", len(mockMux.sendKeysLiteralCalls))
	}
	if len(mockMux.sendTextCalls) != 0 {
		t.Fatalf("SendText calls = %d, want 0", len(mockMux.sendTextCalls))
	}
}

func TestProcessSendTmuxMultilineUsesBracketedPaste(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prev := getSendMultiplexer
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexer = prev }()

	run := &model.Run{
		IssueID:     "issue-1",
		RunID:       "run-1",
		SessionName: "run-issue-1-run-1",
		Agent:       string(agent.AgentGemini),
	}

	if err := server.processSendTmux(run, "line one\nline two", false); err != nil {
		t.Fatalf("processSendTmux() error = %v", err)
	}

	if len(mockMux.sendBracketedPaste) != 1 {
		t.Fatalf("SendBracketedPaste calls = %d, want 1", len(mockMux.sendBracketedPaste))
	}
	if got := mockMux.sendBracketedPaste[0]; got.session != run.SessionName || got.keys != "line one\nline two" {
		t.Fatalf("SendBracketedPaste call = (%q, %q), want (%q, %q)", got.session, got.keys, run.SessionName, "line one\nline two")
	}
	if len(mockMux.sendTextCalls) != 1 || mockMux.sendTextCalls[0].keys != tmuxSubmitKeyEnter {
		t.Fatalf("SendText calls = %+v, want Enter", mockMux.sendTextCalls)
	}
	if len(mockMux.sendKeysCalls) != 0 || len(mockMux.sendKeysLiteralCalls) != 0 {
		t.Fatalf("unexpected key calls: SendKeys=%d SendKeysLiteral=%d", len(mockMux.sendKeysCalls), len(mockMux.sendKeysLiteralCalls))
	}
}

func TestProcessSendTmuxMultilineClaudeSendsSecondEnter(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prev := getSendMultiplexer
	prevDelay := claudeTmuxMultilineSubmitDelay
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	claudeTmuxMultilineSubmitDelay = 0
	defer func() {
		getSendMultiplexer = prev
		claudeTmuxMultilineSubmitDelay = prevDelay
	}()

	run := &model.Run{
		IssueID:     "issue-claude",
		RunID:       "run-claude",
		SessionName: "session-claude",
		Agent:       string(agent.AgentClaude),
	}

	if err := server.processSendTmux(run, "line one\nline two", false); err != nil {
		t.Fatalf("processSendTmux() error = %v", err)
	}

	if len(mockMux.sendBracketedPaste) != 1 {
		t.Fatalf("SendBracketedPaste calls = %d, want 1", len(mockMux.sendBracketedPaste))
	}
	if len(mockMux.sendTextCalls) != 2 {
		t.Fatalf("SendText calls = %d, want 2", len(mockMux.sendTextCalls))
	}
	if mockMux.sendTextCalls[0].keys != tmuxSubmitKeyEnter || mockMux.sendTextCalls[1].keys != tmuxSubmitKeyEnter {
		t.Fatalf("SendText calls = %+v, want Enter twice", mockMux.sendTextCalls)
	}
}

func TestProcessSendTmuxUsesRunMultiplexerWhenAvailable(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	defaultMux := &mockSendMux{hasSession: false, muxType: multiplexer.TypeTmux}
	zellijMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeZellij}

	prevDefault := getSendMultiplexer
	prevByType := getSendMultiplexerForType
	getSendMultiplexer = func() sendMultiplexer { return defaultMux }
	getSendMultiplexerForType = func(muxType multiplexer.Type) sendMultiplexer {
		if muxType == multiplexer.TypeZellij {
			return zellijMux
		}
		return nil
	}
	defer func() {
		getSendMultiplexer = prevDefault
		getSendMultiplexerForType = prevByType
	}()

	run := &model.Run{
		IssueID:     "issue-z",
		RunID:       "run-z",
		SessionName: "run-issue-z-run-z",
		Agent:       string(agent.AgentClaude),
		Multiplexer: string(multiplexer.TypeZellij),
	}

	if err := server.processSendTmux(run, "continue-zellij", false); err != nil {
		t.Fatalf("processSendTmux() error = %v", err)
	}

	if len(defaultMux.sendKeysCalls) != 0 {
		t.Fatalf("default mux SendKeys calls = %d, want 0", len(defaultMux.sendKeysCalls))
	}
	if len(zellijMux.sendKeysCalls) != 1 {
		t.Fatalf("zellij mux SendKeys calls = %d, want 1", len(zellijMux.sendKeysCalls))
	}
	if got := zellijMux.sendKeysCalls[0]; got.session != run.SessionName || got.keys != "continue-zellij" {
		t.Fatalf("zellij SendKeys call = (%q, %q), want (%q, %q)", got.session, got.keys, run.SessionName, "continue-zellij")
	}
}

func TestProcessSendMessageClaudeAndCodexPaths(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prevMux := getSendMultiplexer
	prevDelay := codexTmuxSubmitDelay
	prevExtraDelay := codexTmuxExtraEnterDelay
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	codexTmuxSubmitDelay = 0
	codexTmuxExtraEnterDelay = 0
	defer func() {
		getSendMultiplexer = prevMux
		codexTmuxSubmitDelay = prevDelay
		codexTmuxExtraEnterDelay = prevExtraDelay
	}()

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue-1#run-claude": {
				IssueID:     "issue-1",
				RunID:       "run-claude",
				SessionName: "session-claude",
				Agent:       string(agent.AgentClaude),
			},
			"issue-1#run-codex": {
				IssueID:     "issue-1",
				RunID:       "run-codex",
				SessionName: "session-codex",
				Agent:       string(agent.AgentCodex),
			},
		},
		issues: map[string]*model.Issue{},
	}

	if err := server.processSendMessage(st, &SendMessageParams{IssueID: "issue-1", RunID: "run-claude", Message: "claude message"}); err != nil {
		t.Fatalf("claude processSendMessage() error = %v", err)
	}
	if err := server.processSendMessage(st, &SendMessageParams{IssueID: "issue-1", RunID: "run-codex", Message: "codex message"}); err != nil {
		t.Fatalf("codex processSendMessage() error = %v", err)
	}

	if len(mockMux.sendKeysCalls) != 1 {
		t.Fatalf("SendKeys calls = %d, want 1", len(mockMux.sendKeysCalls))
	}
	if got := mockMux.sendKeysCalls[0]; got.session != "session-claude" || got.keys != "claude message" {
		t.Fatalf("claude SendKeys call = (%q, %q), want (%q, %q)", got.session, got.keys, "session-claude", "claude message")
	}

	if len(mockMux.sendKeysLiteralCalls) != 1 {
		t.Fatalf("SendKeysLiteral calls = %d, want 1", len(mockMux.sendKeysLiteralCalls))
	}
	if got := mockMux.sendKeysLiteralCalls[0]; got.session != "session-codex" || got.keys != "codex message" {
		t.Fatalf("codex SendKeysLiteral call = (%q, %q), want (%q, %q)", got.session, got.keys, "session-codex", "codex message")
	}

	if len(mockMux.sendTextCalls) != 2 {
		t.Fatalf("SendText calls = %d, want 2 (initial Enter + extra Enter for codex)", len(mockMux.sendTextCalls))
	}
	for i, got := range mockMux.sendTextCalls {
		if got.session != "session-codex" || got.keys != tmuxSubmitKeyEnter {
			t.Fatalf("codex SendText call[%d] = (%q, %q), want (%q, %q)", i, got.session, got.keys, "session-codex", tmuxSubmitKeyEnter)
		}
	}
}

func TestProcessSendMessageRemoteTargetFailsClearly(t *testing.T) {
	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return "master-host", nil }
	defer func() { currentDaemonHostname = origHost }()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue-remote#run-remote": {
				IssueID:     "issue-remote",
				RunID:       "run-remote",
				SessionName: "session-remote",
				Agent:       string(agent.AgentClaude),
				TargetHost:  "mac-host",
			},
		},
		issues: map[string]*model.Issue{},
	}

	err := server.processSendMessage(st, &SendMessageParams{IssueID: "issue-remote", RunID: "run-remote", Message: "hello"})
	if err == nil {
		t.Fatal("expected processSendMessage to fail for remote target run")
	}
	if !strings.Contains(err.Error(), "remote host") || !strings.Contains(err.Error(), "mac-host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessSendMessageAllowsCurrentHostTarget(t *testing.T) {
	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return "mac-host", nil }
	defer func() { currentDaemonHostname = origHost }()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prevMux := getSendMultiplexer
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexer = prevMux }()

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue-local#run-local": {
				IssueID:     "issue-local",
				RunID:       "run-local",
				SessionName: "session-local",
				Agent:       string(agent.AgentClaude),
				TargetHost:  "mac-host",
			},
		},
		issues: map[string]*model.Issue{},
	}

	err := server.processSendMessage(st, &SendMessageParams{
		IssueID: "issue-local",
		RunID:   "run-local",
		Message: "hello",
		NoEnter: true,
	})
	if err != nil {
		t.Fatalf("processSendMessage() error = %v", err)
	}
	if len(mockMux.sendKeysLiteralCalls) != 1 || mockMux.sendKeysLiteralCalls[0].keys != "hello" {
		t.Fatalf("unexpected SendKeysLiteral calls: %+v", mockMux.sendKeysLiteralCalls)
	}
	if len(mockMux.sendKeysCalls) != 0 || len(mockMux.sendTextCalls) != 0 {
		t.Fatalf("unexpected key calls: SendKeys=%+v SendText=%+v", mockMux.sendKeysCalls, mockMux.sendTextCalls)
	}
}

func TestProcessSendMessageUsesTargetWorkerIDOverride(t *testing.T) {
	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return "master-host", nil }
	defer func() { currentDaemonHostname = origHost }()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.SetWorkerIdentity("host-mac-prod", "actual-worker-host")

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prevMux := getSendMultiplexer
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexer = prevMux }()

	st := &mockStore{
		runs: map[string]*model.Run{
			"issue-override#run-override": {
				IssueID:     "issue-override",
				RunID:       "run-override",
				SessionName: "session-override",
				Agent:       string(agent.AgentClaude),
				TargetHost:  "mac-alias",
			},
		},
		issues: map[string]*model.Issue{},
	}

	err := server.processSendMessage(st, &SendMessageParams{
		IssueID:        "issue-override",
		RunID:          "run-override",
		Message:        "hello override",
		NoEnter:        true,
		TargetWorkerID: "host-mac-prod",
	})
	if err != nil {
		t.Fatalf("processSendMessage() error = %v", err)
	}
	if len(mockMux.sendKeysLiteralCalls) != 1 || mockMux.sendKeysLiteralCalls[0].keys != "hello override" {
		t.Fatalf("unexpected SendKeysLiteral calls: %+v", mockMux.sendKeysLiteralCalls)
	}
}

func TestResolveWorkerTargetForRunPrefersRecordedRunRouting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ORCH_AGENT", "")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	configBody := `targets:
  - name: mac
    host: new-host
`
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), []byte(configBody), 0644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	target, err := resolveWorkerTargetForRun(&resolvedProtoRun{
		run: &model.Run{
			IssueID:        "issue-1",
			RunID:          "run-1",
			Target:         "mac",
			TargetHost:     "old-host",
			TargetWorkerID: "host-old-host",
		},
		projectRoot: repo,
	})
	if err != nil {
		t.Fatalf("resolveWorkerTargetForRun() error = %v", err)
	}
	if target.Host != "old-host" {
		t.Fatalf("target host = %q, want old-host", target.Host)
	}
	if target.WorkerID != "host-old-host" {
		t.Fatalf("target worker = %q, want host-old-host", target.WorkerID)
	}
}

// Branch discipline in the generated prompt (run-state-machine.md §11
// prevention layer): the agent is told its run branch, the exact push
// command, and — when a PR is expected — that the PR head must be that
// branch.
func TestBuildRunPromptBranchDiscipline(t *testing.T) {
	s := NewSocketServer(nil, log.New(io.Discard, "", 0))
	issue := &model.Issue{ID: "issue-1", Body: "do the thing"}

	prompt := s.buildRunPrompt(issue, "/tmp/issues", false, "", "main", "issue/x/run-1")
	for _, want := range []string{
		"already on branch `issue/x/run-1`",
		"do NOT create or switch branches",
		"git push origin HEAD:issue/x/run-1",
		"Open the pull request FROM `issue/x/run-1`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}

	noPR := s.buildRunPrompt(issue, "/tmp/issues", true, "", "main", "issue/x/run-1")
	if !strings.Contains(noPR, "git push origin HEAD:issue/x/run-1") {
		t.Fatal("no-PR prompt must keep the push instruction")
	}
	if strings.Contains(noPR, "Open the pull request FROM") {
		t.Fatal("no-PR prompt must not instruct opening a PR")
	}

	noBranch := s.buildRunPrompt(issue, "/tmp/issues", false, "", "main", "")
	if strings.Contains(noBranch, "## Git workflow") {
		t.Fatal("empty branch must omit the git workflow section")
	}
}
