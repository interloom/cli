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
		Short: "Manage saved configs",
	}
	cmd.AddCommand(newConfigListCmd(), newConfigUseCmd(), newConfigCurrentCmd())
	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved configs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := config.ListInstances()
			if err != nil {
				return err
			}
			current := config.CurrentInstance()
			type item struct {
				Name             string `json:"name"`
				BaseURL          string `json:"base_url"`
				OrganizationSlug string `json:"organization_slug,omitempty"`
				Current          bool   `json:"current"`
			}
			items := make([]item, 0, len(names))
			for _, n := range names {
				inst, _ := config.LoadInstance(n)
				items = append(items, item{Name: n, BaseURL: inst.BaseURL, OrganizationSlug: inst.OrganizationSlug, Current: n == current})
			}
			out, err := json.Marshal(struct {
				Current string `json:"current"`
				Configs []item `json:"configs"`
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
		Use:   "use <config>",
		Short: "Set the current config (no API key needed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			inst, err := config.LoadInstance(name)
			if err != nil {
				return fmt.Errorf("config %q not found: run `interloom auth login %s`", name, name)
			}
			if err = config.SetCurrentInstance(name); err != nil {
				return err
			}
			out, err := json.Marshal(struct {
				Config           string `json:"config"`
				BaseURL          string `json:"base_url"`
				OrganizationSlug string `json:"organization_slug,omitempty"`
				Status           string `json:"status"`
			}{name, inst.BaseURL, inst.OrganizationSlug, "current"})
			if err != nil {
				return err
			}
			return printResult(out)
		},
	}
}

func newConfigCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the current config",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			name := config.CurrentInstance()
			if name == "" {
				return fmt.Errorf("no current config: run `interloom auth login` or `interloom config use <config>`")
			}
			inst, _ := config.LoadInstance(name)
			out, err := json.Marshal(struct {
				Config           string `json:"config"`
				BaseURL          string `json:"base_url"`
				OrganizationSlug string `json:"organization_slug,omitempty"`
			}{name, inst.BaseURL, inst.OrganizationSlug})
			if err != nil {
				return err
			}
			return printResult(out)
		},
	}
}
