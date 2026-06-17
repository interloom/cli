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
		Short: "Manage authentication and instances",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthStatusCmd(), newAuthLogoutCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login [instance]",
		Short: "Store an API key for an instance",
		Long: "Store an API key for an instance.\n\n" +
			"Defaults to app.interloom.com. To target another instance, pass it as an\n" +
			"argument or via --instance: a short name (dev), a host (dev.interloom.com),\n" +
			"or a local address (localhost:8080, always http).\n\n" +
			"The API key is never read from a flag: pipe it via stdin, set INTERLOOM_API_KEY,\n" +
			"or paste it when prompted. When prompting, the instance's personal-tokens page\n" +
			"is opened in your browser so you can create a key.",
		Args: cobra.MaximumNArgs(1),
		RunE: runAuthLogin,
	}
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	// 1. Resolve the instance (arg > --instance > default).
	name, baseURL, err := config.Normalize(loginInstanceInput(args))
	if err != nil {
		return err
	}

	// 2. Resolve the API key (piped stdin > env > hidden prompt). Never a flag.
	apiKey, err := readAPIKey(baseURL)
	if err != nil {
		return err
	}

	// 3. Verify the key against /users/me before persisting anything.
	c := client.New(baseURL, apiKey)
	if _, err := c.Get(cmd.Context(), "users", "me"); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
			return fmt.Errorf("authentication failed for %s: invalid API key", baseURL)
		}
		return fmt.Errorf("could not reach %s: %w", baseURL, err)
	}

	// 4. Persist and make current.
	if err := config.SaveInstance(name, config.Instance{APIKey: apiKey, BaseURL: baseURL}); err != nil {
		return err
	}
	if err := config.SetCurrentInstance(name); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Logged in to %s (instance %q) and set as current.\n", baseURL, name)
	return printResult([]byte(fmt.Sprintf(`{"instance":%q,"base_url":%q,"status":"authenticated"}`, name, baseURL)))
}

// defaultLoginInstance is used when no instance is given as an argument or via --instance.
const defaultLoginInstance = "app.interloom.com"

// loginInstanceInput resolves the instance string: positional arg > --instance
// flag > default. The user is never prompted.
func loginInstanceInput(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	if flagInstance != "" {
		return flagInstance
	}
	return defaultLoginInstance
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verify the current credentials and show the authenticated user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := config.Resolve(flagInstance, flagBaseURL)
			if err != nil {
				return err
			}
			c := client.New(r.BaseURL, r.APIKey)
			user, err := c.Get(cmd.Context(), "users", "me")
			if err != nil {
				return err
			}
			out, err := json.Marshal(struct {
				Instance string          `json:"instance"`
				BaseURL  string          `json:"base_url"`
				Status   string          `json:"status"`
				User     json.RawMessage `json:"user"`
			}{r.Instance, r.BaseURL, "ok", user})
			if err != nil {
				return err
			}
			return printResult(out)
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [instance]",
		Short: "Remove a saved instance (defaults to the current one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := flagInstance
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				name = config.CurrentInstance()
			}
			if name == "" {
				return fmt.Errorf("no instance to log out: pass one as an argument")
			}
			if err := config.DeleteInstance(name); err != nil {
				return err
			}
			return printResult([]byte(fmt.Sprintf(`{"instance":%q,"status":"logged_out"}`, name)))
		},
	}
}

// readAPIKey returns the key from piped stdin, then INTERLOOM_API_KEY, then a
// hidden interactive prompt. It never accepts the key from a flag. When
// prompting interactively it first opens the instance's personal-tokens page so
// the user can create a key.
func readAPIKey(baseURL string) (string, error) {
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
		tokenURL := strings.TrimRight(baseURL, "/") + "/personal-tokens?autoCreate=true&name=Interloom%20CLI"
		if err := openBrowser(tokenURL); err != nil {
			fmt.Fprintf(os.Stderr, "Open this page to create an API key:\n  %s\n\n", tokenURL)
		} else {
			fmt.Fprintf(os.Stderr, "Opened %s in your browser to create an API key.\n\n", tokenURL)
		}
		return promptHidden("Paste your API key: ")
	}
	return "", fmt.Errorf("no API key: pipe it via stdin or set %s", config.EnvAPIKey)
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
