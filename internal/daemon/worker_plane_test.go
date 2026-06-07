package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	filestore "github.com/s22625/orch/internal/store/file"
)

func TestMarshalAndDecodeWorkerEffectResult_StartRun(t *testing.T) {
	resultJSON := marshalWorkerEffectResult(&WorkerEffectResult{
		StartRunResult: &StartRunResult{RunID: "run-123", Branch: "feature/x"},
	})
	if resultJSON == "" {
		t.Fatal("marshalWorkerEffectResult() = empty, want non-empty json")
	}

	decoded, err := decodeWorkerEffectResult(resultJSON)
	if err != nil {
		t.Fatalf("decodeWorkerEffectResult() error = %v", err)
	}

	if decoded.StartRunResult == nil {
		t.Fatal("decoded.StartRunResult = nil, want populated result")
	}
	if decoded.StartRunResult.RunID != "run-123" {
		t.Fatalf("run_id = %q, want %q", decoded.StartRunResult.RunID, "run-123")
	}
}

func TestWaitForWorkerLeaseCompletionReturnsResultJSON(t *testing.T) {
	server := NewSocketServer(func(issuesRoot string) (store.Store, error) {
		return filestore.New(issuesRoot)
	}, log.New(io.Discard, "", 0))
	if _, ttl := server.registerWorker("worker-json", "external", "localhost", "external", []string{"stop_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for test worker")
	}

	lease, err := server.acquireWorkerLease("project-test", "stop_run", "orch-9", "run-9", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}

	expected := `{"continue_run_result":{"run_id":"run-9"}}`
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = server.acknowledgeWorkerLease(lease.WorkerID, lease.LeaseID, true, "", expected)
	}()

	completed, err := server.waitForWorkerLeaseCompletion(lease.LeaseID, time.Second)
	if err != nil {
		t.Fatalf("waitForWorkerLeaseCompletion() error = %v", err)
	}
	if completed == nil {
		t.Fatal("completed lease = nil, want lease")
	}
	if completed.ResultJSON != expected {
		t.Fatalf("result_json = %q, want %q", completed.ResultJSON, expected)
	}
}

func TestWorkerProfileDefaultDistributedDoesNotRegisterEmbeddedWorker(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	if len(server.listWorkers()) != 0 {
		t.Fatal("expected no implicit workers in default distributed profile")
	}
}

func TestWorkerSchedulingUsesExplicitExternalWorkersOnly(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus", nil }
	t.Cleanup(func() { currentHostname = orig })

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	if len(server.listWorkers()) != 0 {
		t.Fatal("expected no implicit workers in distributed profile")
	}

	if _, ttl := server.registerWorker("host-zeus", "external", "localhost", "external", []string{"stop_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for external registration")
	}
	lease, err := server.acquireWorkerLease("project-test", "stop_run", "issue-x", "run-x", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}
	if lease.WorkerID != "host-zeus" {
		t.Fatalf("lease worker = %q, want host-zeus", lease.WorkerID)
	}
}

func TestWorkerSchedulingPrefersTargetNamedWorker(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	if _, ttl := server.registerWorker("host-zeus", "external", "zeus", "external", []string{"start_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for zeus worker")
	}
	targetWorkerID := HostWorkerID("mac-host")
	if _, ttl := server.registerWorker(targetWorkerID, "external", "mac-host", "external", []string{"start_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for mac worker")
	}

	payload := &WorkerEffectPayload{StartRun: &StartRunOptions{Target: "mac", TargetHost: "mac-host", TargetWorkerID: targetWorkerID}}
	lease, err := server.acquireWorkerLease("project-test", "start_run", "issue-x", "run-x", payload)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}
	if lease.WorkerID != targetWorkerID {
		t.Fatalf("lease worker = %q, want %q", lease.WorkerID, targetWorkerID)
	}
}

