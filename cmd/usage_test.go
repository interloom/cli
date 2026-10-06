package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

// usageCaseGroups are group_by=case breakdowns in the API's response shape.
// They cover a null cost (pricing disabled), a cost tie, and a null token total.
var usageCaseGroups = map[string]string{
	"case-a": `{"group_by":"case","case":{"id":"case-a","type":"CASE","url":"/api/v1/public/cases/case-a"},"totals":{"invocation_count":2,"total_interactions":6,"total_tokens":900,"total_cost_amount":null}}`,
	"case-b": `{"group_by":"case","case":{"id":"case-b","type":"CASE","url":"/api/v1/public/cases/case-b"},"totals":{"invocation_count":5,"total_interactions":14,"total_tokens":4000,"total_cost_amount":0.42}}`,
	"case-c": `{"group_by":"case","case":{"id":"case-c","type":"CASE","url":"/api/v1/public/cases/case-c"},"totals":{"invocation_count":9,"total_interactions":40,"total_tokens":12000,"total_cost_amount":3.1}}`,
	"case-d": `{"group_by":"case","case":{"id":"case-d","type":"CASE","url":"/api/v1/public/cases/case-d"},"totals":{"invocation_count":1,"total_interactions":3,"total_tokens":null,"total_cost_amount":0.42}}`,
}

// usageCaseResponse builds a breakdowns response with the groups in the given order.
func usageCaseResponse(ids ...string) string {
	groups := make([]string, len(ids))
	for i, id := range ids {
		groups[i] = usageCaseGroups[id]
	}
	return `{"data":[` + strings.Join(groups, ",") + `]}`
}

func TestUsageBreakdownsSort(t *testing.T) {
	// The API returns groups in group-key order.
	keyOrder := usageCaseResponse("case-a", "case-b", "case-c", "case-d")
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"cost", usageCaseResponse("case-c", "case-b", "case-d", "case-a"), []string{"--" + keySort, usageSortCost}},
		{"cost with limit", usageCaseResponse("case-c", "case-b"), []string{"--" + keySort, usageSortCost, "--" + argLimit, "2"}},
		{"tokens", usageCaseResponse("case-c", "case-b", "case-a", "case-d"), []string{"--" + keySort, usageSortTokens}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				// A sorted request must fetch every group: the API applies limit in key order.
				if r.URL.EscapedPath() != "/api/v1/public"+usageBreakdownsPath || r.URL.RawQuery != "group_by=case" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				_, _ = w.Write([]byte(keyOrder))
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
			root.SetArgs(append([]string{resourceSpaces, usageCommand, usageBreakdowns, usageTestID, usageGroupFlag, usageGroupCase}, tc.args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("requests = %d, want 1", calls)
			}
			assertInvocationOutput(t, out, tc.want)
		})
	}
}

func TestMCPUsageTools(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() + "?" + r.URL.RawQuery {
		case "/api/v1/public/cases/" + testCaseID + "/usage?":
			_, _ = w.Write([]byte(`{"invocation_count":4,"total_cost_amount":1.5}`))
		case "/api/v1/public/spaces/" + testSpaceID + "/usage/breakdowns?group_by=case":
			_, _ = w.Write([]byte(usageCaseResponse("case-a", "case-b", "case-c", "case-d")))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer apiServer.Close()
	session := newTestMCPSession(t, client.New(apiServer.URL, "test-key"))

	for _, tc := range []struct {
		name, tool, want string
		args             map[string]any
	}{
		{"case totals", toolCasesUsage, `{"invocation_count":4,"total_cost_amount":1.5}`, map[string]any{"id": testCaseID}},
		{
			"top cases by cost", toolSpacesUsageBreakdowns, usageCaseResponse("case-c", "case-b"),
			map[string]any{"id": testSpaceID, keyGroupBy: usageGroupCase, keySort: usageSortCost, argLimit: 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if result.IsError {
				t.Fatalf("tool returned error: %s", toolResultText(t, result))
			}
			var got, want any
			if err := json.Unmarshal([]byte(toolResultText(t, result)), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("result = %v, want %v", got, want)
			}
		})
	}

	for _, args := range []map[string]any{
		{"id": testSpaceID},
		{"id": testSpaceID, keyGroupBy: "other"},
		{"id": testSpaceID, keyGroupBy: usageGroupCase, keySort: "spend"},
		{"id": testSpaceID, keyGroupBy: usageGroupCase, argLimit: 0},
	} {
		result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: toolSpacesUsageBreakdowns, Arguments: args})
		if err != nil {
			t.Fatalf("CallTool(%v): %v", args, err)
		}
		if !result.IsError {
			t.Fatalf("CallTool(%v) succeeded, want a tool error", args)
		}
	}
}

func TestUsageInvalidArguments(t *testing.T) {
	const (
		argumentError = "accepts 1 arg(s)"
		unknownFlag   = "unknown flag"
	)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id"}, "--group-by must be"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, "other"}, "--group-by must be"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + argLimit, "0"}, "--limit must be at least 1"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + argLimit, "-1"}, "--limit must be at least 1"},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + keySort, "spend"}, "--sort must be cost or tokens"},
		{[]string{resourceCases, usageCommand, "id", "--" + keySort, usageSortCost}, unknownFlag},
		{[]string{resourceCases, usageCommand}, argumentError},
		{[]string{resourceSpaces, usageCommand, "id", "extra"}, argumentError},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, usageGroupFlag, usageGroupCase}, argumentError},
		{[]string{resourceCases, usageCommand, "id", "--all"}, unknownFlag},
		{[]string{resourceSpaces, usageCommand, usageBreakdowns, "id", usageGroupFlag, usageGroupCase, "--" + keyCursor, "next"}, unknownFlag},
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
