package github

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
)

type Backend struct {
	owner       string
	repo        string
	labelFilter string
	cfg         *config.GitHubConfig
	cache       *IssueCache
	client      Client
}

type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	Labels    []ghLabel `json:"labels"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type ghLabel struct {
	Name string `json:"name"`
}

func NewBackend(cfg *config.GitHubConfig, cachePath string) (*Backend, error) {
	return NewBackendWithClient(cfg, cachePath, NewCLIClient())
}

func NewBackendWithClient(cfg *config.GitHubConfig, cachePath string, client Client) (*Backend, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("github backend not configured: missing owner or repo")
	}
	if client == nil {
		client = NewCLIClient()
	}

	cache, err := NewIssueCache(cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cache: %w", err)
	}

	return &Backend{
		owner:       cfg.Owner,
		repo:        cfg.Repo,
		labelFilter: cfg.LabelFilter,
		cfg:         cfg,
		cache:       cache,
		client:      client,
	}, nil
}

func (b *Backend) repoArg() string {
	return fmt.Sprintf("%s/%s", b.owner, b.repo)
}

func (b *Backend) runGH(args ...string) ([]byte, error) {
	out, err := b.client.Run(args...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// List fetches issues from GitHub and updates the cache.
// Returns an error if GitHub is unreachable - use ListFromCache for cached data.
func (b *Backend) List() ([]*model.Issue, error) {
	args := []string{
		"issue", "list",
		"-R", b.repoArg(),
		"--json", "number,title,body,state,url,labels,updatedAt,createdAt",
		"--state", "all",
		"--limit", "500",
	}
	if b.labelFilter != "" {
		args = append(args, "--label", b.labelFilter)
	}

	output, err := b.runGH(args...)
	if err != nil {
		// Return error - caller should decide whether to use cache
		return nil, fmt.Errorf("failed to fetch from GitHub: %w", err)
	}

	var ghIssues []ghIssue
	if err := json.Unmarshal(output, &ghIssues); err != nil {
		return nil, fmt.Errorf("failed to parse gh output: %w", err)
	}

	issues := make([]*model.Issue, 0, len(ghIssues))
	for _, gh := range ghIssues {
		issue := b.ghToIssue(&gh)
		issues = append(issues, issue)
		_ = b.cache.Upsert(issue)
	}

	return issues, nil
}

// ListFromCache returns issues from the local cache without hitting GitHub.
func (b *Backend) ListFromCache() ([]*model.Issue, error) {
	return b.cache.ListAll()
}

func (b *Backend) Get(issueNumber int) (*model.Issue, error) {
	args := []string{
		"issue", "view",
		strconv.Itoa(issueNumber),
		"-R", b.repoArg(),
		"--json", "number,title,body,state,url,labels,updatedAt,createdAt",
	}

	output, err := b.runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue #%d from GitHub: %w", issueNumber, err)
	}

	var gh ghIssue
	if err := json.Unmarshal(output, &gh); err != nil {
		return nil, fmt.Errorf("failed to parse gh output: %w", err)
	}

	issue := b.ghToIssue(&gh)
	_ = b.cache.Upsert(issue)
	return issue, nil
}

func (b *Backend) GetFromCache(issueNumber int) (*model.Issue, error) {
	return b.cache.Get(issueNumber)
}

func (b *Backend) GetByID(issueID string) (*model.Issue, error) {
	number, err := b.parseIssueNumber(issueID)
	if err != nil {
		return nil, fmt.Errorf("invalid issue ID format: %s", issueID)
	}
	return b.Get(number)
}

func (b *Backend) GetByIDFromCache(issueID string) (*model.Issue, error) {
	return b.cache.GetByID(issueID)
}

func (b *Backend) Create(title, body string, labels []string) (*model.Issue, error) {
	args := []string{
		"issue", "create",
		"-R", b.repoArg(),
		"--title", title,
		"--body", body,
	}

	if b.labelFilter != "" {
		labels = append(labels, b.labelFilter)
	}
	for _, label := range labels {
		args = append(args, "--label", label)
	}

	output, err := b.runGH(args...)
	if err != nil {
		return nil, err
	}

	url := strings.TrimSpace(string(output))
	number := b.extractNumberFromURL(url)
	if number <= 0 {
		return nil, fmt.Errorf("failed to parse issue number from: %s", url)
	}

	return b.Get(number)
}

func (b *Backend) Update(issueNumber int, title, body string) error {
	args := []string{
		"issue", "edit",
		strconv.Itoa(issueNumber),
		"-R", b.repoArg(),
	}

	if title != "" {
		args = append(args, "--title", title)
	}
	if body != "" {
		args = append(args, "--body", body)
	}

	_, err := b.runGH(args...)
	if err != nil {
		return err
	}

	_, _ = b.Get(issueNumber)
	return nil
}

func (b *Backend) Close(issueNumber int, comment string) error {
	args := []string{
		"issue", "close",
		strconv.Itoa(issueNumber),
		"-R", b.repoArg(),
	}

	if comment != "" {
		args = append(args, "--comment", comment)
	}

	_, err := b.runGH(args...)
	if err != nil {
		return err
	}

	_, _ = b.Get(issueNumber)
	return nil
}

func (b *Backend) AddLabel(issueNumber int, label string) error {
	args := []string{
		"issue", "edit",
		strconv.Itoa(issueNumber),
		"-R", b.repoArg(),
		"--add-label", label,
	}
	_, err := b.runGH(args...)
	return err
}

func (b *Backend) RemoveLabel(issueNumber int, label string) error {
	args := []string{
		"issue", "edit",
		strconv.Itoa(issueNumber),
		"-R", b.repoArg(),
		"--remove-label", label,
	}
	_, err := b.runGH(args...)
	return err
}

func (b *Backend) SetStatus(issueNumber int, status model.IssueStatus) error {
	if b.cfg.StatusLabels == nil {
		return nil
	}

	for label, mappedStatus := range b.cfg.StatusLabels {
		if mappedStatus == string(status) {
			return b.AddLabel(issueNumber, label)
		}
	}
	return nil
}

func (b *Backend) Sync() error {
	_, err := b.List()
	return err
}

func (b *Backend) SyncUpdatedSince(since time.Time) ([]*model.Issue, error) {
	issues, err := b.List()
	if err != nil {
		return nil, err
	}

	var updated []*model.Issue
	for _, issue := range issues {
		if fm, ok := issue.Frontmatter["updated_at"]; ok {
			if t, err := time.Parse(time.RFC3339, fm); err == nil && t.After(since) {
				updated = append(updated, issue)
			}
		}
	}
	return updated, nil
}

func (b *Backend) OpenInBrowser(issueNumber int) error {
	args := []string{
		"issue", "view",
		strconv.Itoa(issueNumber),
		"-R", b.repoArg(),
		"--web",
	}
	_, err := b.runGH(args...)
	return err
}

func (b *Backend) Cache() *IssueCache {
	return b.cache
}

func (b *Backend) ghToIssue(gh *ghIssue) *model.Issue {
	labels := make([]string, len(gh.Labels))
	for i, l := range gh.Labels {
		labels[i] = l.Name
	}

	status := b.mapLabelsToStatus(labels)
	if status == "" {
		if strings.EqualFold(gh.State, "open") {
			status = model.IssueStatusOpen
		} else {
			status = model.IssueStatusClosed
		}
	}

	issueID := fmt.Sprintf("gh-%d", gh.Number)

	return &model.Issue{
		ID:      model.IssueID(issueID),
		Title:   gh.Title,
		Summary: truncateSummary(gh.Title, 50),
		Status:  status,
		Body:    gh.Body,
		Path:    gh.URL,
		Frontmatter: map[string]string{
			"number":     strconv.Itoa(gh.Number),
			"url":        gh.URL,
			"state":      gh.State,
			"labels":     strings.Join(labels, ","),
			"updated_at": gh.UpdatedAt.Format(time.RFC3339),
			"created_at": gh.CreatedAt.Format(time.RFC3339),
		},
	}
}

func (b *Backend) mapLabelsToStatus(labels []string) model.IssueStatus {
	if b.cfg.StatusLabels == nil {
		return ""
	}

	labelSet := make(map[string]bool)
	for _, l := range labels {
		labelSet[l] = true
	}

	for label, status := range b.cfg.StatusLabels {
		if labelSet[label] {
			return model.IssueStatus(status)
		}
	}
	return ""
}

func (b *Backend) parseIssueNumber(issueID string) (int, error) {
	issueID = strings.TrimPrefix(issueID, "gh-")
	issueID = strings.TrimPrefix(issueID, "gh#") // backward compat
	issueID = strings.TrimPrefix(issueID, "#")
	return strconv.Atoi(issueID)
}

func (b *Backend) extractNumberFromURL(url string) int {
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return 0
	}
	num, _ := strconv.Atoi(parts[len(parts)-1])
	return num
}

func truncateSummary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
