package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/query"
	"github.com/s22625/orch/internal/store"
)

// mockQueryStore implements store.Store for query testing
type mockQueryStore struct {
	issues []*model.Issue
	runs   []*model.Run
}

func (m *mockQueryStore) ResolveIssue(issueID string) (*model.Issue, error) {
	for _, issue := range m.issues {
		if issue.ID == issueID {
			return issue, nil
		}
	}
	return nil, nil
}

func (m *mockQueryStore) ListIssues() ([]*model.Issue, error) {
	return m.issues, nil
}

func (m *mockQueryStore) SetIssueStatus(issueID string, status model.IssueStatus) error {
	return nil
}

func (m *mockQueryStore) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	return nil, nil
}

func (m *mockQueryStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	return nil
}

func (m *mockQueryStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	return m.runs, nil
}

func (m *mockQueryStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	return nil, nil
}

func (m *mockQueryStore) GetRunByShortID(shortID string) (*model.Run, error) {
	return nil, nil
}

func (m *mockQueryStore) GetLatestRun(issueID string) (*model.Run, error) {
	return nil, nil
}

func (m *mockQueryStore) RootPath() string {
	return "/test"
}

func TestQueryCommand_BasicQuery(t *testing.T) {
	now := time.Now()
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "test-issue-1", Title: "Test Issue 1", Status: model.IssueStatusOpen},
			{ID: "test-issue-2", Title: "Test Issue 2", Status: model.IssueStatusResolved},
		},
		runs: []*model.Run{
			{IssueID: "test-issue-1", RunID: "run-1", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test basic query
	result, err := engine.Execute("SELECT id, title FROM issues ORDER BY id")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}
}

func TestQueryCommand_JSONOutput(t *testing.T) {
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test", Status: model.IssueStatusOpen},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	result, err := engine.Execute("SELECT id, title FROM issues")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatJSON_(&sb, result); err != nil {
		t.Fatalf("FormatJSON_: %v", err)
	}

	var jsonOut struct {
		OK      bool                     `json:"ok"`
		Columns []string                 `json:"columns"`
		Rows    []map[string]interface{} `json:"rows"`
	}

	if err := json.Unmarshal([]byte(sb.String()), &jsonOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !jsonOut.OK {
		t.Error("expected ok: true")
	}

	if len(jsonOut.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(jsonOut.Columns))
	}

	if len(jsonOut.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(jsonOut.Rows))
	}
}

func TestQueryCommand_TSVOutput(t *testing.T) {
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "First", Status: model.IssueStatusOpen},
			{ID: "issue-2", Title: "Second", Status: model.IssueStatusOpen},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	result, err := engine.Execute("SELECT id, title FROM issues ORDER BY id")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatTSV_(&sb, result); err != nil {
		t.Fatalf("FormatTSV_: %v", err)
	}

	out := sb.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")

	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	// Check TSV format
	if !strings.Contains(lines[0], "\t") {
		t.Error("expected tab-separated values")
	}
}

func TestQueryCommand_TableOutput(t *testing.T) {
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test Issue", Status: model.IssueStatusOpen},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	result, err := engine.Execute("SELECT id, title FROM issues")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatTable_(&sb, result); err != nil {
		t.Fatalf("FormatTable_: %v", err)
	}

	out := sb.String()

	// Check header is present and uppercase
	if !strings.Contains(out, "ID") {
		t.Error("expected ID header")
	}
	if !strings.Contains(out, "TITLE") {
		t.Error("expected TITLE header")
	}

	// Check data is present
	if !strings.Contains(out, "issue-1") {
		t.Error("expected issue-1 in output")
	}
}

