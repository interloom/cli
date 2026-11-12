package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSpaceEmailRequests(t *testing.T) {
	const response = `{"domain":"example.test","addresses":[{"local_part":"old","inbound_enabled":false,"is_outbound":false}]}`
	const retryKey = "email-retry"
	const inboundBody = `{"local_part":"inbound","previous_alias_inbound":"retain","set_as_sender":false}`
	const previousAliasKey = "previous_alias_inbound"
	for _, tc := range []struct {
		action, method, suffix, body, key string
		flags                             []string
		args                              map[string]any
	}{
		{commandNameGet, http.MethodGet, "", "", "", nil, map[string]any{}},
		{
			commandConfigure, http.MethodPut, "", `{"local_part":"new","previous_alias_inbound":"retain"}`, retryKey,
			[]string{"--" + keyData, `{"local_part":"new","previous_alias_inbound":"retain"}`, "--" + flagIdempotency, retryKey},
			map[string]any{keyData: map[string]any{keyLocalPart: "new", previousAliasKey: "retain"}, keyIdempotency: retryKey},
		},
		{
			commandConfigure, http.MethodPut, "", `{"local_part":null,"previous_alias_inbound":"disable"}`, retryKey,
			[]string{"--" + keyData, `{"local_part":null,"previous_alias_inbound":"disable"}`, "--" + flagIdempotency, retryKey},
			map[string]any{keyData: map[string]any{keyLocalPart: nil, previousAliasKey: commandDisable}, keyIdempotency: retryKey},
		},
		{
			commandConfigure, http.MethodPut, "", inboundBody, retryKey,
			[]string{"--" + keyData, inboundBody, "--" + flagIdempotency, retryKey},
			map[string]any{keyData: map[string]any{keyLocalPart: "inbound", previousAliasKey: "retain", "set_as_sender": false}, keyIdempotency: retryKey},
		},
		{
			commandDisable, http.MethodDelete, "/a%2Fb", "", retryKey,
			[]string{usageTestID, "--" + flagIdempotency, retryKey},
			map[string]any{keyLocalPart: usageTestID, keyIdempotency: retryKey},
		},
	} {
		t.Run(tc.action+tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.EscapedPath() != "/api/v1/public/spaces/a%2Fb/email-addresses"+tc.suffix || r.Header.Get(headerIdempotency) != tc.key {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if string(body) != tc.body {
					t.Errorf("body: got %s, want %s", body, tc.body)
				}
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			setDatabaseTestEnv(t, server.URL)
			cli := []string{resourceSpaces, spaceEmailAddresses, tc.action, usageTestID}
			cli = append(cli, tc.flags...)
			tc.args[keySpaceID] = usageTestID
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			original := os.Stdout
			os.Stdout = out
			t.Cleanup(func() { os.Stdout = original; _ = out.Close() })
			root := newRootCmd()
			root.SetArgs(cli)
			if execErr := root.Execute(); execErr != nil {
				t.Fatal(execErr)
			}
			assertInvocationOutput(t, out, response)
			session := newTestMCPSession(t, client.New(server.URL, "test-key"))
			result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "spaces_email_addresses_" + tc.action, Arguments: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]any{
				"domain":    "example.test",
				"addresses": []any{map[string]any{keyLocalPart: "old", "inbound_enabled": false, "is_outbound": false}},
			}
			if !reflect.DeepEqual(result.StructuredContent, expected) {
				t.Fatalf("unexpected MCP result: %v", result)
			}
		})
	}
}
