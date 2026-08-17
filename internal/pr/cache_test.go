package pr

import (
	"fmt"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/model"
)

type fakeGitHubClient struct {
	available bool
	output    []byte
	outputs   [][]byte
	err       error

	lastDir   string
	lastArgs  []string
	callCount int
}

func (f *fakeGitHubClient) IsAvailable() bool {
	return f.available
}

func (f *fakeGitHubClient) nextOutput() []byte {
	callIndex := f.callCount
	f.callCount++
	if callIndex < len(f.outputs) {
		return f.outputs[callIndex]
	}
	return f.output
}

func (f *fakeGitHubClient) Run(args ...string) ([]byte, error) {
	f.lastArgs = append([]string(nil), args...)
	return f.nextOutput(), f.err
}

func (f *fakeGitHubClient) RunInDir(dir string, args ...string) ([]byte, error) {
	f.lastDir = dir
	f.lastArgs = append([]string(nil), args...)
	return f.nextOutput(), f.err
}

func TestLookupInfoWithClient_UsesInjectedClient(t *testing.T) {
	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/7","number":7,"state":"OPEN","headRefName":"feature/test"}]`),
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
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/19","number":19,"state":"OPEN","headRefName":"feature/test"}]`),
	}

	runs := []*model.Run{
		{
			IssueID: "orch-446",
			RunID:   "run-1",
			Branch:  "feature/cache",
		},
	}

	infoMap := populateRunInfoWithClient(client, runs, t.TempDir(), time.Now)
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

