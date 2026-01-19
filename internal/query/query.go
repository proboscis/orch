package query

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB represents an in-memory SQLite database for querying entities
type DB struct {
	db *sql.DB
}

// OpenDB creates a new in-memory SQLite database with query-only mode
func OpenDB() (*DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open in-memory database: %w", err)
	}

	// Set pragmas for safety and performance
	pragmas := []string{
		"PRAGMA query_only = ON", // Read-only mode
		"PRAGMA busy_timeout = 5000",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %q: %w", pragma, err)
		}
	}

	return &DB{db: db}, nil
}

// openDBReadWrite creates a new in-memory SQLite database without query-only mode
// This is used internally for setup before enabling query-only mode
func openDBReadWrite() (*DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open in-memory database: %w", err)
	}

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	return &DB{db: db}, nil
}

// Execute runs a SQL query and returns the results
func (d *DB) Execute(query string) (*QueryResult, error) {
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	result := &QueryResult{
		Columns: columns,
		Rows:    make([][]interface{}, 0),
	}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert byte slices to strings for easier handling
		row := make([]interface{}, len(columns))
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = v
			}
		}
		result.Rows = append(result.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return result, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// exec is an internal method for executing statements during setup
func (d *DB) exec(query string, args ...interface{}) error {
	_, err := d.db.Exec(query, args...)
	return err
}

// QueryResult holds the results of a SQL query
type QueryResult struct {
	Columns []string
	Rows    [][]interface{}
}
