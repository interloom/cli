package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/interloom/cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage saved configs",
	}
	cmd.AddCommand(newConfigListCmd(), newConfigUseCmd(), newConfigCurrentCmd(), newConfigDeleteCmd())
	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandUseList,
		Short: "List saved configs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := config.ListConfigs()
			if err != nil {
				return err
			}
			current := config.CurrentConfigName()
			type item struct {
				Name             string `json:"name"`
				BaseURL          string `json:"base_url"`
				OrganizationSlug string `json:"organization_slug,omitempty"`
				Current          bool   `json:"current"`
			}
			items := make([]item, 0, len(names))
			for _, n := range names {
				cfg, _ := config.LoadConfig(n)
				items = append(items, item{Name: n, BaseURL: cfg.BaseURL, OrganizationSlug: cfg.OrganizationSlug, Current: n == current})
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
			cfg, err := config.LoadConfig(name)
			if err != nil {
				return fmt.Errorf("config %q not found: run `interloom auth login %s`", name, name)
			}
			if err = config.SetCurrentConfigName(name); err != nil {
				return err
			}
			out, err := json.Marshal(struct {
				Config           string `json:"config"`
				BaseURL          string `json:"base_url"`
				OrganizationSlug string `json:"organization_slug,omitempty"`
				Status           string `json:"status"`
			}{name, cfg.BaseURL, cfg.OrganizationSlug, "current"})
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
			name := config.CurrentConfigName()
			if name == "" {
				return fmt.Errorf("no current config: run `interloom auth login` or `interloom config use <config>`")
			}
			cfg, _ := config.LoadConfig(name)
			out, err := json.Marshal(struct {
				Config           string `json:"config"`
				BaseURL          string `json:"base_url"`
				OrganizationSlug string `json:"organization_slug,omitempty"`
			}{name, cfg.BaseURL, cfg.OrganizationSlug})
			if err != nil {
				return err
			}
			return printResult(out)
		},
	}
}

func newConfigDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <config>",
		Short: "Remove a saved config (does not revoke the API key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := config.DeleteConfig(name); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("config %q not found", name)
				}
				return err
			}
			return printResult([]byte(fmt.Sprintf(`{"config":%q,"status":"deleted"}`, name)))
		},
	}
}
