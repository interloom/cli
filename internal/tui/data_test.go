package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/interloom/cli/internal/client"
)

func TestCaseFetchesUsePositionOrder(t *testing.T) {
	t.Run("space cases", func(t *testing.T) {
		c := queryAssertingClient(t, map[string]string{
			"space_id":     "space-1",
			querySort:      sortPosition,
			queryDirection: directionAsc,
			queryStatus:    "started",
		})
		if _, _, _, err := fetchCasesPage(context.Background(), c, "space-1", "started", ""); err != nil {
			t.Fatalf("fetchCasesPage: %v", err)
		}
	})

	t.Run("child cases", func(t *testing.T) {
		c := queryAssertingClient(t, map[string]string{
			"parent_case_id": "case-1",
			querySort:        sortPosition,
			queryDirection:   directionAsc,
			queryStatus:      "blocked",
		})
		if _, _, _, err := fetchSubCasesPage(context.Background(), c, "case-1", "blocked", ""); err != nil {
			t.Fatalf("fetchSubCasesPage: %v", err)
		}
	})
}

func queryAssertingClient(t *testing.T, want map[string]string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/public/cases" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		for k, v := range want {
			if got := q.Get(k); got != v {
				t.Fatalf("query %s = %q, want %q", k, got, v)
			}
		}
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "test-key")
}
