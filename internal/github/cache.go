package github

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/proboscis/orch/internal/model"
	_ "modernc.org/sqlite"
)

type IssueCache struct {
	db *sql.DB
}

func NewIssueCache(dbPath string) (*IssueCache, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	cache := &IssueCache{db: db}
	if err := cache.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate cache database: %w", err)
	}

	return cache, nil
}

func (c *IssueCache) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS issue_cache (
		id INTEGER PRIMARY KEY,
		issue_id TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		status TEXT NOT NULL,
		body TEXT,
		labels TEXT,
		url TEXT,
		frontmatter TEXT,
		updated_at TEXT,
		synced_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_issue_cache_issue_id ON issue_cache(issue_id);
	CREATE INDEX IF NOT EXISTS idx_issue_cache_status ON issue_cache(status);
	`
	_, err := c.db.Exec(schema)
	return err
}

func (c *IssueCache) Upsert(issue *model.Issue) error {
	number := 0
	if fm, ok := issue.Frontmatter["number"]; ok {
		fmt.Sscanf(fm, "%d", &number)
	}

	labels := ""
	if fm, ok := issue.Frontmatter["labels"]; ok {
		labels = fm
	}

	url := issue.Path
	if fm, ok := issue.Frontmatter["url"]; ok {
		url = fm
	}

	updatedAt := ""
	if fm, ok := issue.Frontmatter["updated_at"]; ok {
		updatedAt = fm
	}

	frontmatterJSON, _ := json.Marshal(issue.Frontmatter)

	query := `
	INSERT INTO issue_cache (id, issue_id, title, status, body, labels, url, frontmatter, updated_at, synced_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(issue_id) DO UPDATE SET
		title = excluded.title,
		status = excluded.status,
		body = excluded.body,
		labels = excluded.labels,
		url = excluded.url,
		frontmatter = excluded.frontmatter,
		updated_at = excluded.updated_at,
		synced_at = excluded.synced_at
	`

	_, err := c.db.Exec(query,
		number,
		issue.ID,
		issue.Title,
		string(issue.Status),
		issue.Body,
		labels,
		url,
		string(frontmatterJSON),
		updatedAt,
		time.Now().Format(time.RFC3339),
	)
	return err
}

func (c *IssueCache) Get(number int) (*model.Issue, error) {
	query := `SELECT issue_id, title, status, body, labels, url, frontmatter FROM issue_cache WHERE id = ?`
	row := c.db.QueryRow(query, number)
	return c.scanIssue(row)
}

func (c *IssueCache) GetByID(issueID string) (*model.Issue, error) {
	query := `SELECT issue_id, title, status, body, labels, url, frontmatter FROM issue_cache WHERE issue_id = ?`
	row := c.db.QueryRow(query, issueID)
	return c.scanIssue(row)
}

func (c *IssueCache) ListAll() ([]*model.Issue, error) {
	query := `SELECT issue_id, title, status, body, labels, url, frontmatter FROM issue_cache ORDER BY id DESC`
	rows, err := c.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*model.Issue
	for rows.Next() {
		issue, err := c.scanIssueRow(rows)
		if err != nil {
			continue
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func (c *IssueCache) ListByStatus(status string) ([]*model.Issue, error) {
	query := `SELECT issue_id, title, status, body, labels, url, frontmatter FROM issue_cache WHERE status = ? ORDER BY id DESC`
	rows, err := c.db.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*model.Issue
	for rows.Next() {
		issue, err := c.scanIssueRow(rows)
		if err != nil {
			continue
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func (c *IssueCache) Delete(issueID string) error {
	_, err := c.db.Exec(`DELETE FROM issue_cache WHERE issue_id = ?`, issueID)
	return err
}

func (c *IssueCache) Clear() error {
	_, err := c.db.Exec(`DELETE FROM issue_cache`)
	return err
}

func (c *IssueCache) Close() error {
	return c.db.Close()
}

func (c *IssueCache) scanIssue(row *sql.Row) (*model.Issue, error) {
	var issueID, title, status, body, labels, url string
	var frontmatterJSON sql.NullString

	err := row.Scan(&issueID, &title, &status, &body, &labels, &url, &frontmatterJSON)
	if err != nil {
		return nil, err
	}

	return c.buildIssue(issueID, title, status, body, labels, url, frontmatterJSON), nil
}

func (c *IssueCache) scanIssueRow(rows *sql.Rows) (*model.Issue, error) {
	var issueID, title, status, body, labels, url string
	var frontmatterJSON sql.NullString

	err := rows.Scan(&issueID, &title, &status, &body, &labels, &url, &frontmatterJSON)
	if err != nil {
		return nil, err
	}

	return c.buildIssue(issueID, title, status, body, labels, url, frontmatterJSON), nil
}

func (c *IssueCache) buildIssue(issueID, title, status, body, labels, url string, frontmatterJSON sql.NullString) *model.Issue {
	frontmatter := make(map[string]string)
	if frontmatterJSON.Valid {
		_ = json.Unmarshal([]byte(frontmatterJSON.String), &frontmatter)
	}

	summary := title
	if len(summary) > 50 {
		summary = summary[:47] + "..."
	}

	return &model.Issue{
		ID:          model.IssueID(issueID),
		Title:       title,
		Summary:     summary,
		Status:      model.IssueStatus(status),
		Body:        body,
		Path:        url,
		Frontmatter: frontmatter,
	}
}
