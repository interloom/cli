package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	testAgentID       = "agent-1"
	testSpaceID       = "space-1"
	testStatusOpen    = "open"
	testStatusStarted = "started"
	testManifestBody  = "{\"title\":\"Case\"}\n"
)

func TestMCPCommandShape(t *testing.T) {
	root := newRootCmd()
	child, _, err := root.Find([]string{mcpCommandName})
	if err != nil || child == nil || child.Use != mcpCommandName {
		t.Fatalf("mcp command not registered: child=%v err=%v", child, err)
	}
	if got := child.Flags().Lookup("addr").DefValue; got != defaultMCPAddr {
		t.Fatalf("default --addr = %q, want %q", got, defaultMCPAddr)
	}
	if got := child.Flags().Lookup("endpoint").DefValue; got != defaultMCPEndpoint {
		t.Fatalf("default --endpoint = %q, want %q", got, defaultMCPEndpoint)
	}
	if got := child.Flags().Lookup("http").DefValue; got != "false" {
		t.Fatalf("default --http = %q, want false", got)
	}
	if child.PersistentFlags().Lookup("config-name") == nil {
		t.Fatal("mcp command should expose --config-name")
	}
}

func TestMCPHTTPOnlyFlagsRequireHTTP(t *testing.T) {
	cmd := newMCPCmd()
	if err := cmd.Flags().Set("addr", "127.0.0.1:9000"); err != nil {
		t.Fatalf("set addr: %v", err)
	}
	if _, err := validateMCPModeFlags(cmd); err == nil || !strings.Contains(err.Error(), "--http") {
		t.Fatalf("validateMCPModeFlags error = %v, want --http requirement", err)
	}

	cmd = newMCPCmd()
	if err := cmd.Flags().Set("http", "true"); err != nil {
		t.Fatalf("set http: %v", err)
	}
	if got, err := validateMCPModeFlags(cmd); err != nil || got != defaultMCPEndpoint {
		t.Fatalf("validateMCPModeFlags with --http = endpoint %q err %v, want %q", got, err, defaultMCPEndpoint)
	}
}

func TestValidateLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8765", "localhost:8765", "[::1]:8765"} {
		if err := validateLoopbackAddr(addr); err != nil {
			t.Fatalf("validateLoopbackAddr(%q): %v", addr, err)
		}
	}
	for _, addr := range []string{":8765", "0.0.0.0:8765", "192.168.1.20:8765", "example.com:8765"} {
		if err := validateLoopbackAddr(addr); err == nil {
			t.Fatalf("validateLoopbackAddr(%q) succeeded, want error", addr)
		}
	}
}

func TestMCPToolRegistration(t *testing.T) {
	session := newTestMCPSession(t, client.New("http://127.0.0.1:1", "test-key"))

	tools, err := session.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{
		"spaces_list", "spaces_get", "spaces_create", "spaces_update", "spaces_delete",
		"cases_list", "cases_create", "agents_update", toolAgentToolsList, toolAgentToolsReplace,
		"secrets_list", "secrets_create", "secrets_delete", "files_upload", toolFilesDownload,
		toolCaseIngestionsCreate, toolCaseIngestionsGet, toolCaseIngestionsErrors,
		"models_list",
		"users_list", "users_get", "users_me", "threads_get", "threads_events", toolThreadsMessagesCreate,
	} {
		if !names[name] {
			t.Fatalf("tool %q not registered", name)
		}
	}
	for _, name := range []string{"agents_delete", "files_create", "models_get", "secrets_get", "secrets_update", "users_create", "users_delete"} {
		if names[name] {
			t.Fatalf("unsupported tool %q should not be registered", name)
		}
	}
}

func TestMCPToolUsesResolvedConfigToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(config.EnvAPIKey, "")
	t.Setenv(config.EnvBaseURL, "")
	t.Setenv(config.EnvConfig, "")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertCaseListRequest(t, r)
		_, _ = w.Write([]byte(`{"data":[{"id":"case-1"}],"has_more":false}`))
	}))
	defer apiServer.Close()

	if err := config.SaveConfig("dev-acme", config.Config{APIKey: "stored-token", BaseURL: apiServer.URL}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := config.SetCurrentConfigName("dev-acme"); err != nil {
		t.Fatalf("SetCurrentConfigName: %v", err)
	}
	r, err := config.Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	session := newTestMCPSession(t, client.New(r.BaseURL, r.APIKey))

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "cases_list",
		Arguments: map[string]any{
			argLimit:   2,
			keySpaceID: testSpaceID,
			keyStatus:  []string{testStatusOpen, testStatusStarted},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", toolResultText(t, result))
	}
	if got := toolResultText(t, result); !strings.Contains(got, "case-1") {
		t.Fatalf("tool result = %s, want case id", got)
	}
}

