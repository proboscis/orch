package git

import (
	"sync"
	"time"
)

type gitCache struct {
	mu            sync.RWMutex
	mergeStates   map[string]string
	repoRoot      string
	target        string
	lastRefreshed time.Time
	ttl           time.Duration
}

var globalGitCache = &gitCache{
	ttl: 10 * time.Second,
}

func GetCachedBranchMergeStates(repoRoot, target string, branches []string) map[string]string {
	globalGitCache.mu.RLock()
	validCache := globalGitCache.mergeStates != nil &&
		globalGitCache.repoRoot == repoRoot &&
		globalGitCache.target == target &&
		time.Since(globalGitCache.lastRefreshed) < globalGitCache.ttl
	if validCache {
		result := make(map[string]string, len(branches))
		for _, b := range branches {
			if state, ok := globalGitCache.mergeStates[b]; ok {
				result[b] = state
			}
		}
		globalGitCache.mu.RUnlock()
		if len(result) == len(branches) {
			return result
		}
	} else {
		globalGitCache.mu.RUnlock()
	}

	states := GetBranchMergeStates(repoRoot, target, branches)

	globalGitCache.mu.Lock()
	defer globalGitCache.mu.Unlock()

	if globalGitCache.mergeStates == nil || globalGitCache.repoRoot != repoRoot || globalGitCache.target != target {
		globalGitCache.mergeStates = make(map[string]string)
		globalGitCache.repoRoot = repoRoot
		globalGitCache.target = target
	}

	for branch, state := range states {
		globalGitCache.mergeStates[branch] = state
	}
	globalGitCache.lastRefreshed = time.Now()

	return states
}

func InvalidateGitCache() {
	globalGitCache.mu.Lock()
	defer globalGitCache.mu.Unlock()
	globalGitCache.lastRefreshed = time.Time{}
	globalGitCache.mergeStates = nil
}
