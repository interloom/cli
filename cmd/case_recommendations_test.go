package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/interloom/cli/internal/config"
)

func TestCaseRecommendations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits []string
	}{
		{"similar-cases", []string{"", "1", "30"}},
		{"relevant-objects", []string{"", "1", "100"}},
	} {
		name := tc.name
		for _, limit := range tc.limits {
			t.Run(name+limit, func(t *testing.T) {
				const response = `{"anchor_case":{"id":"root","type":"CASE"},"score_source":null,"data":[{"score":0.37,"semantic_search_contribution":null,"source_cases":[{"id":"other"}]}]}`
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					query := ""
					if limit != "" {
						query = "limit=" + limit
					}
					if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/public/cases/a%2Fb/"+name || r.URL.RawQuery != query {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					_, _ = w.Write([]byte(response))
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
				args := []string{resourceCases, name, "a/b"}
				if limit != "" {
					args = append(args, "--"+argLimit, limit)
				}
				root := newRootCmd()
				root.SetArgs(args)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatalf("requests = %d, want 1", calls)
				}
				assertInvocationOutput(t, out, response)
			})
		}
	}
}

func TestCaseRecommendationsInvalidArguments(t *testing.T) {
	const limitError = "--limit must be between"
	const unknownFlagError = "unknown flag"
	for _, tc := range []struct {
		name    string
		maximum int
	}{{"similar-cases", 30}, {"relevant-objects", 100}} {
		for _, invalid := range []struct {
			args []string
			want string
		}{
			{[]string{}, "accepts 1 arg(s)"},
			{[]string{"id", "extra"}, "accepts 1 arg(s)"},
			{[]string{"id", "--" + argAll}, unknownFlagError},
			{[]string{"id", "--" + keyCursor, "next"}, unknownFlagError},
			{[]string{"id", "--" + argLimit, "0"}, limitError},
			{[]string{"id", "--" + argLimit, "-1"}, limitError},
			{[]string{"id", "--" + argLimit, fmt.Sprint(tc.maximum + 1)}, limitError},
		} {
			t.Run(tc.name+fmt.Sprint(invalid.args), func(t *testing.T) {
				cmd := newCaseRecommendationsCmd(tc.name, tc.maximum, 10)
				cmd.SetArgs(invalid.args)
				if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), invalid.want) {
					t.Fatalf("error = %v, want %q", err, invalid.want)
				}
			})
		}
	}
}
