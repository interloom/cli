package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/interloom/cli/internal/config"
)

func TestAssistantChats(t *testing.T) {
	for _, tc := range []struct {
		args                    []string
		method, query, response string
	}{
		{[]string{commandUseCreate}, http.MethodPost, "", `{"id":"chat","thread":{"id":"thread","type":"THREAD"}}`},
		{[]string{commandUseList, "--" + argLimit, "2", "--" + keyCursor, "chat-page"}, http.MethodGet, "cursor=chat-page&limit=2", `{"data":[],"has_more":false}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 {
					t.Errorf("expected empty request body: %q, %v", body, err)
				}
				if r.Method != tc.method || r.URL.Path != "/api/v1/public/assistant-chats" || r.URL.RawQuery != tc.query {
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
			root.SetArgs(append([]string{resourceAssistantChats}, tc.args...))
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
