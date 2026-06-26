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
		newResourceCmd(apiResource("spaces")),
		newResourceCmd(apiResource("cases")),
		newResourceCmd(apiResource("notes")),
		newResourceCmd(apiResource("procedures")),
		newResourceCmd(apiResource("agents")),
		newFilesCmd(),
		newUsersCmd(),
		newThreadsCmd(),
		newMCPCmd(),
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
