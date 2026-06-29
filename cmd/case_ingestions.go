package cmd

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newCaseIngestionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceCaseIngestions,
		Short: "Import cases from JSONL manifests",
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(newCaseIngestionsCreateCmd(), newCaseIngestionsGetCmd(), newCaseIngestionsErrorsCmd())
	return cmd
}

func newCaseIngestionsCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <manifest-path>",
		Short: "Create a case ingestion from a JSONL manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			spaceID, _ := cmd.Flags().GetString("space-id")
			if spaceID == "" {
				return fmt.Errorf("missing required flag: --space-id")
			}
			raw, err := c.UploadFile(cmd.Context(), resourceCaseIngestions, keyManifest, args[0], map[string]string{
				keySpaceID: spaceID,
			})
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().String("space-id", "", "target Space ID for imported root cases (required)")
	return cmd
}

func newCaseIngestionsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandUseGet,
		Short: "Get a case ingestion by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), resourceCaseIngestions, args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func newCaseIngestionsErrorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "errors <id>",
		Short: "List failed entries for a case ingestion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			if cmd.Flags().Changed(argLimit) {
				limit, _ := cmd.Flags().GetInt(argLimit)
				q.Set(argLimit, fmt.Sprint(limit))
			}
			if cur, _ := cmd.Flags().GetString(keyCursor); cur != "" {
				q.Set(keyCursor, cur)
			}
			resource := resourceCaseIngestions + "/" + url.PathEscape(args[0]) + "/errors"
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
	cmd.Flags().Int(argLimit, 0, "maximum number of failed entries to return")
	cmd.Flags().String(keyCursor, "", "pagination cursor from a previous next_cursor")
	cmd.Flags().Bool(argAll, false, "fetch all pages and aggregate into a single list")
	return cmd
}
