package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/interloom/cli/internal/config"
)

const testDatabaseID = "database-1"

func TestDatabasesCommandShape(t *testing.T) {
	root := newRootCmd()
	for _, tc := range []struct {
		args []string
		use  string
	}{
		{args: []string{resourceDatabases, commandNameGet, testDatabaseID}, use: commandUseGet},
		{args: []string{resourceDatabases, "query", testDatabaseID}, use: "query <id>"},
	} {
		cmd, _, err := root.Find(tc.args)
		if err != nil || cmd == nil || cmd.Use != tc.use {
			t.Fatalf("command %v not registered: child=%v err=%v", tc.args, cmd, err)
		}
	}
}

func TestDatabasesGetSendsRequest(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/public/databases/"+testDatabaseID; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-key"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"id":"database-1","schema":{"columns":[]}}`))
	}))
	defer apiServer.Close()

	setDatabaseTestEnv(t, apiServer.URL)
	root := newRootCmd()
	root.SetArgs([]string{resourceDatabases, commandNameGet, testDatabaseID})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute databases get: %v", err)
	}
}

func TestDatabasesQuerySendsJSONBody(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/public/databases/"+testDatabaseID+"/query"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}
		var body struct {
			SelectedColumns []string `json:"selected_columns"`
			PageSize        int      `json:"page_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body.SelectedColumns) != 2 || body.SelectedColumns[1] != "state" || body.PageSize != 17 {
			t.Errorf("unexpected body: %+v", body)
		}
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"database":{"id":"database-1"},"observed_revision":1,"returned_count":0}`))
	}))
	defer apiServer.Close()

	setDatabaseTestEnv(t, apiServer.URL)
	root := newRootCmd()
	root.SetArgs([]string{
		resourceDatabases,
		"query",
		testDatabaseID,
		"--data", `{"selected_columns":["row_id","state"],"page_size":17}`,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute databases query: %v", err)
	}
}

func setDatabaseTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv(config.EnvAPIKey, "test-key")
	t.Setenv(config.EnvBaseURL, baseURL)
	t.Setenv(config.EnvConfig, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}
