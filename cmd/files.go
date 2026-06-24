package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newFilesCmd builds the files command: the generic list/get/update/delete
// verbs plus file-specific upload (multipart) and download (signed URL).
func newFilesCmd() *cobra.Command {
	cmd := newResourceCmd(resource{
		name:     "files",
		singular: "file",
		noCreate: true, // creation happens via `upload`
		filters:  []filter{filterSpaceID, filterCaseID, filterSort, filterDirection},
		fields:   []field{fieldSpaceID, fieldCaseID, fieldTags},
	})
	cmd.AddCommand(newFilesUploadCmd(), newFilesDownloadCmd())
	return cmd
}

func newFilesUploadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload <path>",
		Short: "Upload a file (optionally attached to a Space or Case)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			spaceID, _ := cmd.Flags().GetString("space-id")
			caseID, _ := cmd.Flags().GetString("case-id")
			raw, err := c.Upload(cmd.Context(), "files", args[0], map[string]string{
				keySpaceID: spaceID,
				keyCaseID:  caseID,
			})
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().String("space-id", "", "attach the file to a Space")
	cmd.Flags().String("case-id", "", "attach the file to a Case")
	return cmd
}

func newFilesDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a file's bytes (to stdout, or --out)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), "files", args[0])
			if err != nil {
				return err
			}
			var f struct {
				DownloadURL string `json:"download_url"`
			}
			if err = json.Unmarshal(raw, &f); err != nil {
				return err
			}
			if f.DownloadURL == "" {
				return fmt.Errorf("file %s has no download_url", args[0])
			}

			out, _ := cmd.Flags().GetString("out")
			if out == "" || out == "-" {
				return c.FetchTo(cmd.Context(), f.DownloadURL, os.Stdout)
			}
			dst, err := os.Create(out)
			if err != nil {
				return err
			}
			defer func() { _ = dst.Close() }()
			if err := c.FetchTo(cmd.Context(), f.DownloadURL, dst); err != nil {
				return err
			}
			return printResult([]byte(fmt.Sprintf(`{"id":%q,"path":%q,"status":"downloaded"}`, args[0], out)))
		},
	}
	cmd.Flags().String("out", "", "write to this path instead of stdout")
	return cmd
}
