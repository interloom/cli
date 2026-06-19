package cmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// filter is a query parameter exposed as a list flag. When multi is set it is a
// repeatable string-slice flag sent as repeated query params.
type filter struct {
	name  string
	usage string
	multi bool
}

func (f filter) flagName() string {
	return strings.ReplaceAll(f.name, "_", "-")
}

// Common list filters reused across resources.
var (
	filterSpaceID   = filter{name: "space_id", usage: "filter by Space ID"}
	filterCaseID    = filter{name: "case_id", usage: "filter by Case ID"}
	filterSort      = filter{name: "sort", usage: "sort field: created_at or updated_at"}
	filterDirection = filter{name: "direction", usage: "sort direction: asc or desc"}
)

// resource describes a uniform REST resource. The same five verbs
// (list/get/create/update/delete) are generated for every resource; this is
// the single place to extend when wiring spaces, notes, procedures, etc.
type resource struct {
	name     string   // URL segment and command name, e.g. "cases"
	singular string   // e.g. "case", used in help text
	readOnly bool     // only list + get (e.g. users)
	noCreate bool     // no generic create (e.g. files, which uses upload)
	noDelete bool     // no DELETE endpoint (e.g. agents)
	filters  []filter // list query filters
}

func newResourceCmd(r resource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   r.name,
		Short: fmt.Sprintf("Manage %s", r.name),
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(r.listCmd(), r.getCmd())
	if r.readOnly {
		return cmd
	}
	if !r.noCreate {
		cmd.AddCommand(r.createCmd())
	}
	cmd.AddCommand(r.updateCmd())
	if !r.noDelete {
		cmd.AddCommand(r.deleteCmd())
	}
	return cmd
}

// listQuery builds the query string for a list call from the paging flags and
// the resource's filters (single-value or repeatable).
func (r resource) listQuery(cmd *cobra.Command) url.Values {
	q := url.Values{}
	if cmd.Flags().Changed("limit") {
		limit, _ := cmd.Flags().GetInt("limit")
		q.Set("limit", fmt.Sprint(limit))
	}
	if cur, _ := cmd.Flags().GetString("cursor"); cur != "" {
		q.Set("cursor", cur)
	}
	for _, f := range r.filters {
		flagName := f.flagName()
		if f.multi {
			vals, _ := cmd.Flags().GetStringSlice(flagName)
			for _, v := range vals {
				if v != "" {
					q.Add(f.name, v)
				}
			}
			continue
		}
		if v, _ := cmd.Flags().GetString(flagName); v != "" {
			q.Set(f.name, v)
		}
	}
	return q
}

func (r resource) listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List %s", r.name),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			q := r.listQuery(cmd)
			all, _ := cmd.Flags().GetBool("all")
			var raw []byte
			if all {
				raw, err = c.ListAll(cmd.Context(), r.name, q)
			} else {
				raw, err = c.List(cmd.Context(), r.name, q)
			}
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().Int("limit", 0, "maximum number of items to return")
	cmd.Flags().String("cursor", "", "pagination cursor from a previous next_cursor")
	cmd.Flags().Bool("all", false, "fetch all pages and aggregate into a single list")
	for _, f := range r.filters {
		flagName := f.flagName()
		if f.multi {
			cmd.Flags().StringSlice(flagName, nil, f.usage)
		} else {
			cmd.Flags().String(flagName, "", f.usage)
		}
	}
	return cmd
}

func (r resource) getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: fmt.Sprintf("Get a single %s by ID", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), r.name, args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func (r resource) createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: fmt.Sprintf("Create a %s from a JSON body", r.singular),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := readBody(cmd)
			if err != nil {
				return err
			}
			raw, err := c.Create(cmd.Context(), r.name, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	addBodyFlags(cmd)
	return cmd
}

func (r resource) updateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: fmt.Sprintf("Update a %s from a JSON body", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := readBody(cmd)
			if err != nil {
				return err
			}
			raw, err := c.Update(cmd.Context(), r.name, args[0], body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	addBodyFlags(cmd)
	return cmd
}

func (r resource) deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: fmt.Sprintf("Delete a %s by ID", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Delete(cmd.Context(), r.name, args[0])
			if err != nil {
				return err
			}
			if len(raw) == 0 {
				raw = []byte(fmt.Sprintf(`{"id":%q,"deleted":true}`, args[0]))
			}
			return printResult(raw)
		},
	}
}

// addBodyFlags registers the JSON-body input flags shared by create/update.
func addBodyFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("file", "f", "", "path to a JSON body file, or - for stdin")
	cmd.Flags().StringP("data", "d", "", "inline JSON body")
}

// readBody resolves the request body: --data > --file > piped stdin.
func readBody(cmd *cobra.Command) ([]byte, error) {
	if data, _ := cmd.Flags().GetString("data"); data != "" {
		return []byte(data), nil
	}
	file, _ := cmd.Flags().GetString("file")
	if file == "-" || (file == "" && !stdinIsTTY()) {
		return io.ReadAll(os.Stdin)
	}
	if file != "" {
		return os.ReadFile(file)
	}
	return nil, fmt.Errorf("no request body: pass --data, --file, or pipe JSON via stdin")
}
