package cmd

import "github.com/spf13/cobra"

// newUsersCmd builds the read-only users command (list + get) plus the
// `me` convenience for the authenticated user.
func newUsersCmd() *cobra.Command {
	cmd := newResourceCmd(apiResource("users"))
	cmd.AddCommand(&cobra.Command{
		Use:   "me",
		Short: "Show the authenticated user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), "users", "me")
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	})
	return cmd
}
