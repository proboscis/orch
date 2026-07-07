package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/proboscis/orch/internal/query"
	"github.com/spf13/cobra"
)

type queryOptions struct {
	Format     string
	WithEvents bool
}

func newQueryCmd() *cobra.Command {
	opts := &queryOptions{}

	cmd := &cobra.Command{
		Use:     "query [sql]",
		Aliases: []string{"q"},
		Short:   "Query issues and runs using SQL",
		Long: `Query issues and runs using SQL with an in-memory SQLite database.

Tables:
  issues      - All issues with id, title, status, etc.
  runs        - All runs with issue_id, run_id, status, etc.
  issue_tags  - Junction table for issue tags
  events      - Run events (opt-in with --with-events)

Views:
  issues_v    - Issues with computed columns (run_count, tags)
  runs_v      - Runs with issue info (issue_title, issue_status, event_count)

Examples:
  orch query "SELECT * FROM issues WHERE status = 'open'"
  orch q "SELECT id, status FROM issues"
  orch q "SELECT * FROM runs WHERE status = 'running'" --format json
  orch q "SELECT r.hex_id, i.title FROM runs r JOIN issues i ON i.id = r.issue_id"
  orch q "SELECT status, COUNT(*) FROM runs GROUP BY status"
  orch q "SELECT * FROM issues_v WHERE tags LIKE '%cli%'"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuery(args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "table", "Output format (table, json, tsv)")
	cmd.Flags().BoolVar(&opts.WithEvents, "with-events", false, "Load events table (slower)")

	return cmd
}

func runQuery(sql string, opts *queryOptions) error {
	format, err := query.ParseFormat(opts.Format)
	if err != nil {
		return err
	}

	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	engine, err := query.NewEngine(api, &query.LoadOptions{
		WithEvents: opts.WithEvents,
	})
	if err != nil {
		return fmt.Errorf("failed to create query engine: %w", err)
	}
	defer engine.Close()

	// Execute query
	result, err := engine.Execute(sql)
	if err != nil {
		// Provide helpful error message
		errStr := err.Error()
		if strings.Contains(errStr, "no such table") {
			return fmt.Errorf("%w\n\nUse 'orch schema' to see available tables and views", err)
		}
		if strings.Contains(errStr, "no such column") {
			return fmt.Errorf("%w\n\nUse 'orch schema <table>' to see columns", err)
		}
		return err
	}

	// Format output
	return query.FormatResult(os.Stdout, result, format)
}
