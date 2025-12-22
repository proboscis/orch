package pr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
)

const (
	cacheHitTTL           = 24 * time.Hour
	cacheMissTTL          = 30 * time.Second
	cacheMinFetchInterval = 30 * time.Second
	cacheMaxFetches       = 3
)

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

// PopulateRunInfo populates PR URLs and returns PR info for each run's branch.
func PopulateRunInfo(runs []*model.Run) InfoMap {
	prInfoMap := make(InfoMap)
	if len(runs) == 0 {
		return prInfoMap
	}

	if _, err := exec.LookPath("gh"); err != nil {
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

		if entry, ok := c.Entries[r.Branch]; ok {
			ttl := cacheMissTTL
			if entry.URL != "" {
				ttl = cacheHitTTL
			}
			if !entry.CheckedAt.IsZero() && now.Sub(entry.CheckedAt) < ttl {
				if entry.URL != "" {
					r.PRUrl = entry.URL
					prInfoMap[r.Branch] = &Info{
						URL:    entry.URL,
						Number: entry.Number,
						State:  entry.State,
					}
				}
				continue
			}
		}

		if fetches >= cacheMaxFetches {
			break
		}

		info, err := lookupInfo(repoRoot, r.Branch)
		fetchTime := time.Now()
		c.LastFetch = fetchTime
		fetches++
		dirty = true

		if err != nil || info == nil {
			c.Entries[r.Branch] = cacheEntry{CheckedAt: fetchTime}
			continue
		}

		c.Entries[r.Branch] = cacheEntry{
			URL:       info.URL,
			Number:    info.Number,
			State:     info.State,
			CheckedAt: fetchTime,
		}
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
		if !entry.CheckedAt.IsZero() && now.Sub(entry.CheckedAt) > cacheHitTTL {
			continue
		}
		r.PRUrl = entry.URL
		prInfoMap[r.Branch] = &Info{
			URL:    entry.URL,
			Number: entry.Number,
			State:  entry.State,
		}
	}
}

func lookupInfo(repoRoot, branch string) (*Info, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "all", "--json", "url,number,state", "--limit", "1")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
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

// LookupInfo returns PR info for a branch using the GitHub CLI.
func LookupInfo(repoRoot, branch string) (*Info, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, err
	}
	if repoRoot == "" {
		var err error
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return nil, err
		}
	}
	return lookupInfo(repoRoot, branch)
}

func getCachePath(repoRoot string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
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
