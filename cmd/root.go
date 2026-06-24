// Package cmd implements the interloom CLI command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
	"github.com/interloom/cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagConfigName string
	flagBaseURL    string
	flagOutput     string
)

// addConfigNameFlag registers the --config-name/-c flag on commands that resolve
// API credentials. It is intentionally absent from `config` and `version`, where
// it would have no effect. A separate --config-file flag may be added later.
func addConfigNameFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&flagConfigName, "config-name", "c", "",
		"name of the config to use (defaults to the current config)")
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "interloom",
		Short: "Interloom CLI — manage spaces, cases, notes, files and more",
		Long: "interloom is an agent-first command line interface for the Interloom REST API.\n" +
			"Output is JSON by default; errors are a JSON envelope on stderr with stable exit codes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flagBaseURL, "base-url", "", "override the API base URL")
	pf.StringVarP(&flagOutput, "output", "o", "json", "output format: json")

	root.AddCommand(
		newAuthCmd(),
		newConfigCmd(),
		newResourceCmd(resource{name: "spaces", singular: "space", fields: []field{
			{name: "name", usage: "Space name", onCreate: true, onUpdate: true, required: true},
			{name: keyDescription, usage: "Space description", onCreate: true, onUpdate: true},
		}}),
		newResourceCmd(resource{name: "cases", singular: "case", filters: []filter{
			filterSpaceID,
			{name: keyParentCaseID, usage: "filter by parent Case ID"},
			{name: keyAssigneeID, usage: "filter by assignee User ID"},
			{name: keyStatus, usage: "filter by status (repeatable): open, started, completed, cancelled, blocked", multi: true},
			{name: "sort", usage: "sort field: position, created_at, or updated_at"},
			filterDirection,
		}, fields: []field{
			{name: keyTitle, usage: "Case title", onCreate: true, onUpdate: true, required: true},
			{name: keyDescription, usage: "Case description", onCreate: true, onUpdate: true},
			{name: keySpaceID, usage: "owning Space ID (exactly one of space-id or parent-case-id)", onCreate: true, onUpdate: true},
			{name: keyParentCaseID, usage: "parent Case ID (exactly one of space-id or parent-case-id)", onCreate: true, onUpdate: true},
			{name: keyAssigneeID, usage: "assignee User ID", onCreate: true, onUpdate: true},
			{name: "due_at", usage: "due date (RFC3339 timestamp)", onCreate: true, onUpdate: true},
			{name: "external_id", usage: "external identifier", onCreate: true},
			{name: keyStatus, usage: "status: open, started, completed, cancelled, blocked", onUpdate: true},
			fieldTags,
			{name: "attached_file_ids", usage: "attached File IDs (repeatable)", multi: true, onCreate: true},
		}}),
		newResourceCmd(resource{name: "notes", singular: "note", filters: []filter{
			filterSpaceID,
			filterCaseID,
			{name: "thread_id", usage: "filter by thread ID"},
			filterSort,
			filterDirection,
		}, fields: []field{
			{name: keyTitle, usage: "Note title", onCreate: true, onUpdate: true, required: true},
			{name: "body", usage: "Note body", onCreate: true, onUpdate: true},
			{name: keySpaceID, usage: "owning Space ID (exactly one of space-id or case-id)", onCreate: true, onUpdate: true},
			{name: keyCaseID, usage: "owning Case ID (exactly one of space-id or case-id)", onCreate: true, onUpdate: true},
			fieldTags,
		}}),
		newResourceCmd(resource{name: "procedures", singular: "procedure", filters: []filter{
			filterSpaceID,
		}, fields: []field{
			{name: keyTitle, usage: "Procedure name", onCreate: true, onUpdate: true, required: true},
			{name: keySpaceID, usage: "owning Space ID", onCreate: true, required: true},
		}}),
		newResourceCmd(resource{name: "agents", singular: "agent", noDelete: true, fields: []field{
			{name: "name", usage: "Agent name", onCreate: true, onUpdate: true, required: true},
			{name: "job_description", usage: "Agent job description", onCreate: true, onUpdate: true},
			{name: "model", usage: "model the agent uses", onCreate: true, onUpdate: true},
		}}),
		newFilesCmd(),
		newUsersCmd(),
		newThreadsCmd(),
		newTUICmd(),
		newVersionCmd(),
	)
	return root
}

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		return output.EmitError(os.Stderr, err)
	}
	return output.ExitOK
}

// newClient resolves credentials and builds an API client.
func newClient() (*client.Client, error) {
	if flagOutput != "json" {
		return nil, fmt.Errorf("unsupported output format %q (only json is supported)", flagOutput)
	}
	r, err := config.Resolve(flagConfigName, flagBaseURL)
	if err != nil {
		return nil, err
	}
	return client.New(r.BaseURL, r.APIKey), nil
}
