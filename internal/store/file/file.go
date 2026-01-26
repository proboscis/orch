package file

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"gopkg.in/yaml.v3"
)

// FileStore implements store.Store using the filesystem
type FileStore struct {
	rootPath    string
	issueMu     sync.RWMutex
	issueCache  map[string]*model.Issue // id -> issue
	cacheDirty  bool
	warnFunc    func(format string, args ...any) // optional warning function
	warnedFiles sync.Map                         // dedup warnings per file
}

// New creates a new FileStore
func New(rootPath string) (*FileStore, error) {
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid root path: %w", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create vault directory: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("root path does not exist: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("root path is not a directory: %s", absPath)
	}

	return &FileStore{
		rootPath:   absPath,
		issueCache: make(map[string]*model.Issue),
		cacheDirty: true,
		// warnFunc defaults to nil (no warnings); set via SetWarnFunc
	}, nil
}

// RootPath returns the issues root path
func (s *FileStore) RootPath() string {
	return s.rootPath
}

// SetWarnFunc sets a function to receive warnings (e.g., for duplicate frontmatter).
// If nil, warnings are suppressed. CLI can set this to print to stderr,
// daemon can route to its logger.
func (s *FileStore) SetWarnFunc(fn func(format string, args ...any)) {
	s.warnFunc = fn
}

// warnOnce emits a warning via warnFunc, but only once per file path per process.
func (s *FileStore) warnOnce(path, format string, args ...any) {
	if s.warnFunc == nil {
		return
	}
	if _, loaded := s.warnedFiles.LoadOrStore(path, struct{}{}); loaded {
		return // Already warned about this file
	}
	s.warnFunc(format, args...)
}

// frontmatterKeys are known keys that strongly indicate real frontmatter
var frontmatterKeys = map[string]bool{
	"type": true, "id": true, "title": true, "status": true,
	"tags": true, "summary": true, "topic": true,
}

