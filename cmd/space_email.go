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
	spaceEmailAddresses = "email-addresses"
	commandConfigure    = "configure"
	commandDisable      = "disable"
	keyLocalPart        = "local_part"
)

func newSpaceEmailCmd() *cobra.Command {
	cmd := &cobra.Command{Use: spaceEmailAddresses, Short: "Manage a Space's email addresses"}
	for _, action := range []string{commandNameGet, commandConfigure, commandDisable} {
		use, count := action+" <space-id>", 1
		if action == commandDisable {
			use, count = use+" <local-part>", 2
		}
		child := &cobra.Command{
			Use: use, Short: action + " a Space's email addresses", Args: cobra.ExactArgs(count),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := newClient()
				if err != nil {
					return err
				}
				var body []byte
				if action == commandConfigure {
					body, err = readBody(cmd)
					if err != nil {
						return err
					}
				}
				localPart := ""
				if action == commandDisable {
					localPart = args[1]
				}
				headers := http.Header{}
				if action != commandNameGet && cmd.Flags().Changed(flagIdempotency) {
					key, _ := cmd.Flags().GetString(flagIdempotency)
					headers.Set(headerIdempotency, key)
				}
				raw, err := spaceEmailRequest(cmd.Context(), c, action, args[0], localPart, body, headers)
				if err != nil {
					return err
				}
				return printResult(raw)
			},
		}
		if action == commandConfigure {
			addBodyFlags(child)
		}
		if action != commandNameGet {
			child.Flags().String(flagIdempotency, "", idempotencyHelp)
		}
		cmd.AddCommand(child)
	}
	return cmd
}

func spaceEmailRequest(ctx context.Context, c *client.Client, action, spaceID, localPart string, body []byte, headers http.Header) (json.RawMessage, error) {
	path := resourceSpaces + "/" + url.PathEscape(spaceID) + "/" + spaceEmailAddresses
	switch action {
	case commandConfigure:
		return c.Replace(ctx, path, body, headers)
	case commandDisable:
		return c.Delete(ctx, path, localPart, headers)
	default:
		return c.List(ctx, path, nil)
	}
}

func (s *mcpService) registerSpaceEmailTools(server *mcpsdk.Server) {
	for _, action := range []string{commandNameGet, commandConfigure, commandDisable} {
		props := map[string]any{keySpaceID: stringSchema("space ID")}
		required := []string{keySpaceID}
		if action == commandConfigure {
			props[keyData] = dataSchema("JSON body with required local_part (string or null) and previous_alias_inbound (retain or disable)")
			required = append(required, keyData)
		}
		if action == commandDisable {
			props[keyLocalPart] = stringSchema("alias to disable; its name remains reserved")
			required = append(required, keyLocalPart)
		}
		if action != commandNameGet {
			props[keyIdempotency] = stringSchema(idempotencyHelp)
		}
		server.AddTool(&mcpsdk.Tool{
			Name:        "spaces_email_addresses_" + action,
			Description: action + " a Space's email addresses; configure explicitly retains or disables the previous alias's inbound routing",
			InputSchema: objectSchema(props, required...),
		}, s.spaceEmailHandler(action))
	}
}

func (s *mcpService) spaceEmailHandler(action string) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args, err := parseToolArgs(req)
		if err != nil {
			return toolErrorResult(err), nil
		}
		spaceID, err := args.requiredString(keySpaceID)
		if err != nil {
			return toolErrorResult(err), nil
		}
		var body []byte
		localPart := ""
		switch action {
		case commandConfigure:
			body, err = bodyFromArgs(args, resource{}, false)
		case commandDisable:
			localPart, err = args.requiredString(keyLocalPart)
		}
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
		raw, err := spaceEmailRequest(ctx, s.client, action, spaceID, localPart, body, headers)
		if err != nil {
			return toolErrorResult(err), nil
		}
		return toolJSONResult(raw), nil
	}
}
