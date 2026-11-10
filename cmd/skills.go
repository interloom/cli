package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const (
	resourceSkills      = "skills"
	keySkillID          = "skill_id"
	commandEquip        = "equip"
	commandRemove       = "remove"
	skillAssignmentHelp = "Equip requires skill management and agent configuration permission. Remove requires agent configuration permission; absent assignments and unknown skill IDs are successful no-ops. Removing an assignment does not delete the skill."
)

func newAgentSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: resourceSkills, Short: "Manage an agent's skill assignments"}
	for _, action := range []string{commandEquip, commandRemove} {
		child := &cobra.Command{
			Use:   action + " <agent-id> <skill-id>",
			Short: action + " an agent skill assignment",
			Long:  skillAssignmentHelp,
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := newClient()
				if err != nil {
					return err
				}
				headers := http.Header{}
				if cmd.Flags().Changed(flagIdempotency) {
					key, _ := cmd.Flags().GetString(flagIdempotency)
					headers.Set(headerIdempotency, key)
				}
				raw, err := setAgentSkill(cmd.Context(), c, args[0], args[1], action == commandEquip, headers)
				if err != nil {
					return err
				}
				return printResult(raw)
			},
		}
		child.Flags().String(flagIdempotency, "", idempotencyHelp)
		cmd.AddCommand(child)
	}
	return cmd
}

func setAgentSkill(ctx context.Context, c *client.Client, agentID, skillID string, equip bool, headers http.Header) (json.RawMessage, error) {
	path := "agents/" + url.PathEscape(agentID) + "/skills"
	var err error
	if equip {
		_, err = c.Replace(ctx, path+"/"+url.PathEscape(skillID), nil, headers)
	} else {
		_, err = c.Delete(ctx, path, skillID, headers)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{keyAgentID: agentID, keySkillID: skillID, "equipped": equip})
}

func (s *mcpService) registerAgentSkillTools(server *mcpsdk.Server) {
	for _, action := range []string{commandEquip, commandRemove} {
		server.AddTool(&mcpsdk.Tool{
			Name:        "agents_skills_" + action,
			Description: skillAssignmentHelp,
			InputSchema: objectSchema(map[string]any{
				keyAgentID: stringSchema("agent ID"), keySkillID: stringSchema("skill ID"), keyIdempotency: stringSchema(idempotencyHelp),
			}, keyAgentID, keySkillID),
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			args, err := parseToolArgs(req)
			if err != nil {
				return toolErrorResult(err), nil
			}
			agentID, err := args.requiredString(keyAgentID)
			if err != nil {
				return toolErrorResult(err), nil
			}
			skillID, err := args.requiredString(keySkillID)
			if err != nil {
				return toolErrorResult(err), nil
			}
			key, present, err := args.string(keyIdempotency)
			if err != nil {
				return toolErrorResult(err), nil
			}
			headers := http.Header{}
			if present {
				headers.Set(headerIdempotency, key)
			}
			raw, err := setAgentSkill(ctx, s.client, agentID, skillID, action == commandEquip, headers)
			if err != nil {
				return toolErrorResult(err), nil
			}
			return toolJSONResult(raw), nil
		})
	}
}
