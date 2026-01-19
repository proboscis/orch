package query

import (
	"bytes"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

// mockStore implements store.Store for testing
type mockStore struct {
	issues []*model.Issue
	runs   []*model.Run
}

func (m *mockStore) ResolveIssue(issueID string) (*model.Issue, error) {
	for _, issue := range m.issues {
		if issue.ID == issueID {
			return issue, nil
		}
	}
	return nil, nil
}

func (m *mockStore) ListIssues() ([]*model.Issue, error) {
	return m.issues, nil
}

func (m *mockStore) SetIssueStatus(issueID string, status model.IssueStatus) error {
	return nil
}

func (m *mockStore) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	return nil
}

func (m *mockStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	return m.runs, nil
}

func (m *mockStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) GetRunByShortID(shortID string) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) GetLatestRun(issueID string) (*model.Run, error) {
	return nil, nil
}

func (m *mockStore) RootPath() string {
	return "/test"
}

func TestNewEngine(t *testing.T) {
	st := &mockStore{
		issues: []*model.Issue{
			{ID: "test-1", Title: "Test Issue 1", Status: model.IssueStatusOpen, Tags: []string{"cli", "sql"}},
			{ID: "test-2", Title: "Test Issue 2", Status: model.IssueStatusResolved},
		},
		runs: []*model.Run{
			{
				IssueID:   "test-1",
				RunID:     "20240101-120000",
				Status:    model.StatusRunning,
				Agent:     "claude",
				StartedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test basic query
	result, err := engine.Execute("SELECT id, title, status FROM issues ORDER BY id")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}

	if len(result.Columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(result.Columns))
	}
}

func TestQueryIssues(t *testing.T) {
	st := &mockStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "First Issue", Status: model.IssueStatusOpen},
			{ID: "issue-2", Title: "Second Issue", Status: model.IssueStatusOpen},
			{ID: "issue-3", Title: "Third Issue", Status: model.IssueStatusResolved},
		},
	}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test filtering by status
	result, err := engine.Execute("SELECT id FROM issues WHERE status = 'open'")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 open issues, got %d", len(result.Rows))
	}
}

func TestQueryRuns(t *testing.T) {
	now := time.Now()
	st := &mockStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test Issue", Status: model.IssueStatusOpen},
		},
		runs: []*model.Run{
			{IssueID: "issue-1", RunID: "run-1", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
			{IssueID: "issue-1", RunID: "run-2", Status: model.StatusDone, StartedAt: now, UpdatedAt: now},
			{IssueID: "issue-1", RunID: "run-3", Status: model.StatusFailed, StartedAt: now, UpdatedAt: now},
		},
	}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test counting by status
	result, err := engine.Execute("SELECT status, COUNT(*) as count FROM runs GROUP BY status ORDER BY status")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(result.Rows) != 3 {
		t.Errorf("expected 3 status groups, got %d", len(result.Rows))
	}
}

