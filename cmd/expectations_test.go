package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const expectationResponse = `{"id":"suite-1","mode":"any","is_gating":false,"checks":[{"id":"second","definition":{"type":"human_review"}},{"id":"first","definition":{"type":"output_assertion","mode":"all","predicates":[]}}]}`

func expectationDeleteServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.EscapedPath() != "/api/v1/public/expectations/a%2Fb" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.WriteHeader(status)
		if status == http.StatusNotFound {
			_, _ = w.Write([]byte(`{"error":{"code":"expectations_not_found","message":"Expectations not found."}}`))
		}
	}))
}

func TestExpectationsDelete(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := expectationDeleteServer(t, status)
			defer server.Close()
			setDatabaseTestEnv(t, server.URL)
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			original := os.Stdout
			os.Stdout = out
			t.Cleanup(func() { os.Stdout = original; _ = out.Close() })
			root := newRootCmd()
			root.SetArgs([]string{commandExpectations, commandNameDelete, usageTestID})
			err = root.Execute()
			if status == http.StatusNoContent {
				if err != nil {
					t.Fatal(err)
				}
				assertInvocationOutput(t, out, `{"id":"a/b","deleted":true}`)
			} else {
				var apiErr *client.APIError
				if !errors.As(err, &apiErr) || apiErr.Code != "expectations_not_found" {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			session := newTestMCPSession(t, client.New(server.URL, "test-key"))
			result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "expectations_delete", Arguments: map[string]any{"id": usageTestID}})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != (status == http.StatusNotFound) {
				t.Fatalf("unexpected result: %v", result)
			}
			if status == http.StatusNoContent && !reflect.DeepEqual(result.StructuredContent, map[string]any{"id": usageTestID, "deleted": true}) {
				t.Fatalf("unexpected result: %v", result)
			}
		})
	}
}

func TestExpectationsSet(t *testing.T) {
	const body = `{"target":{"type":"PROCEDURE_STAGE","id":"target-1"},"checks":[{"title":"Review","definition":{"type":"human_review"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/public/expectations" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil || string(raw) != body {
			t.Errorf("body = %s, error = %v", raw, err)
		}
		_, _ = w.Write([]byte(expectationResponse))
	}))
	defer server.Close()
	setDatabaseTestEnv(t, server.URL)
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = out
	t.Cleanup(func() { os.Stdout = original; _ = out.Close() })
	root := newRootCmd()
	root.SetArgs([]string{commandExpectations, "set", "--" + keyData, body})
	if execErr := root.Execute(); execErr != nil {
		t.Fatal(execErr)
	}
	assertInvocationOutput(t, out, expectationResponse)
	session := newTestMCPSession(t, client.New(server.URL, "test-key"))
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "expectations_set", Arguments: map[string]any{keyData: json.RawMessage(body)}})
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal([]byte(expectationResponse), &expected); err != nil {
		t.Fatal(err)
	}
	if result.IsError || !reflect.DeepEqual(result.StructuredContent, expected) {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestExpectations(t *testing.T) {
	for _, name := range []string{resourceCases, resourceProcedures, resourceStages, resourceProcedureStages} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/public/"+name+"/a%2Fb/expectations" || r.URL.RawQuery != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				_, _ = w.Write([]byte(expectationResponse))
			}))
			defer server.Close()
			setDatabaseTestEnv(t, server.URL)
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			original := os.Stdout
			os.Stdout = out
			t.Cleanup(func() { os.Stdout = original; _ = out.Close() })
			root := newRootCmd()
			root.SetArgs([]string{name, commandExpectations, usageTestID})
			if execErr := root.Execute(); execErr != nil {
				t.Fatal(execErr)
			}
			assertInvocationOutput(t, out, expectationResponse)
			session := newTestMCPSession(t, client.New(server.URL, "test-key"))
			result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: strings.ReplaceAll(name, "-", "_") + "_expectations", Arguments: map[string]any{"id": usageTestID}})
			if err != nil {
				t.Fatal(err)
			}
			var expected any
			if err := json.Unmarshal([]byte(expectationResponse), &expected); err != nil {
				t.Fatal(err)
			}
			if result.IsError || !reflect.DeepEqual(result.StructuredContent, expected) {
				t.Fatalf("unexpected result: %v", result)
			}
		})
	}
}

func TestExpectationsNotFound(t *testing.T) {
	const response = `{"error":{"code":"expectations_not_found","message":"Expectations not found."}}`
	const missingID = "missing"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	setDatabaseTestEnv(t, server.URL)
	root := newRootCmd()
	root.SetArgs([]string{resourceCases, commandExpectations, missingID})
	err := root.Execute()
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Code != "expectations_not_found" {
		t.Fatalf("unexpected error: %v", err)
	}
	session := newTestMCPSession(t, client.New(server.URL, "test-key"))
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "cases_expectations", Arguments: map[string]any{"id": missingID}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolResultText(t, result), "expectations_not_found") {
		t.Fatalf("unexpected result: %v", result)
	}
}
