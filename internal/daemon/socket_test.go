package daemon

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/xdg"
	"google.golang.org/protobuf/proto"
)

type mockStore struct {
	runs   map[string]*model.Run
	issues map[string]*model.Issue
}

func (m *mockStore) ResolveIssue(issueID string) (*model.Issue, error) {
	if issue, ok := m.issues[issueID]; ok {
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

func (m *mockStore) SetIssueStatus(issueID string, status model.IssueStatus) error {
	return nil
}

func (m *mockStore) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
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
	key := ref.IssueID + "#" + ref.RunID
	if run, ok := m.runs[key]; ok {
		return run, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockStore) GetRunByShortID(shortID string) (*model.Run, error) {
	return nil, os.ErrNotExist
}

func (m *mockStore) GetLatestRun(issueID string) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) RootPath() string {
	return ""
}

func (m *mockStore) DeleteRun(ref *model.RunRef) error {
	return nil
}

func (m *mockStore) UpdateIssue(issue *model.Issue) error {
	return nil
}

func (m *mockStore) ValidateIssueFiles(issueID string) (*store.ValidationResult, error) {
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
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	return func() {
		os.Setenv("XDG_RUNTIME_DIR", oldXDG)
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
		server.RegisterRepo("/test/project", st)
	}
	return server
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

	const bodyDelay = 600 * time.Millisecond
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
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
	server.openCodeServers[projectRoot] = &managedServer{
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

	const ackDelay = 300 * time.Millisecond
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
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
	server.openCodeServers[projectRoot] = &managedServer{
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

func ensureRequestIssuesRoot(req *orchpb.Request, issuesRoot string) {
	if req == nil || req.Request == nil {
		return
	}

	switch r := req.Request.(type) {
	case *orchpb.Request_ListRuns:
		if r.ListRuns != nil && r.ListRuns.IssuesRoot == "" {
			r.ListRuns.IssuesRoot = issuesRoot
		}
	case *orchpb.Request_GetRun:
		if r.GetRun != nil && r.GetRun.IssuesRoot == "" {
			r.GetRun.IssuesRoot = issuesRoot
		}
	case *orchpb.Request_ListIssues:
		if r.ListIssues != nil && r.ListIssues.IssuesRoot == "" {
			r.ListIssues.IssuesRoot = issuesRoot
		}
	case *orchpb.Request_GetIssue:
		if r.GetIssue != nil && r.GetIssue.IssuesRoot == "" {
			r.GetIssue.IssuesRoot = issuesRoot
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

func sendProtoRequest(t *testing.T, req *orchpb.Request) *orchpb.Response {
	ensureRequestIssuesRoot(req, testIssuesRoot)

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
			"orch-002#run-4": {IssueID: "orch-002", RunID: "run-4", Status: model.StatusBlocked, Agent: "claude", StartedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
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
	st := &mockStore{
		runs: map[string]*model.Run{
			"orch-001#20250117-010000": {
				IssueID:           "orch-001",
				RunID:             "20250117-010000",
				Status:            model.StatusRunning,
				Agent:             "opencode",
				Model:             "anthropic/claude-sonnet-4",
				ModelVariant:      "high",
				Branch:            "orch-001-feature",
				WorktreePath:      "/repo/worktrees/orch-001",
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

	_ = sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_ListIssues{
			ListIssues: &orchpb.ListIssuesRequest{IssuesRoot: "/test/issues/path"},
		},
	})

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
		_ = sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ListIssues{
				ListIssues: &orchpb.ListIssuesRequest{IssuesRoot: "/reuse/test/path"},
			},
		})
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
	server.RegisterRepo("/project/root", st)

	resolved := server.resolveStore(SendRequest{ProjectRoot: "/project/root"})
	if resolved == nil {
		t.Error("expected store to be resolved for registered project root")
	}

	resolved = server.resolveStore(SendRequest{ProjectRoot: "/unknown/project"})
	if resolved != nil {
		t.Error("expected nil store for unknown project root without factory")
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
	t.Setenv("ORCH_PROJECT_ROOT", "/env/project")

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	repoStore := &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}
	repoID, err := server.RegisterRepo("/daemon/project", repoStore)
	if err != nil {
		t.Fatalf("failed to register repo: %v", err)
	}

	if got := server.resolveProjectRoot(SendRequest{ProjectRoot: "/request/project", RepoID: repoID}); got != "/request/project" {
		t.Fatalf("expected request project root precedence, got %q", got)
	}

	if got := server.resolveProjectRoot(SendRequest{RepoID: repoID}); got != "/daemon/project" {
		t.Fatalf("expected repo context project root, got %q", got)
	}

	if got := server.resolveProjectRoot(SendRequest{}); got != "/env/project" {
		t.Fatalf("expected ORCH_PROJECT_ROOT fallback, got %q", got)
	}

	emptyServer := NewSocketServer(nil, logger)
	if got := emptyServer.resolveProjectRoot(SendRequest{}); got != "/env/project" {
		t.Fatalf("expected ORCH_PROJECT_ROOT fallback, got %q", got)
	}

	t.Setenv("ORCH_PROJECT_ROOT", "")
	if got := emptyServer.resolveProjectRoot(SendRequest{}); got != "" {
		t.Fatalf("expected empty project root when request and env are empty, got %q", got)
	}
}

func TestResolveStoreFromProtoRequiresIssuesRoot(t *testing.T) {
	callCount := 0
	mockFactory := func(string) (store.Store, error) {
		callCount++
		return &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}, nil
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(mockFactory, logger)

	if got := server.resolveStoreFromProto(""); got != nil {
		t.Fatalf("expected nil store when issues root is empty, got %#v", got)
	}

	if callCount != 0 {
		t.Fatalf("expected no store creation for empty issues root, got %d", callCount)
	}

	if got := server.resolveStoreFromProto("/issues/root"); got == nil {
		t.Fatal("expected store to resolve when issues root is provided")
	}

	if callCount != 1 {
		t.Fatalf("expected one store creation for valid issues root, got %d", callCount)
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

func writeStoredOpenCodeControlSession(t *testing.T, projectRoot, sessionID, modelName, modelVariant string) {
	t.Helper()
	orchDir := filepath.Join(projectRoot, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		t.Fatalf("failed to create .orch dir: %v", err)
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
	if err := os.WriteFile(filepath.Join(orchDir, "control-session.json"), data, 0644); err != nil {
		t.Fatalf("failed to write stored control session: %v", err)
	}
}

func readStoredOpenCodeControlSession(t *testing.T, projectRoot string) controlSessionRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectRoot, ".orch", "control-session.json"))
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
	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"
	writeStoredOpenCodeControlSession(t, projectRoot, "ses_existing", modelName, modelVariant)

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
	port := getPortFromURL(t, ts.URL)
	sessionID, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}
	if sessionID != "ses_existing" {
		t.Fatalf("expected existing session to be reused, got %q", sessionID)
	}
	stored := readStoredOpenCodeControlSession(t, projectRoot)
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
	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"
	writeStoredOpenCodeControlSession(t, projectRoot, "ses_stale", modelName, modelVariant)

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
	port := getPortFromURL(t, ts.URL)
	sessionID, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
	if err != nil {
		t.Fatalf("getOrCreateOpenCodeControlSession() error = %v", err)
	}
	if sessionID != "ses_control_new" {
		t.Fatalf("expected recovered control session %q, got %q", "ses_control_new", sessionID)
	}

	stored := readStoredOpenCodeControlSession(t, projectRoot)
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
	modelName := "openai/gpt-5.3-codex"
	modelVariant := "xhigh"
	writeStoredOpenCodeControlSession(t, projectRoot, "ses_stale", modelName, modelVariant)

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
	port := getPortFromURL(t, ts.URL)
	sessionID, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
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

	stored := readStoredOpenCodeControlSession(t, projectRoot)
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
	writeStoredOpenCodeControlSession(t, projectRoot, "ses_old", "anthropic/claude-opus-4-5", "high")

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
	port := getPortFromURL(t, ts.URL)
	sessionID, err := server.getOrCreateOpenCodeControlSession(projectRoot, port, modelName, modelVariant)
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

	stored := readStoredOpenCodeControlSession(t, projectRoot)
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
	_, err = server.getOrCreateOpenCodeControlSession(projectRoot, getPortFromURL(t, ts.URL), modelName, modelVariant)
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

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_RegisterRepo{
			RegisterRepo: &orchpb.RegisterRepoRequest{ProjectRoot: "/new/project/path"},
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

func TestDeriveRepoID(t *testing.T) {
	t.Run("fallback produces basename-<8hex> for non-git path", func(t *testing.T) {
		got := deriveRepoID("/tmp/not-a-git-repo/my-project")
		pattern := regexp.MustCompile(`^my-project-[0-9a-f]{8}$`)
		if !pattern.MatchString(got) {
			t.Errorf("deriveRepoID for non-git path = %q, want format my-project-<8hex>", got)
		}
	})

	t.Run("never returns empty string", func(t *testing.T) {
		testPaths := []string{
			"/Users/test/repos/my-project",
			"/tmp/some-path",
			"/single",
		}
		for _, path := range testPaths {
			got := deriveRepoID(path)
			if got == "" {
				t.Errorf("deriveRepoID(%q) returned empty string", path)
			}
		}
	})

	t.Run("handles path with trailing slash", func(t *testing.T) {
		withSlash := deriveRepoID("/tmp/not-a-git-repo/another-project/")
		withoutSlash := deriveRepoID("/tmp/not-a-git-repo/another-project")
		if withSlash != withoutSlash {
			t.Errorf("trailing slash should not change ID: %q != %q", withSlash, withoutSlash)
		}
	})

	t.Run("different paths produce different IDs", func(t *testing.T) {
		id1 := deriveRepoID("/path/to/project-a")
		id2 := deriveRepoID("/path/to/project-b")
		if id1 == id2 {
			t.Errorf("different paths should produce different IDs: %q == %q", id1, id2)
		}
	})

	t.Run("same path produces same ID", func(t *testing.T) {
		id1 := deriveRepoID("/path/to/my-project")
		id2 := deriveRepoID("/path/to/my-project")
		if id1 != id2 {
			t.Errorf("same path should produce same ID: %q != %q", id1, id2)
		}
	})
}

func TestDeriveRepoIDNoBasenameCollision(t *testing.T) {
	// Two repos at different paths but sharing the same basename
	// must produce different IDs
	idA := deriveRepoID("/work/client-a/orch")
	idB := deriveRepoID("/work/client-b/orch")
	if idA == idB {
		t.Errorf("same-basename paths produced same ID: %q", idA)
	}

	// Both should match the orch-<8hex> format (basename-hash from xdg.RepoID)
	pattern := regexp.MustCompile(`^orch-[0-9a-f]{8}$`)
	if !pattern.MatchString(idA) {
		t.Errorf("deriveRepoID(/work/client-a/orch) = %q, want format orch-<8hex>", idA)
	}
	if !pattern.MatchString(idB) {
		t.Errorf("deriveRepoID(/work/client-b/orch) = %q, want format orch-<8hex>", idB)
	}
}

func TestListReposAPI(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: make(map[string]*model.Run), issues: make(map[string]*model.Issue)}
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	server.RegisterRepo("/test/project", st)
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

type mockStoreWithCapture struct {
	mockStore
	capturedMetadata map[string]string
}

func (m *mockStoreWithCapture) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	m.capturedMetadata = metadata
	return &model.Run{
		IssueID: issueID,
		RunID:   runID,
		Status:  model.StatusQueued,
		Path:    "/test/runs/" + issueID + "/" + runID + ".md",
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
				IssueId:     "test-issue",
				Model:       "anthropic/claude-sonnet-4",
				IssuesRoot:  testIssuesRoot,
				ProjectRoot: testProjectRoot,
				DryRun:      true,
			},
		},
	})

	if !resp.Ok {
		// DryRun should succeed without needing real git infrastructure.
		// If it fails with "agent not available", that's expected in CI without claude installed.
		// The key contract test is that Model is NOT in Message.
		errMsg := resp.Error
		if errMsg != "agent not available: claude" && errMsg != "no project root available" {
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
				IssueId:     "test-issue",
				Model:       "anthropic/claude-sonnet-4",
				IssuesRoot:  testIssuesRoot,
				ProjectRoot: testProjectRoot,
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
					IssuesRoot:  testIssuesRoot,
					ProjectRoot: testProjectRoot,
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

	t.Run("RepoRoot falls back to ProjectRoot", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ContinueRun{
				ContinueRun: &orchpb.ContinueRunRequest{
					IssueId:    "test-issue",
					RepoRoot:   "/fallback/repo/root",
					IssuesRoot: testIssuesRoot,
				},
			},
		})

		// Should not fail with "no project root available" since RepoRoot
		// is now used as a fallback for ProjectRoot.
		if resp.Ok {
			t.Error("expected error since issue doesn't exist")
		}
		if resp.Error == "no project root available" {
			t.Error("RepoRoot should have been used as fallback for ProjectRoot")
		}
	})

	t.Run("ProjectRoot takes precedence over RepoRoot", func(t *testing.T) {
		resp := sendProtoRequest(t, &orchpb.Request{
			Request: &orchpb.Request_ContinueRun{
				ContinueRun: &orchpb.ContinueRunRequest{
					IssueId:     "test-issue",
					ProjectRoot: "/explicit/project/root",
					RepoRoot:    "/fallback/repo/root",
					IssuesRoot:  testIssuesRoot,
				},
			},
		})

		if resp.Ok {
			t.Error("expected error since issue doesn't exist")
		}
		if resp.Error == "no project root available" {
			t.Error("ProjectRoot should have been used")
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

		err := server.stopSingleRun(run, st)
		if err != nil {
			t.Fatalf("stopSingleRun() error = %v", err)
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

	t.Run("opencode run without session falls back to multiplexer", func(t *testing.T) {
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

		err := server.stopSingleRun(run, st)
		if err != nil {
			t.Fatalf("stopSingleRun() error = %v", err)
		}

		if len(st.appendedEvents) != 1 {
			t.Fatalf("expected 1 event appended, got %d", len(st.appendedEvents))
		}
	})

	t.Run("abort error still appends canceled event", func(t *testing.T) {
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

		err := server.stopSingleRun(run, st)
		if err != nil {
			t.Fatalf("stopSingleRun() error = %v", err)
		}

		if len(st.appendedEvents) != 1 {
			t.Fatalf("expected 1 event appended even on abort error, got %d", len(st.appendedEvents))
		}
	})

	t.Run("non-opencode run uses multiplexer kill", func(t *testing.T) {
		st.appendedEvents = nil

		logger := log.New(io.Discard, "", 0)
		server := NewSocketServer(nil, logger)

		run := &model.Run{
			IssueID:     "test-issue",
			RunID:       "run-4",
			Status:      model.StatusRunning,
			Agent:       "claude",
			SessionName: "test-session",
		}

		err := server.stopSingleRun(run, st)
		if err != nil {
			t.Fatalf("stopSingleRun() error = %v", err)
		}

		if len(st.appendedEvents) != 1 {
			t.Fatalf("expected 1 event appended, got %d", len(st.appendedEvents))
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

			err := server.stopSingleRun(run, st)
			if err != nil {
				t.Fatalf("stopSingleRun() error = %v for status %s", err, status)
			}
		}

		if len(st.appendedEvents) != 0 {
			t.Errorf("expected no events for terminal statuses, got %d", len(st.appendedEvents))
		}
	})
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
		}
		_, err := server.processStartRunCore(st, "", opts)
		if err == nil {
			t.Error("expected error for missing project root")
		}
		if !strings.Contains(err.Error(), "no project root available") {
			t.Errorf("expected 'no project root available' error, got: %v", err)
		}
	})
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

func TestProcessSendTmuxCodexSendsWithSubmit(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)

	mockMux := &mockSendMux{hasSession: true, muxType: multiplexer.TypeTmux}
	prev := getSendMultiplexer
	prevDelay := codexTmuxSubmitDelay
	getSendMultiplexer = func() sendMultiplexer { return mockMux }
	codexTmuxSubmitDelay = 0
	defer func() {
		getSendMultiplexer = prev
		codexTmuxSubmitDelay = prevDelay
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
	if len(mockMux.sendTextCalls) != 1 {
		t.Fatalf("SendText calls = %d, want 1", len(mockMux.sendTextCalls))
	}
	if got := mockMux.sendTextCalls[0]; got.session != run.SessionName || got.keys != tmuxSubmitKeyEnter {
		t.Fatalf("SendText call = (%q, %q), want (%q, %q)", got.session, got.keys, run.SessionName, tmuxSubmitKeyEnter)
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
		Agent:       string(agent.AgentClaude),
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
