package cmd

import (
	"net/url"

	"github.com/spf13/cobra"
)

const resourceInvocations = "invocations"

func newInvocationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceInvocations,
		Short: "Inspect agent executions discovered through thread events",
	}
	addConfigNameFlag(cmd)
	r := resource{name: resourceInvocations, singular: "invocation"}
	cmd.AddCommand(r.getCmd(), newInvocationsStepsCmd())
	return cmd
}

func newInvocationsStepsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "steps <id>",
		Short: "List recorded steps for an invocation",
		Long: "List recorded steps in creation-time and ID order. Raw tool inputs and\n" +
			"outputs may contain sensitive data. Thinking and reasoning content is withheld.\n" +
			"Final responses remain in thread messages; steps are not individual LLM turns.\n" +
			"Token counts can arrive late or be null. Cache counts are included in input tokens.\n" +
			"Step counts are allocated shares and may not sum to invocation totals. Costs are not exposed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			paging := resource{}
			q := paging.listQuery(cmd)
			path := resourceInvocations + "/" + url.PathEscape(args[0]) + "/steps"
			all, _ := cmd.Flags().GetBool(argAll)
			var raw []byte
			if all {
				raw, err = c.ListAll(cmd.Context(), path, q)
			} else {
				raw, err = c.List(cmd.Context(), path, q)
			}
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().Int(argLimit, 0, "maximum number of steps to return")
	cmd.Flags().String(keyCursor, "", "pagination cursor from a previous next_cursor")
	cmd.Flags().Bool(argAll, false, "fetch all pages and aggregate into a single list")
	return cmd
}
