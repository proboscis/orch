package pr

import (
	"fmt"
	"testing"
	"time"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
)

type fakeGitHubClient struct {
	available bool
	output    []byte
	err       error

	lastDir   string
	lastArgs  []string
	callCount int
}

func (f *fakeGitHubClient) IsAvailable() bool {
	return f.available
}

func (f *fakeGitHubClient) Run(args ...string) ([]byte, error) {
	f.callCount++
	f.lastArgs = append([]string(nil), args...)
	return f.output, f.err
}

func (f *fakeGitHubClient) RunInDir(dir string, args ...string) ([]byte, error) {
	f.callCount++
	f.lastDir = dir
	f.lastArgs = append([]string(nil), args...)
	return f.output, f.err
}

func TestLookupInfoWithClient_UsesInjectedClient(t *testing.T) {
	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/7","number":7,"state":"OPEN"}]`),
	}

	info, err := LookupInfoWithClient(client, "/tmp/repo", "feature/test")
	if err != nil {
		t.Fatalf("LookupInfoWithClient returned error: %v", err)
	}
	if info == nil {
		t.Fatalf("LookupInfoWithClient returned nil info")
	}
	if info.URL != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("info.URL = %q", info.URL)
	}
	if info.Number != 7 {
		t.Fatalf("info.Number = %d", info.Number)
	}
	if info.State != "OPEN" {
		t.Fatalf("info.State = %q", info.State)
	}
	if client.lastDir != "/tmp/repo" {
		t.Fatalf("RunInDir dir = %q", client.lastDir)
	}
}

func TestPopulateRunInfoWithClient_UsesInjectedClient(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/19","number":19,"state":"OPEN"}]`),
	}

	runs := []*model.Run{
		{
			IssueID: "orch-446",
			RunID:   "run-1",
			Branch:  "feature/cache",
		},
	}

	infoMap := PopulateRunInfoWithClient(client, runs)
	info := infoMap["feature/cache"]
	if info == nil {
		t.Fatalf("expected PR info for branch")
	}
	if info.URL != "https://github.com/acme/repo/pull/19" {
		t.Fatalf("info.URL = %q", info.URL)
	}
	if runs[0].PRUrl != "https://github.com/acme/repo/pull/19" {
		t.Fatalf("run.PRUrl = %q", runs[0].PRUrl)
	}
}

// ---------------------------------------------------------------------------
// Regression tests: verify existing PopulateRunInfo caching works (must PASS)
// ---------------------------------------------------------------------------

func TestPopulateRunInfo_RespectsMaxFetches(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/1","number":1,"state":"OPEN"}]`),
	}

	runs := make([]*model.Run, 6)
	for i := range runs {
		runs[i] = &model.Run{
			IssueID: "test",
			RunID:   fmt.Sprintf("run-%d", i),
			Branch:  fmt.Sprintf("branch-%d", i),
		}
	}

	PopulateRunInfoWithClient(client, runs)

	if client.callCount > cacheMaxFetches {
		t.Fatalf("expected at most %d API calls (cacheMaxFetches), got %d",
			cacheMaxFetches, client.callCount)
	}
}

func TestPopulateRunInfo_RespectsMinFetchInterval(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/1","number":1,"state":"OPEN"}]`),
	}

	runs := []*model.Run{
		{IssueID: "test", RunID: "run-1", Branch: "branch-a"},
	}

	PopulateRunInfoWithClient(client, runs)
	firstCallCount := client.callCount

	runs2 := []*model.Run{
		{IssueID: "test", RunID: "run-2", Branch: "branch-b"},
	}
	PopulateRunInfoWithClient(client, runs2)

	if client.callCount != firstCallCount {
		t.Fatalf("expected 0 additional API calls within cacheMinFetchInterval, got %d",
			client.callCount-firstCallCount)
	}
}

func TestPopulateRunInfo_CacheHitSkipsAPI(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	repoRoot, err := git.FindMainRepoRoot("")
	if err != nil {
		t.Skip("no git repo root available")
	}
	branch := "feature/pre-cached"

	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}
	preCached := cache{
		Entries: map[string]cacheEntry{
			branch: {
				URL:       "https://github.com/acme/repo/pull/77",
				Number:    77,
				State:     "OPEN",
				CheckedAt: time.Now(),
			},
		},
	}
	saveCache(cachePath, preCached)

	client := &fakeGitHubClient{available: true}

	run := &model.Run{
		IssueID: "test",
		RunID:   "run-1",
		Branch:  branch,
	}

	infoMap := PopulateRunInfoWithClient(client, []*model.Run{run})
	info := infoMap[branch]

	if info == nil {
		t.Fatal("expected cached info, got nil")
	}
	if info.URL != "https://github.com/acme/repo/pull/77" {
		t.Fatalf("expected cached URL, got %q", info.URL)
	}
	if client.callCount != 0 {
		t.Fatalf("expected 0 API calls (cache hit), got %d", client.callCount)
	}
}

// ---------------------------------------------------------------------------
// TDD RED tests: define desired caching for currently-uncached functions
// These tests FAIL against current code — fix tracked by orch-gh-ratelimit
// ---------------------------------------------------------------------------

func TestLookupInfoWithClient_MustUseCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/42","number":42,"state":"OPEN"}]`),
	}
	repoRoot := t.TempDir()
	branch := "feature/must-cache"

	info1, err := LookupInfoWithClient(client, repoRoot, branch)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if info1 == nil {
		t.Fatal("first call: expected info")
	}
	if client.callCount != 1 {
		t.Fatalf("first call: expected 1 API call, got %d", client.callCount)
	}

	info2, err := LookupInfoWithClient(client, repoRoot, branch)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if info2 == nil {
		t.Fatal("second call: expected cached info, got nil")
	}
	if client.callCount != 1 {
		t.Fatalf("second call: expected still 1 API call (cache hit), got %d", client.callCount)
	}
	if info2.URL != info1.URL || info2.Number != info1.Number {
		t.Fatal("cached info doesn't match original")
	}
}

