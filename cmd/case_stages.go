package cmd

import (
	"context"
	"net/url"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newCaseStagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stages <case-id>",
		Short: "List all stages of a case in order, without pagination",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.List(cmd.Context(), resourceCases+"/"+url.PathEscape(args[0])+"/stages", nil)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func (s *mcpService) registerCaseStageTools(server *mcpsdk.Server) {
	server.AddTool(&mcpsdk.Tool{
		Name:        "cases_stages",
		Description: "List all stages of a case in order, without pagination",
		InputSchema: objectSchema(map[string]any{keyCaseID: stringSchema("case ID")}, keyCaseID),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		id, err := args.requiredString(keyCaseID)
		if err != nil {
			return toolErrorResult(err), nil
		}
		raw, err := s.client.List(ctx, resourceCases+"/"+url.PathEscape(id)+"/stages", nil)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	})
}
