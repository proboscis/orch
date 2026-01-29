package query

import (
	"github.com/s22625/orch/internal/orchapi"
)

// Engine provides the main query interface
type Engine struct {
	db *DB
}

// NewEngine creates a new query engine with data loaded via API
func NewEngine(api orchapi.OrchAPI, opts *LoadOptions) (*Engine, error) {
	// Create database in read-write mode for setup
	db, err := openDBReadWrite()
	if err != nil {
		return nil, err
	}

	// Create schema and views
	if err := CreateSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := CreateViews(db); err != nil {
		db.Close()
		return nil, err
	}

	// Load data
	if err := LoadAll(db, api, opts); err != nil {
		db.Close()
		return nil, err
	}

	// Enable query-only mode now that setup is complete
	if err := db.exec("PRAGMA query_only = ON"); err != nil {
		db.Close()
		return nil, err
	}

	return &Engine{db: db}, nil
}

// Execute runs a SQL query and returns the results
func (e *Engine) Execute(query string) (*QueryResult, error) {
	return e.db.Execute(query)
}

// GetSchema returns schema information for all tables and views
func (e *Engine) GetSchema() ([]SchemaInfo, error) {
	return GetSchemaInfo(e.db)
}

// GetTableSchema returns schema information for a specific table or view
func (e *Engine) GetTableSchema(name string) (*SchemaInfo, error) {
	return GetTableSchema(e.db, name)
}

// Close closes the query engine
func (e *Engine) Close() error {
	return e.db.Close()
}
