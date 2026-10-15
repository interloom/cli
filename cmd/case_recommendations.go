package cmd

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newCaseRecommendationsCmd(name string, maximum, defaultLimit int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name + " <case-id>",
		Short: "Get ranked " + name + " for a case",
		Long: "Get ranked results without pagination. Scores are comparable only within this response,\n" +
			"not confidence estimates. Both commands use the containing root case.\n" +
			"Similar cases combine semantic and shared-object matches.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if cmd.Flags().Changed(argLimit) {
				limit, _ := cmd.Flags().GetInt(argLimit)
				if limit < 1 || limit > maximum {
					return fmt.Errorf("--limit must be between 1 and %d", maximum)
				}
				q.Set(argLimit, fmt.Sprint(limit))
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.List(cmd.Context(), resourceCases+"/"+url.PathEscape(args[0])+"/"+name, q)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().Int(argLimit, defaultLimit, fmt.Sprintf("maximum ranked results (1–%d); does not change scores", maximum))
	return cmd
}
