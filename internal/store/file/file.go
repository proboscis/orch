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

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
	"gopkg.in/yaml.v3"
)

// FileStore implements store.Store using the filesystem
type FileStore struct {
	rootPath             string
	issueMu              sync.RWMutex
	issueCache           map[model.IssueID]*model.Issue // id -> issue
	issueFileStates      map[string]issueFileState      // path -> daemon-observed state
	issueScanInitialized bool
	cacheDirty           bool
	warnFunc             func(format string, args ...any) // optional warning function
	warnedFiles          sync.Map                         // dedup warnings per file
}

type issueFileState struct {
	modTime time.Time
	size    int64
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
		rootPath:        absPath,
		issueCache:      make(map[model.IssueID]*model.Issue),
		issueFileStates: make(map[string]issueFileState),
		cacheDirty:      true,
		// warnFunc defaults to nil (no warnings); set via SetWarnFunc
	}, nil
}

// RootPath returns the issues root path
func (s *FileStore) RootPath() string {
	return s.rootPath
}

// issuesDir returns the path to the issues directory, preferring "Issues"
// (Obsidian convention) if it exists, falling back to "issues".
func (s *FileStore) issuesDir() string {
	if info, err := os.Stat(filepath.Join(s.rootPath, "Issues")); err == nil && info.IsDir() {
		return filepath.Join(s.rootPath, "Issues")
	}
	return filepath.Join(s.rootPath, "issues")
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

func (s *FileStore) warn(format string, args ...any) {
	if s.warnFunc == nil {
		return
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
	issuesDir := s.issuesDir()
	issues := make(map[model.IssueID]*model.Issue)
	fileStates := make(map[string]issueFileState)
	seenIssueFiles := make(map[string]struct{})

	s.issueMu.Lock()
	defer s.issueMu.Unlock()

	if _, err := os.Stat(issuesDir); os.IsNotExist(err) {
		if s.issueScanInitialized && len(s.issueFileStates) > 0 {
			for path := range s.issueFileStates {
				return storeDriftError(path)
			}
		}
		s.issueCache = issues
		s.issueFileStates = fileStates
		s.issueScanInitialized = true
		s.cacheDirty = false
		return nil
	}

	// Use walkWithSymlinks to support symlinked issues directories
	err := walkWithSymlinks(issuesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to scan issue path %s: %w", path, err)
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		if expected, ok := s.issueFileStates[path]; ok && !expected.matches(info) {
			return storeDriftError(path)
		}

		issue, err := s.parseIssueFile(path)
		if err != nil {
			return fmt.Errorf("failed to parse issue file %s: %w", path, err)
		}
		if issue == nil {
			return nil
		}
		if _, known := s.issueFileStates[path]; !known && s.issueScanInitialized {
			return storeDriftError(path)
		}

		issue.ModifiedAt = info.ModTime()
		issues[issue.ID] = issue
		fileStates[path] = issueFileStateFromInfo(info)
		seenIssueFiles[path] = struct{}{}
		return nil
	})

	if err != nil {
		s.cacheDirty = true
		return err
	}
	if s.issueScanInitialized {
		for path := range s.issueFileStates {
			if _, ok := seenIssueFiles[path]; !ok {
				return storeDriftError(path)
			}
		}
	}

	s.issueCache = issues
	s.issueFileStates = fileStates
	s.issueScanInitialized = true
	s.cacheDirty = false
	return nil
}

func issueFileStateFromInfo(info os.FileInfo) issueFileState {
	return issueFileState{modTime: info.ModTime(), size: info.Size()}
}

func (state issueFileState) matches(info os.FileInfo) bool {
	return state.modTime.Equal(info.ModTime()) && state.size == info.Size()
}

func storeDriftError(path string) error {
	return fmt.Errorf("store drift detected: %s was modified outside the daemon (ADR-0004); restart the daemon to adopt the external change", path)
}

func (s *FileStore) verifyIssueFile(path string) error {
	s.issueMu.RLock()
	defer s.issueMu.RUnlock()
	return s.verifyIssueFileLocked(path)
}

func (s *FileStore) verifyIssueFileLocked(path string) error {
	expected, ok := s.issueFileStates[path]
	if !ok {
		if s.issueScanInitialized {
			return storeDriftError(path)
		}
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !expected.matches(info) {
		return storeDriftError(path)
	}
	return nil
}

func (s *FileStore) recordIssueFileWriteLocked(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat daemon-written issue file %s: %w", path, err)
	}
	s.issueFileStates[path] = issueFileStateFromInfo(info)
	s.cacheDirty = true
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
		return nil, nil, 0, fmt.Errorf("invalid frontmatter YAML: %w", err)
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

func frontmatterDeclaresIssue(content []byte) bool {
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			return false
		}
		if !strings.HasPrefix(line, "type:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "type:"))
		value = strings.Trim(value, `"'`)
		return value == "issue"
	}

	return false
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

	declaresIssue := frontmatterDeclaresIssue(content)

	// Parse frontmatter using YAML parser for proper multi-line support
	yamlFM, stringFM, bodyStart, err := extractFrontmatter(content)
	if err != nil {
		if declaresIssue {
			return nil, err
		}
		s.warnOnce(path, "orch: warning: skipping non-issue markdown with invalid frontmatter %s: %v\n", filepath.Base(path), err)
		return nil, nil
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
	typedIssueID := model.IssueID(issueID)

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
	status, err := parseFileIssueStatus(stringFM["status"])
	if err != nil {
		return nil, err
	}

	return &model.Issue{
		ID:          typedIssueID,
		Title:       title,
		Topic:       topic,
		Summary:     summary,
		Status:      status,
		Body:        body,
		Tags:        tags,
		Path:        path,
		BaseBranch:  stringFM["base_branch"],
		Frontmatter: stringFM,
	}, nil
}

func parseFileIssueStatus(value string) (model.IssueStatus, error) {
	status, err := model.ParseIssueStatus(value)
	if err == nil {
		return status, nil
	}

	switch strings.TrimSpace(value) {
	case "in-progress", "proposed", "reopened":
		return model.IssueStatusOpen, nil
	case "done":
		return model.IssueStatusResolved, nil
	case "cancelled", "cannot-reproduce", "closed-negative", "closed_negative", "deferred", "deprioritized":
		return model.IssueStatusClosed, nil
	default:
		return "", err
	}
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

func (s *FileStore) issueFromCache(issueID model.IssueID) (*model.Issue, bool) {
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
func (s *FileStore) runPath(issueID model.IssueID, runID model.RunID) string {
	return filepath.Join(s.resolveRunsDir(issueID), string(runID)+".md")
}

// runsDir returns the path to the runs directory for an issue
func (s *FileStore) runsDir(issueID model.IssueID) string {
	return s.resolveRunsDir(issueID)
}

// resolveRunsDir resolves the runs directory, checking both gh- and gh# formats for backward compat.
func (s *FileStore) resolveRunsDir(issueID model.IssueID) string {
	issueIDStr := string(issueID)
	runsRoot := filepath.Join(s.rootPath, "runs")

	if strings.HasPrefix(issueIDStr, "gh-") {
		canonicalDir := filepath.Join(runsRoot, issueIDStr)
		if _, err := os.Stat(canonicalDir); err == nil {
			return canonicalDir
		}

		legacyID := "gh#" + strings.TrimPrefix(issueIDStr, "gh-")
		legacyDir := filepath.Join(runsRoot, legacyID)
		if _, err := os.Stat(legacyDir); err == nil {
			return legacyDir
		}

		return canonicalDir
	}

	if strings.HasPrefix(issueIDStr, "gh#") {
		legacyDir := filepath.Join(runsRoot, issueIDStr)
		if _, err := os.Stat(legacyDir); err == nil {
			return legacyDir
		}

		canonicalID := "gh-" + strings.TrimPrefix(issueIDStr, "gh#")
		canonicalDir := filepath.Join(runsRoot, canonicalID)
		if _, err := os.Stat(canonicalDir); err == nil {
			return canonicalDir
		}

		return legacyDir
	}

	return filepath.Join(runsRoot, issueIDStr)
}

// canonicalIssueID maps an issue hex ref (ADR-0001) to the canonical issue
// ID. Anything that is not syntactically a hex ref passes through untouched,
// as does an exact issue ID that happens to look like one (exact match always
// wins) and a ref that matches nothing (so downstream not-found errors keep
// reporting what the caller typed). A ref matching more than one issue fails
// loud with the candidate list.
func (s *FileStore) canonicalIssueID(issueID model.IssueID) (model.IssueID, error) {
	if !model.IsIssueHexRef(string(issueID)) {
		return issueID, nil
	}

	if s.isCacheDirty() {
		if err := s.scanIssues(); err != nil {
			return "", err
		}
	}

	if _, ok := s.issueFromCache(issueID); ok {
		return issueID, nil
	}

	matches := s.issuesMatchingHexRef(string(issueID))
	if len(matches) == 0 {
		// The file may have been added since the last scan; rescan once.
		s.markCacheDirty()
		if err := s.scanIssues(); err != nil {
			return "", err
		}
		matches = s.issuesMatchingHexRef(string(issueID))
	}

	switch len(matches) {
	case 0:
		return issueID, nil
	case 1:
		return matches[0].ID, nil
	default:
		names := make([]string, 0, len(matches))
		for _, issue := range matches {
			names = append(names, fmt.Sprintf("%s (%s)", issue.ID, model.IssueShortHexID(issue.ID)))
		}
		sort.Strings(names)
		return "", fmt.Errorf("ambiguous issue hex ref %s: matches %s", issueID, strings.Join(names, ", "))
	}
}

func (s *FileStore) issuesMatchingHexRef(ref string) []*model.Issue {
	var matches []*model.Issue
	for _, issue := range s.issuesFromCache() {
		if strings.HasPrefix(model.IssueHexID(issue.ID), ref) {
			matches = append(matches, issue)
		}
	}
	return matches
}

// ResolveIssue retrieves an issue by ID or by issue hex ref (ADR-0001)
func (s *FileStore) ResolveIssue(issueID model.IssueID) (*model.Issue, error) {
	canonical, err := s.canonicalIssueID(issueID)
	if err != nil {
		return nil, err
	}
	issueID = canonical

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
	if err := s.verifyIssueFile(issue.Path); err != nil {
		return nil, err
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

// CreateRun creates a new run for an issue, verifying the issue exists in this
// store first (for non-GitHub issues). Use this on the co-located/master path
// where this store owns the issue.
func (s *FileStore) CreateRun(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
	// Skip verification for GitHub issues - they're not local files
	if !strings.HasPrefix(string(issueID), "gh-") && !strings.HasPrefix(string(issueID), "gh#") {
		issue, err := s.ResolveIssue(issueID)
		if err != nil {
			return nil, err
		}
		// A hex ref (ADR-0001) must never become a run directory name.
		issueID = issue.ID
	}

	return s.createRunDocument(issueID, runID, metadata)
}

// CreateRunForExistingIssue creates a run WITHOUT verifying the issue against
// this store. It is for worker-delegated execution: a worker may run on a
// different host than the master and therefore have no issue store at all, yet
// the master (the issue-store SSOT) has already resolved and verified the issue
// before delegating. The run document layout (runs/<issueID>/<runID>.md under
// this store's root) does not depend on the issue file being present; the runs
// directory is created as needed.
func (s *FileStore) CreateRunForExistingIssue(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
	canonical, err := s.canonicalIssueID(issueID)
	if err != nil {
		return nil, err
	}
	return s.createRunDocument(canonical, runID, metadata)
}

// createRunDocument writes the run document and returns the run. It assumes the
// issue has already been verified by the caller (or intentionally skipped for a
// worker-delegated run / GitHub issue).
func (s *FileStore) createRunDocument(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error) {
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
		newStatus, err := model.NormalizeStatus(event.Name)
		if err != nil {
			return fmt.Errorf("invalid status event for %s: %w", run.Ref().String(), err)
		}
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

	issueID, err := s.canonicalIssueID(ref.IssueID)
	if err != nil {
		return nil, err
	}

	runPath := s.runPath(issueID, ref.RunID)
	return s.loadRun(issueID, ref.RunID, runPath)
}

// GetLatestRun retrieves the latest run for an issue
func (s *FileStore) GetLatestRun(issueID model.IssueID) (*model.Run, error) {
	canonical, err := s.canonicalIssueID(issueID)
	if err != nil {
		return nil, err
	}
	issueID = canonical

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

	runID := model.RunID(latestName)
	return s.loadRun(issueID, runID, s.runPath(issueID, runID))
}

// GetRunByShortID finds a run by its short ID prefix (2-6 hex chars)
// Returns an error if no match found or if multiple runs match (ambiguous)
func (s *FileStore) GetRunByShortID(shortID model.ShortID) (*model.Run, error) {
	// List all runs and find matching short ID prefix
	runs, err := s.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return nil, err
	}

	var matches []*model.Run
	for _, run := range runs {
		if strings.HasPrefix(string(run.ShortID()), string(shortID)) {
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
func formatAmbiguousError(shortID model.ShortID, matches []*model.Run) error {
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
func (s *FileStore) loadRun(issueID model.IssueID, runID model.RunID, path string) (*model.Run, error) {
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
					case "profile":
						run.Profile = value
					case "target":
						run.Target = value
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

	if err := run.DeriveState(); err != nil {
		return nil, fmt.Errorf("failed to derive run state from %s: %w", path, err)
	}

	// Resolve relative worktree paths against the vault path
	// This handles runs created before worktree paths were made absolute
	if run.WorktreePath != "" && !filepath.IsAbs(run.WorktreePath) {
		run.WorktreePath = filepath.Join(s.rootPath, run.WorktreePath)
	}

	return run, nil
}

// ListRuns lists runs matching the filter
func (s *FileStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	if filter != nil && filter.IssueID != "" {
		canonical, err := s.canonicalIssueID(filter.IssueID)
		if err != nil {
			return nil, err
		}
		if canonical != filter.IssueID {
			scoped := *filter
			scoped.IssueID = canonical
			filter = &scoped
		}
	}
	return s.listRunsIndexed(filter)
}

// SetIssueStatus updates the status of an issue in its frontmatter
func (s *FileStore) SetIssueStatus(issueID model.IssueID, status model.IssueStatus) error {
	issue, err := s.ResolveIssue(issueID)
	if err != nil {
		return err
	}
	s.issueMu.Lock()
	defer s.issueMu.Unlock()
	if err := s.verifyIssueFileLocked(issue.Path); err != nil {
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

	return s.recordIssueFileWriteLocked(issue.Path)
}

func (s *FileStore) DeleteRun(ref *model.RunRef) error {
	issueID, err := s.canonicalIssueID(ref.IssueID)
	if err != nil {
		return err
	}
	runPath := s.runPath(issueID, ref.RunID)
	if _, err := os.Stat(runPath); os.IsNotExist(err) {
		return fmt.Errorf("run not found: %s#%s", ref.IssueID, ref.RunID)
	}

	if err := os.Remove(runPath); err != nil {
		return fmt.Errorf("failed to delete run: %w", err)
	}

	logDir := strings.TrimSuffix(runPath, ".md") + ".log"
	if info, err := os.Stat(logDir); err == nil && info.IsDir() {
		os.RemoveAll(logDir)
	}

	s.markCacheDirty()
	return nil
}

func (s *FileStore) UpdateIssue(issue *model.Issue) error {
	if issue.Path == "" {
		return fmt.Errorf("issue path is empty")
	}
	s.issueMu.Lock()
	defer s.issueMu.Unlock()
	if err := s.verifyIssueFileLocked(issue.Path); err != nil {
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

	var frontmatter []string
	frontmatter = append(frontmatter, lines[0])
	foundTitle := false
	foundSummary := false
	foundStatus := false
	closedFrontmatter := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			if issue.Title != "" && !foundTitle {
				frontmatter = append(frontmatter, "title: "+model.QuoteYAMLValue(issue.Title))
			}
			if issue.Summary != "" && !foundSummary {
				frontmatter = append(frontmatter, "summary: "+model.QuoteYAMLValue(issue.Summary))
			}
			if issue.Status != "" && !foundStatus {
				frontmatter = append(frontmatter, fmt.Sprintf("status: %s", issue.Status))
			}
			frontmatter = append(frontmatter, line)
			closedFrontmatter = true
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			switch strings.TrimSpace(parts[0]) {
			case "title":
				frontmatter = append(frontmatter, "title: "+model.QuoteYAMLValue(issue.Title))
				foundTitle = true
				continue
			case "summary":
				if issue.Summary != "" {
					frontmatter = append(frontmatter, "summary: "+model.QuoteYAMLValue(issue.Summary))
				} else {
					frontmatter = append(frontmatter, line)
				}
				foundSummary = true
				continue
			case "status":
				frontmatter = append(frontmatter, fmt.Sprintf("status: %s", issue.Status))
				foundStatus = true
				continue
			}
		}
		frontmatter = append(frontmatter, line)
	}
	if !closedFrontmatter {
		return fmt.Errorf("issue file has unterminated frontmatter: %s", issue.Path)
	}

	updated := strings.Join(frontmatter, "\n") + "\n" + issue.Body
	if err := os.WriteFile(issue.Path, []byte(updated), 0644); err != nil {
		return fmt.Errorf("failed to write issue file: %w", err)
	}

	return s.recordIssueFileWriteLocked(issue.Path)
}

func (s *FileStore) ValidateIssueFiles(issueID model.IssueID) (*store.ValidationResult, error) {
	result := &store.ValidationResult{}

	var issues []*model.Issue
	var err error

	if issueID != "" {
		issue, err := s.ResolveIssue(issueID)
		if err != nil {
			return nil, err
		}
		issues = []*model.Issue{issue}
	} else {
		issues, err = s.ListIssues()
		if err != nil {
			return nil, err
		}
	}

	result.Total = len(issues)
	idMap := make(map[model.IssueID][]string)

	for _, issue := range issues {
		idMap[issue.ID] = append(idMap[issue.ID], issue.Path)

		item := &store.ValidationResultItem{
			File:    issue.Path,
			IssueID: issue.ID,
		}

		if issue.ID == "" {
			item.Errors = append(item.Errors, store.ValidationIssue{
				Code:    "missing_id",
				Message: "Missing required field: id",
				Level:   "error",
			})
		}

		if issue.Title == "" {
			item.Errors = append(item.Errors, store.ValidationIssue{
				Code:    "missing_title",
				Message: "Missing required field: title",
				Level:   "error",
			})
		}

		if len(item.Errors) > 0 {
			result.Errors = append(result.Errors, item)
		} else {
			result.Valid++
		}
	}

	for id, files := range idMap {
		if len(files) > 1 {
			result.Duplicates = append(result.Duplicates, &store.DuplicateID{
				ID:    id,
				Files: files,
			})
		}
	}

	return result, nil
}

func (s *FileStore) WriteAgentPrompt(ref *model.RunRef, content string) error {
	run, err := s.GetRun(ref)
	if err != nil {
		return err
	}

	promptPath := filepath.Join(filepath.Dir(run.Path), fmt.Sprintf("%s.prompt.md", ref.RunID))
	return os.WriteFile(promptPath, []byte(content), 0644)
}

func (s *FileStore) ReadAgentPrompt(ref *model.RunRef) (string, error) {
	run, err := s.GetRun(ref)
	if err != nil {
		return "", err
	}

	promptPath := filepath.Join(filepath.Dir(run.Path), fmt.Sprintf("%s.prompt.md", ref.RunID))
	content, err := os.ReadFile(promptPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (s *FileStore) CreateIssue(issue *model.Issue) error {
	if issue.ID == "" {
		return fmt.Errorf("issue ID is required")
	}

	if err := model.ValidateNewIssueID(string(issue.ID)); err != nil {
		return err
	}

	issuePath := filepath.Join(s.issuesDir(), string(issue.ID)+".md")
	if _, err := os.Stat(issuePath); err == nil {
		return fmt.Errorf("issue already exists: %s", issue.ID)
	}

	if err := os.MkdirAll(filepath.Dir(issuePath), 0755); err != nil {
		return fmt.Errorf("failed to create issues directory: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(issue.RenderFrontmatter())
	sb.WriteString("\n")
	if issue.Body != "" {
		sb.WriteString(issue.Body)
	}

	if err := os.WriteFile(issuePath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write issue file: %w", err)
	}

	issue.Path = issuePath
	s.issueMu.Lock()
	defer s.issueMu.Unlock()
	return s.recordIssueFileWriteLocked(issuePath)
}

var _ store.Store = (*FileStore)(nil)
