package git

import (
	"os"
	"testing"
	"time"
)

func TestFetchWithTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fetch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	err = FetchWithTimeout("/nonexistent/path", "", 1*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestFetchWithTimeoutShortDuration(t *testing.T) {
	err := FetchWithTimeout(".", "", 1*time.Nanosecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestMaybeRefreshRemoteRefs_ThrottlesRepeatedCalls(t *testing.T) {
	fetchMu.Lock()
	lastFetchTime = time.Now()
	fetchMu.Unlock()

	worktrees := []struct {
		Path       string
		Branch     string
		BaseBranch string
	}{
		{Path: "/tmp/fake-worktree", Branch: "feature/x", BaseBranch: "main"},
	}

	savedTime := lastFetchTime
	maybeRefreshRemoteRefs(worktrees)

	fetchMu.Lock()
	afterTime := lastFetchTime
	fetchMu.Unlock()

	if !afterTime.Equal(savedTime) {
		t.Fatal("expected fetch to be throttled (lastFetchTime should not change)")
	}
}

func TestMaybeRefreshRemoteRefs_EmptyWorktrees(t *testing.T) {
	fetchMu.Lock()
	lastFetchTime = time.Time{}
	fetchMu.Unlock()

	maybeRefreshRemoteRefs(nil)

	fetchMu.Lock()
	ft := lastFetchTime
	fetchMu.Unlock()

	if !ft.IsZero() {
		t.Fatal("expected no fetch for empty worktrees")
	}
}
