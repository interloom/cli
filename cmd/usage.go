package cmd

import (
	"fmt"
	"net/url"

	"github.com/interloom/cli/internal/api"
	"github.com/spf13/cobra"
)

func newCasesCmd() *cobra.Command {
	cmd := newResourceCmd(apiResource(resourceCases))
	cmd.AddCommand(newUsageCmd(resourceCases))
	return cmd
}

func newUsageCmd(resourceName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage <id>",
		Short: "Get all-time usage totals",
		Long: "Get all-time usage totals. Metrics can arrive late, and costs in EUR can be incomplete or null.\n" +
			"Case totals include descendants. Space usage requires owner or manager access.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			path := resourceName + "/" + url.PathEscape(args[0]) + "/usage"
			raw, err := c.List(cmd.Context(), path, nil)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	if resourceName == resourceSpaces {
		cmd.AddCommand(newUsageBreakdownsCmd())
	}
	return cmd
}

func newUsageBreakdownsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "breakdowns <space-id>",
		Short: "Get all-time Space usage grouped by case, model, or agent",
		Long: "Get all-time Space usage groups in group-key order. Requires owner or manager access.\n" +
			"Case groups can have different totals from Space usage. Omit --limit for all groups.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			group, _ := cmd.Flags().GetString("group-by")
			if !api.ListSpaceUsageBreakdownsParamsGroupBy(group).Valid() {
				return fmt.Errorf("--group-by must be case, model, or agent")
			}
			q := url.Values{"group_by": {group}}
			if cmd.Flags().Changed(argLimit) {
				limit, _ := cmd.Flags().GetInt(argLimit)
				if limit < 1 {
					return fmt.Errorf("--limit must be at least 1")
				}
				q.Set(argLimit, fmt.Sprint(limit))
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			path := resourceSpaces + "/" + url.PathEscape(args[0]) + "/usage/breakdowns"
			raw, err := c.List(cmd.Context(), path, q)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().String("group-by", "", "group by case, model, or agent (required)")
	cmd.Flags().Int(argLimit, 0, "maximum groups to return (at least 1); omit for all groups")
	return cmd
}
