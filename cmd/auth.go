package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(newAuthLoginCmd(), newAuthStatusCmd())
	return cmd
}

// flagOrgSlug scopes the personal-tokens page opened during interactive login.
var flagOrgSlug string

func newAuthLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login [instance]",
		Short: "Store an API key for an instance",
		Long: "Store an API key for an instance.\n\n" +
			"Defaults to app.interloom.com. To target another instance, pass it as an\n" +
			"argument or via --config-name: a short name (dev), a host (dev.interloom.com),\n" +
			"or a local address (localhost:8080, always http).\n\n" +
			"Each config is identified by host and organization, so the same host can\n" +
			"hold several organizations side by side (e.g. app-acme, app-beta).\n\n" +
			"The API key is never read from a flag: pipe it via stdin, set INTERLOOM_API_KEY,\n" +
			"or paste it when prompted. When prompting, the instance's personal-tokens page\n" +
			"is opened in your browser so you can create a key; pass --organization-slug to\n" +
			"open that organization's page (/<slug>/personal-tokens).",
		Args: cobra.MaximumNArgs(1),
		RunE: runAuthLogin,
	}
	cmd.Flags().StringVar(&flagOrgSlug, "organization-slug", "",
		"organization slug used to scope the token-creation page opened in the browser")
	return cmd
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	// 1. Resolve the host (arg > --config-name > default).
	host, baseURL, err := config.Normalize(loginInstanceInput(args))
	if err != nil {
		return err
	}

	// 2. Resolve the API key (piped stdin > env > hidden prompt). Never a flag.
	apiKey, err := readAPIKey(baseURL, flagOrgSlug)
	if err != nil {
		return err
	}

	// 3. Verify the key against /users/me and learn the key's organization.
	c := client.New(baseURL, apiKey)
	user, err := c.Get(cmd.Context(), "users", "me")
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
			return fmt.Errorf("authentication failed for %s: invalid API key", baseURL)
		}
		return fmt.Errorf("could not reach %s: %w", baseURL, err)
	}
	orgSlug, err := orgSlugFromUser(user)
	if err != nil {
		return err
	}

	// 4. Persist under the (host, org) identity and make current.
	name := config.Name(host, orgSlug)
	if err := config.SaveConfig(name, config.Config{APIKey: apiKey, BaseURL: baseURL, OrganizationSlug: orgSlug}); err != nil {
		return err
	}
	if err := config.SetCurrentConfigName(name); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Logged in to %s as organization %q (config %q) and set as current.\n", baseURL, orgSlug, name)
	return printResult([]byte(fmt.Sprintf(`{"config_name":%q,"base_url":%q,"organization_slug":%q,"status":"authenticated"}`, name, baseURL, orgSlug)))
}

// orgSlugFromUser extracts the organization slug from a /users/me response.
func orgSlugFromUser(user json.RawMessage) (string, error) {
	var me struct {
		Organization struct {
			Slug string `json:"slug"`
		} `json:"organization"`
	}
	if err := json.Unmarshal(user, &me); err != nil {
		return "", fmt.Errorf("parse /users/me response: %w", err)
	}
	if me.Organization.Slug == "" {
		return "", fmt.Errorf("no organization in /users/me response")
	}
	return me.Organization.Slug, nil
}

// defaultLoginInstance is used when no instance is given as an argument or via --config-name.
const defaultLoginInstance = "app.interloom.com"

// loginInstanceInput resolves the instance string: positional arg > --config-name
// flag > default. The user is never prompted.
func loginInstanceInput(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	if flagConfigName != "" {
		return flagConfigName
	}
	return defaultLoginInstance
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verify the current credentials and show the authenticated user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := config.Resolve(flagConfigName, flagBaseURL)
			if err != nil {
				return err
			}
			c := client.New(r.BaseURL, r.APIKey)
			user, err := c.Get(cmd.Context(), "users", "me")
			if err != nil {
				return err
			}
			backfillOrgSlug(r.ConfigName, user)
			out, err := json.Marshal(struct {
				ConfigName string          `json:"config_name"`
				BaseURL    string          `json:"base_url"`
				Status     string          `json:"status"`
				User       json.RawMessage `json:"user"`
			}{r.ConfigName, r.BaseURL, "ok", user})
			if err != nil {
				return err
			}
			return printResult(out)
		},
	}
}

// backfillOrgSlug records the organization slug on a stored config that
// predates org tracking. The slug is stable, so it is written only when absent.
// Failures are non-fatal: status should still print even if the file is missing
// (e.g. credentials came purely from environment variables).
func backfillOrgSlug(name string, user json.RawMessage) {
	if name == "" {
		return
	}
	cfg, err := config.LoadConfig(name)
	if err != nil || cfg.OrganizationSlug != "" {
		return
	}
	slug, err := orgSlugFromUser(user)
	if err != nil {
		return
	}
	cfg.OrganizationSlug = slug
	_ = config.SaveConfig(name, cfg)
}

// readAPIKey returns the key from piped stdin, then INTERLOOM_API_KEY, then a
// hidden interactive prompt. It never accepts the key from a flag. When
// prompting interactively it first opens the instance's personal-tokens page so
// the user can create a key; orgSlug, when set, scopes that page to the
// organization (/<slug>/personal-tokens).
func readAPIKey(baseURL, orgSlug string) (string, error) {
	if !stdinIsTTY() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		if key := strings.TrimSpace(string(data)); key != "" {
			return key, nil
		}
	}
	if key := os.Getenv(config.EnvAPIKey); key != "" {
		return key, nil
	}
	if stdinIsTTY() {
		tokenURL := tokenPageURL(baseURL, orgSlug)
		if err := openBrowser(tokenURL); err != nil {
			fmt.Fprintf(os.Stderr, "Open this page to create an API key:\n  %s\n\n", tokenURL)
		} else {
			fmt.Fprintf(os.Stderr, "Opened %s in your browser to create an API key.\n\n", tokenURL)
		}
		return promptHidden("Paste your API key: ")
	}
	return "", fmt.Errorf("no API key: pipe it via stdin or set %s", config.EnvAPIKey)
}

// tokenPageURL builds the personal-tokens page URL, optionally scoped to an
// organization slug: <base>/personal-tokens or <base>/<slug>/personal-tokens.
func tokenPageURL(baseURL, orgSlug string) string {
	base := strings.TrimRight(baseURL, "/")
	if orgSlug != "" {
		base += "/" + orgSlug
	}
	return base + "/personal-tokens?autoCreate=true&name=Interloom%20CLI"
}

// openBrowser opens rawURL in the user's default browser.
func openBrowser(rawURL string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	return exec.Command(name, append(args, rawURL)...).Start()
}

func promptHidden(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	fmt.Fprint(os.Stderr, prompt)

	// term.ReadPassword disables echo and restores it on return, but a Ctrl-C
	// kills the process mid-read before that happens, leaving the terminal with
	// echo off and the cursor hidden. Save the state and restore it on signal.
	oldState, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		if _, ok := <-sigCh; ok {
			_ = term.Restore(fd, oldState)
			fmt.Fprintln(os.Stderr)
			os.Exit(130) // 128 + SIGINT
		}
	}()

	data, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("no API key provided")
	}
	return key, nil
}