func TestWorkerSchedulingFailsWhenTargetWorkerMissing(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	if _, ttl := server.registerWorker("host-zeus", "external", "zeus", "external", []string{"start_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for zeus worker")
	}

	payload := &WorkerEffectPayload{StartRun: &StartRunOptions{Target: "mac", TargetHost: "mac-host", TargetWorkerID: HostWorkerID("mac-host")}}
	_, err := server.acquireWorkerLease("project-test", "start_run", "issue-x", "run-x", payload)
	if err == nil {
		t.Fatal("acquireWorkerLease() error = nil, want missing target worker error")
	}
	if !strings.Contains(err.Error(), HostWorkerID("mac-host")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteLeaseEffectUsesLocalExecutionForMatchingTargetWorker(t *testing.T) {
	projectRoot := t.TempDir()
	issuesRoot := filepath.Join(projectRoot, "issues-store")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(issuesRoot, "issues"), 0o755); err != nil {
		t.Fatalf("mkdir issues root: %v", err)
	}

	configBody := `issues:
  path: ` + issuesRoot + `
worktree_dir: worktrees
targets:
  - name: mac
    host: mac
`
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	st, err := filestore.New(issuesRoot)
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	if err := st.CreateIssue(&model.Issue{ID: "issue-target", Title: "target issue", Status: model.IssueStatusOpen}); err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	targetWorkerID := HostWorkerID("mac")
	server.SetWorkerIdentity(targetWorkerID, "mac-host")
	if _, err := server.registerRepoContext("project-target", projectRoot, "", st); err != nil {
		t.Fatalf("registerRepoContext() error = %v", err)
	}

	lease := &WorkerLease{
		LeaseID:   "lease-target-local",
		WorkerID:  targetWorkerID,
		ProjectID: "project-target",
		Effect:    "start_run",
		Payload: &WorkerEffectPayload{
			StartRun: &StartRunOptions{
				IssueID:        "issue-target",
				Target:         "mac",
				TargetHost:     "mac",
				TargetWorkerID: targetWorkerID,
				Agent:          "custom",
				AgentCmd:       "echo test",
				DryRun:         true,
			},
		},
	}

	result, err := server.executeLeaseEffect(lease)
	if err != nil {
		t.Fatalf("executeLeaseEffect() error = %v", err)
	}
	if result == nil || result.StartRunResult == nil {
		t.Fatal("expected start_run result")
	}
	if !strings.HasPrefix(result.StartRunResult.WorktreePath, filepath.Join(projectRoot, "worktrees")) {
		t.Fatalf("worktree path = %q, want local project-root-based path", result.StartRunResult.WorktreePath)
	}
}

// TestProcessStartRunCoreUsesSnapshotWithEmptyWorkerIssueStore is the authoritative
// regression for the cross-machine worker bug: a worker pinned to a different host
// than the master has an EMPTY issue store. The master (issue-store SSOT) resolves a
// non-GitHub issue and carries it in opts.IssueSnapshot. processStartRunCore must get
// PAST issue resolution AND past st.CreateRun (which previously called ResolveIssue
// internally and failed "issue not found") by using CreateRunForExistingIssue, then
// reach worktree creation / booting. The run document must land even though the
// worker's issue store has no issue file.
func TestProcessStartRunCoreUsesSnapshotWithEmptyWorkerIssueStore(t *testing.T) {
	// Real git repo so worktree creation (which happens AFTER st.CreateRun) can run.
	projectRoot := createGitRepoWithOrigin(t, "https://github.com/example/snapshot-worker.git")

	// FileStore with an EMPTY issues store: the worker has no issue file at all.
	issuesRoot := filepath.Join(projectRoot, "issues-store")
	if err := os.MkdirAll(filepath.Join(issuesRoot, "issues"), 0o755); err != nil {
		t.Fatalf("mkdir issues root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configBody := "issues:\n  path: " + issuesRoot + "\nworktree_dir: worktrees\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	st, err := filestore.New(issuesRoot)
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	// Sanity: the issue genuinely does not exist on the worker store.
	if _, err := st.ResolveIssue("remote-issue"); err == nil {
		t.Fatal("precondition failed: worker store unexpectedly has the issue")
	}

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	const issueID model.IssueID = "remote-issue" // non-gh id: CreateRun would verify it
	const runID model.RunID = "20231220-101010"
	opts := &StartRunOptions{
		IssueID:     issueID,
		RunID:       runID,
		Agent:       "custom",
		AgentCmd:    "true",
		Multiplexer: "tmux",
		// Master-resolved snapshot: this is what the worker must consume.
		IssueSnapshot: &model.Issue{
			ID:     issueID,
			Title:  "Remote",
			Body:   "do the thing",
			Status: model.IssueStatusOpen,
		},
	}

	// The call may still fail LATER (multiplexer/session launch is environment
	// dependent), but it must NOT fail at issue resolution or at st.CreateRun.
	_, err = server.processStartRunCore(st, projectRoot, opts)
	if err != nil && strings.Contains(err.Error(), "issue not found") {
		t.Fatalf("worker must use opts.IssueSnapshot and CreateRunForExistingIssue, not verify against its empty store; got: %v", err)
	}

	// The run document must have landed coherently under runs/<issueID>/<runID>.md,
	// proving st.CreateRun's verification gate was bypassed without an issue file.
	runDoc := filepath.Join(issuesRoot, "runs", string(issueID), string(runID)+".md")
	if _, statErr := os.Stat(runDoc); statErr != nil {
		t.Fatalf("expected run document at %q (CreateRun gate must be bypassed); stat err=%v; core err=%v", runDoc, statErr, err)
	}
}
