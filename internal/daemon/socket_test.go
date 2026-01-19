package daemon

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/xdg"
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
	return nil, nil
}

func (m *mockStore) GetLatestRun(issueID string) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) RootPath() string {
	return ""
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

	conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := SendRequest{
		Type:    "send",
		IssueID: "issue",
		RunID:   "run",
		Message: "test message",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	decoder := json.NewDecoder(conn)
	var resp SendResponse
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if !resp.OK {
		t.Errorf("expected OK=true, got error: %s", resp.Error)
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

func sendRequest(t *testing.T, req SendRequest) *json.Decoder {
	if req.ProjectRoot == "" {
		req.ProjectRoot = testProjectRoot
	}

	conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	return json.NewDecoder(conn)
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
		decoder := sendRequest(t, SendRequest{Type: "list_runs"})
		var resp ListRunsResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Total != 3 {
			t.Errorf("expected 3 runs, got %d", resp.Total)
		}
	})

	t.Run("filter by issue_id", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_runs", IssueID: "orch-001"})
		var resp ListRunsResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Total != 2 {
			t.Errorf("expected 2 runs for orch-001, got %d", resp.Total)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_runs", Status: []string{"running"}})
		var resp ListRunsResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Total != 2 {
			t.Errorf("expected 2 running runs, got %d", resp.Total)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_runs", Limit: 1})
		var resp ListRunsResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if len(resp.Runs) != 1 {
			t.Errorf("expected 1 run with limit=1, got %d", len(resp.Runs))
		}
		if resp.NextCursor == nil {
			t.Error("expected next_cursor to be set")
		}
		if resp.Total != 3 {
			t.Errorf("expected total=3, got %d", resp.Total)
		}
	})

	t.Run("run summary has URI", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_runs", Limit: 1})
		var resp ListRunsResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Runs) == 0 {
			t.Fatal("expected at least 1 run")
		}
		if resp.Runs[0].URI == "" {
			t.Error("expected URI to be set")
		}
		if resp.Runs[0].ShortID == "" {
			t.Error("expected ShortID to be set")
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
		decoder := sendRequest(t, SendRequest{Type: "get_run", IssueID: "orch-001", RunID: "20250117-010000"})
		var resp GetRunResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Run == nil {
			t.Fatal("expected run to be set")
		}
		if resp.Run.IssueID != "orch-001" {
			t.Errorf("expected IssueID=orch-001, got %s", resp.Run.IssueID)
		}
		if resp.Run.URI == "" {
			t.Error("expected URI to be set")
		}
		if len(resp.Run.Events) != 2 {
			t.Errorf("expected 2 events, got %d", len(resp.Run.Events))
		}
		if resp.Run.ServerPort != 8080 {
			t.Errorf("expected ServerPort=8080, got %d", resp.Run.ServerPort)
		}
	})

	t.Run("get non-existent run", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "get_run", IssueID: "orch-999", RunID: "20250117-010000"})
		var resp GetRunResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.OK {
			t.Error("expected OK=false for non-existent run")
		}
		if resp.Error != "not_found" {
			t.Errorf("expected error=not_found, got %s", resp.Error)
		}
	})

	t.Run("get run without issue_id", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "get_run", RunID: "20250117-010000"})
		var resp GetRunResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.OK {
			t.Error("expected OK=false when issue_id missing")
		}
		if resp.Error != "invalid_request: issue_id required" {
			t.Errorf("expected invalid_request error, got %s", resp.Error)
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
		decoder := sendRequest(t, SendRequest{Type: "list_issues"})
		var resp ListIssuesResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Total != 3 {
			t.Errorf("expected 3 issues, got %d", resp.Total)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_issues", Status: []string{"open"}})
		var resp ListIssuesResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Total != 2 {
			t.Errorf("expected 2 open issues, got %d", resp.Total)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_issues", Limit: 2})
		var resp ListIssuesResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if len(resp.Issues) != 2 {
			t.Errorf("expected 2 issues with limit=2, got %d", len(resp.Issues))
		}
		if resp.NextCursor == nil {
			t.Error("expected next_cursor to be set")
		}
	})

	t.Run("issue summary has URI", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "list_issues", Limit: 1})
		var resp ListIssuesResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Issues) == 0 {
			t.Fatal("expected at least 1 issue")
		}
		if resp.Issues[0].URI == "" {
			t.Error("expected URI to be set")
		}
	})
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
		decoder := sendRequest(t, SendRequest{Type: "get_issue", IssueID: "orch-001"})
		var resp GetIssueResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.OK {
			t.Errorf("expected OK=true, got error: %s", resp.Error)
		}
		if resp.Issue == nil {
			t.Fatal("expected issue to be set")
		}
		if resp.Issue.ID != "orch-001" {
			t.Errorf("expected ID=orch-001, got %s", resp.Issue.ID)
		}
		if resp.Issue.Body == "" {
			t.Error("expected Body to be set")
		}
		if resp.Issue.URI == "" {
			t.Error("expected URI to be set")
		}
		if resp.Issue.Frontmatter == nil {
			t.Error("expected Frontmatter to be set")
		}
	})

	t.Run("get non-existent issue", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "get_issue", IssueID: "orch-999"})
		var resp GetIssueResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.OK {
			t.Error("expected OK=false for non-existent issue")
		}
		if resp.Error != "not_found" {
			t.Errorf("expected error=not_found, got %s", resp.Error)
		}
	})

	t.Run("get issue without issue_id", func(t *testing.T) {
		decoder := sendRequest(t, SendRequest{Type: "get_issue"})
		var resp GetIssueResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.OK {
			t.Error("expected OK=false when issue_id missing")
		}
		if resp.Error != "invalid_request: issue_id required" {
			t.Errorf("expected invalid_request error, got %s", resp.Error)
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

	conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := SendRequest{
		Type:       "list_issues",
		IssuesRoot: "/test/issues/path",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	decoder := json.NewDecoder(conn)
	var resp ListIssuesResponse
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
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
		conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		req := SendRequest{
			Type:       "list_issues",
			IssuesRoot: "/reuse/test/path",
		}
		encoder := json.NewEncoder(conn)
		if err := encoder.Encode(req); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		decoder := json.NewDecoder(conn)
		var resp ListIssuesResponse
		if err := decoder.Decode(&resp); err != nil {
			t.Fatalf("failed to read response: %v", err)
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

func TestRegisterRepoAPI(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := SendRequest{
		Type:        "register_repo",
		ProjectRoot: "/new/project/path",
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	decoder := json.NewDecoder(conn)
	var resp map[string]interface{}
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	if resp["repo_id"] == nil || resp["repo_id"] == "" {
		t.Error("expected repo_id to be set")
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

	conn, err := net.DialTimeout("unix", xdg.SocketPath(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := SendRequest{Type: "list_repos"}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	decoder := json.NewDecoder(conn)
	var resp map[string]interface{}
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	repos, ok := resp["repos"].([]interface{})
	if !ok {
		t.Fatal("expected repos to be an array")
	}
	if len(repos) == 0 {
		t.Error("expected at least one registered repo")
	}
}
