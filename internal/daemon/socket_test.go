package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
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

func (m *mockStore) VaultPath() string {
	return ""
}

func TestSocketFilePath(t *testing.T) {
	path := SocketFilePath("/vault")
	expected := filepath.Join("/vault", ".orch", "daemon.sock")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestSocketServerStartStop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("/tmp", "orch-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	orchDir := filepath.Join(tmpDir, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		t.Fatal(err)
	}

	st := &mockStore{runs: make(map[string]*model.Run)}
	logger := log.New(io.Discard, "", 0)

	server := NewSocketServer(tmpDir, st, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	socketPath := SocketFilePath(tmpDir)
	if _, err := os.Stat(socketPath); err != nil {
		t.Errorf("socket file not created: %v", err)
	}

	server.Stop()

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file not cleaned up")
	}
}

func TestSocketServerSendRequest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("/tmp", "orch-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	orchDir := filepath.Join(tmpDir, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		t.Fatal(err)
	}

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
	logger := log.New(io.Discard, "", 0)

	server := NewSocketServer(tmpDir, st, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	conn, err := net.DialTimeout("unix", SocketFilePath(tmpDir), 5*time.Second)
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
	tmpDir := t.TempDir()

	if IsDaemonSocketAvailable(tmpDir) {
		t.Error("expected socket not available initially")
	}

	orchDir := filepath.Join(tmpDir, ".orch")
	os.MkdirAll(orchDir, 0755)

	socketPath := SocketFilePath(tmpDir)
	f, _ := os.Create(socketPath)
	f.Close()

	if !IsDaemonSocketAvailable(tmpDir) {
		t.Error("expected socket available after creation")
	}
}

func setupTestServer(t *testing.T, st *mockStore) (*SocketServer, string, func()) {
	tmpDir, err := os.MkdirTemp("/tmp", "orch-test-")
	if err != nil {
		t.Fatal(err)
	}

	orchDir := filepath.Join(tmpDir, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		t.Fatal(err)
	}

	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(tmpDir, st, logger)
	if err := server.Start(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to start server: %v", err)
	}

	cleanup := func() {
		server.Stop()
		os.RemoveAll(tmpDir)
	}

	return server, tmpDir, cleanup
}

func sendRequest(t *testing.T, tmpDir string, req interface{}) *json.Decoder {
	conn, err := net.DialTimeout("unix", SocketFilePath(tmpDir), 5*time.Second)
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

	_, tmpDir, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("list all runs", func(t *testing.T) {
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_runs"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_runs", IssueID: "orch-001"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_runs", Status: []string{"running"}})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_runs", Limit: 1})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_runs", Limit: 1})
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

	_, tmpDir, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("get existing run", func(t *testing.T) {
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "get_run", IssueID: "orch-001", RunID: "20250117-010000"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "get_run", IssueID: "orch-999", RunID: "20250117-010000"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "get_run", RunID: "20250117-010000"})
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

	_, tmpDir, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("list all issues", func(t *testing.T) {
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_issues"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_issues", Status: []string{"open"}})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_issues", Limit: 2})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "list_issues", Limit: 1})
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

	_, tmpDir, cleanup := setupTestServer(t, st)
	defer cleanup()

	t.Run("get existing issue", func(t *testing.T) {
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "get_issue", IssueID: "orch-001"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "get_issue", IssueID: "orch-999"})
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
		decoder := sendRequest(t, tmpDir, SendRequest{Type: "get_issue"})
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
}