// Hermeticity regression: the lookup core must be a function of its arguments,
// not of the directory the test binary runs in. The counterexample this pins is
// the shipped tree being verified on a machine that has no checkout around it
// (the daemon's `.git`-less rsync of the sources): populateRunInfoWithClient
// used to resolve its cache scope from the process working directory, so with no
// ambient repository it returned an empty map and every injected-client test
// silently asserted nothing.
func TestPopulateRunInfoWithClient_DoesNotDependOnAmbientCheckout(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// A directory that is deliberately not inside any git repository.
	t.Chdir(t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/23","number":23,"state":"OPEN","headRefName":"feature/hermetic"}]`),
	}
	runs := []*model.Run{{IssueID: "orch-hermetic", RunID: "run-1", Branch: "feature/hermetic"}}

	infoMap := populateRunInfoWithClient(client, runs, t.TempDir(), time.Now)
	if infoMap["feature/hermetic"] == nil {
		t.Fatal("core consulted the ambient checkout: no PR info returned outside a git repository")
	}
	if client.callCount != 1 {
		t.Fatalf("expected the injected client to be asked exactly once, got %d calls", client.callCount)
	}

	// The ambient read is allowed at the exported boundary and nowhere else, so
	// the exported entrypoint reports "no scope" rather than inventing one.
	ambient := []*model.Run{{IssueID: "orch-hermetic", RunID: "run-2", Branch: "feature/hermetic"}}
	if got := PopulateRunInfoWithClient(client, ambient); len(got) != 0 {
		t.Fatalf("exported entrypoint must return an empty map with no ambient repo, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Regression tests: verify existing PopulateRunInfo caching works (must PASS)
// ---------------------------------------------------------------------------

func TestPopulateRunInfo_RespectsMaxFetches(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/1","number":1,"state":"OPEN","headRefName":"feature/test"}]`),
	}

	runs := make([]*model.Run, 6)
	for i := range runs {
		runs[i] = &model.Run{
			IssueID: "test",
			RunID:   model.RunID(fmt.Sprintf("run-%d", i)),
			Branch:  fmt.Sprintf("branch-%d", i),
		}
	}

	populateRunInfoWithClient(client, runs, t.TempDir(), time.Now)

	if client.callCount > cacheMaxFetches {
		t.Fatalf("expected at most %d API calls (cacheMaxFetches), got %d",
			cacheMaxFetches, client.callCount)
	}
}

func TestPopulateRunInfo_SameKeyCacheHitSkipsFetch(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/1","number":1,"state":"OPEN","headRefName":"feature/test"}]`),
	}

	// Both calls must share one cache scope, or the second call looks up a
	// different cache file and the throttle under test never applies.
	repoRoot := t.TempDir()

	runs := []*model.Run{
		{IssueID: "test", RunID: "run-1", Branch: "branch-a"},
	}

	populateRunInfoWithClient(client, runs, repoRoot, time.Now)
	firstCallCount := client.callCount
	if firstCallCount != 1 {
		t.Fatalf("first populate: expected 1 API call, got %d", firstCallCount)
	}

	runs2 := []*model.Run{
		{IssueID: "test", RunID: "run-2", Branch: "branch-a"},
	}
	populateRunInfoWithClient(client, runs2, repoRoot, time.Now)

	if client.callCount != firstCallCount {
		t.Fatalf("expected 0 additional API calls within cacheMinFetchInterval, got %d",
			client.callCount-firstCallCount)
	}
}

func TestShouldThrottleFetch_IsScopedPerKey(t *testing.T) {
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	c := cache{
		// A recent legacy file-wide timestamp must not throttle any key.
		LastFetch: now,
		LastFetchByKey: map[string]time.Time{
			"recent": now.Add(-cacheMinFetchInterval + time.Second),
			"ready":  now.Add(-cacheMinFetchInterval),
		},
	}

	if !shouldThrottleFetch(c, "recent", now) {
		t.Fatal("same key must be throttled within cacheMinFetchInterval")
	}
	if shouldThrottleFetch(c, "ready", now) {
		t.Fatal("same key must be fetchable at cacheMinFetchInterval")
	}
	if shouldThrottleFetch(c, "other", now) {
		t.Fatal("one key's fetch must not throttle a different key")
	}
}

func TestLookupInfoByURL_ExpiredKeysDoNotStarveEachOther(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	nowFunc := func() time.Time { return now }
	cachePath, err := getCachePath(globalURLCacheScope)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}

	urls := []string{
		"https://github.com/acme/repo/pull/101",
		"https://github.com/acme/repo/pull/102",
		"https://github.com/acme/repo/pull/103",
	}
	entries := make(map[string]cacheEntry, len(urls))
	for i, prURL := range urls {
		entries[urlCacheKey(prURL)] = cacheEntry{
			URL:         prURL,
			Number:      101 + i,
			State:       "OPEN",
			HeadRefName: fmt.Sprintf("feature/%d", 101+i),
			CheckedAt:   now.Add(-cacheHitActiveTTL - time.Second),
		}
	}
	saveCache(cachePath, cache{
		// Under the old file-wide throttle, only the first key would fetch:
		// its fetch moved LastFetch to now and blocked the remaining keys.
		LastFetch: now.Add(-cacheMinFetchInterval),
		Entries:   entries,
	})

	client := &fakeGitHubClient{
		available: true,
		outputs: [][]byte{
			[]byte(`{"url":"https://github.com/acme/repo/pull/101","number":101,"state":"OPEN","headRefName":"feature/101"}`),
			[]byte(`{"url":"https://github.com/acme/repo/pull/102","number":102,"state":"OPEN","headRefName":"feature/102"}`),
			[]byte(`{"url":"https://github.com/acme/repo/pull/103","number":103,"state":"OPEN","headRefName":"feature/103"}`),
		},
	}
	for _, prURL := range urls {
		info, lookupErr := lookupInfoByURLWithCache(client, cachePath, prURL, nowFunc)
		if lookupErr != nil {
			t.Fatalf("lookupInfoByURLWithCache(%q): %v", prURL, lookupErr)
		}
		if info == nil {
			t.Fatalf("lookupInfoByURLWithCache(%q) returned nil", prURL)
		}
		if info.URL != prURL {
			t.Fatalf("lookupInfoByURLWithCache(%q) returned URL %q", prURL, info.URL)
		}
	}

	if client.callCount != len(urls) {
		t.Fatalf("expected every expired key to fetch once; got %d calls for %d keys", client.callCount, len(urls))
	}
	stored := loadCache(cachePath)
	for _, prURL := range urls {
		key := urlCacheKey(prURL)
		if got := stored.LastFetchByKey[key]; !got.Equal(now) {
			t.Errorf("LastFetchByKey[%q] = %v, want %v", key, got, now)
		}
	}
}

