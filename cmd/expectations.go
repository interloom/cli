package cmd

import (
	"context"
	"net/url"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const (
	commandExpectations     = "expectations"
	resourceStages          = "stages"
	resourceProcedureStages = "procedure-stages"
	resourceProcedures      = "procedures"
	expectationsHelp        = "Read the directly attached root suite and ordered checks; does not evaluate or combine stage expectations. Returns 404 when no accessible suite exists."
)

func newExpectationsCmd(name, singular string) *cobra.Command {
	return &cobra.Command{
		Use:   commandExpectations + " <" + singular + "-id>",
		Short: "Get " + singular + " expectations",
		Long:  expectationsHelp,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.List(cmd.Context(), name+"/"+url.PathEscape(args[0])+"/"+commandExpectations, nil)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newStageExpectationsCmd(name, singular string) *cobra.Command {
	cmd := &cobra.Command{Use: name, Short: "Read " + singular + " expectations"}
	addConfigNameFlag(cmd)
	cmd.AddCommand(newExpectationsCmd(name, singular))
	return cmd
}

func (s *mcpService) registerExpectationsTools(server *mcpsdk.Server) {
	for _, name := range []string{resourceCases, resourceProcedures, resourceStages, resourceProcedureStages} {
		server.AddTool(&mcpsdk.Tool{
			Name:        strings.ReplaceAll(name, "-", "_") + "_expectations",
			Description: expectationsHelp,
			InputSchema: objectSchema(map[string]any{"id": stringSchema("target resource ID")}, "id"),
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			args, err := parseToolArgs(req)
			if err != nil {
				return toolErrorResult(err), nil
			}
			id, err := args.requiredString("id")
			if err != nil {
				return toolErrorResult(err), nil
			}
			raw, err := s.client.List(ctx, name+"/"+url.PathEscape(id)+"/"+commandExpectations, nil)
			if err != nil {
				return toolErrorResult(err), nil
			}
			return toolJSONResult(raw), nil
		})
	}
}
