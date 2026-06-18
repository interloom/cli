// Package config manages Interloom CLI configs stored under
// ~/.config/interloom/<config-name>.json and resolves the effective
// credentials for a command, honoring flag > env > file precedence.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Environment variables. When set, these always override values from config files.
const (
	EnvAPIKey  = "INTERLOOM_API_KEY"
	EnvBaseURL = "INTERLOOM_BASE_URL"
	EnvConfig  = "INTERLOOM_CONFIG"
)

const (
	defaultDomain = "interloom.com"
	rootFile      = "config.json"
)

// Config is the credential set persisted per (host, organization).
//
// A config is identified by the pair (host, organization): the same host
// may hold several organizations, each in its own file. OrganizationSlug is the
// stable slug returned by /users/me; it never changes for a given key.
type Config struct {
	APIKey           string `json:"api_key"`
	BaseURL          string `json:"base_url"`
	OrganizationSlug string `json:"organization_slug,omitempty"`
}

type rootConfig struct {
	CurrentConfigName string `json:"current_config_name"`
}

// Dir returns ~/.config/interloom (honoring XDG_CONFIG_HOME), creating nothing.
func Dir() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "interloom"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "interloom"), nil
}

func configPath(dir, name string) string { return filepath.Join(dir, name+".json") }

// Name builds the config name from a normalized host name and an organization
// slug. Each (host, org) pair is a distinct config, so the same host can hold
// several organizations side by side. An empty slug yields the bare host name
// (used only before an org is known).
func Name(host, orgSlug string) string {
	if orgSlug == "" {
		return host
	}
	return host + "-" + orgSlug
}

// SaveConfig writes the config file (0600) and ensures the dir exists (0700).
func SaveConfig(name string, cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(dir, name), append(data, '\n'), 0o600)
}

// ListConfigs returns the names of all saved configs, sorted.
func ListConfigs() ([]string, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == rootFile || !strings.HasSuffix(name, ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(names)
	return names, nil
}

// DeleteConfig removes a saved config file, clearing it as current if set.
func DeleteConfig(name string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.Remove(configPath(dir, name)); err != nil {
		return err
	}
	if CurrentConfigName() == name {
		return SetCurrentConfigName("")
	}
	return nil
}

// LoadConfig reads a stored config by name.
func LoadConfig(name string) (Config, error) {
	var cfg Config
	dir, err := Dir()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(configPath(dir, name))
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(data, &cfg)
}

// CurrentConfigName returns the active config name, or "" if unset.
func CurrentConfigName() string {
	dir, err := Dir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, rootFile))
	if err != nil {
		return ""
	}
	var rc rootConfig
	if json.Unmarshal(data, &rc) != nil {
		return ""
	}
	return rc.CurrentConfigName
}

// SetCurrentConfigName records the active config name.
func SetCurrentConfigName(name string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rootConfig{CurrentConfigName: name}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rootFile), append(data, '\n'), 0o600)
}

// Resolved is the effective configuration for a command invocation.
type Resolved struct {
	ConfigName string
	BaseURL    string
	APIKey     string
}

// Resolve computes effective credentials.
//
//	base URL: --base-url flag > INTERLOOM_BASE_URL > config file
//	api key : INTERLOOM_API_KEY > config file   (never a flag)
//
// The config is chosen from --config-name flag > INTERLOOM_CONFIG > current.
func Resolve(flagConfigName, flagBaseURL string) (Resolved, error) {
	name := firstNonEmpty(flagConfigName, os.Getenv(EnvConfig), CurrentConfigName())

	var cfg Config
	if name != "" {
		// A missing file is not fatal: env vars alone may suffice.
		cfg, _ = LoadConfig(name)
	}

	r := Resolved{
		ConfigName: name,
		BaseURL:    firstNonEmpty(flagBaseURL, os.Getenv(EnvBaseURL), cfg.BaseURL),
		APIKey:     firstNonEmpty(os.Getenv(EnvAPIKey), cfg.APIKey),
	}
	if r.BaseURL == "" {
		return r, fmt.Errorf("no base URL: run `interloom auth login` or set %s", EnvBaseURL)
	}
	if r.APIKey == "" {
		return r, fmt.Errorf("no API key: run `interloom auth login` or set %s", EnvAPIKey)
	}
	return r, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Normalize maps user input to a canonical host name and base URL.
//
//	dev                  -> dev,             https://dev.interloom.com
//	dev.interloom.com    -> dev,             https://dev.interloom.com
//	localhost:8080       -> localhost-8080,  http://localhost:8080
//	api.example.com      -> api.example.com, https://api.example.com
func Normalize(input string) (name, baseURL string, err error) {
	in := strings.TrimSpace(strings.ToLower(input))
	if in == "" {
		return "", "", fmt.Errorf("empty host")
	}

	scheme := ""
	if i := strings.Index(in, "://"); i != -1 {
		scheme = in[:i]
		in = in[i+3:]
	}
	// Drop any path/query, keep authority only.
	if i := strings.IndexAny(in, "/?#"); i != -1 {
		in = in[:i]
	}

	host, port := splitHostPort(in)
	if host == "" {
		return "", "", fmt.Errorf("invalid host %q", input)
	}

	if isLocal(host) {
		baseURL = "http://" + in // preserve literal host:port
		return sanitize(in), baseURL, nil
	}

	if !strings.Contains(host, ".") {
		host = host + "." + defaultDomain // bare label -> dev.interloom.com
	}
	if scheme == "" {
		scheme = "https"
	}

	authority := host
	if port != "" {
		authority = host + ":" + port
	}
	baseURL = scheme + "://" + authority

	switch {
	case strings.HasSuffix(host, "."+defaultDomain):
		name = strings.TrimSuffix(host, "."+defaultDomain)
	default:
		name = sanitize(authority)
	}
	return name, baseURL, nil
}

func splitHostPort(authority string) (host, port string) {
	// IPv6 literal: [::1]:9000
	if strings.HasPrefix(authority, "[") {
		if i := strings.Index(authority, "]"); i != -1 {
			host = authority[1:i]
			if rest := authority[i+1:]; strings.HasPrefix(rest, ":") {
				port = rest[1:]
			}
			return host, port
		}
	}
	if i := strings.LastIndex(authority, ":"); i != -1 {
		return authority[:i], authority[i+1:]
	}
	return authority, ""
}

func isLocal(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return true
	}
	return false
}

func sanitize(s string) string {
	return strings.NewReplacer(":", "-", "/", "-", "[", "", "]", "").Replace(s)
}
