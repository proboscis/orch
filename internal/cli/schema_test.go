package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/query"
)

func TestSchemaCommand_ListAll(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, &query.LoadOptions{WithEvents: true})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	schemas, err := engine.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}

	// Should have tables: issues, runs, issue_tags, events
	// And views: issues_v, runs_v
	tableCount := 0
	viewCount := 0
	tableNames := make(map[string]bool)
	viewNames := make(map[string]bool)

	for _, s := range schemas {
		if s.Type == "table" {
			tableCount++
			tableNames[s.Name] = true
		} else if s.Type == "view" {
			viewCount++
			viewNames[s.Name] = true
		}
	}

	if tableCount != 4 {
		t.Errorf("expected 4 tables, got %d", tableCount)
	}
	if viewCount != 2 {
		t.Errorf("expected 2 views, got %d", viewCount)
	}

	// Check specific tables exist
	expectedTables := []string{"issues", "runs", "issue_tags", "events"}
	for _, name := range expectedTables {
		if !tableNames[name] {
			t.Errorf("expected table %s", name)
		}
	}

	// Check specific views exist
	expectedViews := []string{"issues_v", "runs_v"}
	for _, name := range expectedViews {
		if !viewNames[name] {
			t.Errorf("expected view %s", name)
		}
	}
}

func TestSchemaCommand_TableDetail(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test issues table schema
	info, err := engine.GetTableSchema("issues")
	if err != nil {
		t.Fatalf("GetTableSchema: %v", err)
	}

	if info.Name != "issues" {
		t.Errorf("expected name 'issues', got %s", info.Name)
	}

	if info.Type != "table" {
		t.Errorf("expected type 'table', got %s", info.Type)
	}

	// Check expected columns
	expectedCols := []string{"id", "title", "topic", "summary", "status", "body", "path"}
	colNames := make(map[string]bool)
	for _, col := range info.Columns {
		colNames[col.Name] = true
	}

	for _, expected := range expectedCols {
		if !colNames[expected] {
			t.Errorf("expected column %s in issues table", expected)
		}
	}
}

func TestSchemaCommand_RunsTable(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	info, err := engine.GetTableSchema("runs")
	if err != nil {
		t.Fatalf("GetTableSchema: %v", err)
	}

	// Check expected columns in runs table
	expectedCols := []string{"issue_id", "run_id", "hex_id", "status", "phase", "agent", "model", "branch", "worktree_path", "session_name", "pr_url", "started_at", "updated_at"}
	colNames := make(map[string]bool)
	for _, col := range info.Columns {
		colNames[col.Name] = true
	}

	for _, expected := range expectedCols {
		if !colNames[expected] {
			t.Errorf("expected column %s in runs table", expected)
		}
	}
}

func TestSchemaCommand_ViewDetail(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Test issues_v view schema
	info, err := engine.GetTableSchema("issues_v")
	if err != nil {
		t.Fatalf("GetTableSchema: %v", err)
	}

	if info.Type != "view" {
		t.Errorf("expected type 'view', got %s", info.Type)
	}

	// Check for computed columns
	colNames := make(map[string]bool)
	for _, col := range info.Columns {
		colNames[col.Name] = true
	}

	// issues_v should have run_count and tags computed columns
	if !colNames["run_count"] {
		t.Error("expected run_count computed column in issues_v")
	}
	if !colNames["tags"] {
		t.Error("expected tags computed column in issues_v")
	}
}

func TestSchemaCommand_NonexistentTable(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	_, err = engine.GetTableSchema("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent table")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestSchemaCommand_JSONOutput(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	schemas, err := engine.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatSchemaList(&sb, schemas, query.FormatJSON); err != nil {
		t.Fatalf("FormatSchemaList: %v", err)
	}

	var jsonOut struct {
		OK      bool `json:"ok"`
		Schemas []struct {
			Name string `json:"Name"`
			Type string `json:"Type"`
		} `json:"schemas"`
	}

	if err := json.Unmarshal([]byte(sb.String()), &jsonOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !jsonOut.OK {
		t.Error("expected ok: true")
	}

	if len(jsonOut.Schemas) == 0 {
		t.Error("expected non-empty schemas")
	}
}

func TestSchemaCommand_TableOutput(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	schemas, err := engine.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatSchemaList(&sb, schemas, query.FormatTable); err != nil {
		t.Fatalf("FormatSchemaList: %v", err)
	}

	out := sb.String()

	// Check for TABLES and VIEWS headers
	if !strings.Contains(out, "TABLES:") {
		t.Error("expected TABLES: header")
	}
	if !strings.Contains(out, "VIEWS:") {
		t.Error("expected VIEWS: header")
	}

	// Check for table names
	if !strings.Contains(out, "issues") {
		t.Error("expected 'issues' in output")
	}
	if !strings.Contains(out, "runs") {
		t.Error("expected 'runs' in output")
	}
}

func TestSchemaCommand_TableDetailOutput(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	info, err := engine.GetTableSchema("issues")
	if err != nil {
		t.Fatalf("GetTableSchema: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatTableDetail(&sb, info, query.FormatTable); err != nil {
		t.Fatalf("FormatTableDetail: %v", err)
	}

	out := sb.String()

	// Check header
	if !strings.Contains(out, "TABLE issues") {
		t.Error("expected 'TABLE issues' header")
	}

	// Check column names are listed
	if !strings.Contains(out, "id") {
		t.Error("expected 'id' column")
	}
	if !strings.Contains(out, "title") {
		t.Error("expected 'title' column")
	}

	// Check PK indicator
	if !strings.Contains(out, "[PK]") {
		t.Error("expected [PK] indicator for primary key")
	}
}

func TestSchemaCommand_TSVOutput(t *testing.T) {
	api := &mockQueryAPI{}

	engine, err := query.NewEngine(api, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	schemas, err := engine.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}

	var sb strings.Builder
	if err := query.FormatSchemaList(&sb, schemas, query.FormatTSV); err != nil {
		t.Fatalf("FormatSchemaList: %v", err)
	}

	out := sb.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")

	// Should have lines for each table and view
	if len(lines) < 4 {
		t.Errorf("expected at least 4 lines, got %d", len(lines))
	}

	// Each line should be tab-separated with type and name
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			t.Errorf("expected 2 tab-separated fields, got %d: %s", len(parts), line)
		}
		if parts[0] != "table" && parts[0] != "view" {
			t.Errorf("expected type 'table' or 'view', got %s", parts[0])
		}
	}
}
