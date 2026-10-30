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
	setExpectationsHelp     = "Create or replace the complete definition for a STAGE or PROCEDURE_STAGE target. Requires edit permission. Omitted mode and is_gating reset to all and false. Identical submissions leave the suite unchanged; changed check lists receive new IDs. Existing suite titles and descriptions are preserved."
)

func newExpectationsWriteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: commandExpectations, Short: "Set stage expectations"}
	addConfigNameFlag(cmd)
	r := resource{name: commandExpectations, singular: "expectation suite"}
	set := r.createCmd()
	set.Use = "set"
	set.Short = "Create or replace a stage's complete expectation definition"
	set.Long = setExpectationsHelp
	cmd.AddCommand(set)
	return cmd
}

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
	server.AddTool(&mcpsdk.Tool{
		Name:        "expectations_set",
		Description: setExpectationsHelp,
		InputSchema: objectSchema(map[string]any{keyData: dataSchema("complete definition: target, checks, optional mode and is_gating")}, keyData),
	}, s.createResourceHandler(resource{name: commandExpectations}))
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
