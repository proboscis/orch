package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
)

const (
	prCacheHitTTL           = 24 * time.Hour
	prCacheMissTTL          = 30 * time.Second
	prCacheMinFetchInterval = 30 * time.Second
	prCacheMaxFetches       = 3
)

type prCacheEntry struct {
	URL       string    `json:"url,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type prCache struct {
	LastFetch time.Time               `json:"last_fetch"`
	Entries   map[string]prCacheEntry `json:"entries"`
}

func populatePRUrls(runs []*model.Run) {
	if len(runs) == 0 {
		return
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return
	}

	repoRoot, err := git.FindMainRepoRoot("")
	if err != nil {
		return
	}

	cachePath, err := prCachePath(repoRoot)
	if err != nil {
		return
	}

	cache := loadPRCache(cachePath)
	if cache.Entries == nil {
		cache.Entries = make(map[string]prCacheEntry)
	}

	now := time.Now()
	applyCachedPRUrls(runs, cache, now)

	if time.Since(cache.LastFetch) < prCacheMinFetchInterval {
		return
	}

	dirty := false
	fetches := 0
	for _, r := range runs {
		if r.PRUrl != "" || r.Branch == "" {
			continue
		}

		if entry, ok := cache.Entries[r.Branch]; ok {
			ttl := prCacheMissTTL
			if entry.URL != "" {
				ttl = prCacheHitTTL
			}
			if !entry.CheckedAt.IsZero() && now.Sub(entry.CheckedAt) < ttl {
				if entry.URL != "" {
					r.PRUrl = entry.URL
				}
				continue
			}
		}

		if fetches >= prCacheMaxFetches {
			break
		}

		url, err := lookupPRUrl(repoRoot, r.Branch)
		fetchTime := time.Now()
		cache.LastFetch = fetchTime
		fetches++
		dirty = true

		if err != nil {
			cache.Entries[r.Branch] = prCacheEntry{CheckedAt: fetchTime}
			continue
		}

		cache.Entries[r.Branch] = prCacheEntry{URL: url, CheckedAt: fetchTime}
		if url != "" {
			r.PRUrl = url
		}
	}

	if dirty {
		savePRCache(cachePath, cache)
	}
}

func applyCachedPRUrls(runs []*model.Run, cache prCache, now time.Time) {
	if len(cache.Entries) == 0 {
		return
	}
	for _, r := range runs {
		if r.PRUrl != "" || r.Branch == "" {
			continue
		}
		entry, ok := cache.Entries[r.Branch]
		if !ok || entry.URL == "" {
			continue
		}
		if !entry.CheckedAt.IsZero() && now.Sub(entry.CheckedAt) > prCacheHitTTL {
			continue
		}
		r.PRUrl = entry.URL
	}
}

func lookupPRUrl(repoRoot, branch string) (string, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "all", "--json", "url", "--limit", "1")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var prs []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(output, &prs); err != nil {
		return "", err
	}
	if len(prs) == 0 {
		return "", nil
	}
	return prs[0].URL, nil
}

func prCachePath(repoRoot string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "orch")
	name := "ps_pr_cache_" + hashString(repoRoot) + ".json"
	return filepath.Join(dir, name), nil
}

func loadPRCache(path string) prCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return prCache{}
	}
	var cache prCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return prCache{}
	}
	return cache
}

func savePRCache(path string, cache prCache) {
	data, err := json.Marshal(cache)
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
