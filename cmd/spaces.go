package cmd

import (
	"net/url"

	"github.com/spf13/cobra"
)

// newSpacesCmd adds the non-uniform trigger sub-resource to the standard Space
// resource commands.
func newSpacesCmd() *cobra.Command {
	cmd := newResourceCmd(apiResource(resourceSpaces))
	cmd.AddCommand(newSpacesTriggerCmd())
	return cmd
}

func newSpacesTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandNameTrigger,
		Short: "Manage a Space's triage trigger",
	}
	cmd.AddCommand(newSpacesTriggerGetCmd(), newSpacesTriggerUpdateCmd())
	return cmd
}

func newSpacesTriggerGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandNameGet + " <space-id>",
		Short: "Get a Space's triage trigger",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			resource := resourceSpaces + "/" + url.PathEscape(args[0]) + "/" + commandNameTrigger
			raw, err := c.List(cmd.Context(), resource, nil)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newSpacesTriggerUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandNameUpdate + " <space-id>",
		Short: "Update a Space's triage trigger from a JSON body",
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
			resource := resourceSpaces + "/" + url.PathEscape(args[0])
			raw, err := c.Update(cmd.Context(), resource, commandNameTrigger, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	addBodyFlags(cmd)
	return cmd
}
