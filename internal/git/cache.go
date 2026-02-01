package git

import (
	"sync"
	"time"
)

// cache is a generic TTL cache for key-value data.
type cache[V any] struct {
	mu            sync.RWMutex
	data          map[string]V
	lastRefreshed time.Time
	ttl           time.Duration
}

func newCache[V any](ttl time.Duration) *cache[V] {
	return &cache[V]{ttl: ttl}
}

// get retrieves cached values for the given keys.
// Returns partial results for keys that exist in cache (does not require all keys).
// Returns nil only if cache is completely expired or empty.
func (c *cache[V]) get(keys []string) map[string]V {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil || time.Since(c.lastRefreshed) >= c.ttl {
		return nil
	}

	result := make(map[string]V, len(keys))
	for _, key := range keys {
		if val, ok := c.data[key]; ok {
			result[key] = val
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// set stores values in the cache and updates the refresh timestamp.
func (c *cache[V]) set(values map[string]V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.data == nil {
		c.data = make(map[string]V)
	}
	for k, v := range values {
		c.data[k] = v
	}
	c.lastRefreshed = time.Now()
}

// invalidate clears the cache.
func (c *cache[V]) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastRefreshed = time.Time{}
	c.data = nil
}

// mergeStateCache extends cache with repo/target context validation.
type mergeStateCache struct {
	*cache[string]
	repoRoot string
	target   string
}

var globalMergeCache = &mergeStateCache{
	cache: newCache[string](10 * time.Second),
}

var globalWorktreeCache = newCache[bool](10 * time.Second)
var globalWorktreeStatusCache = newCache[WorktreeStatus](10 * time.Second)

func GetCachedBranchMergeStates(repoRoot, target string, branches []string) map[string]string {
	globalMergeCache.mu.RLock()
	contextChanged := globalMergeCache.repoRoot != repoRoot || globalMergeCache.target != target
	globalMergeCache.mu.RUnlock()

	if contextChanged {
		globalMergeCache.mu.Lock()
		globalMergeCache.data = nil
		globalMergeCache.repoRoot = repoRoot
		globalMergeCache.target = target
		globalMergeCache.mu.Unlock()
	}

	if result := globalMergeCache.get(branches); result != nil {
		return result
	}

	states := GetBranchMergeStates(repoRoot, target, branches)
	globalMergeCache.set(states)
	return states
}

func InvalidateGitCache() {
	globalMergeCache.invalidate()
}

func GetCachedWorktreeDirtyStates(worktreePaths []string) map[string]bool {
	if len(worktreePaths) == 0 {
		return nil
	}

	if result := globalWorktreeCache.get(worktreePaths); result != nil {
		return result
	}

	states := GetWorktreeDirtyStates(worktreePaths)
	globalWorktreeCache.set(states)
	return states
}

func InvalidateWorktreeCache() {
	globalWorktreeCache.invalidate()
	globalWorktreeStatusCache.invalidate()
}

func GetCachedWorktreeStatusBatch(worktrees []struct {
	Path       string
	Branch     string
	BaseBranch string
}) map[string]WorktreeStatus {
	if len(worktrees) == 0 {
		return nil
	}

	paths := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		if wt.Path != "" {
			paths = append(paths, wt.Path)
		}
	}

	cached := globalWorktreeStatusCache.get(paths)
	if cached != nil && len(cached) == len(paths) {
		return cached
	}

	var missing []struct {
		Path       string
		Branch     string
		BaseBranch string
	}
	for _, wt := range worktrees {
		if wt.Path == "" {
			continue
		}
		if cached == nil {
			missing = append(missing, wt)
		} else if _, ok := cached[wt.Path]; !ok {
			missing = append(missing, wt)
		}
	}

	if len(missing) == 0 {
		return cached
	}

	computed := GetWorktreeStatusBatch(missing)
	globalWorktreeStatusCache.set(computed)

	if cached == nil {
		return computed
	}
	for k, v := range computed {
		cached[k] = v
	}
	return cached
}
