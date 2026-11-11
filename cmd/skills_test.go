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
	"testing"

	"github.com/interloom/cli/internal/client"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const testSkillsAgentResource = "agents"

func TestSkillsRequests(t *testing.T) {
	const (
		agents       = testSkillsAgentResource
		instructions = "instructions"
		equipped     = "equipped"
		createKey    = "create-key"
		updateKey    = "update-key"
		equipKey     = "equip-key"
		skillID      = "c/d"
	)
	for _, tc := range []struct {
		name, method, path, query, body, key, response string
		cli                                            []string
		args                                           map[string]any
	}{
		{
			name: "agents_create", method: http.MethodPost, path: "/agents", body: `{"name":"Equipped agent","skill_ids":["first","second"]}`, response: `{"id":"new-agent"}`,
			cli: []string{agents, commandUseCreate, "--" + keyName, "Equipped agent", "--skill-ids", "first,second"}, args: map[string]any{keyName: "Equipped agent", "skill_ids": []string{"first", "second"}},
		},
		{
			name: "skills_list", method: http.MethodGet, path: "/skills", query: "agent_id=agent-7&scope=equipped&search=review", response: `{"data":[],"has_more":false}`,
			cli: []string{resourceSkills, commandUseList, "--agent-id", "agent-7", "--scope", equipped, "--search", "review"}, args: map[string]any{keyAgentID: "agent-7", "scope": equipped, "search": "review"},
		},
		{
			name: "skills_get", method: http.MethodGet, path: "/skills/a%2Fb", response: `{"id":"a/b","instructions":"Review"}`,
			cli: []string{resourceSkills, commandNameGet, usageTestID}, args: map[string]any{"id": usageTestID},
		},
		{
			name: "skills_create", method: http.MethodPost, path: "/skills", body: `{"name":"Review","description":"Test","instructions":"Check"}`, key: createKey, response: `{"id":"skill-1"}`,
			cli: []string{resourceSkills, commandUseCreate, "--name", "Review", "--description", "Test", "--instructions", "Check", "--" + flagIdempotency, createKey}, args: map[string]any{keyName: "Review", keyDescription: "Test", instructions: "Check", keyIdempotency: createKey},
		},
		{
			name: "skills_update", method: http.MethodPatch, path: "/skills/a%2Fb", body: `{"instructions":""}`, key: updateKey, response: `{"id":"a/b","instructions":""}`,
			cli: []string{resourceSkills, commandNameUpdate, usageTestID, "--instructions", "", "--" + flagIdempotency, updateKey}, args: map[string]any{"id": usageTestID, instructions: "", keyIdempotency: updateKey},
		},
		{
			name: "agents_skills_equip", method: http.MethodPut, path: "/agents/a%2Fb/skills/c%2Fd", key: equipKey, response: `{"agent_id":"a/b","skill_id":"c/d","equipped":true}`,
			cli: []string{agents, resourceSkills, commandEquip, usageTestID, skillID, "--" + flagIdempotency, equipKey}, args: map[string]any{keyAgentID: usageTestID, keySkillID: skillID, keyIdempotency: equipKey},
		},
		{
			name: "agents_skills_remove", method: http.MethodDelete, path: "/agents/a%2Fb/skills/c%2Fd", response: `{"agent_id":"a/b","skill_id":"c/d","equipped":false}`,
			cli: []string{agents, resourceSkills, commandRemove, usageTestID, skillID}, args: map[string]any{keyAgentID: usageTestID, keySkillID: skillID},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.EscapedPath() != "/api/v1/public"+tc.path || r.URL.RawQuery != tc.query || r.Header.Get("Idempotency-Key") != tc.key {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				assertSkillJSON(t, string(raw), tc.body)
				switch tc.method {
				case http.MethodPut, http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				default:
					_, _ = w.Write([]byte(tc.response))
				}
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
			root.SetArgs(tc.cli)
			if execErr := root.Execute(); execErr != nil {
				t.Fatal(execErr)
			}
			assertInvocationOutput(t, out, tc.response)
			session := newTestMCPSession(t, client.New(server.URL, "test-key"))
			result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatal(toolResultText(t, result))
			}
			assertSkillJSON(t, toolResultText(t, result), tc.response)
		})
	}
}

func TestAgentSkillPermissionErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"custom_skills_disabled","message":"Custom skills are not enabled."}}`))
	}))
	defer server.Close()
	setDatabaseTestEnv(t, server.URL)
	session := newTestMCPSession(t, client.New(server.URL, "test-key"))
	for _, action := range []string{commandEquip, commandRemove} {
		root := newRootCmd()
		root.SetArgs([]string{testSkillsAgentResource, resourceSkills, action, "restricted-agent", "skill-1"})
		var apiErr *client.APIError
		if err := root.Execute(); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
			t.Fatalf("unexpected error: %v", err)
		}
		result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "agents_skills_" + action, Arguments: map[string]any{keyAgentID: "restricted-agent", keySkillID: "skill-1"}})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("expected API error, got %v", result)
		}
	}
}

func assertSkillJSON(t *testing.T, got, want string) {
	t.Helper()
	if want == "" {
		if got != "" {
			t.Errorf("unexpected body: %s", got)
		}
		return
	}
	var actual, expected any
	if err := json.Unmarshal([]byte(got), &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("got %s, want %s", got, want)
	}
}
