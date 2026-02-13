package daemon

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureOpenCodeServerRunningNormalizesWorktreePath(t *testing.T) {
	repoRoot, worktreeA, worktreeB := createMainRepoWithTwoWorktrees(t)
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))

	normalizedRepoRoot := server.normalizeProjectRoot(repoRoot)
	if normalizedRepoRoot == "" {
		t.Fatal("expected normalized repo root to be non-empty")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
		case "/project/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"proj","worktree":%q,"sandboxes":[]}`, normalizedRepoRoot))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	port := getPortFromURL(t, ts.URL)

	server.openCodeServers[normalizedRepoRoot] = &managedServer{
		ProjectRoot: normalizedRepoRoot,
		Port:        port,
		WaitResult:  make(chan error),
	}

	gotA, err := server.ensureOpenCodeServerRunning(worktreeA)
	if err != nil {
		t.Fatalf("ensureOpenCodeServerRunning(worktreeA) error = %v", err)
	}
	gotB, err := server.ensureOpenCodeServerRunning(worktreeB)
	if err != nil {
		t.Fatalf("ensureOpenCodeServerRunning(worktreeB) error = %v", err)
	}

	if gotA != port || gotB != port {
		t.Fatalf("expected both worktrees to resolve to port %d, got %d and %d", port, gotA, gotB)
	}
	if len(server.openCodeServers) != 1 {
		t.Fatalf("expected one server entry, got %d", len(server.openCodeServers))
	}
	if _, ok := server.openCodeServers[normalizedRepoRoot]; !ok {
		t.Fatalf("expected server map entry keyed by repo root %q", normalizedRepoRoot)
	}

	gotPort := server.getOpenCodeServerPort(worktreeB)
	if gotPort != port {
		t.Fatalf("getOpenCodeServerPort(worktreeB) = %d, want %d", gotPort, port)
	}
}