func TestMCPCasesListUsesUnscopedDefaults(t *testing.T) {
	q, err := listQueryFromArgs(toolArgs{}, apiResource(resourceCases))
	if err != nil {
		t.Fatalf("listQueryFromArgs: %v", err)
	}
	if got := q.Get(keySort); got != defaultUnscopedCasesSort {
		t.Fatalf("sort query = %q, want %q", got, defaultUnscopedCasesSort)
	}
	if got := q.Get(keyDirection); got != defaultUnscopedCasesDirection {
		t.Fatalf("direction query = %q, want %q", got, defaultUnscopedCasesDirection)
	}

	q, err = listQueryFromArgs(toolArgs{keySpaceID: json.RawMessage(`"space-1"`)}, apiResource(resourceCases))
	if err != nil {
		t.Fatalf("listQueryFromArgs scoped: %v", err)
	}
	if got := q.Get(keySort); got != defaultScopedCasesSort {
		t.Fatalf("scoped sort query = %q, want %q", got, defaultScopedCasesSort)
	}
	if got := q.Get(keyDirection); got != defaultScopedCasesDirection {
		t.Fatalf("scoped direction query = %q, want %q", got, defaultScopedCasesDirection)
	}
}

func TestMCPModelsListHasNoPagingArgs(t *testing.T) {
	r := apiResource(resourceModels)
	q, err := listQueryFromArgs(toolArgs{
		argLimit:  json.RawMessage(`10`),
		keyCursor: json.RawMessage(`"cursor"`),
		argAll:    json.RawMessage(`true`),
	}, r)
	if err != nil {
		t.Fatalf("listQueryFromArgs: %v", err)
	}
	if len(q) != 0 {
		t.Fatalf("models query = %v, want empty", q)
	}
	schema := listInputSchema(r)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %v", schema)
	}
	for _, name := range []string{argLimit, keyCursor, argAll} {
		if _, ok := props[name]; ok {
			t.Fatalf("models schema should not expose %q: %v", name, props)
		}
	}
}

func TestMCPCaseIngestionCreateUploadsManifest(t *testing.T) {
	manifestPath := writeMCPTestManifest(t)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertMCPCaseIngestionCreateRequest(t, r)
		_, _ = w.Write([]byte("{\"id\":\"ingestion-1\"}"))
	}))
	defer apiServer.Close()
	session := newTestMCPSession(t, client.New(apiServer.URL, "test-key"))

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: toolCaseIngestionsCreate,
		Arguments: map[string]any{
			keySpaceID:  testSpaceID,
			keyManifest: manifestPath,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", toolResultText(t, result))
	}
	if got := toolResultText(t, result); !strings.Contains(got, "ingestion-1") {
		t.Fatalf("tool result = %s, want ingestion id", got)
	}
}

func writeMCPTestManifest(t *testing.T) string {
	t.Helper()
	manifest, err := os.CreateTemp(t.TempDir(), "manifest-*.jsonl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, writeErr := manifest.WriteString(testManifestBody); writeErr != nil {
		t.Fatalf("WriteString: %v", writeErr)
	}
	if closeErr := manifest.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	return manifest.Name()
}

func assertMCPCaseIngestionCreateRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if got, want := r.Method, http.MethodPost; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := r.URL.Path, "/api/v1/public/case-ingestions"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if parseErr := r.ParseMultipartForm(1 << 20); parseErr != nil {
		t.Fatalf("ParseMultipartForm: %v", parseErr)
	}
	if got, want := r.FormValue(keySpaceID), testSpaceID; got != want {
		t.Errorf("space_id = %q, want %q", got, want)
	}
	assertMCPManifestField(t, r)
}

func assertMCPManifestField(t *testing.T, r *http.Request) {
	t.Helper()
	f, _, err := r.FormFile(keyManifest)
	if err != nil {
		t.Fatalf("manifest file missing: %v", err)
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != testManifestBody {
		t.Fatalf("manifest body = %q", body)
	}
}

func TestMCPCaseIngestionErrorsQuery(t *testing.T) {
	q, err := caseIngestionErrorsQueryFromArgs(toolArgs{
		argLimit:  json.RawMessage(`5`),
		keyCursor: json.RawMessage(`"cursor-1"`),
		argAll:    json.RawMessage(`true`),
	})
	if err != nil {
		t.Fatalf("caseIngestionErrorsQueryFromArgs: %v", err)
	}
	if got, want := q.Get(argLimit), "5"; got != want {
		t.Fatalf("limit query = %q, want %q", got, want)
	}
	if got, want := q.Get(keyCursor), "cursor-1"; got != want {
		t.Fatalf("cursor query = %q, want %q", got, want)
	}
	if q.Has(argAll) {
		t.Fatalf("all should not be forwarded as query: %v", q)
	}
}

func assertCaseListRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if got, want := r.Header.Get("Authorization"), "Bearer stored-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got, want := r.URL.Path, "/api/v1/public/cases"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	q := r.URL.Query()
	if got, want := q.Get(argLimit), "2"; got != want {
		t.Errorf("limit query = %q, want %q", got, want)
	}
	if got, want := q.Get(keySpaceID), testSpaceID; got != want {
		t.Errorf("space_id query = %q, want %q", got, want)
	}
	if got, want := q.Get(keySort), defaultScopedCasesSort; got != want {
		t.Errorf("sort query = %q, want %q", got, want)
	}
	if got, want := q.Get(keyDirection), defaultScopedCasesDirection; got != want {
		t.Errorf("direction query = %q, want %q", got, want)
	}
	if got := q[keyStatus]; len(got) != 2 || got[0] != testStatusOpen || got[1] != testStatusStarted {
		t.Errorf("status query = %v, want [open started]", got)
	}
}