func TestQueryIssueTags(t *testing.T) {
	st := &mockStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "CLI Issue", Tags: []string{"cli", "ux"}},
			{ID: "issue-2", Title: "Query Issue", Tags: []string{"sql", "query"}},
		},
	}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test tag query via join
	result, err := engine.Execute(`
		SELECT i.id, i.title 
		FROM issues i 
		JOIN issue_tags t ON i.id = t.issue_id 
		WHERE t.tag = 'cli'
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 issue with 'cli' tag, got %d", len(result.Rows))
	}
}

func TestQueryViews(t *testing.T) {
	now := time.Now()
	st := &mockStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test Issue", Status: model.IssueStatusOpen, Tags: []string{"tag1", "tag2"}},
		},
		runs: []*model.Run{
			{IssueID: "issue-1", RunID: "run-1", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
			{IssueID: "issue-1", RunID: "run-2", Status: model.StatusDone, StartedAt: now, UpdatedAt: now},
		},
	}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Test issues_v view
	result, err := engine.Execute("SELECT id, run_count, tags FROM issues_v")
	if err != nil {
		t.Fatalf("Execute issues_v failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row from issues_v, got %d", len(result.Rows))
	}

	// Check run_count column
	if result.Rows[0][1] != int64(2) {
		t.Errorf("expected run_count=2, got %v", result.Rows[0][1])
	}

	// Test runs_v view
	result, err = engine.Execute("SELECT issue_id, run_id, issue_title, issue_status FROM runs_v")
	if err != nil {
		t.Fatalf("Execute runs_v failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows from runs_v, got %d", len(result.Rows))
	}
}

func TestFormatTable(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"id", "title"},
		Rows: [][]interface{}{
			{"1", "First"},
			{"2", "Second"},
		},
	}

	var buf bytes.Buffer
	err := FormatTable_(&buf, result)
	if err != nil {
		t.Fatalf("FormatTable failed: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("expected non-empty output")
	}

	// Check that header is present
	if !bytes.Contains([]byte(output), []byte("ID")) {
		t.Error("expected ID header in output")
	}
}

func TestFormatJSON(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"id", "name"},
		Rows: [][]interface{}{
			{"1", "Test"},
		},
	}

	var buf bytes.Buffer
	err := FormatJSON_(&buf, result)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte(`"ok": true`)) {
		t.Error("expected ok: true in JSON output")
	}
}

func TestFormatTSV(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"a", "b"},
		Rows: [][]interface{}{
			{"x", "y"},
			{"1", "2"},
		},
	}

	var buf bytes.Buffer
	err := FormatTSV_(&buf, result)
	if err != nil {
		t.Fatalf("FormatTSV failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("x\ty")) {
		t.Errorf("expected tab-separated output, got: %s", output)
	}
}

func TestGetSchema(t *testing.T) {
	st := &mockStore{}

	engine, err := NewEngine(st, &LoadOptions{WithEvents: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	schemas, err := engine.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}

	// Should have tables: issues, runs, issue_tags, events
	// And views: issues_v, runs_v
	tableCount := 0
	viewCount := 0
	for _, s := range schemas {
		if s.Type == "table" {
			tableCount++
		} else if s.Type == "view" {
			viewCount++
		}
	}

	if tableCount != 4 {
		t.Errorf("expected 4 tables, got %d", tableCount)
	}
	if viewCount != 2 {
		t.Errorf("expected 2 views, got %d", viewCount)
	}
}

func TestGetTableSchema(t *testing.T) {
	st := &mockStore{}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	info, err := engine.GetTableSchema("issues")
	if err != nil {
		t.Fatalf("GetTableSchema failed: %v", err)
	}

	if info.Name != "issues" {
		t.Errorf("expected name 'issues', got %s", info.Name)
	}

	if info.Type != "table" {
		t.Errorf("expected type 'table', got %s", info.Type)
	}

	// Check that id column exists and is PK
	foundID := false
	for _, col := range info.Columns {
		if col.Name == "id" {
			foundID = true
			if !col.PK {
				t.Error("expected id to be primary key")
			}
		}
	}
	if !foundID {
		t.Error("expected to find 'id' column")
	}
}

func TestQueryReadOnly(t *testing.T) {
	st := &mockStore{
		issues: []*model.Issue{
			{ID: "test-1", Title: "Test", Status: model.IssueStatusOpen},
		},
	}

	engine, err := NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Attempt to insert should fail due to query_only pragma
	_, err = engine.Execute("INSERT INTO issues (id, title) VALUES ('new', 'New Issue')")
	if err == nil {
		t.Error("expected INSERT to fail in read-only mode")
	}

	// Attempt to update should fail
	_, err = engine.Execute("UPDATE issues SET title = 'Modified' WHERE id = 'test-1'")
	if err == nil {
		t.Error("expected UPDATE to fail in read-only mode")
	}

	// Attempt to delete should fail
	_, err = engine.Execute("DELETE FROM issues")
	if err == nil {
		t.Error("expected DELETE to fail in read-only mode")
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
		wantErr  bool
	}{
		{"table", FormatTable, false},
		{"TABLE", FormatTable, false},
		{"json", FormatJSON, false},
		{"JSON", FormatJSON, false},
		{"tsv", FormatTSV, false},
		{"TSV", FormatTSV, false},
		{"", FormatTable, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseFormat(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