func TestPopulateRunInfo_ExpiredBranchesDoNotStarveEachOther(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	repoRoot := t.TempDir()
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}

	branches := []string{"feature/301", "feature/302", "feature/303"}
	entries := make(map[string]cacheEntry, len(branches))
	runs := make([]*model.Run, len(branches))
	for i, branch := range branches {
		entries[branch] = cacheEntry{
			URL:         fmt.Sprintf("https://github.com/acme/repo/pull/%d", 301+i),
			Number:      301 + i,
			State:       "OPEN",
			HeadRefName: branch,
			CheckedAt:   now.Add(-cacheHitActiveTTL - time.Second),
		}
		runs[i] = &model.Run{IssueID: "test", RunID: model.RunID(fmt.Sprintf("run-%d", i)), Branch: branch}
	}
	saveCache(cachePath, cache{
		LastFetch: now.Add(-cacheMinFetchInterval),
		Entries:   entries,
	})

	client := &fakeGitHubClient{
		available: true,
		outputs: [][]byte{
			[]byte(`[{"url":"https://github.com/acme/repo/pull/301","number":301,"state":"OPEN","headRefName":"feature/301"}]`),
			[]byte(`[{"url":"https://github.com/acme/repo/pull/302","number":302,"state":"OPEN","headRefName":"feature/302"}]`),
			[]byte(`[{"url":"https://github.com/acme/repo/pull/303","number":303,"state":"OPEN","headRefName":"feature/303"}]`),
		},
	}
	infoMap := populateRunInfoWithClient(client, runs, repoRoot, func() time.Time { return now })

	if client.callCount != len(branches) {
		t.Fatalf("expected every expired branch to fetch once; got %d calls for %d keys", client.callCount, len(branches))
	}
	for _, branch := range branches {
		if infoMap[branch] == nil {
			t.Errorf("missing refreshed info for branch %q", branch)
		}
	}
}

func TestLookupInfoByURL_ThrottleReturnsStaleEntry(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	cachePath, err := getCachePath(globalURLCacheScope)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}
	prURL := "https://github.com/acme/repo/pull/201"
	key := urlCacheKey(prURL)
	saveCache(cachePath, cache{
		Entries: map[string]cacheEntry{
			key: {
				URL:         prURL,
				Number:      201,
				State:       "OPEN",
				HeadRefName: "feature/201",
				CheckedAt:   now.Add(-cacheHitActiveTTL - time.Second),
			},
		},
	})

	client := &fakeGitHubClient{available: true, err: fmt.Errorf("GitHub unavailable")}
	info, err := lookupInfoByURLWithCache(client, cachePath, prURL, func() time.Time { return now })
	if err == nil {
		t.Fatal("first stale lookup must report the fetch error")
	}
	if info != nil {
		t.Fatalf("failed fetch returned unexpected info: %#v", info)
	}

	// The failed attempt is now throttled for this key. It must retain and
	// return the prior OPEN state rather than looking like a cache miss.
	info, err = lookupInfoByURLWithCache(client, cachePath, prURL, func() time.Time { return now })
	if err != nil {
		t.Fatalf("lookupInfoByURLWithCache: %v", err)
	}
	if info == nil || info.State != "OPEN" || info.Number != 201 {
		t.Fatalf("expected stale OPEN entry while throttled, got %#v", info)
	}
	if client.callCount != 1 {
		t.Fatalf("throttled lookup made an additional API call; total = %d, want 1", client.callCount)
	}
}