func TestQueryCommand_JoinQuery(t *testing.T) {
	now := time.Now()
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test Issue", Status: model.IssueStatusOpen},
		},
		runs: []*model.Run{
			{IssueID: "issue-1", RunID: "run-1", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
			{IssueID: "issue-1", RunID: "run-2", Status: model.StatusDone, StartedAt: now, UpdatedAt: now},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test join query
	result, err := engine.Execute(`
		SELECT r.run_id, i.title 
		FROM runs r 
		JOIN issues i ON i.id = r.issue_id
		ORDER BY r.run_id
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows from join, got %d", len(result.Rows))
	}
}

func TestQueryCommand_Aggregation(t *testing.T) {
	now := time.Now()
	st := &mockQueryStore{
		runs: []*model.Run{
			{IssueID: "issue-1", RunID: "run-1", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
			{IssueID: "issue-1", RunID: "run-2", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
			{IssueID: "issue-1", RunID: "run-3", Status: model.StatusDone, StartedAt: now, UpdatedAt: now},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test aggregation
	result, err := engine.Execute("SELECT status, COUNT(*) as count FROM runs GROUP BY status ORDER BY status")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 status groups, got %d", len(result.Rows))
	}

	// Find running count
	for _, row := range result.Rows {
		if row[0] == "running" {
			if row[1] != int64(2) {
				t.Errorf("expected 2 running, got %v", row[1])
			}
		}
	}
}

func TestQueryCommand_ViewsQuery(t *testing.T) {
	now := time.Now()
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test", Status: model.IssueStatusOpen, Tags: []string{"cli", "sql"}},
		},
		runs: []*model.Run{
			{IssueID: "issue-1", RunID: "run-1", Status: model.StatusRunning, StartedAt: now, UpdatedAt: now},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test issues_v view
	result, err := engine.Execute("SELECT id, run_count, tags FROM issues_v")
	if err != nil {
		t.Fatalf("Execute issues_v: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}

	// Check run_count
	if result.Rows[0][1] != int64(1) {
		t.Errorf("expected run_count=1, got %v", result.Rows[0][1])
	}

	// Test runs_v view
	result, err = engine.Execute("SELECT run_id, issue_title FROM runs_v")
	if err != nil {
		t.Fatalf("Execute runs_v: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}

	// Check issue_title from join
	if result.Rows[0][1] != "Test" {
		t.Errorf("expected issue_title='Test', got %v", result.Rows[0][1])
	}
}

func TestQueryCommand_InvalidSQL(t *testing.T) {
	st := &mockQueryStore{}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test invalid table
	_, err = engine.Execute("SELECT * FROM nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent table")
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Errorf("expected 'no such table' error, got: %v", err)
	}

	// Test invalid column
	_, err = engine.Execute("SELECT nonexistent FROM issues")
	if err == nil {
		t.Error("expected error for nonexistent column")
	}

	// Test syntax error
	_, err = engine.Execute("SELEC * FROM issues")
	if err == nil {
		t.Error("expected error for syntax error")
	}
}

func TestQueryCommand_WithEvents(t *testing.T) {
	now := time.Now()
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "Test", Status: model.IssueStatusOpen},
		},
		runs: []*model.Run{
			{
				IssueID:   "issue-1",
				RunID:     "run-1",
				Status:    model.StatusRunning,
				StartedAt: now,
				UpdatedAt: now,
				Events: []*model.Event{
					{Timestamp: now, Type: model.EventTypeStatus, Name: "running"},
					{Timestamp: now, Type: model.EventTypePhase, Name: "implement"},
				},
			},
		},
	}

	// Without events
	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	result, err := engine.Execute("SELECT COUNT(*) FROM events")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Rows[0][0] != int64(0) {
		t.Errorf("expected 0 events without --with-events, got %v", result.Rows[0][0])
	}
	engine.Close()

	// With events
	engine, err = query.NewEngine(st, &query.LoadOptions{WithEvents: true})
	if err != nil {
		t.Fatalf("NewEngine with events: %v", err)
	}
	defer engine.Close()

	result, err = engine.Execute("SELECT COUNT(*) FROM events")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Rows[0][0] != int64(2) {
		t.Errorf("expected 2 events with --with-events, got %v", result.Rows[0][0])
	}
}

func TestQueryCommand_TagsQuery(t *testing.T) {
	st := &mockQueryStore{
		issues: []*model.Issue{
			{ID: "issue-1", Title: "CLI Issue", Status: model.IssueStatusOpen, Tags: []string{"cli", "ux"}},
			{ID: "issue-2", Title: "Query Issue", Status: model.IssueStatusOpen, Tags: []string{"sql", "query"}},
			{ID: "issue-3", Title: "Both", Status: model.IssueStatusOpen, Tags: []string{"cli", "sql"}},
		},
	}

	engine, err := query.NewEngine(st, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test tag query via join
	result, err := engine.Execute(`
		SELECT DISTINCT i.id
		FROM issues i 
		JOIN issue_tags t ON i.id = t.issue_id 
		WHERE t.tag = 'cli'
		ORDER BY i.id
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 issues with 'cli' tag, got %d", len(result.Rows))
	}

	// Test multiple tags query
	result, err = engine.Execute(`
		SELECT i.id
		FROM issues i 
		WHERE EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.tag = 'cli')
		AND EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.tag = 'sql')
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 issue with both 'cli' and 'sql' tags, got %d", len(result.Rows))
	}
}
