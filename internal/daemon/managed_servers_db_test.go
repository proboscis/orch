package daemon

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

func setupManagedServerDBEnv(t *testing.T) {
	t.Helper()

	baseDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(baseDir, "data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(baseDir, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(baseDir, "state"))
}

func TestManagedServerStoreCRUD(t *testing.T) {
	setupManagedServerDBEnv(t)

	store, err := newManagedServerStore(xdg.DaemonDBPath())
	if err != nil {
		t.Fatalf("newManagedServerStore() error = %v", err)
	}
	defer store.Close()

	startedAt := time.Now().Add(-1 * time.Minute).UTC().Truncate(time.Second)
	record := managedServerRecord{
		RepoID:      "repo-a",
		ProjectRoot: "/tmp/project-a",
		PID:         12345,
		Port:        4096,
		LogPath:     "/tmp/opencode-a.log",
		StartedAt:   startedAt,
	}

	if err := store.Upsert(record); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	rows, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].RepoID != record.RepoID {
		t.Fatalf("expected repo_id %q, got %q", record.RepoID, rows[0].RepoID)
	}
	if rows[0].ProjectRoot != record.ProjectRoot {
		t.Fatalf("expected project_root %q, got %q", record.ProjectRoot, rows[0].ProjectRoot)
	}
	if rows[0].PID != record.PID {
		t.Fatalf("expected pid %d, got %d", record.PID, rows[0].PID)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpdateLastHealthy(record.RepoID, now); err != nil {
		t.Fatalf("UpdateLastHealthy() error = %v", err)
	}

	rows, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after update, got %d", len(rows))
	}
	if rows[0].LastHealthy.IsZero() {
		t.Fatal("expected non-zero last_healthy after update")
	}

	if err := store.Delete(record.RepoID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	rows, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows after delete, got %d", len(rows))
	}
}

func TestManagedServerStoreMigratesRuntimeTables(t *testing.T) {
	setupManagedServerDBEnv(t)

	store, err := newManagedServerStore(xdg.DaemonDBPath())
	if err != nil {
		t.Fatalf("newManagedServerStore() error = %v", err)
	}
	defer store.Close()

	tables := []string{
		"managed_servers",
		"events",
		"run_state_projection",
		"issue_state_projection",
		"idempotency_keys",
		"outbox",
	}

	for _, table := range tables {
		var name string
		err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %q to exist: %v", table, err)
		}
		if name != table {
			t.Fatalf("sqlite returned table %q, want %q", name, table)
		}
	}
}

func TestReconcileManagedServersOnStartupAdoptsHealthyServer(t *testing.T) {
	setupManagedServerDBEnv(t)

	projectRoot := "/tmp/orch-worktree-adopt"
	port, shutdown := startFakeOpenCodeServer(t, projectRoot)
	defer shutdown()

	proc := exec.Command("sleep", "30")
	if err := proc.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = proc.Process.Kill()
		_, _ = proc.Process.Wait()
	})

	store, err := newManagedServerStore(xdg.DaemonDBPath())
	if err != nil {
		t.Fatalf("newManagedServerStore() error = %v", err)
	}
	defer store.Close()

	if err := store.Upsert(managedServerRecord{
		RepoID:      "repo-adopt",
		ProjectRoot: projectRoot,
		PID:         proc.Process.Pid,
		Port:        port,
		LogPath:     "/tmp/opencode-adopt.log",
		StartedAt:   time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.managedServerStore = store
	if err := server.reconcileManagedServersOnStartup(); err != nil {
		t.Fatalf("reconcileManagedServersOnStartup() error = %v", err)
	}

	srv, ok := server.openCodeServers["repo-adopt"]
	if !ok {
		t.Fatalf("expected adopted server entry for %s", "repo-adopt")
	}
	if !srv.Adopted {
		t.Fatal("expected adopted server flag to be true")
	}
	if srv.PID != proc.Process.Pid {
		t.Fatalf("expected pid %d, got %d", proc.Process.Pid, srv.PID)
	}
	if srv.Cmd != nil {
		t.Fatal("expected adopted server Cmd to be nil")
	}

	rows, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 persisted record after adoption, got %d", len(rows))
	}
	if rows[0].LastHealthy.IsZero() {
		t.Fatal("expected last_healthy to be updated for adopted server")
	}
}

