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
// correctly when the run uses a worktree path (orch-347 fix).
//
// This test simulates the exact bug scenario:
// 1. OpenCode server is running
// 2. Session is created with a worktree directory
// 3. Daemon queries for session status
// 4. Session should be found (not return "done" prematurely)
func TestOpenCodeSessionLookupWithWorktreePath(t *testing.T) {
	worktreePath := "/test/repo/.git-worktrees/issue-123/abc123_run"
	sessionID := "ses_test_worktree"

	var receivedDirHeader string
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
			receivedDirHeader = r.Header.Get("X-OpenCode-Directory")
			json.NewEncoder(w).Encode([]agent.Session{
				{ID: sessionID, Directory: worktreePath},
			})
		case "/session/status":
			receivedDirHeader = r.Header.Get("X-OpenCode-Directory")
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

	// Test 1: IsAlive should return true when server is running for this worktree (orch-354)
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

	// Verify the directory header was sent for status query
	if receivedDirHeader != worktreePath {
		t.Errorf("Expected X-OpenCode-Directory header %q for status query, got %q", worktreePath, receivedDirHeader)
	}
}

// TestOpenCodeSessionNotFoundWithoutDirectory verifies that without proper
// directory scoping, the session might not be found (the bug we fixed).
func TestOpenCodeSessionDirectoryMismatchDetection(t *testing.T) {
	mainRepoPath := "/test/repo"
	worktreePath := "/test/repo/.git-worktrees/issue-123/abc123_run"
	sessionID := "ses_worktree_session"

	// Server serves the worktree path - manager with matching path will find it alive
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dir := r.Header.Get("X-OpenCode-Directory")

		switch r.URL.Path {
		case "/global/health":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"healthy": true,
				"version": "test",
			})
		case "/project/current":
			// Server is running for worktreePath
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       "test-project",
				"worktree": worktreePath,
			})
		case "/session":
			if dir == worktreePath {
				json.NewEncoder(w).Encode([]agent.Session{
					{ID: sessionID, Directory: worktreePath},
				})
			} else if dir == mainRepoPath || dir == "" {
				json.NewEncoder(w).Encode([]agent.Session{})
			} else {
				json.NewEncoder(w).Encode([]agent.Session{})
			}
		case "/session/status":
			if dir == worktreePath {
				json.NewEncoder(w).Encode(map[string]string{
					sessionID: "idle",
				})
			} else {
				json.NewEncoder(w).Encode(map[string]string{})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	port := extractTestPort(server.URL)

	// Manager with CORRECT directory (worktree path)
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

	// With correct directory, server is running for this worktree (orch-354)
	if !correctManager.IsAlive(run) {
		t.Error("With correct directory, IsAlive() should return true")
	}

	// Manager with WRONG directory (main repo instead of worktree)
	wrongManager := &agent.OpenCodeManager{
		Port:      port,
		SessionID: sessionID,
		Directory: mainRepoPath,
		RunRef:    "issue-123#run-001",
	}

	// With wrong directory, IsAlive returns false because server worktree doesn't match
	if wrongManager.IsAlive(run) {
		t.Error("With wrong directory, IsAlive() should return false (server worktree doesn't match)")
	}
}

// TestNewRunStatusNotImmediatelyDone verifies that a new run doesn't
// immediately show "done" status (the main symptom of orch-347).
func TestNewRunStatusNotImmediatelyDone(t *testing.T) {
	worktreePath := filepath.Join(testRepo, ".git-worktrees", "orch-347-test", "worktree")
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
			dir := r.Header.Get("X-OpenCode-Directory")
			if dir == worktreePath {
				json.NewEncoder(w).Encode([]agent.Session{
					{ID: sessionID, Directory: worktreePath},
				})
			} else {
				json.NewEncoder(w).Encode([]agent.Session{})
			}
		case "/session/status":
			dir := r.Header.Get("X-OpenCode-Directory")
			if dir == worktreePath {
				json.NewEncoder(w).Encode(map[string]string{
					sessionID: "busy",
				})
			} else {
				json.NewEncoder(w).Encode(map[string]string{})
			}
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
		IssueID:           "orch-347-test",
		RunID:             model.RunID(time.Now().Format("20060102-150405")),
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

	// Verify IsAlive returns true when server is running for this worktree (orch-354)
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
