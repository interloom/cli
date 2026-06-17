package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/interloom/cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage saved instances",
	}
	cmd.AddCommand(newConfigListCmd(), newConfigUseCmd(), newConfigCurrentCmd())
	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved instances",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := config.ListInstances()
			if err != nil {
				return err
			}
			current := config.CurrentInstance()
			type item struct {
				Name    string `json:"name"`
				BaseURL string `json:"base_url"`
				Current bool   `json:"current"`
			}
			items := make([]item, 0, len(names))
			for _, n := range names {
				inst, _ := config.LoadInstance(n)
				items = append(items, item{Name: n, BaseURL: inst.BaseURL, Current: n == current})
			}
			out, err := json.Marshal(struct {
				Current   string `json:"current"`
				Instances []item `json:"instances"`
			}{current, items})
			if err != nil {
				return err
			}
			return printResult(out)
		},
	}
}

func newConfigUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <instance>",
		Short: "Set the current instance (no API key needed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			inst, err := config.LoadInstance(name)
			if err != nil {
				return fmt.Errorf("instance %q not found: run `interloom auth login %s`", name, name)
			}
			if err := config.SetCurrentInstance(name); err != nil {
				return err
			}
			return printResult([]byte(fmt.Sprintf(`{"instance":%q,"base_url":%q,"status":"current"}`, name, inst.BaseURL)))
		},
	}
}

func newConfigCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the current instance",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			name := config.CurrentInstance()
			if name == "" {
				return fmt.Errorf("no current instance: run `interloom auth login` or `interloom config use <instance>`")
			}
			inst, _ := config.LoadInstance(name)
			return printResult([]byte(fmt.Sprintf(`{"instance":%q,"base_url":%q}`, name, inst.BaseURL)))
		},
	}
}
