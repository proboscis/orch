package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

type runIndexEntry struct {
	IssueID           string       `json:"issue_id"`
	RunID             string       `json:"run_id"`
	Status            model.Status `json:"status"`
	Agent             string       `json:"agent,omitempty"`
	Target            string       `json:"target,omitempty"`
	TargetHost        string       `json:"target_host,omitempty"`
	TargetWorkerID    string       `json:"target_worker_id,omitempty"`
	Model             string       `json:"model,omitempty"`
	ModelVariant      string       `json:"model_variant,omitempty"`
	Branch            string       `json:"branch,omitempty"`
	WorktreePath      string       `json:"worktree_path,omitempty"`
	SessionName       string       `json:"session_name,omitempty"`
	Multiplexer       string       `json:"multiplexer,omitempty"`
	PRUrl             string       `json:"pr_url,omitempty"`
	ServerPort        int          `json:"server_port,omitempty"`
	OpenCodeSessionID string       `json:"opencode_session_id,omitempty"`
	StartedAt         time.Time    `json:"started_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	FileMtime         time.Time    `json:"file_mtime"`
}

// UnmarshalJSON implements custom unmarshalling to support both the legacy
// "tmux_session" key and the current "session_name" key in persisted index files.
func (e *runIndexEntry) UnmarshalJSON(data []byte) error {
	type Alias runIndexEntry
	aux := &struct {
		*Alias
		LegacySessionName string `json:"tmux_session,omitempty"`
	}{Alias: (*Alias)(e)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if e.SessionName == "" && aux.LegacySessionName != "" {
		e.SessionName = aux.LegacySessionName
	}
	return nil
}

type runIndex struct {
	Version   int                       `json:"version"`
	UpdatedAt time.Time                 `json:"updated_at"`
	Entries   map[string]*runIndexEntry `json:"entries"`
	DirMtimes map[string]time.Time      `json:"dir_mtimes"`
}

const (
	runIndexVersion  = 3
	runIndexFileName = ".orch_run_index.json"
)

type runIndexCache struct {
	mu      sync.RWMutex
	index   *runIndex
	rootDir string
}

var globalRunIndex = &runIndexCache{}

func (s *FileStore) getRunIndex() *runIndex {
	globalRunIndex.mu.RLock()
	if globalRunIndex.index != nil && globalRunIndex.rootDir == s.rootPath {
		idx := globalRunIndex.index
		globalRunIndex.mu.RUnlock()
		return idx
	}
	globalRunIndex.mu.RUnlock()

	globalRunIndex.mu.Lock()
	defer globalRunIndex.mu.Unlock()

	return s.getRunIndexLocked()
}

func (s *FileStore) getRunIndexLocked() *runIndex {
	if globalRunIndex.index != nil && globalRunIndex.rootDir == s.rootPath {
		return globalRunIndex.index
	}

	idx := s.loadRunIndex()
	globalRunIndex.index = idx
	globalRunIndex.rootDir = s.rootPath
	return idx
}

func (s *FileStore) loadRunIndex() *runIndex {
	indexPath := filepath.Join(s.rootPath, runIndexFileName)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return &runIndex{
			Version:   runIndexVersion,
			Entries:   make(map[string]*runIndexEntry),
			DirMtimes: make(map[string]time.Time),
		}
	}

	var idx runIndex
	if err := json.Unmarshal(data, &idx); err != nil || idx.Version != runIndexVersion {
		return &runIndex{
			Version:   runIndexVersion,
			Entries:   make(map[string]*runIndexEntry),
			DirMtimes: make(map[string]time.Time),
		}
	}

	if idx.Entries == nil {
		idx.Entries = make(map[string]*runIndexEntry)
	}
	if idx.DirMtimes == nil {
		idx.DirMtimes = make(map[string]time.Time)
	}

	return &idx
}

func (s *FileStore) saveRunIndex(idx *runIndex) {
	idx.UpdatedAt = time.Now()
	data, err := json.Marshal(idx)
	if err != nil {
		return
	}

	indexPath := filepath.Join(s.rootPath, runIndexFileName)
	tmp := indexPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, indexPath)
}

func (s *FileStore) listRunsIndexed(filter *store.ListRunsFilter) ([]*model.Run, error) {
	globalRunIndex.mu.Lock()
	defer globalRunIndex.mu.Unlock()

	idx := s.getRunIndexLocked()
	runsRoot := filepath.Join(s.rootPath, "runs")

	var issueDirs []string
	if filter != nil && filter.IssueID != "" {
		resolvedDir := s.resolveRunsDir(filter.IssueID)
		issueDirs = []string{filepath.Base(resolvedDir)}
	} else {
		entries, err := os.ReadDir(runsRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				issueDirs = append(issueDirs, e.Name())
			}
		}
	}

	statusSet := make(map[model.Status]bool)
	if filter != nil {
		for _, st := range filter.Status {
			statusSet[st] = true
		}
	}

	var sinceTime time.Time
	if filter != nil && filter.Since != "" {
		sinceTime, _ = time.Parse(time.RFC3339, filter.Since)
	}

	var olderThanTime time.Time
	if filter != nil && filter.OlderThan != "" {
		olderThanTime, _ = time.Parse(time.RFC3339, filter.OlderThan)
	}

	var timeRangeCutoff time.Time
	if filter != nil && filter.TimeRange != "" && filter.TimeRange != "all" {
		now := time.Now()
		switch filter.TimeRange {
		case "hour":
			timeRangeCutoff = now.Add(-1 * time.Hour)
		case "today":
			timeRangeCutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		case "week":
			timeRangeCutoff = now.Add(-7 * 24 * time.Hour)
		}
	}

	textSearch := ""
	if filter != nil && filter.TextSearch != "" {
		textSearch = strings.ToLower(filter.TextSearch)
	}

	agentFilter := ""
	if filter != nil && filter.Agent != "" {
		agentFilter = strings.ToLower(filter.Agent)
	}

	needsFullLoad := make(map[string]bool)
	validEntries := make(map[string]*runIndexEntry)
	indexDirty := false
	seenKeys := make(map[string]bool)

	for _, issueID := range issueDirs {
		issueRunsDir := filepath.Join(runsRoot, issueID)
		dirInfo, err := os.Stat(issueRunsDir)
		if err != nil {
			continue
		}

		currentDirMtime := dirInfo.ModTime()
		idx.DirMtimes[issueID] = currentDirMtime

		entries, err := os.ReadDir(issueRunsDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}

			runID := strings.TrimSuffix(e.Name(), ".md")
			key := issueID + "/" + runID
			runPath := filepath.Join(issueRunsDir, e.Name())

			info, err := e.Info()
			if err != nil {
				needsFullLoad[key] = true
				continue
			}
			fileMtime := info.ModTime()

			seenKeys[key] = true
			if cached, ok := idx.Entries[key]; ok && cached.FileMtime.UnixNano() == fileMtime.UnixNano() {
				if !matchesRunFilters(cached, statusSet, sinceTime, olderThanTime, timeRangeCutoff, textSearch, agentFilter) {
					continue
				}
				validEntries[key] = cached
				continue
			}

			run, err := s.loadRun(issueID, runID, runPath)
			if err != nil {
				continue
			}

			entry := &runIndexEntry{
				IssueID:           run.IssueID,
				RunID:             run.RunID,
				Status:            run.Status,
				Agent:             run.Agent,
				Target:            run.Target,
				TargetHost:        run.TargetHost,
				TargetWorkerID:    run.TargetWorkerID,
				Model:             run.Model,
				ModelVariant:      run.ModelVariant,
				Branch:            run.Branch,
				WorktreePath:      run.WorktreePath,
				SessionName:       run.SessionName,
				Multiplexer:       run.Multiplexer,
				PRUrl:             run.PRUrl,
				ServerPort:        run.ServerPort,
				OpenCodeSessionID: run.OpenCodeSessionID,
				StartedAt:         run.StartedAt,
				UpdatedAt:         run.UpdatedAt,
				FileMtime:         fileMtime,
			}
			if old, exists := idx.Entries[key]; !exists || !runEntryEqual(old, entry) || old.FileMtime.UnixNano() != fileMtime.UnixNano() {
				indexDirty = true
			}
			idx.Entries[key] = entry

			if !matchesRunFilters(entry, statusSet, sinceTime, olderThanTime, timeRangeCutoff, textSearch, agentFilter) {
				continue
			}
			validEntries[key] = entry
		}

		idx.DirMtimes[issueID] = currentDirMtime
	}

	scannedIssueDirs := make(map[string]bool)
	for _, dir := range issueDirs {
		scannedIssueDirs[dir] = true
	}

	for key := range idx.Entries {
		if !seenKeys[key] {
			keyIssueID, _, ok := strings.Cut(key, "/")
			if !ok {
				delete(idx.Entries, key)
				indexDirty = true
				continue
			}

			// When filtering by issue, only clean up entries for that specific issue
			// This prevents accidentally deleting entries for other issues
			if filter != nil && filter.IssueID != "" {
				if scannedIssueDirs[keyIssueID] {
					delete(idx.Entries, key)
					indexDirty = true
				}
			} else {
				delete(idx.Entries, key)
				indexDirty = true
			}
		}
	}

	if indexDirty {
		s.saveRunIndex(idx)
	}

	runs := make([]*model.Run, 0, len(validEntries))
	for _, entry := range validEntries {
		runs = append(runs, entryToRun(entry))
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].UpdatedAt.After(runs[j].UpdatedAt)
	})

	if filter != nil && filter.Limit > 0 && len(runs) > filter.Limit {
		runs = runs[:filter.Limit]
	}

	return runs, nil
}

func runEntryEqual(a, b *runIndexEntry) bool {
	return a.IssueID == b.IssueID &&
		a.RunID == b.RunID &&
		a.Status == b.Status &&
		a.Agent == b.Agent &&
		a.Target == b.Target &&
		a.TargetHost == b.TargetHost &&
		a.TargetWorkerID == b.TargetWorkerID &&
		a.Model == b.Model &&
		a.ModelVariant == b.ModelVariant &&
		a.Branch == b.Branch &&
		a.WorktreePath == b.WorktreePath &&
		a.SessionName == b.SessionName &&
		a.Multiplexer == b.Multiplexer &&
		a.PRUrl == b.PRUrl &&
		a.ServerPort == b.ServerPort &&
		a.OpenCodeSessionID == b.OpenCodeSessionID &&
		a.StartedAt.Equal(b.StartedAt) &&
		a.UpdatedAt.Equal(b.UpdatedAt)
}

func matchesRunFilters(entry *runIndexEntry, statusSet map[model.Status]bool, sinceTime, olderThanTime, timeRangeCutoff time.Time, textSearch, agentFilter string) bool {
	if len(statusSet) > 0 && !statusSet[entry.Status] {
		return false
	}
	if !sinceTime.IsZero() && entry.UpdatedAt.Before(sinceTime) {
		return false
	}
	if !olderThanTime.IsZero() && !entry.UpdatedAt.Before(olderThanTime) {
		return false
	}
	if !timeRangeCutoff.IsZero() && entry.StartedAt.Before(timeRangeCutoff) {
		return false
	}
	if agentFilter != "" && strings.ToLower(entry.Agent) != agentFilter {
		return false
	}
	if textSearch != "" {
		if !strings.Contains(strings.ToLower(entry.RunID), textSearch) &&
			!strings.Contains(strings.ToLower(entry.IssueID), textSearch) &&
			!strings.Contains(strings.ToLower(entry.Branch), textSearch) {
			return false
		}
	}
	return true
}

func entryToRun(e *runIndexEntry) *model.Run {
	return &model.Run{
		IssueID:           e.IssueID,
		RunID:             e.RunID,
		Status:            e.Status,
		Agent:             e.Agent,
		Target:            e.Target,
		TargetHost:        e.TargetHost,
		TargetWorkerID:    e.TargetWorkerID,
		Model:             e.Model,
		ModelVariant:      e.ModelVariant,
		Branch:            e.Branch,
		WorktreePath:      e.WorktreePath,
		SessionName:       e.SessionName,
		Multiplexer:       e.Multiplexer,
		PRUrl:             e.PRUrl,
		ServerPort:        e.ServerPort,
		OpenCodeSessionID: e.OpenCodeSessionID,
		StartedAt:         e.StartedAt,
		UpdatedAt:         e.UpdatedAt,
	}
}

func InvalidateRunIndex() {
	globalRunIndex.mu.Lock()
	defer globalRunIndex.mu.Unlock()
	globalRunIndex.index = nil
}
