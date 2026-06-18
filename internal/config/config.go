// Package config manages Interloom CLI instances stored under
// ~/.config/interloom/<instance>.json and resolves the effective
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

// Instance is the credential set persisted per instance.
//
// An instance is identified by the pair (host, organization): the same host
// may hold several organizations, each in its own file. OrganizationSlug is the
// stable slug returned by /users/me; it never changes for a given key.
type Instance struct {
	APIKey           string `json:"api_key"`
	BaseURL          string `json:"base_url"`
	OrganizationSlug string `json:"organization_slug,omitempty"`
}

type rootConfig struct {
	CurrentInstance string `json:"current_instance"`
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

func instancePath(dir, name string) string { return filepath.Join(dir, name+".json") }

// InstanceName builds the instance identifier from a normalized host name and an
// organization slug. Each (host, org) pair is a distinct instance, so the same
// host can hold several organizations side by side. An empty slug yields the
// bare host name (used only before an org is known).
func InstanceName(host, orgSlug string) string {
	if orgSlug == "" {
		return host
	}
	return host + "-" + orgSlug
}

// SaveInstance writes the instance file (0600) and ensures the dir exists (0700).
func SaveInstance(name string, inst Instance) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(instancePath(dir, name), append(data, '\n'), 0o600)
}

// ListInstances returns the names of all saved instances, sorted.
func ListInstances() ([]string, error) {
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

// DeleteInstance removes a saved instance file, clearing it as current if set.
func DeleteInstance(name string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.Remove(instancePath(dir, name)); err != nil {
		return err
	}
	if CurrentInstance() == name {
		return SetCurrentInstance("")
	}
	return nil
}

// LoadInstance reads a stored instance by name.
func LoadInstance(name string) (Instance, error) {
	var inst Instance
	dir, err := Dir()
	if err != nil {
		return inst, err
	}
	data, err := os.ReadFile(instancePath(dir, name))
	if err != nil {
		return inst, err
	}
	return inst, json.Unmarshal(data, &inst)
}

// CurrentInstance returns the active instance name, or "" if unset.
func CurrentInstance() string {
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
	return rc.CurrentInstance
}

// SetCurrentInstance records the active instance name.
func SetCurrentInstance(name string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rootConfig{CurrentInstance: name}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rootFile), append(data, '\n'), 0o600)
}

// Resolved is the effective configuration for a command invocation.
type Resolved struct {
	Instance string
	BaseURL  string
	APIKey   string
}

// Resolve computes effective credentials.
//
//	base URL: --base-url flag > INTERLOOM_BASE_URL > instance file
//	api key : INTERLOOM_API_KEY > instance file   (never a flag)
//
// The config is chosen from --config flag > INTERLOOM_CONFIG > current.
func Resolve(flagConfig, flagBaseURL string) (Resolved, error) {
	name := firstNonEmpty(flagConfig, os.Getenv(EnvConfig), CurrentInstance())

	var inst Instance
	if name != "" {
		// A missing file is not fatal: env vars alone may suffice.
		inst, _ = LoadInstance(name)
	}

	r := Resolved{
		Instance: name,
		BaseURL:  firstNonEmpty(flagBaseURL, os.Getenv(EnvBaseURL), inst.BaseURL),
		APIKey:   firstNonEmpty(os.Getenv(EnvAPIKey), inst.APIKey),
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

// Normalize maps user input to a canonical instance name and base URL.
//
//	dev                  -> dev,             https://dev.interloom.com
//	dev.interloom.com    -> dev,             https://dev.interloom.com
//	localhost:8080       -> localhost-8080,  http://localhost:8080
//	api.example.com      -> api.example.com, https://api.example.com
func Normalize(input string) (name, baseURL string, err error) {
	in := strings.TrimSpace(strings.ToLower(input))
	if in == "" {
		return "", "", fmt.Errorf("empty instance")
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
		return "", "", fmt.Errorf("invalid instance %q", input)
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