func TestLookupInfoByURL_PreLawEntryBypassesThrottle(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	cachePath, err := getCachePath(globalURLCacheScope)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}
	prURL := "https://github.com/acme/repo/pull/401"
	key := urlCacheKey(prURL)
	saveCache(cachePath, cache{
		LastFetchByKey: map[string]time.Time{key: now.Add(-time.Second)},
		Entries: map[string]cacheEntry{
			key: {
				URL:       prURL,
				Number:    401,
				State:     "OPEN",
				CheckedAt: now.Add(-time.Second),
			},
		},
	})

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`{"url":"https://github.com/acme/repo/pull/401","number":401,"state":"OPEN","headRefName":"feature/401"}`),
	}
	info, err := lookupInfoByURLWithCache(client, cachePath, prURL, func() time.Time { return now })
	if err != nil {
		t.Fatalf("lookupInfoByURLWithCache: %v", err)
	}
	if info == nil || info.HeadRefName != "feature/401" {
		t.Fatalf("expected pre-law entry to be refetched with head branch, got %#v", info)
	}
	if client.callCount != 1 {
		t.Fatalf("pre-law entry made %d API calls, want 1", client.callCount)
	}
}

func TestPopulateRunInfo_CacheHitSkipsAPI(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	repoRoot := t.TempDir()
	branch := "feature/pre-cached"

	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		t.Fatalf("getCachePath: %v", err)
	}
	preCached := cache{
		Entries: map[string]cacheEntry{
			branch: {
				URL:         "https://github.com/acme/repo/pull/77",
				Number:      77,
				State:       "OPEN",
				HeadRefName: branch,
				CheckedAt:   time.Now(),
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

	infoMap := populateRunInfoWithClient(client, []*model.Run{run}, repoRoot, time.Now)
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
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/42","number":42,"state":"OPEN","headRefName":"feature/must-cache"}]`),
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
				URL:         "https://github.com/acme/repo/pull/99",
				Number:      99,
				State:       "MERGED",
				HeadRefName: branch,
				CheckedAt:   time.Now(),
			},
		},
	}
	saveCache(cachePath, preCached)

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/999","number":999,"state":"OPEN","headRefName":"feature/cache-hit"}]`),
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
		output:    []byte(`{"url":"https://github.com/acme/repo/pull/42","number":42,"state":"OPEN","headRefName":"feature/by-url"}`),
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
		output:    []byte(`{"url":"https://github.com/acme/repo/pull/55","number":55,"state":"MERGED","headRefName":"feature/url-hit"}`),
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
		URL:         "https://github.com/acme/repo/pull/1",
		Number:      1,
		State:       "MERGED",
		HeadRefName: "feature/merged",
		CheckedAt:   now.Add(-1 * time.Hour),
	}
	if !isCacheEntryFresh(entry, now) {
		t.Fatal("MERGED PR cached 1 hour ago should still be fresh")
	}
}

// L-PR1 (run-state-machine.md §11): entries persisted before the pr-attach
// law (URL set, head branch missing) cannot answer ownership checks and are
// treated as stale so they refetch.
func TestPreLawCacheEntryIsStale(t *testing.T) {
	now := time.Now()
	entry := cacheEntry{
		URL:       "https://github.com/acme/repo/pull/1",
		Number:    1,
		State:     "MERGED",
		CheckedAt: now.Add(-1 * time.Minute),
	}
	if isCacheEntryFresh(entry, now) {
		t.Fatal("entry without HeadRefName must be stale regardless of TTL")
	}
	// Negative entries (no PR found) carry no URL and stay cacheable.
	negative := cacheEntry{CheckedAt: now.Add(-1 * time.Second)}
	if !isCacheEntryFresh(negative, now) {
		t.Fatal("negative cache entry must remain fresh")
	}
}
