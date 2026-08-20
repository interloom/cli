package cmd

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// newRelationshipsCmd lists the resources connected to one resource item.
func newRelationshipsCmd(r resource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandNameRelationships + " <id>",
		Short: fmt.Sprintf("List a %s's relationships", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			paging := resource{}
			query := paging.listQuery(cmd)
			resourcePath := r.name + "/" + url.PathEscape(args[0]) + "/" + commandNameRelationships
			all, _ := cmd.Flags().GetBool(argAll)
			var raw []byte
			if all {
				raw, err = c.ListAll(cmd.Context(), resourcePath, query)
			} else {
				raw, err = c.List(cmd.Context(), resourcePath, query)
			}
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().Int(argLimit, 0, "maximum number of relationships to return")
	cmd.Flags().String(keyCursor, "", "pagination cursor from a previous next_cursor")
	cmd.Flags().Bool(argAll, false, "fetch all pages and aggregate into a single list")
	return cmd
}
