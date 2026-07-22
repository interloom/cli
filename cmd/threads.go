package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

const threadMessageFileIDsFlag = "file-ids"

// newThreadsCmd builds the threads command. Threads have no collection list
// endpoint, so there is no `list` verb: `get` fetches a single thread, `events`
// lists its paginated event stream, and `messages create` posts to the thread.
func newThreadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "threads",
		Short: "Inspect threads",
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(newThreadsGetCmd(), newThreadsEventsCmd(), newThreadsMessagesCmd())
	return cmd
}

func newThreadsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandUseGet,
		Short: "Get a single thread by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), "threads", args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newThreadsEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events <id>",
		Short: "List a thread's events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			if cmd.Flags().Changed("limit") {
				limit, _ := cmd.Flags().GetInt("limit")
				q.Set("limit", fmt.Sprint(limit))
			}
			if cur, _ := cmd.Flags().GetString("cursor"); cur != "" {
				q.Set("cursor", cur)
			}
			if dir, _ := cmd.Flags().GetString("direction"); dir != "" {
				q.Set("direction", dir)
			}
			// The events sub-resource shares the cursor pagination shape, so the
			// generic List/ListAll drive it via a composed resource path.
			resource := "threads/" + url.PathEscape(args[0]) + "/events"
			all, _ := cmd.Flags().GetBool(argAll)
			var raw []byte
			if all {
				raw, err = c.ListAll(cmd.Context(), resource, q)
			} else {
				raw, err = c.List(cmd.Context(), resource, q)
			}
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().Int("limit", 0, "maximum number of events to return")
	cmd.Flags().String("cursor", "", "pagination cursor from a previous next_cursor")
	cmd.Flags().String("direction", "", "sort direction: asc or desc")
	cmd.Flags().Bool(argAll, false, "fetch all pages and aggregate into a single list")
	return cmd
}

func newThreadsMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Manage thread messages",
	}
	cmd.AddCommand(newThreadsMessagesCreateCmd())
	return cmd
}

func newThreadsMessagesCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <thread-id>",
		Short: "Create a message in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := threadMessageBody(cmd)
			if err != nil {
				return err
			}
			resource := "threads/" + url.PathEscape(args[0]) + "/messages"
			raw, err := c.Create(cmd.Context(), resource, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().String("text", "", "message text (equivalent to --data '{\"text\":...}')")
	cmd.Flags().StringSlice(threadMessageFileIDsFlag, nil, "File IDs to attach to the created thread event (repeatable)")
	addBodyFlags(cmd)
	return cmd
}

func threadMessageBody(cmd *cobra.Command) ([]byte, error) {
	text, _ := cmd.Flags().GetString("text")
	fileIDs, _ := cmd.Flags().GetStringSlice(threadMessageFileIDsFlag)
	hasText := cmd.Flags().Changed("text")
	hasFileIDs := cmd.Flags().Changed(threadMessageFileIDsFlag)
	if !hasText && !hasFileIDs {
		return readBody(cmd)
	}
	if cmd.Flags().Changed(keyData) || cmd.Flags().Changed("file") {
		return nil, fmt.Errorf("pass either typed flags or a JSON body, not both")
	}
	if !hasText {
		return nil, fmt.Errorf("missing required flag: --text")
	}
	return json.Marshal(struct {
		Text    string   `json:"text"`
		FileIDs []string `json:"file_ids,omitempty"`
	}{Text: text, FileIDs: fileIDs})
}
