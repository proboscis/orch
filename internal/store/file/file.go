package file

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

// FileStore implements store.Store using the filesystem
type FileStore struct {
	vaultPath  string
	issueCache map[string]*model.Issue // id -> issue
	cacheDirty bool
}

// New creates a new FileStore
func New(vaultPath string) (*FileStore, error) {
	absPath, err := filepath.Abs(vaultPath)
	if err != nil {
		return nil, fmt.Errorf("invalid vault path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("vault path does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vault path is not a directory: %s", absPath)
	}

	return &FileStore{
		vaultPath:  absPath,
		issueCache: make(map[string]*model.Issue),
		cacheDirty: true,
	}, nil
}

// VaultPath returns the vault root path
func (s *FileStore) VaultPath() string {
	return s.vaultPath
}

// scanIssues walks the vault and finds all files with type: issue frontmatter
func (s *FileStore) scanIssues() error {
	s.issueCache = make(map[string]*model.Issue)

	err := filepath.Walk(s.vaultPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip directories and non-markdown files
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Skip runs directory
		if strings.Contains(path, filepath.Join(s.vaultPath, "runs")) {
			return nil
		}

		// Try to parse as issue
		issue, err := s.parseIssueFile(path)
		if err != nil || issue == nil {
			return nil // Not an issue file
		}

		s.issueCache[issue.ID] = issue
		return nil
	})

	if err != nil {
		return err
	}

	s.cacheDirty = false
	return nil
}

// parseIssueFile reads a file and returns an Issue if it has type: issue frontmatter
func (s *FileStore) parseIssueFile(path string) (*model.Issue, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse frontmatter
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, nil // No frontmatter
	}

	frontmatter := make(map[string]string)
	bodyStart := 0
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
				frontmatter[key] = value
			}
		}
	}

	// Check if this is an issue file
	if frontmatter["type"] != "issue" {
		return nil, nil
	}

	// Get issue ID from frontmatter or filename
	issueID := frontmatter["id"]
	if issueID == "" {
		issueID = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	// Get title
	title := frontmatter["title"]
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
	topic := frontmatter["topic"]

	// Get summary (fall back to truncated title if not set)
	summary := frontmatter["summary"]
	if summary == "" && title != "" {
		summary = title
		if len(summary) > 50 {
			summary = summary[:47] + "..."
		}
	}

	return &model.Issue{
		ID:          issueID,
		Title:       title,
		Topic:       topic,
		Summary:     summary,
		Body:        body,
		Path:        path,
		Frontmatter: frontmatter,
	}, nil
}

// runPath returns the path to a run document
func (s *FileStore) runPath(issueID, runID string) string {
	return filepath.Join(s.vaultPath, "runs", issueID, runID+".md")
}

// runsDir returns the path to the runs directory for an issue
func (s *FileStore) runsDir(issueID string) string {
	return filepath.Join(s.vaultPath, "runs", issueID)
}

// ResolveIssue retrieves an issue by ID
func (s *FileStore) ResolveIssue(issueID string) (*model.Issue, error) {
	// Scan if cache is dirty
	if s.cacheDirty {
		if err := s.scanIssues(); err != nil {
			return nil, err
		}
	}

	issue, ok := s.issueCache[issueID]
	if !ok {
		// Try rescanning in case file was added
		s.cacheDirty = true
		if err := s.scanIssues(); err != nil {
			return nil, err
		}
		issue, ok = s.issueCache[issueID]
		if !ok {
			return nil, fmt.Errorf("issue not found: %s", issueID)
		}
	}

	return issue, nil
}

// ListIssues returns all issues in the vault
func (s *FileStore) ListIssues() ([]*model.Issue, error) {
	if s.cacheDirty {
		if err := s.scanIssues(); err != nil {
			return nil, err
		}
	}

	issues := make([]*model.Issue, 0, len(s.issueCache))
	for _, issue := range s.issueCache {
		issues = append(issues, issue)
	}
	return issues, nil
}

// CreateRun creates a new run for an issue
func (s *FileStore) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	// Verify issue exists
	_, err := s.ResolveIssue(issueID)
	if err != nil {
		return nil, err
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

	// Append event line to file
	f, err := os.OpenFile(run.Path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open run file: %w", err)
	}
	defer f.Close()

	line := event.String() + "\n"
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("failed to append event: %w", err)
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
					if key == "agent" {
						run.Agent = value
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
		run.WorktreePath = filepath.Join(s.vaultPath, run.WorktreePath)
	}

	return run, nil
}

// ListRuns lists runs matching the filter
func (s *FileStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	var runs []*model.Run
	runsRoot := filepath.Join(s.vaultPath, "runs")

	// Get list of issue directories
	var issueDirs []string
	if filter != nil && filter.IssueID != "" {
		issueDirs = []string{filter.IssueID}
	} else {
		entries, err := os.ReadDir(runsRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return runs, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				issueDirs = append(issueDirs, e.Name())
			}
		}
	}

	// Parse since filter
	var sinceTime time.Time
	if filter != nil && filter.Since != "" {
		var err error
		sinceTime, err = time.Parse(time.RFC3339, filter.Since)
		if err != nil {
			return nil, fmt.Errorf("invalid since timestamp: %w", err)
		}
	}

	// Status filter set
	statusSet := make(map[model.Status]bool)
	if filter != nil {
		for _, s := range filter.Status {
			statusSet[s] = true
		}
	}

	// Load runs from each issue directory
	for _, issueID := range issueDirs {
		issueRunsDir := filepath.Join(runsRoot, issueID)
		entries, err := os.ReadDir(issueRunsDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}

			runID := strings.TrimSuffix(e.Name(), ".md")
			run, err := s.loadRun(issueID, runID, filepath.Join(issueRunsDir, e.Name()))
			if err != nil {
				continue
			}

			// Apply filters
			if len(statusSet) > 0 && !statusSet[run.Status] {
				continue
			}
			if !sinceTime.IsZero() && run.UpdatedAt.Before(sinceTime) {
				continue
			}

			runs = append(runs, run)
		}
	}

	// Sort by updated_at descending
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].UpdatedAt.After(runs[j].UpdatedAt)
	})

	// Apply limit
	if filter != nil && filter.Limit > 0 && len(runs) > filter.Limit {
		runs = runs[:filter.Limit]
	}

	return runs, nil
}

// SetIssueStatus updates the status of an issue in its frontmatter
func (s *FileStore) SetIssueStatus(issueID string, status string) error {
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
					newLines = append(newLines, fmt.Sprintf("status: %s", status))
				}
				newLines = append(newLines, line)
				inFrontmatter = false
				continue
			}

			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "status" {
				newLines = append(newLines, fmt.Sprintf("status: %s", status))
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

	// Update cache
	issue.Frontmatter["status"] = status
	s.cacheDirty = true // Mark dirty to be safe, although we updated the object

	return nil
}

// Ensure FileStore implements Store
var _ store.Store = (*FileStore)(nil)
