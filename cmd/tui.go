package cmd

import (
	"github.com/interloom/cli/internal/config"
	"github.com/interloom/cli/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Browse spaces and case trees in an interactive terminal UI",
		Long: "Launch a full-screen, mouse-friendly terminal UI for your workspace.\n" +
			"Browse spaces and their cases, then drill into case trees to explore\n" +
			"sub-cases, files and notes. Notes and descriptions render as markdown,\n" +
			"images and text files preview inline, and cases can be filtered by\n" +
			"status. Shortcuts are shown along the bottom of the screen.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			r, _ := config.Resolve(flagConfigName, flagBaseURL)
			return tui.Run(cmd.Context(), c, r.ConfigName)
		},
	}
	addConfigNameFlag(cmd)
	return cmd
}
