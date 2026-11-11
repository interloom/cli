package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCaseStages(t *testing.T) {
	const response = `{"data":[{"id":"stage-1","position":1000,"status":"completed","assignee":null,"title":null},{"id":"stage-2","position":3000,"status":"waiting","started_at":null,"completed_at":null}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/public/cases/a%2Fb/stages" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(response))
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
	root.SetArgs([]string{resourceCases, resourceStages, usageTestID})
	if execErr := root.Execute(); execErr != nil {
		t.Fatal(execErr)
	}
	assertInvocationOutput(t, out, response)
	session := newTestMCPSession(t, client.New(server.URL, "test-key"))
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "cases_stages", Arguments: map[string]any{keyCaseID: usageTestID}})
	if err != nil {
		t.Fatal(err)
	}
	assertSkillJSON(t, toolResultText(t, result), response)
}
