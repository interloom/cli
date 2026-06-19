package cmd

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// newThreadsCmd builds the read-only threads command. Threads have no
// collection list endpoint, so there is no `list` verb: `get` fetches a single
// thread and `events` lists its paginated event stream.
func newThreadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "threads",
		Short: "Inspect threads",
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(newThreadsGetCmd(), newThreadsEventsCmd())
	return cmd
}

func newThreadsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single thread by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), "threads", args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newThreadsEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events <id>",
		Short: "List a thread's events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			if cmd.Flags().Changed("limit") {
				limit, _ := cmd.Flags().GetInt("limit")
				q.Set("limit", fmt.Sprint(limit))
			}
			if cur, _ := cmd.Flags().GetString("cursor"); cur != "" {
				q.Set("cursor", cur)
			}
			if dir, _ := cmd.Flags().GetString("direction"); dir != "" {
				q.Set("direction", dir)
			}
			// The events sub-resource shares the cursor pagination shape, so the
			// generic List/ListAll drive it via a composed resource path.
			resource := "threads/" + url.PathEscape(args[0]) + "/events"
			all, _ := cmd.Flags().GetBool("all")
			var raw []byte
			if all {
				raw, err = c.ListAll(cmd.Context(), resource, q)
			} else {
				raw, err = c.List(cmd.Context(), resource, q)
			}
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().Int("limit", 0, "maximum number of events to return")
	cmd.Flags().String("cursor", "", "pagination cursor from a previous next_cursor")
	cmd.Flags().String("direction", "", "sort direction: asc or desc")
	cmd.Flags().Bool("all", false, "fetch all pages and aggregate into a single list")
	return cmd
}
