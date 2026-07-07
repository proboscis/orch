package cli

import (
	"os"

	"github.com/proboscis/orch/internal/query"
	"github.com/spf13/cobra"
)

type schemaOptions struct {
	Format string
}

func newSchemaCmd() *cobra.Command {
	opts := &schemaOptions{}

	cmd := &cobra.Command{
		Use:   "schema [table]",
		Short: "Show database schema for SQL queries",
		Long: `Show the database schema used by 'orch query'.

Without arguments, lists all tables and views.
With a table name, shows columns for that table or view.

Examples:
  orch schema           # List all tables and views
  orch schema runs      # Show columns in runs table
  orch schema issues_v  # Show columns in issues_v view
  orch schema --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tableName := ""
			if len(args) > 0 {
				tableName = args[0]
			}
			return runSchema(tableName, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "table", "Output format (table, json, tsv)")

	return cmd
}

func runSchema(tableName string, opts *schemaOptions) error {
	format, err := query.ParseFormat(opts.Format)
	if err != nil {
		return err
	}

	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	engine, err := query.NewEngine(api, &query.LoadOptions{
		WithEvents: true,
	})
	if err != nil {
		return err
	}
	defer engine.Close()

	if tableName == "" {
		// List all tables and views
		schemas, err := engine.GetSchema()
		if err != nil {
			return err
		}
		return query.FormatSchemaList(os.Stdout, schemas, format)
	}

	// Show specific table
	info, err := engine.GetTableSchema(tableName)
	if err != nil {
		return err
	}
	return query.FormatTableDetail(os.Stdout, info, format)
}
