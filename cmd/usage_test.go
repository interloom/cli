package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
)

const (
	usageTestID         = "a/b"
	usageGroupFlag      = "--group-by"
	usageGroupCase      = "case"
	usageGroupModel     = "model"
	usageGroupAgent     = "agent"
	usageCommand        = "usage"
	usageBreakdowns     = "breakdowns"
	usageBreakdownsPath = "/spaces/a%2Fb/usage/breakdowns"
)

func TestUsageCommands(t *testing.T) {
	const totals = `{"invocation_count":3,"total_interactions":7,"total_input_tokens":120,"total_output_tokens":31,"total_cached_input_tokens":40,"total_tokens":151,"total_cost_amount":null}`
	for _, tc := range []struct {
		name, path, query, response string
		args                        []string
	}{
		{usageGroupCase, "/cases/a%2Fb/usage", "", totals, []string{resourceCases, usageCommand, usageTestID}},
		{"space", "/spaces/a%2Fb/usage", "", `{"invocation_count":0,"total_tokens":null,"total_cost_amount":null}`, []string{resourceSpaces, usageCommand, usageTestID}},
		{"by case", usageBreakdownsPath, "group_by=case", `{"data":[{"group_by":"case","case":{"id":"deleted-case","type":"CASE"},"totals":` + totals + `}]}`, []string{resourceSpaces, usageCommand, usageBreakdowns, usageTestID, usageGroupFlag, usageGroupCase}},
		{"by model", usageBreakdownsPath, "group_by=model&limit=1", `{"data":[{"group_by":"model","model":"example","totals":` + totals + `}]}`, []string{resourceSpaces, usageCommand, usageBreakdowns, usageTestID, usageGroupFlag, usageGroupModel, "--" + argLimit, "1"}},
		{"by agent", usageBreakdownsPath, "group_by=agent&limit=20", `{"data":[]}`, []string{resourceSpaces, usageCommand, usageBreakdowns, usageTestID, usageGroupFlag, usageGroupAgent, "--" + argLimit, "20"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/public"+tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			t.Setenv(config.EnvAPIKey, "test-key")
			t.Setenv(config.EnvBaseURL, server.URL)
			t.Setenv(config.EnvConfig, "")
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			original := os.Stdout
			os.Stdout = out
			t.Cleanup(func() { os.Stdout = original; _ = out.Close() })
			root := newRootCmd()
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("requests = %d, want 1", calls)
			}
			assertInvocationOutput(t, out, tc.response)
		})
	}
}

func TestUsageInvalidArguments(t *testing.T) {
	const argumentError = "accepts 1 arg(s)"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id"}, "--group-by must be"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, "other"}, "--group-by must be"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + argLimit, "0"}, "--limit must be at least 1"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + argLimit, "-1"}, "--limit must be at least 1"},
		{[]string{resourceCases, usageCommand}, argumentError},
		{[]string{resourceSpaces, usageCommand, "id", "extra"}, argumentError},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, usageGroupFlag, usageGroupCase}, argumentError},
		{[]string{resourceCases, usageCommand, "id", "--all"}, "unknown flag"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + keyCursor, "next"}, "unknown flag"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root := newRootCmd()
			root.SetArgs(tc.args)
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestUsageAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"space_not_found","message":"Space not found."}}`))
	}))
	defer server.Close()
	t.Setenv(config.EnvAPIKey, "test-key")
	t.Setenv(config.EnvBaseURL, server.URL)
	t.Setenv(config.EnvConfig, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range [][]string{
		{resourceSpaces, usageCommand, "missing"},
		{resourceSpaces, usageCommand, usageBreakdowns, "missing", usageGroupFlag, usageGroupAgent},
	} {
		root := newRootCmd()
		root.SetArgs(args)
		err := root.Execute()
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Code != "space_not_found" {
			t.Fatalf("error = %v, want space_not_found (404)", err)
		}
	}
}
