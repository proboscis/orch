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

type worktreeDirtyCache struct {
	mu            sync.RWMutex
	dirtyStates   map[string]bool
	lastRefreshed time.Time
	ttl           time.Duration
}

var globalGitCache = &gitCache{
	ttl: 10 * time.Second,
}

var globalWorktreeCache = &worktreeDirtyCache{
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

func GetCachedWorktreeDirtyStates(worktreePaths []string) map[string]bool {
	if len(worktreePaths) == 0 {
		return nil
	}

	globalWorktreeCache.mu.RLock()
	validCache := globalWorktreeCache.dirtyStates != nil &&
		time.Since(globalWorktreeCache.lastRefreshed) < globalWorktreeCache.ttl
	if validCache {
		result := make(map[string]bool, len(worktreePaths))
		allFound := true
		for _, path := range worktreePaths {
			if state, ok := globalWorktreeCache.dirtyStates[path]; ok {
				result[path] = state
			} else {
				allFound = false
				break
			}
		}
		globalWorktreeCache.mu.RUnlock()
		if allFound {
			return result
		}
	} else {
		globalWorktreeCache.mu.RUnlock()
	}

	states := GetWorktreeDirtyStates(worktreePaths)

	globalWorktreeCache.mu.Lock()
	defer globalWorktreeCache.mu.Unlock()

	if globalWorktreeCache.dirtyStates == nil {
		globalWorktreeCache.dirtyStates = make(map[string]bool)
	}

	for path, dirty := range states {
		globalWorktreeCache.dirtyStates[path] = dirty
	}
	globalWorktreeCache.lastRefreshed = time.Now()

	return states
}

func InvalidateWorktreeCache() {
	globalWorktreeCache.mu.Lock()
	defer globalWorktreeCache.mu.Unlock()
	globalWorktreeCache.lastRefreshed = time.Time{}
	globalWorktreeCache.dirtyStates = nil
}