func TestMCPCreateBodyFromTypedFields(t *testing.T) {
	args := toolArgs{
		"title":             json.RawMessage(`"New case"`),
		keyDescription:      json.RawMessage(`"Details"`),
		"attached_file_ids": json.RawMessage(`["file-1","file-2"]`),
	}
	body, err := bodyFromArgs(args, apiResource(resourceCases), true)
	if err != nil {
		t.Fatalf("bodyFromArgs: %v", err)
	}
	var got struct {
		Title           string   `json:"title"`
		Description     string   `json:"description"`
		AttachedFileIDs []string `json:"attached_file_ids"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.Title != "New case" || got.Description != "Details" || len(got.AttachedFileIDs) != 2 || got.AttachedFileIDs[1] != testFileID2 {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestMCPAgentBodyIncludesReasoningEffort(t *testing.T) {
	body, err := bodyFromArgs(toolArgs{
		keyName:            json.RawMessage(`"Reasoning agent"`),
		keyReasoningEffort: json.RawMessage(`"HIGH"`),
	}, apiResource("agents"), true)
	if err != nil {
		t.Fatalf("bodyFromArgs: %v", err)
	}
	var got struct {
		Name            string `json:"name"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.Name != "Reasoning agent" || got.ReasoningEffort != "HIGH" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestMCPThreadMessageCreateSendsFileIDs(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/public/threads/"+testThreadID+"/messages"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		var body struct {
			Text    string   `json:"text"`
			FileIDs []string `json:"file_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.Text != testMessageText || len(body.FileIDs) != 2 || body.FileIDs[1] != testFileID2 {
			t.Fatalf("unexpected body: %+v", body)
		}
		_, _ = w.Write([]byte(`{"id":"event-1"}`))
	}))
	defer apiServer.Close()
	session := newTestMCPSession(t, client.New(apiServer.URL, "test-key"))

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: toolThreadsMessagesCreate,
		Arguments: map[string]any{
			keyThreadID: testThreadID,
			"text":      testMessageText,
			keyFileIDs:  []string{testFileID1, testFileID2},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", toolResultText(t, result))
	}
	if got := toolResultText(t, result); !strings.Contains(got, "event-1") {
		t.Fatalf("tool result = %s, want event id", got)
	}
}

func TestMCPAgentToolsReplaceUsesPut(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPut; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/api/v1/public/agents/"+testAgentID+"/tools"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		var body struct {
			ToolIDs []string `json:"tool_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.ToolIDs) != 2 || body.ToolIDs[1] != "tool-2" {
			t.Fatalf("unexpected body: %+v", body)
		}
		_, _ = w.Write([]byte(`[{"id":"tool-1"},{"id":"tool-2"}]`))
	}))
	defer apiServer.Close()
	session := newTestMCPSession(t, client.New(apiServer.URL, "test-key"))

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: toolAgentToolsReplace,
		Arguments: map[string]any{
			keyAgentID: testAgentID,
			keyToolIDs: []string{"tool-1", "tool-2"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", toolResultText(t, result))
	}
}

func TestMCPAPIErrorsAreToolErrors(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"bad list"}}`))
	}))
	defer apiServer.Close()
	session := newTestMCPSession(t, client.New(apiServer.URL, "test-key"))

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "spaces_list"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if got := toolResultText(t, result); !strings.Contains(got, "bad list") {
		t.Fatalf("tool error = %s, want API error body", got)
	}
}

func newTestMCPSession(t *testing.T, c *client.Client) *mcpsdk.ClientSession {
	t.Helper()
	httpServer := httptest.NewServer(newMCPHTTPHandler(newInterloomMCPServer(c)))
	t.Cleanup(httpServer.Close)

	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "test"}, nil).Connect(
		context.Background(),
		&mcpsdk.StreamableClientTransport{
			Endpoint:             httpServer.URL,
			DisableStandaloneSSE: true,
			MaxRetries:           -1,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func toolResultText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatalf("missing tool content")
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		data, _ := json.Marshal(result.Content[0])
		t.Fatalf("first content is %T (%s), want TextContent", result.Content[0], data)
	}
	return text.Text
}

func TestMCPFileDownloadWritesLocalPath(t *testing.T) {
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "file bytes")
	}))
	defer downloadServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/public/files/file-1"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_, _ = fmt.Fprintf(w, `{"id":"file-1","download_url":%q}`, downloadServer.URL)
	}))
	defer apiServer.Close()
	session := newTestMCPSession(t, client.New(apiServer.URL, "test-key"))
	out := t.TempDir() + "/download.txt"

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      toolFilesDownload,
		Arguments: map[string]any{"id": "file-1", "out": out},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", toolResultText(t, result))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "file bytes" {
		t.Fatalf("downloaded bytes = %q", data)
	}
}