func TestReconcileManagedServersOnStartupRemovesDeadRecord(t *testing.T) {
	setupManagedServerDBEnv(t)

	store, err := newManagedServerStore(xdg.DaemonDBPath())
	if err != nil {
		t.Fatalf("newManagedServerStore() error = %v", err)
	}
	defer store.Close()

	deadPID := nonExistentPID()
	if err := store.Upsert(managedServerRecord{
		RepoID:      "repo-dead",
		ProjectRoot: "/tmp/orch-worktree-dead",
		PID:         deadPID,
		Port:        4099,
		StartedAt:   time.Now().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.managedServerStore = store
	if err := server.reconcileManagedServersOnStartup(); err != nil {
		t.Fatalf("reconcileManagedServersOnStartup() error = %v", err)
	}

	if len(server.openCodeServers) != 0 {
		t.Fatalf("expected no adopted servers, got %d", len(server.openCodeServers))
	}

	rows, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected dead record cleanup, got %d records", len(rows))
	}
}

func TestReconcileManagedServersOnStartupKillsUnhealthyProcess(t *testing.T) {
	setupManagedServerDBEnv(t)

	pid := startDetachedSleepProcess(t, 30)
	t.Cleanup(func() {
		process, err := os.FindProcess(pid)
		if err == nil {
			_ = process.Signal(syscall.SIGKILL)
		}
	})

	port := reserveTCPPort(t)

	store, err := newManagedServerStore(xdg.DaemonDBPath())
	if err != nil {
		t.Fatalf("newManagedServerStore() error = %v", err)
	}
	defer store.Close()

	if err := store.Upsert(managedServerRecord{
		RepoID:      "repo-unhealthy",
		ProjectRoot: "/tmp/orch-worktree-unhealthy",
		PID:         pid,
		Port:        port,
		StartedAt:   time.Now().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	server.managedServerStore = store
	if err := server.reconcileManagedServersOnStartup(); err != nil {
		t.Fatalf("reconcileManagedServersOnStartup() error = %v", err)
	}

	if waitForProcessExit(pid, 2*time.Second) == false {
		t.Fatalf("expected helper process %d to be terminated", pid)
	}

	rows, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected unhealthy record cleanup, got %d records", len(rows))
	}
}

func TestIsServerProcessAliveFallsBackToPID(t *testing.T) {
	proc := exec.Command("sleep", "30")
	if err := proc.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = proc.Process.Kill()
		_, _ = proc.Process.Wait()
	})

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	srv := &managedServer{PID: proc.Process.Pid}

	if !server.isServerProcessAlive(srv) {
		t.Fatalf("expected process %d to be alive", proc.Process.Pid)
	}

	if err := proc.Process.Kill(); err != nil {
		t.Fatalf("failed to kill helper process: %v", err)
	}
	_, _ = proc.Process.Wait()

	if waitForProcessExit(proc.Process.Pid, 1*time.Second) == false {
		t.Fatalf("expected process %d to exit", proc.Process.Pid)
	}

	if server.isServerProcessAlive(srv) {
		t.Fatalf("expected process %d to be dead", proc.Process.Pid)
	}
}

func startFakeOpenCodeServer(t *testing.T, projectRoot string) (int, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
	})
	mux.HandleFunc("/project/current", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"proj","worktree":%q,"sandboxes":[]}`, projectRoot))
	})

	httpServer := &http.Server{Handler: mux}
	go func() {
		_ = httpServer.Serve(listener)
	}()

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}

	return listener.Addr().(*net.TCPAddr).Port, shutdown
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve tcp port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !IsProcessRunning(pid)
}

func nonExistentPID() int {
	candidates := []int{os.Getpid() + 1000000, os.Getpid() + 2000000, 99999999}
	for _, pid := range candidates {
		if !IsProcessRunning(pid) {
			return pid
		}
	}
	return 99999999
}

func startDetachedSleepProcess(t *testing.T, seconds int) int {
	t.Helper()

	cmd := exec.Command("sh", "-c", fmt.Sprintf("sleep %d >/dev/null 2>&1 & echo $!", seconds))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to start detached sleep process: %v", err)
	}

	pidText := strings.TrimSpace(string(out))
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		t.Fatalf("failed to parse detached pid %q: %v", pidText, err)
	}

	return pid
}