func TestLookupInfoWithClient_CacheHitSkipsAPI(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	repoRoot := t.TempDir()
	branch := "feature/pre-populated"

	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}
	preCached := cache{
		LastFetch: time.Now(),
		Entries: map[string]cacheEntry{
			branch: {
				URL:       "https://github.com/acme/repo/pull/99",
				Number:    99,
				State:     "MERGED",
				CheckedAt: time.Now(),
			},
		},
	}
	saveCache(cachePath, preCached)

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/999","number":999,"state":"OPEN"}]`),
	}

	info, err := LookupInfoWithClient(client, repoRoot, branch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected cached info, got nil")
	}
	if client.callCount != 0 {
		t.Fatalf("expected 0 API calls (cache hit), got %d", client.callCount)
	}
	if info.URL != "https://github.com/acme/repo/pull/99" {
		t.Fatalf("expected cached URL https://github.com/acme/repo/pull/99, got %q (cache was ignored)", info.URL)
	}
	if info.Number != 99 {
		t.Fatalf("expected cached number 99, got %d", info.Number)
	}
	if info.State != "MERGED" {
		t.Fatalf("expected cached state MERGED, got %q", info.State)
	}
}

func TestLookupInfoByURL_MustUseCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	oldClient := ghClient
	defer func() { ghClient = oldClient }()

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`{"url":"https://github.com/acme/repo/pull/42","number":42,"state":"OPEN"}`),
	}
	ghClient = client

	prURL := "https://github.com/acme/repo/pull/42"

	info1, err := LookupInfoByURL(prURL)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if info1 == nil {
		t.Fatal("first call: expected info")
	}
	if client.callCount != 1 {
		t.Fatalf("first call: expected 1 API call, got %d", client.callCount)
	}

	info2, err := LookupInfoByURL(prURL)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if info2 == nil {
		t.Fatal("second call: expected cached info, got nil")
	}
	if client.callCount != 1 {
		t.Fatalf("second call: expected still 1 API call (cache hit), got %d", client.callCount)
	}
	if info2.URL != info1.URL || info2.Number != info1.Number {
		t.Fatal("cached info doesn't match original")
	}
}

func TestLookupInfoByURL_CacheHitSkipsAPI(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	oldClient := ghClient
	defer func() { ghClient = oldClient }()

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`{"url":"https://github.com/acme/repo/pull/55","number":55,"state":"MERGED"}`),
	}
	ghClient = client

	prURL := "https://github.com/acme/repo/pull/55"

	info1, err := LookupInfoByURL(prURL)
	if err != nil {
		t.Fatalf("seed call error: %v", err)
	}
	if info1 == nil {
		t.Fatal("seed call: expected info")
	}

	seedCalls := client.callCount

	info2, err := LookupInfoByURL(prURL)
	if err != nil {
		t.Fatalf("cached call error: %v", err)
	}
	if info2 == nil {
		t.Fatal("cached call: expected info from cache, got nil")
	}
	if client.callCount != seedCalls {
		t.Fatalf("cached call: expected 0 additional API calls, got %d", client.callCount-seedCalls)
	}
	if info2.State != "MERGED" {
		t.Fatalf("cached call: expected state MERGED, got %q", info2.State)
	}
}

func TestCacheEntryTTL_TerminalStateGetsLongTTL(t *testing.T) {
	for _, state := range []string{"MERGED", "CLOSED", "merged", "closed"} {
		ttl := cacheEntryTTL(cacheEntry{URL: "https://example.com/pr/1", State: state})
		if ttl != cacheHitTerminalTTL {
			t.Errorf("state %q: expected TTL %v, got %v", state, cacheHitTerminalTTL, ttl)
		}
	}
}

func TestCacheEntryTTL_ActiveStateGetsShortTTL(t *testing.T) {
	ttl := cacheEntryTTL(cacheEntry{URL: "https://example.com/pr/1", State: "OPEN"})
	if ttl != cacheHitActiveTTL {
		t.Errorf("OPEN state: expected TTL %v, got %v", cacheHitActiveTTL, ttl)
	}
}

func TestCacheEntryTTL_MissGetsShortTTL(t *testing.T) {
	ttl := cacheEntryTTL(cacheEntry{})
	if ttl != cacheMissTTL {
		t.Errorf("miss: expected TTL %v, got %v", cacheMissTTL, ttl)
	}
}

func TestOpenPRCacheExpiresFast(t *testing.T) {
	now := time.Now()
	entry := cacheEntry{
		URL:       "https://github.com/acme/repo/pull/1",
		Number:    1,
		State:     "OPEN",
		CheckedAt: now.Add(-2 * time.Minute),
	}
	if isCacheEntryFresh(entry, now) {
		t.Fatal("OPEN PR cached 2 minutes ago should be stale")
	}
}

func TestMergedPRCacheStaysFresh(t *testing.T) {
	now := time.Now()
	entry := cacheEntry{
		URL:       "https://github.com/acme/repo/pull/1",
		Number:    1,
		State:     "MERGED",
		CheckedAt: now.Add(-1 * time.Hour),
	}
	if !isCacheEntryFresh(entry, now) {
		t.Fatal("MERGED PR cached 1 hour ago should still be fresh")
	}
}
