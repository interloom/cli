package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const testNoteTitle = "Test note"

func assertNoteRequest(t *testing.T, r *http.Request, key string) {
	t.Helper()
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/public/notes" || r.Header.Get("Idempotency-Key") != key {
		t.Errorf("unexpected method, path, or idempotency header")
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	if len(body) != 1 || body[keyTitle] != testNoteTitle {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestNoteIdempotency(t *testing.T) {
	const notes = "notes"
	for _, key := range []string{"", "note-retry-17"} {
		for _, rawBody := range []bool{false, true} {
			t.Run(key+map[bool]string{false: "fields", true: "raw"}[rawBody], func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					assertNoteRequest(t, r, key)
					_, _ = w.Write([]byte(`{"id":"note-1"}`))
				}))
				defer server.Close()
				setDatabaseTestEnv(t, server.URL)
				args := []string{notes, commandUseCreate, "--title", testNoteTitle}
				mcpArgs := map[string]any{keyTitle: testNoteTitle}
				if rawBody {
					args = []string{notes, commandUseCreate, "--" + keyData, `{"title":"Test note"}`}
					mcpArgs = map[string]any{keyData: map[string]any{keyTitle: testNoteTitle}}
				}
				if key != "" {
					args = append(args, "--"+flagIdempotency, key)
					mcpArgs[keyIdempotency] = key
				}
				root := newRootCmd()
				root.SetArgs(args)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				session := newTestMCPSession(t, client.New(server.URL, "test-key"))
				result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "notes_create", Arguments: mcpArgs})
				if err != nil {
					t.Fatal(err)
				}
				if result.IsError {
					t.Fatal(toolResultText(t, result))
				}
				if calls != 2 {
					t.Fatalf("requests = %d, want 2", calls)
				}
			})
		}
	}
}