// hasDuplicateFrontmatter checks if the body contains what looks like a second
// frontmatter block. Returns true only with strong evidence to minimize false positives.
func hasDuplicateFrontmatter(lines []string, bodyStart int) bool {
	if bodyStart <= 0 || bodyStart >= len(lines) {
		return false // No valid body to scan
	}

	inCodeFence := false
	var fencePrefix string

	for i := bodyStart; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Track code fence state (handle both ``` and ~~~)
		if !inCodeFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inCodeFence = true
				fencePrefix = trimmed[:3]
				continue
			}
		} else {
			if strings.HasPrefix(trimmed, fencePrefix) {
				inCodeFence = false
				fencePrefix = ""
			}
			continue
		}

		// Look for potential duplicate frontmatter start
		if trimmed == "---" && i+1 < len(lines) {
			// Scan following lines for frontmatter-like content
			keyCount := 0
			hasKnownKey := false

			for j := i + 1; j < len(lines) && j < i+10; j++ {
				nextLine := strings.TrimSpace(lines[j])
				if nextLine == "" {
					continue
				}
				if nextLine == "---" {
					// Found closing delimiter - this looks like real frontmatter
					if keyCount >= 2 || hasKnownKey {
						return true
					}
					break
				}
				if strings.HasPrefix(nextLine, "#") {
					break // Hit a heading, stop scanning
				}

				// Check for key: value pattern
				parts := strings.SplitN(nextLine, ":", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					if key != "" && !strings.Contains(key, " ") {
						keyCount++
						if frontmatterKeys[key] {
							hasKnownKey = true
						}
					}
				}
			}

			// Require strong evidence: known key OR multiple key-value pairs
			if hasKnownKey || keyCount >= 3 {
				return true
			}
		}
	}

	return false
}

func walkWithSymlinks(root string, walkFn filepath.WalkFunc) error {
	visited := make(map[string]struct{})

	var walk func(path string) error
	walk = func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			return walkFn(path, nil, err)
		}

		skipChildren := false
		if info.IsDir() {
			realPath, err := filepath.EvalSymlinks(path)
			if err == nil {
				if _, ok := visited[realPath]; ok {
					skipChildren = true
				} else {
					visited[realPath] = struct{}{}

				}

			} else if absPath, absErr := filepath.Abs(path); absErr == nil {
				if _, ok := visited[absPath]; ok {
					skipChildren = true
				} else {
					visited[absPath] = struct{}{}

				}

			}

		}

		switch err := walkFn(path, info, nil); err {
		case nil:
		case filepath.SkipDir:
			if info.IsDir() {
				return nil
			}

			return filepath.SkipDir
		case filepath.SkipAll:
			return filepath.SkipAll
		default:
			return err
		}

		if !info.IsDir() || skipChildren {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			switch err := walkFn(path, info, err); err {
			case nil, filepath.SkipDir:
				return nil
			case filepath.SkipAll:
				return filepath.SkipAll
			default:
				return err
			}

		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		for _, entry := range entries {
			childPath := filepath.Join(path, entry.Name())
			if err := walk(childPath); err != nil {
				if err == filepath.SkipDir {
					return nil
				}

				if err == filepath.SkipAll {
					return filepath.SkipAll
				}

				return err
			}

		}

		return nil
	}

	if err := walk(root); err != nil && err != filepath.SkipAll {
		return err
	}

	return nil
}

func (s *FileStore) scanIssues() error {
	issuesDir := filepath.Join(s.rootPath, "issues")
	issues := make(map[string]*model.Issue)

	s.issueMu.Lock()
	defer s.issueMu.Unlock()

	if _, err := os.Stat(issuesDir); os.IsNotExist(err) {
		s.issueCache = issues
		s.cacheDirty = false
		return nil
	}

	// Use walkWithSymlinks to support symlinked issues directories
	err := walkWithSymlinks(issuesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		issue, err := s.parseIssueFile(path)
		if err != nil || issue == nil {
			return nil
		}

		issue.ModifiedAt = info.ModTime()
		issues[issue.ID] = issue
		return nil
	})

	if err != nil {
		s.cacheDirty = true
		return err
	}

	s.issueCache = issues
	s.cacheDirty = false
	return nil
}

// extractFrontmatter extracts the frontmatter block from content and returns:
// - yamlFM: the YAML-parsed frontmatter as map[string]interface{}
// - stringFM: a string version of simple values for backward compatibility
// - bodyStart: the line index where the body starts
// - error: any parsing error
func extractFrontmatter(content []byte) (map[string]interface{}, map[string]string, int, error) {
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, nil, 0, nil // No frontmatter
	}

	// Find the closing ---
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}

	if endIdx == -1 {
		return nil, nil, 0, nil // No closing delimiter
	}

	// Extract frontmatter content
	fmContent := strings.Join(lines[1:endIdx], "\n")
	bodyStart := endIdx + 1

	// Parse with yaml.v3 to properly handle multi-line YAML lists
	var yamlFM map[string]interface{}
	if err := yaml.Unmarshal([]byte(fmContent), &yamlFM); err != nil {
		// Fall back to simple parsing if YAML fails
		yamlFM = make(map[string]interface{})
	}

	// Also create string map for backward compatibility
	stringFM := make(map[string]string)
	for key, value := range yamlFM {
		switch v := value.(type) {
		case string:
			stringFM[key] = v
		case []interface{}:
			// Convert YAML list to comma-separated string for backward compat
			// This is primarily for display in Frontmatter map
			var parts []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					parts = append(parts, s)
				}
			}
			stringFM[key] = strings.Join(parts, ", ")
		case int, int64, float64:
			stringFM[key] = fmt.Sprintf("%v", v)
		case bool:
			stringFM[key] = fmt.Sprintf("%v", v)
		default:
			// For other types, try to stringify
			if v != nil {
				stringFM[key] = fmt.Sprintf("%v", v)
			}
		}
	}

	return yamlFM, stringFM, bodyStart, nil
}

// parseTagsFromYAML extracts tags from YAML-parsed frontmatter.
// Handles both multi-line YAML lists ([]interface{}) and inline formats (string).
func parseTagsFromYAML(yamlFM map[string]interface{}) []string {
	if yamlFM == nil {
		return nil
	}

	tagsValue, ok := yamlFM["tags"]
	if !ok || tagsValue == nil {
		return nil
	}

	switch v := tagsValue.(type) {
	case []interface{}:
		// Multi-line YAML list: tags:\n  - bug\n  - feature
		var tags []string
		for _, item := range v {
			if tag, ok := item.(string); ok && tag != "" {
				tags = append(tags, tag)
			}
		}
		return tags
	case string:
		// Inline format: "tag1, tag2" or "[tag1, tag2]"
		return parseTags(v)
	default:
		return nil
	}
}

// parseIssueFile reads a file and returns an Issue if it has type: issue frontmatter
func (s *FileStore) parseIssueFile(path string) (*model.Issue, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse frontmatter using YAML parser for proper multi-line support
	yamlFM, stringFM, bodyStart, err := extractFrontmatter(content)
	if err != nil {
		return nil, err
	}
	if yamlFM == nil {
		return nil, nil // No frontmatter
	}

	lines := strings.Split(string(content), "\n")

	// Check for duplicate frontmatter blocks (causes data loss - tags in second block are ignored)
	if hasDuplicateFrontmatter(lines, bodyStart) {
		s.warnOnce(path, "orch: warning: %s has duplicate frontmatter blocks, tags may be lost\n", filepath.Base(path))
	}

	// Check if this is an issue file
	if stringFM["type"] != "issue" {
		return nil, nil
	}

	// Get issue ID from frontmatter or filename
	issueID := stringFM["id"]
	if issueID == "" {
		issueID = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	// Get title
	title := stringFM["title"]
	if title == "" && bodyStart < len(lines) {
		for _, line := range lines[bodyStart:] {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimPrefix(line, "# ")
				break
			}

		}

	}

	// Get body
	body := ""
	if bodyStart < len(lines) {
		body = strings.Join(lines[bodyStart:], "\n")
	}

	// Get topic
	topic := stringFM["topic"]

	// Get summary (fall back to truncated title if not set)
	summary := stringFM["summary"]
	if summary == "" && title != "" {
		summary = title
		if len(summary) > 50 {
			summary = summary[:47] + "..."
		}

	}

	// Get tags using YAML-aware parsing (handles multi-line YAML lists)
	tags := parseTagsFromYAML(yamlFM)
	status := model.ParseIssueStatus(stringFM["status"])

	return &model.Issue{
		ID:          issueID,
		Title:       title,
		Topic:       topic,
		Summary:     summary,
		Status:      status,
		Body:        body,
		Tags:        tags,
		Path:        path,
		Frontmatter: stringFM,
	}, nil
}

// parseTags parses tags from frontmatter value.
// Supports formats: "tag1, tag2", "[tag1, tag2]", "tag1,tag2"
// parseTags parses tags from frontmatter value.
// Supports formats: "tag1, tag2", "[tag1, tag2]", '["tag1", "tag2"]'
func parseTags(value string) []string {
	if value == "" {
		return nil
	}

	// Remove YAML list brackets if present
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = value[1 : len(value)-1]
	}

	// Split by comma and clean up
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		tag = strings.TrimSpace(tag)
		// Strip surrounding quotes (single or double)
		if len(tag) >= 2 {
			if (tag[0] == '"' && tag[len(tag)-1] == '"') ||
				(tag[0] == '\'' && tag[len(tag)-1] == '\'') {
				tag = tag[1 : len(tag)-1]
			}
		}
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func (s *FileStore) isCacheDirty() bool {
	s.issueMu.RLock()
	dirty := s.cacheDirty
	s.issueMu.RUnlock()
	return dirty
}

func (s *FileStore) markCacheDirty() {
	s.issueMu.Lock()
	s.cacheDirty = true
	s.issueMu.Unlock()
}

func (s *FileStore) issueFromCache(issueID string) (*model.Issue, bool) {
	s.issueMu.RLock()
	issue, ok := s.issueCache[issueID]
	if !ok {
		s.issueMu.RUnlock()
		return nil, false
	}

	clone := cloneIssue(issue)
	s.issueMu.RUnlock()
	return clone, true
}

func (s *FileStore) issuesFromCache() []*model.Issue {
	s.issueMu.RLock()
	issues := make([]*model.Issue, 0, len(s.issueCache))
	for _, issue := range s.issueCache {
		issues = append(issues, cloneIssue(issue))
	}

	s.issueMu.RUnlock()
	return issues
}

func cloneIssue(issue *model.Issue) *model.Issue {
	if issue == nil {
		return nil
	}

	clone := *issue
	if issue.Frontmatter != nil {
		clone.Frontmatter = make(map[string]string, len(issue.Frontmatter))
		for k, v := range issue.Frontmatter {
			clone.Frontmatter[k] = v
		}

	}

	return &clone
}

// runPath returns the path to a run document
func (s *FileStore) runPath(issueID, runID string) string {
	return filepath.Join(s.resolveRunsDir(issueID), runID+".md")
}

// runsDir returns the path to the runs directory for an issue
func (s *FileStore) runsDir(issueID string) string {
	return s.resolveRunsDir(issueID)
}

// resolveRunsDir resolves the runs directory, checking both gh- and gh# formats for backward compat.
func (s *FileStore) resolveRunsDir(issueID string) string {
	runsRoot := filepath.Join(s.rootPath, "runs")

	if strings.HasPrefix(issueID, "gh-") {
		canonicalDir := filepath.Join(runsRoot, issueID)
		if _, err := os.Stat(canonicalDir); err == nil {
			return canonicalDir
		}

		legacyID := "gh#" + strings.TrimPrefix(issueID, "gh-")
		legacyDir := filepath.Join(runsRoot, legacyID)
		if _, err := os.Stat(legacyDir); err == nil {
			return legacyDir
		}

		return canonicalDir
	}

	if strings.HasPrefix(issueID, "gh#") {
		legacyDir := filepath.Join(runsRoot, issueID)
		if _, err := os.Stat(legacyDir); err == nil {
			return legacyDir
		}

		canonicalID := "gh-" + strings.TrimPrefix(issueID, "gh#")
		canonicalDir := filepath.Join(runsRoot, canonicalID)
		if _, err := os.Stat(canonicalDir); err == nil {
			return canonicalDir
		}

		return legacyDir
	}

	return filepath.Join(runsRoot, issueID)
}

// ResolveIssue retrieves an issue by ID
func (s *FileStore) ResolveIssue(issueID string) (*model.Issue, error) {
	// Scan if cache is dirty
	if s.isCacheDirty() {
		if err := s.scanIssues(); err != nil {
			return nil, err
		}

	}

	issue, ok := s.issueFromCache(issueID)
	if !ok {
		// Try rescanning in case file was added
		s.markCacheDirty()
		if err := s.scanIssues(); err != nil {
			return nil, err
		}

		issue, ok = s.issueFromCache(issueID)
		if !ok {
			return nil, fmt.Errorf("issue not found: %s", issueID)
		}

	}

	return issue, nil
}

// ListIssues returns all issues in the vault
func (s *FileStore) ListIssues() ([]*model.Issue, error) {
	// Always rescan issues from disk to ensure fresh data for auto-refresh
	if err := s.scanIssues(); err != nil {
		return nil, err
	}

	issues := s.issuesFromCache()
	return issues, nil
}

// CreateRun creates a new run for an issue
func (s *FileStore) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	// Skip verification for GitHub issues - they're not local files
	if !strings.HasPrefix(issueID, "gh-") && !strings.HasPrefix(issueID, "gh#") {
		_, err := s.ResolveIssue(issueID)
		if err != nil {
			return nil, err
		}

	}

	// Create runs directory for issue if needed
	runsDir := s.runsDir(issueID)
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create runs directory: %w", err)
	}

	// Create run document
	runPath := s.runPath(issueID, runID)
	if _, err := os.Stat(runPath); err == nil {
		return nil, fmt.Errorf("run already exists: %s#%s", issueID, runID)
	}

	// Build frontmatter
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("issue: %s\n", issueID))
	sb.WriteString(fmt.Sprintf("run: %s\n", runID))
	sb.WriteString(fmt.Sprintf("created: %s\n", time.Now().Format(time.RFC3339)))
	for k, v := range metadata {
		sb.WriteString(fmt.Sprintf("%s: %s\n", k, v))
	}

	sb.WriteString("---\n\n")
	sb.WriteString("# Events\n\n")

	if err := os.WriteFile(runPath, []byte(sb.String()), 0644); err != nil {
		return nil, fmt.Errorf("failed to create run document: %w", err)
	}

	run := &model.Run{
		IssueID:   issueID,
		RunID:     runID,
		Path:      runPath,
		Status:    model.StatusQueued,
		Events:    []*model.Event{},
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return run, nil
}

// AppendEvent appends an event to a run
func (s *FileStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	run, err := s.GetRun(ref)
	if err != nil {
		return err
	}

	if event.Type == model.EventTypeStatus {
		newStatus := model.Status(event.Name)
		source := model.EventSource(event.Attrs["source"])
		if source == "" {
			source = model.EventSourceDaemon
		}
		if !model.CanTransitionStatus(run.Status, newStatus, source) {
			return fmt.Errorf("cannot transition from %s to %s (source: %s)", run.Status, newStatus, source)
		}
	}

	f, err := os.OpenFile(run.Path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open run file: %w", err)
	}

	defer f.Close()

	line := event.String() + "\n"
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("failed to append event: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync run file: %w", err)
	}

	return nil
}

// GetRun retrieves a run by reference
func (s *FileStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	if ref.IsLatest() {
		return s.GetLatestRun(ref.IssueID)
	}

	runPath := s.runPath(ref.IssueID, ref.RunID)
	return s.loadRun(ref.IssueID, ref.RunID, runPath)
}

// GetLatestRun retrieves the latest run for an issue
func (s *FileStore) GetLatestRun(issueID string) (*model.Run, error) {
	runsDir := s.runsDir(issueID)
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no runs found for issue: %s", issueID)
		}

		return nil, err
	}

	// Find latest run by filename (they're timestamped)
	var latestName string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(e.Name(), ".md")
		if name > latestName {
			latestName = name
		}

	}

	if latestName == "" {
		return nil, fmt.Errorf("no runs found for issue: %s", issueID)
	}

	return s.loadRun(issueID, latestName, s.runPath(issueID, latestName))
}

// GetRunByShortID finds a run by its short ID prefix (2-6 hex chars)
// Returns an error if no match found or if multiple runs match (ambiguous)
func (s *FileStore) GetRunByShortID(shortID string) (*model.Run, error) {
	// List all runs and find matching short ID prefix
	runs, err := s.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return nil, err
	}

	var matches []*model.Run
	for _, run := range runs {
		if strings.HasPrefix(run.ShortID(), shortID) {
			matches = append(matches, run)
		}

	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("run not found: %s", shortID)
	}

	if len(matches) > 1 {
		return nil, formatAmbiguousError(shortID, matches)
	}

	return matches[0], nil
}

// formatAmbiguousError formats an error message for ambiguous short ID matches
func formatAmbiguousError(shortID string, matches []*model.Run) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ambiguous run ID '%s': matches %d runs\n", shortID, len(matches)))

	// Show up to 5 matches with their details
	limit := 5
	if len(matches) < limit {
		limit = len(matches)
	}

	for i := 0; i < limit; i++ {
		run := matches[i]
		sb.WriteString(fmt.Sprintf("  %s  %s#%s\n", run.ShortID(), run.IssueID, run.RunID))
	}

	if len(matches) > 5 {
		sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(matches)-5))
	}

	sb.WriteString("Hint: use more characters to disambiguate")

	return fmt.Errorf("%s", sb.String())
}

// loadRun loads a run from its file
func (s *FileStore) loadRun(issueID, runID, path string) (*model.Run, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("run not found: %s#%s", issueID, runID)
		}

		return nil, err
	}

	run := &model.Run{
		IssueID: issueID,
		RunID:   runID,
		Path:    path,
		Events:  []*model.Event{},
	}

	// Parse frontmatter
	lines := strings.Split(string(content), "\n")
	bodyStart := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		inFrontmatter := true
		for i := 1; i < len(lines); i++ {
			line := lines[i]
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				bodyStart = i + 1
				break
			}

			if inFrontmatter {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					switch key {
					case "agent":
						run.Agent = value
					case "model":
						run.Model = value
					case "model_variant":
						run.ModelVariant = value
					case "continued_from":
						run.ContinuedFrom = value
					}

				}

			}

		}

	}

	// Parse events from body
	eventPattern := regexp.MustCompile(`^-\s+\d{4}-\d{2}-\d{2}`)
	for i := bodyStart; i < len(lines); i++ {
		line := lines[i]
		if eventPattern.MatchString(line) {
			event, err := model.ParseEvent(line)
			if err == nil {
				run.Events = append(run.Events, event)
			}

		}

	}

	run.DeriveState()

	// Resolve relative worktree paths against the vault path
	// This handles runs created before worktree paths were made absolute
	if run.WorktreePath != "" && !filepath.IsAbs(run.WorktreePath) {
		run.WorktreePath = filepath.Join(s.rootPath, run.WorktreePath)
	}

	return run, nil
}

// ListRuns lists runs matching the filter
func (s *FileStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	return s.listRunsIndexed(filter)
}

// SetIssueStatus updates the status of an issue in its frontmatter
func (s *FileStore) SetIssueStatus(issueID string, status model.IssueStatus) error {
	issue, err := s.ResolveIssue(issueID)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(issue.Path)
	if err != nil {
		return fmt.Errorf("failed to read issue file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fmt.Errorf("issue file has no frontmatter: %s", issue.Path)
	}

	statusStr := string(status)
	var newLines []string
	newLines = append(newLines, lines[0])
	foundStatus := false
	inFrontmatter := true

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				if !foundStatus {
					// Add status if not found in frontmatter
					newLines = append(newLines, fmt.Sprintf("status: %s", statusStr))
				}

				newLines = append(newLines, line)
				inFrontmatter = false
				continue
			}

			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "status" {
				newLines = append(newLines, fmt.Sprintf("status: %s", statusStr))
				foundStatus = true
			} else {
				newLines = append(newLines, line)
			}

		} else {
			newLines = append(newLines, line)
		}

	}

	if err := os.WriteFile(issue.Path, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write issue file: %w", err)
	}

	s.markCacheDirty()

	return nil
}

// Ensure FileStore implements Store
var _ store.Store = (*FileStore)(nil)
