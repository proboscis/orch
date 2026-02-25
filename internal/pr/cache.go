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

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/github"
	"github.com/s22625/orch/internal/model"
)

const (
	cacheHitTTL           = 24 * time.Hour
	cacheMissTTL          = 30 * time.Second
	cacheMinFetchInterval = 30 * time.Second
	cacheMaxFetches       = 3
)

var ghClient = github.NewCLIClient()

// Info holds details about a pull request.
type Info struct {
	URL    string
	Number int
	State  string // OPEN, MERGED, CLOSED
}

// InfoMap holds PR information keyed by branch name.
type InfoMap map[string]*Info

type cacheEntry struct {
	URL       string    `json:"url,omitempty"`
	Number    int       `json:"number,omitempty"`
	State     string    `json:"state,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type cache struct {
	LastFetch time.Time             `json:"last_fetch"`
	Entries   map[string]cacheEntry `json:"entries"`
}

func cacheKeyForPRURL(prURL string) string {
	return "url:" + prURL
}

func cacheEntryTTL(entry cacheEntry) time.Duration {
	if entry.URL != "" {
		return cacheHitTTL
	}
	return cacheMissTTL
}

func cacheEntryToInfo(entry cacheEntry) *Info {
	if entry.URL == "" {
		return nil
	}
	return &Info{
		URL:    entry.URL,
		Number: entry.Number,
		State:  entry.State,
	}
}

// lookupCachedEntry returns cached info and whether the cache entry is still fresh.
// A fresh cached miss returns (nil, true).
func lookupCachedEntry(c cache, key string, now time.Time) (*Info, bool) {
	if len(c.Entries) == 0 {
		return nil, false
	}
	entry, ok := c.Entries[key]
	if !ok {
		return nil, false
	}
	if !entry.CheckedAt.IsZero() && now.Sub(entry.CheckedAt) < cacheEntryTTL(entry) {
		return cacheEntryToInfo(entry), true
	}
	return nil, false
}

func upsertCacheEntry(c *cache, key string, info *Info, checkedAt time.Time) {
	if c.Entries == nil {
		c.Entries = make(map[string]cacheEntry)
	}
	entry := cacheEntry{CheckedAt: checkedAt}
	if info != nil {
		entry.URL = info.URL
		entry.Number = info.Number
		entry.State = info.State
	}
	c.Entries[key] = entry
}

// PopulateRunInfo populates PR URLs and returns PR info for each run's branch.
func PopulateRunInfo(runs []*model.Run) InfoMap {
	return PopulateRunInfoWithClient(ghClient, runs)
}

// PopulateRunInfoWithClient populates PR URLs and returns PR info for each run's
// branch using the provided GitHub client.
func PopulateRunInfoWithClient(client github.Client, runs []*model.Run) InfoMap {
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

	repoRoot, err := git.FindMainRepoRoot("")
	if err != nil {
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

	now := time.Now()
	applyCachedInfo(runs, c, now, prInfoMap)

	if time.Since(c.LastFetch) < cacheMinFetchInterval {
		return prInfoMap
	}

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

		if info, ok := lookupCachedEntry(c, r.Branch, now); ok {
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
		fetchTime := time.Now()
		c.LastFetch = fetchTime
		fetches++
		dirty = true

		if err != nil || info == nil {
			upsertCacheEntry(&c, r.Branch, nil, fetchTime)
			continue
		}

		upsertCacheEntry(&c, r.Branch, info, fetchTime)
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
		info, ok := lookupCachedEntry(c, r.Branch, now)
		if !ok || info == nil {
			continue
		}
		r.PRUrl = info.URL
		prInfoMap[r.Branch] = info
	}
}

func lookupInfo(client github.Client, repoRoot, branch string) (*Info, error) {
	if client == nil {
		client = ghClient
	}
	output, err := client.RunInDir(repoRoot, "pr", "list", "--head", branch, "--state", "all", "--json", "url,number,state", "--limit", "1")
	if err != nil {
		return nil, err
	}

	var prs []struct {
		URL    string `json:"url"`
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(output, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &Info{
		URL:    prs[0].URL,
		Number: prs[0].Number,
		State:  prs[0].State,
	}, nil
}

func lookupInfoByURL(client github.Client, prURL string) (*Info, error) {
	if client == nil {
		client = ghClient
	}
	output, err := client.Run("pr", "view", prURL, "--json", "url,number,state")
	if err != nil {
		return nil, err
	}

	var pr struct {
		URL    string `json:"url"`
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(output, &pr); err != nil {
		return nil, err
	}
	if pr.URL == "" {
		pr.URL = prURL
	}

	return &Info{
		URL:    pr.URL,
		Number: pr.Number,
		State:  pr.State,
	}, nil
}

// LookupInfoCached returns cached PR info for a branch.
func LookupInfoCached(repoRoot, branch string) (*Info, error) {
	return LookupInfoWithClient(ghClient, repoRoot, branch)
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

	if info, statErr := os.Stat(repoRoot); statErr != nil || !info.IsDir() {
		return lookupInfo(client, repoRoot, branch)
	}

	cachePath, err := getCachePath(repoRoot)
	if err != nil {
		return lookupInfo(client, repoRoot, branch)
	}

	now := time.Now()
	c := loadCache(cachePath)
	if c.Entries == nil {
		c.Entries = make(map[string]cacheEntry)
	}

	if info, ok := lookupCachedEntry(c, branch, now); ok {
		return info, nil
	}

	if !c.LastFetch.IsZero() && now.Sub(c.LastFetch) < cacheMinFetchInterval {
		if entry, ok := c.Entries[branch]; ok && entry.URL != "" {
			return cacheEntryToInfo(entry), nil
		}
		return nil, nil
	}

	info, lookupErr := lookupInfo(client, repoRoot, branch)
	fetchTime := time.Now()
	c.LastFetch = fetchTime
	if lookupErr != nil || info == nil {
		upsertCacheEntry(&c, branch, nil, fetchTime)
		saveCache(cachePath, c)
		return nil, lookupErr
	}

	upsertCacheEntry(&c, branch, info, fetchTime)
	saveCache(cachePath, c)
	return info, nil
}

// LookupInfoByURLCached returns cached PR info by URL.
func LookupInfoByURLCached(prURL string) (*Info, error) {
	return LookupInfoByURLWithClient(ghClient, prURL)
}

// LookupInfoByURL returns PR info by URL using the GitHub CLI.
// This works even if the local worktree has been deleted.
// Only GitHub URLs are supported (gh CLI requirement).
func LookupInfoByURL(prURL string) (*Info, error) {
	return LookupInfoByURLWithClient(ghClient, prURL)
}

// LookupInfoByURLWithClient returns PR info by URL using the provided GitHub client.
func LookupInfoByURLWithClient(client github.Client, prURL string) (*Info, error) {
	if strings.TrimSpace(prURL) == "" {
		return nil, fmt.Errorf("PR URL is required")
	}
	if !strings.HasPrefix(prURL, "https://github.com/") {
		return nil, fmt.Errorf("only GitHub URLs are supported: %s", prURL)
	}
	if client == nil {
		client = ghClient
	}
	if !client.IsAvailable() {
		return nil, fmt.Errorf("gh CLI not available")
	}

	cachePath, err := getCachePath("")
	if err != nil {
		return lookupInfoByURL(client, prURL)
	}

	now := time.Now()
	key := cacheKeyForPRURL(prURL)
	c := loadCache(cachePath)
	if c.Entries == nil {
		c.Entries = make(map[string]cacheEntry)
	}

	if info, ok := lookupCachedEntry(c, key, now); ok {
		return info, nil
	}

	if !c.LastFetch.IsZero() && now.Sub(c.LastFetch) < cacheMinFetchInterval {
		if entry, ok := c.Entries[key]; ok && entry.URL != "" {
			return cacheEntryToInfo(entry), nil
		}
		return nil, nil
	}

	info, lookupErr := lookupInfoByURL(client, prURL)
	fetchTime := time.Now()
	c.LastFetch = fetchTime
	if lookupErr != nil || info == nil {
		upsertCacheEntry(&c, key, nil, fetchTime)
		saveCache(cachePath, c)
		return nil, lookupErr
	}

	upsertCacheEntry(&c, key, info, fetchTime)
	saveCache(cachePath, c)
	return info, nil
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
