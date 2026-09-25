package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/interloom/cli/internal/config"
)

func TestInvocationsReadCommands(t *testing.T) {
	const invocationID = "run/7"
	for _, tc := range []struct {
		name     string
		args     []string
		path     string
		queries  []string
		pages    []string
		expected string
	}{
		{
			name: "get", args: []string{commandNameGet, invocationID},
			path: "/api/v1/public/invocations/run%2F7", queries: []string{""},
			pages:    []string{`{"id":"run/7","outcome":"succeeded","duration_seconds":1.25,"token_usage":{"input_tokens":120,"output_tokens":31,"cached_input_tokens":40,"cache_write_input_tokens":null}}`},
			expected: `{"id":"run/7","outcome":"succeeded","duration_seconds":1.25,"token_usage":{"input_tokens":120,"output_tokens":31,"cached_input_tokens":40,"cache_write_input_tokens":null}}`,
		},
		{
			name: "page", args: []string{"steps", invocationID, "--" + argLimit, "3", "--" + keyCursor, "start"},
			path: "/api/v1/public/invocations/run%2F7/steps", queries: []string{"cursor=start&limit=3"},
			pages:    []string{`{"data":[{"id":"s2","type":"thinking","content_status":"withheld","token_usage":null}],"has_more":true,"next_cursor":"later"}`},
			expected: `{"data":[{"id":"s2","type":"thinking","content_status":"withheld","token_usage":null}],"has_more":true,"next_cursor":"later"}`,
		},
		{
			name: "all", args: []string{"steps", invocationID, "--" + argLimit, "3", "--all"},
			path: "/api/v1/public/invocations/run%2F7/steps", queries: []string{"limit=3", "cursor=later&limit=3"},
			pages: []string{
				`{"data":[{"id":"s1","type":"tool_call","output":"","token_usage":{"input_tokens":17,"output_tokens":5,"cached_input_tokens":0,"cache_write_input_tokens":null}}],"has_more":true,"next_cursor":"later"}`,
				`{"data":[{"id":"s2","type":"tool_call","output":null}],"has_more":false}`,
			},
			expected: `{"data":[{"id":"s1","type":"tool_call","output":"","token_usage":{"input_tokens":17,"output_tokens":5,"cached_input_tokens":0,"cache_write_input_tokens":null}},{"id":"s2","type":"tool_call","output":null}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls >= len(tc.pages) {
					t.Error("unexpected extra request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodGet || r.URL.EscapedPath() != tc.path || r.URL.RawQuery != tc.queries[calls] {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				_, _ = w.Write([]byte(tc.pages[calls]))
				calls++
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
			root.SetArgs(append([]string{resourceInvocations}, tc.args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != len(tc.pages) {
				t.Fatalf("requests = %d, want %d", calls, len(tc.pages))
			}
			assertInvocationOutput(t, out, tc.expected)
		})
	}
}

func assertInvocationOutput(t *testing.T, out *os.File, expected string) {
	t.Helper()
	if _, err := out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.NewDecoder(out).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("output = %v, want %v", got, want)
	}
}
