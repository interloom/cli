package cmd

import (
	"net/url"

	"github.com/spf13/cobra"
)

const (
	resourceDatabases = "databases"
	keyDatabaseID     = "database_id"
)

// newDatabasesCmd builds the databases command. Databases have no collection
// endpoint: get describes one database, query reads rows, and aggregate
// calculates scalar values. Upsert adds or replaces rows.
func newDatabasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceDatabases,
		Short: "Inspect, query, aggregate, and write databases",
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(newDatabasesGetCmd(), newDatabasesQueryCmd(), newDatabasesAggregateCmd(), newDatabasesUpsertCmd())
	return cmd
}

func newDatabasesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandUseGet,
		Short: "Describe a database by ID without loading rows",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), resourceDatabases, args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newDatabasesQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <id>",
		Short: "Query a bounded page of database rows",
		Long: "Query a bounded page of database rows. The JSON body must contain\n" +
			"selected_columns and page_size, and may contain filters, sort, and cursor.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := readBody(cmd)
			if err != nil {
				return err
			}
			resource := resourceDatabases + "/" + url.PathEscape(args[0]) + "/query"
			raw, err := c.Create(cmd.Context(), resource, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	addBodyFlags(cmd)
	return cmd
}

func newDatabasesUpsertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upsert <id>",
		Short: "Add or fully replace database rows",
		Long: "Add or fully replace database rows. The JSON body must contain\n" +
			"expected_revision and rows (up to 1,000 complete rows with their keys).\n" +
			"Fetch the schema and revision first. Omitted optional values become null.\n" +
			"For an exact retry, preserve the original expected_revision and rows.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := readBody(cmd)
			if err != nil {
				return err
			}
			resource := resourceDatabases + "/" + url.PathEscape(args[0]) + "/upsert"
			raw, err := c.Create(cmd.Context(), resource, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	addBodyFlags(cmd)
	return cmd
}

func newDatabasesAggregateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate <id>",
		Short: "Calculate aggregate values for database rows",
		Long: "Calculate aggregate values for database rows. The JSON body must contain\n" +
			"expressions and may contain up to five equality filters.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := readBody(cmd)
			if err != nil {
				return err
			}
			resource := resourceDatabases + "/" + url.PathEscape(args[0]) + "/aggregate"
			raw, err := c.Create(cmd.Context(), resource, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	addBodyFlags(cmd)
	return cmd
}
