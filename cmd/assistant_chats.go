package cmd

import "github.com/spf13/cobra"

const resourceAssistantChats = "assistant-chats"

func newAssistantChatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceAssistantChats,
		Short: "List or create chats with your personal Odin assistant",
	}
	addConfigNameFlag(cmd)
	r := resource{name: resourceAssistantChats, singular: "assistant chat"}
	cmd.AddCommand(r.listCmd(), &cobra.Command{
		Use:   "create",
		Short: "Create an empty chat with your personal Odin assistant",
		Long: "Create an empty chat without a request body. Use the returned thread ID\n" +
			"with threads events and threads messages create. Assistant replies arrive asynchronously.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Create(cmd.Context(), resourceAssistantChats, nil)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	})
	return cmd
}
