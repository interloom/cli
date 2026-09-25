package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

const (
	commandNameAdd     = "add"
	commandNameMembers = "members"
	commandNameRemove  = "remove"
	keyRole            = "role"
)

// newSpacesCmd adds the non-uniform sub-resources to the standard Space
// resource commands.
func newSpacesCmd() *cobra.Command {
	cmd := newResourceCmd(apiResource(resourceSpaces))
	cmd.AddCommand(newSpacesMembersCmd(), newSpacesTriggerCmd(), newUsageCmd(resourceSpaces))
	return cmd
}

func newSpacesMembersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandNameMembers,
		Short: "Manage a Space's members",
	}
	cmd.AddCommand(newSpacesMembersAddCmd(), newSpacesMembersListCmd(), newSpacesMembersRemoveCmd())
	return cmd
}

func newSpacesMembersListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandUseList + " <space-id>",
		Short: "List a Space's members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			paging := resource{}
			query := paging.listQuery(cmd)
			resourcePath := resourceSpaces + "/" + url.PathEscape(args[0]) + "/" + commandNameMembers
			all, _ := cmd.Flags().GetBool(argAll)
			var raw json.RawMessage
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
	cmd.Flags().Int("limit", 0, "maximum number of members to return")
	cmd.Flags().String(keyCursor, "", "pagination cursor from a previous next_cursor")
	cmd.Flags().Bool(argAll, false, "fetch all pages and aggregate into a single list")
	return cmd
}

func newSpacesMembersAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandNameAdd + " <space-id> <user-id>",
		Short: "Add a Space member or update their role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := spaceMemberBody(cmd)
			if err != nil {
				return err
			}
			resourcePath := resourceSpaces + "/" + url.PathEscape(args[0]) + "/" + commandNameMembers + "/" + url.PathEscape(args[1])
			raw, err := c.Replace(cmd.Context(), resourcePath, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().String(keyRole, "", "member role: member, manager, or viewer")
	addBodyFlags(cmd)
	return cmd
}

func spaceMemberBody(cmd *cobra.Command) ([]byte, error) {
	if !cmd.Flags().Changed(keyRole) {
		return readBody(cmd)
	}
	if cmd.Flags().Changed(keyData) || cmd.Flags().Changed("file") {
		return nil, fmt.Errorf("pass either --%s or a JSON body, not both", keyRole)
	}
	role, _ := cmd.Flags().GetString(keyRole)
	return json.Marshal(struct {
		Role string `json:"role"`
	}{Role: role})
}

func newSpacesMembersRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandNameRemove + " <space-id> <user-id>",
		Short: "Remove a member from a Space",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			resourcePath := resourceSpaces + "/" + url.PathEscape(args[0]) + "/" + commandNameMembers
			raw, err := c.Delete(cmd.Context(), resourcePath, args[1])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
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
