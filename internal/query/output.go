package query

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Format represents an output format
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatTSV   Format = "tsv"
)

// ParseFormat parses a format string
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "table", "":
		return FormatTable, nil
	case "json":
		return FormatJSON, nil
	case "tsv":
		return FormatTSV, nil
	default:
		return "", fmt.Errorf("unknown format: %s (valid: table, json, tsv)", s)
	}
}

// FormatResult formats a QueryResult in the specified format
func FormatResult(w io.Writer, result *QueryResult, format Format) error {
	switch format {
	case FormatJSON:
		return FormatJSON_(w, result)
	case FormatTSV:
		return FormatTSV_(w, result)
	default:
		return FormatTable_(w, result)
	}
}

// FormatTable_ outputs the result as a formatted table
func FormatTable_(w io.Writer, result *QueryResult) error {
	if len(result.Columns) == 0 {
		return nil
	}

	// Calculate column widths
	widths := make([]int, len(result.Columns))
	for i, col := range result.Columns {
		widths[i] = len(col)
	}

	for _, row := range result.Rows {
		for i, val := range row {
			s := formatValue(val)
			if len(s) > widths[i] {
				widths[i] = len(s)
			}
		}
	}

	// Cap column widths at reasonable max
	const maxWidth = 60
	for i := range widths {
		if widths[i] > maxWidth {
			widths[i] = maxWidth
		}
	}

	// Print header
	for i, col := range result.Columns {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprintf(w, "%-*s", widths[i], strings.ToUpper(col))
	}
	fmt.Fprintln(w)

	// Print rows
	for _, row := range result.Rows {
		for i, val := range row {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			s := formatValue(val)
			if len(s) > widths[i] {
				s = s[:widths[i]-3] + "..."
			}
			fmt.Fprintf(w, "%-*s", widths[i], s)
		}
		fmt.Fprintln(w)
	}

	return nil
}

