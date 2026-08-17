package pr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/git"
	"github.com/proboscis/orch/internal/github"
	"github.com/proboscis/orch/internal/model"
)

const (
	cacheHitTerminalTTL   = 24 * time.Hour
	cacheHitActiveTTL     = 60 * time.Second
	cacheMissTTL          = 30 * time.Second
	cacheMinFetchInterval = 30 * time.Second
	cacheMaxFetches       = 3
	urlCacheKeyPrefix     = "url:"
	globalURLCacheScope   = "__global_pr_url_cache__"
)

var ghClient = github.NewCLIClient()

// Info holds details about a pull request.
type Info struct {
	URL         string
	Number      int
	State       string // OPEN, MERGED, CLOSED
	HeadRefName string // PR head branch; required to verify a PR belongs to a run (pr-attach law)
}

// InfoMap holds PR information keyed by branch name.
type InfoMap map[string]*Info

type cacheEntry struct {
	URL         string    `json:"url,omitempty"`
	Number      int       `json:"number,omitempty"`
	State       string    `json:"state,omitempty"`
	HeadRefName string    `json:"head_ref_name,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

type cache struct {
	// LastFetch is a deprecated on-disk field retained during migration.
	// Throttling must never use this file-wide timestamp: doing so lets one hot
	// key starve every other key in the shared cache file.
	LastFetch      time.Time             `json:"last_fetch,omitempty"`
	LastFetchByKey map[string]time.Time  `json:"last_fetch_by_key,omitempty"`
	Entries        map[string]cacheEntry `json:"entries"`
}

// PopulateRunInfo populates PR URLs and returns PR info for each run's branch.
func PopulateRunInfo(runs []*model.Run) InfoMap {
	return PopulateRunInfoWithClient(ghClient, runs)
}

// PopulateRunInfoWithClient populates PR URLs and returns PR info for each run's
// branch using the provided GitHub client.
//
// Resolving the repo root from the process working directory is an ambient read,
// so it happens here at the boundary and nowhere deeper. The core below takes
// repoRoot as an argument, which keeps its result a function of its inputs
// instead of a function of where the caller (or a test binary) happens to run.
func PopulateRunInfoWithClient(client github.Client, runs []*model.Run) InfoMap {
	repoRoot, err := git.FindMainRepoRoot("")
	if err != nil {
		return make(InfoMap)
	}
	return populateRunInfoWithClient(client, runs, repoRoot, time.Now)
}

func populateRunInfoWithClient(client github.Client, runs []*model.Run, repoRoot string, now func() time.Time) InfoMap {
	prInfoMap := make(InfoMap)
	if len(runs) == 0 {
		return prInfoMap
	}

	if client == nil {
		client = ghClient
	}

	if !client.IsAvailable() {
		return prInfoMap
	}

	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		return prInfoMap
	}

	c := loadCache(cachePath)
	if c.Entries == nil {
		c.Entries = make(map[string]cacheEntry)
	}

	currentTime := now()
	applyCachedInfo(runs, c, currentTime, prInfoMap)

	dirty := false
	fetches := 0
	for _, r := range runs {
		if r.PRUrl != "" || r.Branch == "" {
			continue
		}
		// Skip if already fetched from cache
		if prInfoMap[r.Branch] != nil {
			continue
		}

		if info, handled := cachedInfoForLookup(c, r.Branch, currentTime); handled {
			if info != nil {
				r.PRUrl = info.URL
				prInfoMap[r.Branch] = info
			}
			continue
		}

		if fetches >= cacheMaxFetches {
			break
		}

		info, err := lookupInfo(client, repoRoot, r.Branch)
		fetchTime := now()
		recordFetch(&c, r.Branch, fetchTime)
		fetches++
		dirty = true

		if err != nil {
			// Keep the last known PR state. The per-key fetch timestamp will
			// throttle an immediate retry, which can then return this stale
			// entry instead of degrading the result to unknown.
			continue
		}
		if info == nil {
			c.Entries[r.Branch] = cacheEntry{CheckedAt: fetchTime}
			continue
		}

		saveLookupCacheEntry(&c, r.Branch, info, fetchTime)
		if info.URL != "" {
			r.PRUrl = info.URL
			prInfoMap[r.Branch] = info
		}
	}

	if dirty {
		saveCache(cachePath, c)
	}
	return prInfoMap
}

func applyCachedInfo(runs []*model.Run, c cache, now time.Time, prInfoMap InfoMap) {
	if len(c.Entries) == 0 {
		return
	}
	for _, r := range runs {
		if r.PRUrl != "" || r.Branch == "" {
			continue
		}
		entry, ok := c.Entries[r.Branch]
		if !ok || entry.URL == "" {
			continue
		}
		if !isCacheEntryFresh(entry, now) {
			continue
		}
		info := infoFromCacheEntry(entry)
		if info == nil {
			continue
		}
		r.PRUrl = info.URL
		prInfoMap[r.Branch] = info
	}
}

func isTerminalPRState(state string) bool {
	upper := strings.ToUpper(state)
	return upper == "MERGED" || upper == "CLOSED"
}

func cacheEntryTTL(entry cacheEntry) time.Duration {
	if entry.URL == "" {
		return cacheMissTTL
	}
	if isTerminalPRState(entry.State) {
		return cacheHitTerminalTTL
	}
	return cacheHitActiveTTL
}

func isCacheEntryFresh(entry cacheEntry, now time.Time) bool {
	if entry.CheckedAt.IsZero() {
		return false
	}
	// Entries persisted before the pr-attach law carry no head branch and
	// cannot answer ownership checks — treat them as stale so they refetch.
	if isPreLawCacheEntry(entry) {
		return false
	}
	return now.Sub(entry.CheckedAt) < cacheEntryTTL(entry)
}

func isPreLawCacheEntry(entry cacheEntry) bool {
	return entry.URL != "" && entry.HeadRefName == ""
}

func infoFromCacheEntry(entry cacheEntry) *Info {
	if entry.URL == "" {
		return nil
	}
	return &Info{
		URL:         entry.URL,
		Number:      entry.Number,
		State:       entry.State,
		HeadRefName: entry.HeadRefName,
	}
}

func saveLookupCacheEntry(c *cache, key string, info *Info, checkedAt time.Time) {
	if c.Entries == nil {
		c.Entries = make(map[string]cacheEntry)
	}
	entry := cacheEntry{CheckedAt: checkedAt}
	if info != nil {
		entry.URL = info.URL
		entry.Number = info.Number
		entry.State = info.State
		entry.HeadRefName = info.HeadRefName
	}
	c.Entries[key] = entry
	if info != nil && info.URL != "" {
		c.Entries[urlCacheKey(info.URL)] = entry
	}
}

func recordFetch(c *cache, key string, fetchedAt time.Time) {
	if c.LastFetchByKey == nil {
		c.LastFetchByKey = make(map[string]time.Time)
	}
	c.LastFetchByKey[key] = fetchedAt
}

func shouldThrottleFetch(c cache, key string, now time.Time) bool {
	lastFetch := c.LastFetchByKey[key]
	if lastFetch.IsZero() {
		return false
	}
	return now.Sub(lastFetch) < cacheMinFetchInterval
}

func cachedInfoForLookup(c cache, key string, now time.Time) (*Info, bool) {
	entry, cached := c.Entries[key]
	if cached && isCacheEntryFresh(entry, now) {
		return infoFromCacheEntry(entry), true
	}
	if !shouldThrottleFetch(c, key, now) {
		return nil, false
	}
	// Pre-law entries cannot prove PR ownership and must be refetched even if
	// a persisted per-key timestamp would otherwise throttle the lookup.
	if cached && isPreLawCacheEntry(entry) {
		return nil, false
	}
	// A throttled lookup still has information: preserve the last known PR
	// state instead of degrading it to an unknown result. For a cached miss,
	// info remains nil while handled distinguishes it from a fetchable miss.
	return infoFromCacheEntry(entry), true
}

func urlCacheKey(prURL string) string {
	return urlCacheKeyPrefix + prURL
}

func lookupInfo(client github.Client, repoRoot, branch string) (*Info, error) {
	if client == nil {
		client = ghClient
	}
	output, err := client.RunInDir(repoRoot, "pr", "list", "--head", branch, "--state", "all", "--json", "url,number,state,headRefName", "--limit", "1")
	if err != nil {
		return nil, err
	}

	var prs []struct {
		URL         string `json:"url"`
		Number      int    `json:"number"`
		State       string `json:"state"`
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(output, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &Info{
		URL:         prs[0].URL,
		Number:      prs[0].Number,
		State:       prs[0].State,
		HeadRefName: prs[0].HeadRefName,
	}, nil
}

func lookupInfoByURL(client github.Client, prURL string) (*Info, error) {
	if client == nil {
		client = ghClient
	}
	output, err := client.Run("pr", "view", prURL, "--json", "url,number,state,headRefName")
	if err != nil {
		return nil, err
	}

	var pr struct {
		URL         string `json:"url"`
		Number      int    `json:"number"`
		State       string `json:"state"`
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(output, &pr); err != nil {
		return nil, err
	}

	return &Info{
		URL:         pr.URL,
		Number:      pr.Number,
		State:       pr.State,
		HeadRefName: pr.HeadRefName,
	}, nil
}

// LookupInfo returns PR info for a branch using the default GitHub client.
func LookupInfo(repoRoot, branch string) (*Info, error) {
	return LookupInfoWithClient(ghClient, repoRoot, branch)
}

// LookupInfoWithClient returns PR info for a branch using the provided GitHub client.
func LookupInfoWithClient(client github.Client, repoRoot, branch string) (*Info, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, fmt.Errorf("branch is required")
	}

	if client == nil {
		client = ghClient
	}

	if !client.IsAvailable() {
		return nil, fmt.Errorf("gh CLI not available")
	}
	if repoRoot == "" {
		var err error
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return nil, err
		}
	}
	// Skip cache when repoRoot does not exist on disk. This keeps behavior
	// stable for injected-client tests and avoids reusing stale cache entries
	// tied to synthetic paths.
	if _, err := os.Stat(repoRoot); err != nil {
		return lookupInfo(client, repoRoot, branch)
	}

	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		return lookupInfo(client, repoRoot, branch)
	}

	c := loadCache(cachePath)
	now := time.Now()
	if cachedInfo, handled := cachedInfoForLookup(c, branch, now); handled {
		return cachedInfo, nil
	}

	info, err := lookupInfo(client, repoRoot, branch)
	fetchTime := time.Now()
	recordFetch(&c, branch, fetchTime)
	if err == nil {
		saveLookupCacheEntry(&c, branch, info, fetchTime)
	}
	saveCache(cachePath, c)

	if err != nil {
		return nil, err
	}
	return info, nil
}

// LookupInfoByURL returns PR info by URL using the GitHub CLI.
// This works even if the local worktree has been deleted.
// Only GitHub URLs are supported (gh CLI requirement).
func LookupInfoByURL(prURL string) (*Info, error) {
	if strings.TrimSpace(prURL) == "" {
		return nil, fmt.Errorf("PR URL is required")
	}
	if !strings.HasPrefix(prURL, "https://github.com/") {
		return nil, fmt.Errorf("only GitHub URLs are supported: %s", prURL)
	}
	if !ghClient.IsAvailable() {
		return nil, fmt.Errorf("gh CLI not available")
	}

	cachePath, err := cachePathForURLLookup()
	if err != nil {
		return lookupInfoByURL(ghClient, prURL)
	}
	return lookupInfoByURLWithCache(ghClient, cachePath, prURL, time.Now)
}

func lookupInfoByURLWithCache(client github.Client, cachePath, prURL string, now func() time.Time) (*Info, error) {
	c := loadCache(cachePath)
	cacheKey := urlCacheKey(prURL)
	currentTime := now()
	if cachedInfo, handled := cachedInfoForLookup(c, cacheKey, currentTime); handled {
		return cachedInfo, nil
	}

	info, err := lookupInfoByURL(client, prURL)
	fetchTime := now()
	recordFetch(&c, cacheKey, fetchTime)
	if err == nil {
		saveLookupCacheEntry(&c, cacheKey, info, fetchTime)
	}
	saveCache(cachePath, c)

	if err != nil {
		return nil, err
	}
	return info, nil
}

// LookupCachedInfo is an explicit cache-aware lookup entrypoint for daemon code.
func LookupCachedInfo(repoRoot, branch string) (*Info, error) {
	return LookupInfo(repoRoot, branch)
}

// LookupCachedInfoByURL is an explicit cache-aware lookup entrypoint for daemon code.
func LookupCachedInfoByURL(prURL string) (*Info, error) {
	return LookupInfoByURL(prURL)
}

func cachePathForURLLookup() (string, error) {
	repoRoot, err := git.FindMainRepoRoot("")
	if err == nil && repoRoot != "" {
		return getCachePath(repoRoot)
	}
	return getCachePath(globalURLCacheScope)
}

func getCachePath(repoRoot string) (string, error) {
	cacheDir := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME"))
	if cacheDir == "" {
		var err error
		cacheDir, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(cacheDir, "orch")
	name := "pr_cache_" + hashString(repoRoot) + ".json"
	return filepath.Join(dir, name), nil
}

func loadCache(path string) cache {
	data, err := os.ReadFile(path)
	if err != nil {
		return cache{}
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return cache{}
	}
	return c
}

func saveCache(path string, c cache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
