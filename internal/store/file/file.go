package file

import (
	"bufio"
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
	vaultPath string
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

	return &FileStore{vaultPath: absPath}, nil
}

// VaultPath returns the vault root path
func (s *FileStore) VaultPath() string {
	return s.vaultPath
}

// issuePath returns the path to an issue document
func (s *FileStore) issuePath(issueID string) string {
	return filepath.Join(s.vaultPath, "issues", issueID+".md")
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
	path := s.issuePath(issueID)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("issue not found: %s", issueID)
		}
		return nil, err
	}

	issue := &model.Issue{
		ID:   issueID,
		Path: path,
	}

	// Parse frontmatter and body
	lines := strings.Split(string(content), "\n")
	inFrontmatter := false
	frontmatterLines := []string{}
	bodyStart := 0

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				bodyStart = i + 1
				continue
			}
			frontmatterLines = append(frontmatterLines, line)
		}
	}

	// Parse simple frontmatter (key: value)
	issue.Frontmatter = make(map[string]string)
	for _, line := range frontmatterLines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			issue.Frontmatter[key] = value
			if key == "title" {
				issue.Title = value
			}
		}
	}

	// Extract title from first heading if not in frontmatter
	if issue.Title == "" && bodyStart < len(lines) {
		for _, line := range lines[bodyStart:] {
			if strings.HasPrefix(line, "# ") {
				issue.Title = strings.TrimPrefix(line, "# ")
				break
			}
		}
	}

	if bodyStart < len(lines) {
		issue.Body = strings.Join(lines[bodyStart:], "\n")
	}

	return issue, nil
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

// GetRunByShortID finds a run by its 6-char short ID
func (s *FileStore) GetRunByShortID(shortID string) (*model.Run, error) {
	// List all runs and find matching short ID
	runs, err := s.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return nil, err
	}

	var matches []*model.Run
	for _, run := range runs {
		if run.ShortID() == shortID {
			matches = append(matches, run)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("run not found: %s", shortID)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous short ID %s: matches %d runs", shortID, len(matches))
	}

	return matches[0], nil
}

// loadRun loads a run from its file
func (s *FileStore) loadRun(issueID, runID, path string) (*model.Run, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("run not found: %s#%s", issueID, runID)
		}
		return nil, err
	}
	defer file.Close()

	run := &model.Run{
		IssueID: issueID,
		RunID:   runID,
		Path:    path,
		Events:  []*model.Event{},
	}

	scanner := bufio.NewScanner(file)
	eventPattern := regexp.MustCompile(`^-\s+\d{4}-\d{2}-\d{2}`)

	for scanner.Scan() {
		line := scanner.Text()
		if eventPattern.MatchString(line) {
			event, err := model.ParseEvent(line)
			if err == nil {
				run.Events = append(run.Events, event)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read run file: %w", err)
	}

	run.DeriveState()
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

// Ensure FileStore implements Store
var _ store.Store = (*FileStore)(nil)
