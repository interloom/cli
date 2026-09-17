package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
)

const (
	testDatabaseID     = "database-1"
	testDatabaseUpsert = "upsert"
)

func TestDatabasesCommandShape(t *testing.T) {
	root := newRootCmd()
	for _, tc := range []struct {
		args []string
		use  string
	}{
		{args: []string{resourceDatabases, commandNameGet, testDatabaseID}, use: commandUseGet},
		{args: []string{resourceDatabases, "query", testDatabaseID}, use: "query <id>"},
		{args: []string{resourceDatabases, "aggregate", testDatabaseID}, use: "aggregate <id>"},
		{args: []string{resourceDatabases, testDatabaseUpsert, testDatabaseID}, use: "upsert <id>"},
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

func TestDatabasesAggregateSendsJSONBody(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/public/databases/"+testDatabaseID+"/aggregate"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		var body struct {
			Expressions []struct {
				Name     string `json:"name"`
				Function string `json:"function"`
				Column   string `json:"column"`
			} `json:"expressions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body.Expressions) != 1 || body.Expressions[0].Name != "total" ||
			body.Expressions[0].Function != "sum" || body.Expressions[0].Column != "amount" {
			t.Errorf("unexpected body: %+v", body)
		}
		_, _ = w.Write([]byte(`{"database":{"id":"database-1"},"observed_revision":2,"values":{"total":"42.5"}}`))
	}))
	defer apiServer.Close()

	setDatabaseTestEnv(t, apiServer.URL)
	root := newRootCmd()
	root.SetArgs([]string{
		resourceDatabases,
		"aggregate",
		testDatabaseID,
		"--data", `{"expressions":[{"name":"total","function":"sum","column":"amount"}]}`,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute databases aggregate: %v", err)
	}
}

func TestDatabasesUpsertSendsExactJSONBody(t *testing.T) {
	const body = `{"expected_revision":9007199254740993,"rows":[{"row_id":"row-2","count":7,"active":false,"value":null,"amount":"12.30"}]}`
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/public/databases/"+testDatabaseID+"/upsert" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing JSON content type")
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != body {
			t.Errorf("body = %s, error = %v; want %s", got, err, body)
		}
		_, _ = w.Write([]byte(`{"database_id":"database-1","committed_revision":9007199254740994,"resulting_row_count":3,"inserted_count":1,"updated_count":0}`))
	}))
	defer apiServer.Close()

	setDatabaseTestEnv(t, apiServer.URL)
	root := newRootCmd()
	root.SetArgs([]string{resourceDatabases, testDatabaseUpsert, testDatabaseID, "-d", body})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute databases upsert: %v", err)
	}
}

func TestDatabasesUpsertConflictDoesNotRetry(t *testing.T) {
	calls := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"write_conflict","message":"Database revision does not match expected revision."}}`))
	}))
	defer apiServer.Close()

	setDatabaseTestEnv(t, apiServer.URL)
	root := newRootCmd()
	root.SetArgs([]string{resourceDatabases, testDatabaseUpsert, testDatabaseID, "-d", `{"expected_revision":3,"rows":[]}`})
	err := root.Execute()
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "write_conflict" || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("expected write_conflict, got %v", err)
	}
	if calls != 1 {
		t.Errorf("requests = %d, want 1 (no automatic retry)", calls)
	}
}

func setDatabaseTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv(config.EnvAPIKey, "test-key")
	t.Setenv(config.EnvBaseURL, baseURL)
	t.Setenv(config.EnvConfig, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}
