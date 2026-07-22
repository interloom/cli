package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

const agentToolIDsFlag = "tool-ids"

// newAgentsCmd adds the tools sub-resource to the standard Agent commands.
func newAgentsCmd() *cobra.Command {
	cmd := newResourceCmd(apiResource("agents"))
	cmd.AddCommand(newAgentToolsCmd())
	return cmd
}

func newAgentToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Manage an Agent's assigned tools",
	}
	cmd.AddCommand(newAgentToolsListCmd(), newAgentToolsReplaceCmd())
	return cmd
}

func newAgentToolsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <agent-id>",
		Short: "List an Agent's assigned tools",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			resource := "agents/" + url.PathEscape(args[0]) + "/tools"
			raw, err := c.List(cmd.Context(), resource, nil)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newAgentToolsReplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replace <agent-id>",
		Short: "Replace an Agent's assigned tools",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := agentToolsBody(cmd)
			if err != nil {
				return err
			}
			resource := "agents/" + url.PathEscape(args[0]) + "/tools"
			raw, err := c.Replace(cmd.Context(), resource, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().StringSlice(agentToolIDsFlag, nil, "Tool IDs to assign (repeatable)")
	addBodyFlags(cmd)
	return cmd
}

func agentToolsBody(cmd *cobra.Command) ([]byte, error) {
	if !cmd.Flags().Changed(agentToolIDsFlag) {
		return readBody(cmd)
	}
	if cmd.Flags().Changed(keyData) || cmd.Flags().Changed("file") {
		return nil, fmt.Errorf("pass either --%s or a JSON body, not both", agentToolIDsFlag)
	}
	toolIDs, _ := cmd.Flags().GetStringSlice(agentToolIDsFlag)
	return json.Marshal(struct {
		ToolIDs []string `json:"tool_ids"`
	}{ToolIDs: toolIDs})
}
