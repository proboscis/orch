package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
)

// TestOpenCodeSessionLookupWithWorktreePath verifies that session lookup works
// correctly when the run uses a worktree path.
//
// Fix for orch-308: Queries without directory filter because OpenCode scopes
// sessions by project (git root), not by exact worktree path.
func TestOpenCodeSessionLookupWithWorktreePath(t *testing.T) {
	worktreePath := "/test/repo/.git-worktrees/issue-123/abc123_run"
	sessionID := "ses_test_worktree"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/global/health":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"healthy": true,
				"version": "test",
			})
		case "/project/current":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       "test-project",
				"worktree": worktreePath,
			})
		case "/session":
			// Return sessions regardless of directory header (orch-308 fix)
			json.NewEncoder(w).Encode([]agent.Session{
				{ID: sessionID, Directory: worktreePath},
			})
		case "/session/status":
			// Return status regardless of directory header (orch-308 fix)
			json.NewEncoder(w).Encode(map[string]string{
				sessionID: "busy",
			})
		case "/session/" + sessionID:
			json.NewEncoder(w).Encode(agent.Session{
				ID:        sessionID,
				Directory: worktreePath,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	port := extractTestPort(server.URL)

	manager := &agent.OpenCodeManager{
		Port:      port,
		SessionID: sessionID,
		Directory: worktreePath,
		RunRef:    "issue-123#run-001",
	}

	run := &model.Run{
		IssueID:           "issue-123",
		RunID:             "run-001",
		Agent:             "opencode",
		Status:            model.StatusRunning,
		WorktreePath:      worktreePath,
		OpenCodeSessionID: sessionID,
		ServerPort:        port,
	}

	// Test 1: IsAlive should return true when server is running for this worktree
	alive := manager.IsAlive(run)
	if !alive {
		t.Error("IsAlive() returned false - server should be alive for matching worktree")
	}

	// Test 2: GetStatus should detect busy status as running
	state := &agent.RunState{}
	status := manager.GetStatus(run, "", state, false, false)
	if status != model.StatusRunning {
		t.Errorf("GetStatus() = %v, want %v (running when agent is busy)", status, model.StatusRunning)
	}
}

// TestOpenCodeSessionDirectoryMismatchDetection verifies that session detection
// works correctly when manager directory differs from server worktree.
// With orch-308 fix, sessions are queried without directory filter.
func TestOpenCodeSessionDirectoryMismatchDetection(t *testing.T) {
	mainRepoPath := "/test/repo"
	worktreePath := "/test/repo/.git-worktrees/issue-123/abc123_run"
	sessionID := "ses_worktree_session"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/global/health":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"healthy": true,
				"version": "test",
			})
		case "/project/current":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       "test-project",
				"worktree": worktreePath,
			})
		case "/session":
			// Return all sessions without directory filtering (orch-308 fix)
			json.NewEncoder(w).Encode([]agent.Session{
				{ID: sessionID, Directory: worktreePath},
			})
		case "/session/status":
			// Return status for all sessions without directory filtering (orch-308 fix)
			json.NewEncoder(w).Encode(map[string]string{
				sessionID: "idle",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	port := extractTestPort(server.URL)

	// Manager with worktree path
	correctManager := &agent.OpenCodeManager{
		Port:      port,
		SessionID: sessionID,
		Directory: worktreePath,
		RunRef:    "issue-123#run-001",
	}

	run := &model.Run{
		IssueID:           "issue-123",
		RunID:             "run-001",
		Agent:             "opencode",
		Status:            model.StatusRunning,
		WorktreePath:      worktreePath,
		OpenCodeSessionID: sessionID,
		ServerPort:        port,
	}

	// Server is running for this worktree
	if !correctManager.IsAlive(run) {
		t.Error("IsAlive() should return true when server is running for worktree")
	}

	// Manager with main repo path (different from server worktree)
	wrongManager := &agent.OpenCodeManager{
		Port:      port,
		SessionID: sessionID,
		Directory: mainRepoPath,
		RunRef:    "issue-123#run-001",
	}

	// IsAlive checks server worktree, not session directory
	if wrongManager.IsAlive(run) {
		t.Error("IsAlive() should return false when manager directory doesn't match server worktree")
	}
}

// TestNewRunStatusNotImmediatelyDone verifies that a new run doesn't
// immediately show "done" status. With orch-308 fix, sessions are queried
// without directory filter to ensure they are always found.
func TestNewRunStatusNotImmediatelyDone(t *testing.T) {
	worktreePath := filepath.Join(testRepo, ".git-worktrees", "orch-308-test", "worktree")
	sessionID := "ses_new_run"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/global/health":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"healthy": true,
				"version": "test",
			})
		case "/project/current":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       "test-project",
				"worktree": worktreePath,
			})
		case "/session":
			// Return sessions without directory filtering (orch-308 fix)
			json.NewEncoder(w).Encode([]agent.Session{
				{ID: sessionID, Directory: worktreePath},
			})
		case "/session/status":
			// Return status without directory filtering (orch-308 fix)
			json.NewEncoder(w).Encode(map[string]string{
				sessionID: "busy",
			})
		case "/session/" + sessionID:
			json.NewEncoder(w).Encode(agent.Session{
				ID:        sessionID,
				Directory: worktreePath,
				Time: agent.SessionTimeMillis{
					Updated: time.Now().UnixMilli(),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	port := extractTestPort(server.URL)

	run := &model.Run{
		IssueID:           "orch-308-test",
		RunID:             time.Now().Format("20060102-150405"),
		Agent:             "opencode",
		Status:            model.StatusBooting,
		WorktreePath:      worktreePath,
		OpenCodeSessionID: sessionID,
		ServerPort:        port,
	}

	manager := &agent.OpenCodeManager{
		Port:      port,
		SessionID: sessionID,
		Directory: worktreePath,
		RunRef:    run.Ref().String(),
	}

	if !manager.IsAlive(run) {
		t.Fatal("IsAlive() should return true for server running on matching worktree")
	}

	state := &agent.RunState{}
	status := manager.GetStatus(run, "", state, false, false)

	if status == model.StatusDone || status == model.StatusUnknown {
		t.Errorf("New run should not have status %v immediately after creation", status)
	}

	if status != model.StatusRunning {
		t.Errorf("GetStatus() = %v, want %v for busy session", status, model.StatusRunning)
	}
}

func extractTestPort(url string) int {
	var port int
	fmt.Sscanf(url, "http://127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(url, "http://localhost:%d", &port)
	}
	if port == 0 {
		// Try extracting from [::1] format
		if strings.Contains(url, "[::1]:") {
			fmt.Sscanf(url, "http://[::1]:%d", &port)
		}
	}
	return port
}