// FormatJSON_ outputs the result as JSON
func FormatJSON_(w io.Writer, result *QueryResult) error {
	output := struct {
		OK      bool                     `json:"ok"`
		Columns []string                 `json:"columns"`
		Rows    []map[string]interface{} `json:"rows"`
	}{
		OK:      true,
		Columns: result.Columns,
		Rows:    make([]map[string]interface{}, len(result.Rows)),
	}

	for i, row := range result.Rows {
		rowMap := make(map[string]interface{})
		for j, col := range result.Columns {
			rowMap[col] = row[j]
		}
		output.Rows[i] = rowMap
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// FormatTSV_ outputs the result as tab-separated values
func FormatTSV_(w io.Writer, result *QueryResult) error {
	for _, row := range result.Rows {
		for i, val := range row {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, formatValue(val))
		}
		fmt.Fprintln(w)
	}
	return nil
}

// formatValue converts a value to a string for display
func formatValue(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// SchemaInfo holds schema information for a table or view
type SchemaInfo struct {
	Name    string
	Type    string // "table" or "view"
	Columns []ColumnInfo
}

// ColumnInfo holds column information
type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	PK       bool
}

// GetSchemaInfo returns schema information for all tables and views
func GetSchemaInfo(db *DB) ([]SchemaInfo, error) {
	// Get tables
	tables, err := getObjectSchema(db, "table")
	if err != nil {
		return nil, err
	}

	// Get views
	views, err := getObjectSchema(db, "view")
	if err != nil {
		return nil, err
	}

	return append(tables, views...), nil
}

// GetTableSchema returns schema information for a specific table or view
func GetTableSchema(db *DB, name string) (*SchemaInfo, error) {
	// Check if table exists
	result, err := db.Execute(
		fmt.Sprintf("SELECT type FROM sqlite_master WHERE name = '%s'", name))
	if err != nil {
		return nil, err
	}
	if len(result.Rows) == 0 {
		return nil, fmt.Errorf("table or view %q not found", name)
	}

	objType, _ := result.Rows[0][0].(string)

	// Get column info using PRAGMA
	result, err = db.Execute(fmt.Sprintf("PRAGMA table_info('%s')", name))
	if err != nil {
		return nil, err
	}

	info := &SchemaInfo{
		Name:    name,
		Type:    objType,
		Columns: make([]ColumnInfo, 0, len(result.Rows)),
	}

	for _, row := range result.Rows {
		col := ColumnInfo{}
		if len(row) > 1 {
			col.Name, _ = row[1].(string)
		}
		if len(row) > 2 {
			col.Type, _ = row[2].(string)
		}
		if len(row) > 3 {
			if v, ok := row[3].(int64); ok {
				col.Nullable = v == 0
			}
		}
		if len(row) > 5 {
			if v, ok := row[5].(int64); ok {
				col.PK = v == 1
			}
		}
		info.Columns = append(info.Columns, col)
	}

	return info, nil
}

func getObjectSchema(db *DB, objType string) ([]SchemaInfo, error) {
	result, err := db.Execute(
		fmt.Sprintf("SELECT name FROM sqlite_master WHERE type = '%s' AND name NOT LIKE 'sqlite_%%' ORDER BY name", objType))
	if err != nil {
		return nil, err
	}

	var schemas []SchemaInfo
	for _, row := range result.Rows {
		name, _ := row[0].(string)
		info, err := GetTableSchema(db, name)
		if err != nil {
			continue
		}
		schemas = append(schemas, *info)
	}

	return schemas, nil
}

// FormatSchemaList outputs schema information
func FormatSchemaList(w io.Writer, schemas []SchemaInfo, format Format) error {
	switch format {
	case FormatJSON:
		return formatSchemaJSON(w, schemas)
	case FormatTSV:
		return formatSchemaTSV(w, schemas)
	default:
		return formatSchemaTable(w, schemas)
	}
}

func formatSchemaTable(w io.Writer, schemas []SchemaInfo) error {
	// Group by type
	tables := make([]SchemaInfo, 0)
	views := make([]SchemaInfo, 0)

	for _, s := range schemas {
		if s.Type == "view" {
			views = append(views, s)
		} else {
			tables = append(tables, s)
		}
	}

	if len(tables) > 0 {
		fmt.Fprintln(w, "TABLES:")
		for _, t := range tables {
			fmt.Fprintf(w, "  %s\n", t.Name)
		}
		fmt.Fprintln(w)
	}

	if len(views) > 0 {
		fmt.Fprintln(w, "VIEWS:")
		for _, v := range views {
			fmt.Fprintf(w, "  %s\n", v.Name)
		}
		fmt.Fprintln(w)
	}

	return nil
}

func formatSchemaJSON(w io.Writer, schemas []SchemaInfo) error {
	output := struct {
		OK      bool         `json:"ok"`
		Schemas []SchemaInfo `json:"schemas"`
	}{
		OK:      true,
		Schemas: schemas,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func formatSchemaTSV(w io.Writer, schemas []SchemaInfo) error {
	for _, s := range schemas {
		fmt.Fprintf(w, "%s\t%s\n", s.Type, s.Name)
	}
	return nil
}

// FormatTableDetail outputs detailed schema for a table
func FormatTableDetail(w io.Writer, info *SchemaInfo, format Format) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			OK     bool        `json:"ok"`
			Schema *SchemaInfo `json:"schema"`
		}{OK: true, Schema: info})
	case FormatTSV:
		for _, col := range info.Columns {
			pk := ""
			if col.PK {
				pk = "PK"
			}
			nullable := "NULL"
			if !col.Nullable {
				nullable = "NOT NULL"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", col.Name, col.Type, nullable, pk)
		}
		return nil
	default:
		fmt.Fprintf(w, "%s %s\n", strings.ToUpper(info.Type), info.Name)
		fmt.Fprintln(w, strings.Repeat("-", len(info.Type)+1+len(info.Name)))
		for _, col := range info.Columns {
			pk := ""
			if col.PK {
				pk = " [PK]"
			}
			nullable := ""
			if !col.Nullable {
				nullable = " NOT NULL"
			}
			fmt.Fprintf(w, "  %-20s %-10s%s%s\n", col.Name, col.Type, nullable, pk)
		}
		return nil
	}
}
