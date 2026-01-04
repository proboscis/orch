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
	runs map[string]*model.Run
}

func (m *mockStore) ResolveIssue(issueID string) (*model.Issue, error) {
	return nil, nil
}

func (m *mockStore) ListIssues() ([]*model.Issue, error) {
	return nil, nil
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
	return nil, nil
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

	if IsDaemonSocketAvailable(tmpDir) {
		t.Error("expected socket not available without running daemon")
	}
}

func TestIsDaemonSocketAvailableWithDaemon(t *testing.T) {
	tmpDir, err := os.MkdirTemp("/tmp", "orch-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	orchDir := filepath.Join(tmpDir, ".orch")
	os.MkdirAll(orchDir, 0755)

	if err := WritePID(tmpDir); err != nil {
		t.Fatal(err)
	}

	socketPath := SocketFilePath(tmpDir)
	f, _ := os.Create(socketPath)
	f.Close()

	if !IsDaemonSocketAvailable(tmpDir) {
		t.Error("expected socket available with running daemon and socket file")
	}
}
