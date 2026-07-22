package cmd

import (
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

// bodyResource mirrors a resource's create/update body fields for tests.
var bodyResource = resource{name: "things", singular: "thing", fields: []field{
	{name: keyTitle, usage: "title", onCreate: true, onUpdate: true, required: true},
	{name: keyDescription, usage: "description", onCreate: true, onUpdate: true},
	{name: "external_id", usage: "external id", onCreate: true},
	{name: keyStatus, usage: "status", onUpdate: true},
	fieldTags,
}}

func mustSet(t *testing.T, cmd *cobra.Command, name, val string) {
	t.Helper()
	if err := cmd.Flags().Set(name, val); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

func TestCreateBodyFromFlagsUsesSnakeCaseAndArrays(t *testing.T) {
	cmd := bodyResource.createCmd()
	mustSet(t, cmd, "title", "New case")
	mustSet(t, cmd, "description", "desc")
	mustSet(t, cmd, "tags", "a,b")

	body, err := bodyResource.body(cmd, true)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	var got struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Status      *string  `json:"status"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Title != "New case" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Description != "desc" {
		t.Fatalf("description = %q", got.Description)
	}
	if want := []string{"a", "b"}; len(got.Tags) != 2 || got.Tags[0] != want[0] || got.Tags[1] != want[1] {
		t.Fatalf("tags = %v", got.Tags)
	}
	if got.Status != nil {
		t.Fatalf("status should be absent when unset: %s", body)
	}
}

func TestCreateBodyMissingRequiredFlagErrors(t *testing.T) {
	cmd := bodyResource.createCmd()
	mustSet(t, cmd, "description", "desc")
	if _, err := bodyResource.body(cmd, true); err == nil {
		t.Fatal("expected error for missing required --title")
	}
}

func TestUpdateBodyDoesNotRequireCreateRequiredFields(t *testing.T) {
	cmd := bodyResource.updateCmd()
	mustSet(t, cmd, "status", "completed")
	body, err := bodyResource.body(cmd, false)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["status"] != "completed" || len(got) != 1 {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestAgentBodyIncludesReasoningEffort(t *testing.T) {
	agents := apiResource("agents")
	cmd := agents.createCmd()
	mustSet(t, cmd, "name", "Reasoning agent")
	mustSet(t, cmd, "reasoning-effort", "HIGH")
	body, err := agents.body(cmd, true)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	var got struct {
		Name            string `json:"name"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "Reasoning agent" || got.ReasoningEffort != "HIGH" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestBodyFlagsAndJSONAreMutuallyExclusive(t *testing.T) {
	cmd := bodyResource.createCmd()
	mustSet(t, cmd, "title", "New case")
	mustSet(t, cmd, keyData, `{"title":"x"}`)
	if _, err := bodyResource.body(cmd, true); err == nil {
		t.Fatal("expected error when both field flags and --data are set")
	}
}

func TestResourceFilterFlagsUseKebabCaseAndQuerySnakeCase(t *testing.T) {
	r := resource{name: resourceCases, singular: "case", filters: []filter{
		filterSpaceID,
		{name: "parent_case_id", usage: "filter by parent Case ID"},
		{name: "assignee_id", usage: "filter by assignee User ID"},
	}}
	cmd := r.listCmd()
	if err := cmd.Flags().Set("space-id", "space-1"); err != nil {
		t.Fatalf("set space-id: %v", err)
	}
	if err := cmd.Flags().Set("parent-case-id", "case-1"); err != nil {
		t.Fatalf("set parent-case-id: %v", err)
	}
	if err := cmd.Flags().Set("assignee-id", "user-1"); err != nil {
		t.Fatalf("set assignee-id: %v", err)
	}
	if cmd.Flags().Lookup("parent_case_id") != nil {
		t.Fatalf("unexpected snake_case flag registered")
	}

	q := r.listQuery(cmd)
	if got := q.Get("space_id"); got != testSpaceID {
		t.Fatalf("space_id query = %q", got)
	}
	if got := q.Get("parent_case_id"); got != "case-1" {
		t.Fatalf("parent_case_id query = %q", got)
	}
	if got := q.Get("assignee_id"); got != "user-1" {
		t.Fatalf("assignee_id query = %q", got)
	}
}

func TestCasesListDefaultsUseChronologicalSortWhenUnscoped(t *testing.T) {
	r := apiResource(resourceCases)
	cmd := r.listCmd()

	q := r.listQuery(cmd)
	if got := q.Get(keySort); got != defaultUnscopedCasesSort {
		t.Fatalf("sort query = %q, want %q", got, defaultUnscopedCasesSort)
	}
	if got := q.Get(keyDirection); got != defaultUnscopedCasesDirection {
		t.Fatalf("direction query = %q, want %q", got, defaultUnscopedCasesDirection)
	}
}

func TestCasesListDefaultsPreserveTreeOrderWhenScoped(t *testing.T) {
	r := apiResource(resourceCases)
	cmd := r.listCmd()
	mustSet(t, cmd, "space-id", "space-1")

	q := r.listQuery(cmd)
	if got := q.Get(keySort); got != defaultScopedCasesSort {
		t.Fatalf("sort query = %q, want %q", got, defaultScopedCasesSort)
	}
	if got := q.Get(keyDirection); got != defaultScopedCasesDirection {
		t.Fatalf("direction query = %q, want %q", got, defaultScopedCasesDirection)
	}
}

func TestCasesListDefaultsRespectExplicitSort(t *testing.T) {
	r := apiResource(resourceCases)
	cmd := r.listCmd()
	mustSet(t, cmd, "sort", "position")

	q := r.listQuery(cmd)
	if got := q.Get(keySort); got != "position" {
		t.Fatalf("sort query = %q, want position", got)
	}
	if got := q.Get(keyDirection); got != defaultScopedCasesDirection {
		t.Fatalf("direction query = %q, want %q", got, defaultScopedCasesDirection)
	}
}

func TestCasesListDefaultsRespectExplicitDirection(t *testing.T) {
	r := apiResource(resourceCases)
	cmd := r.listCmd()
	mustSet(t, cmd, "direction", "asc")

	q := r.listQuery(cmd)
	if got := q.Get(keySort); got != defaultUnscopedCasesSort {
		t.Fatalf("sort query = %q, want %q", got, defaultUnscopedCasesSort)
	}
	if got := q.Get(keyDirection); got != "asc" {
		t.Fatalf("direction query = %q, want asc", got)
	}
}

func TestModelsCommandIsListOnlyWithoutPaginationFlags(t *testing.T) {
	models := newResourceCmd(apiResource(resourceModels))
	list, _, err := models.Find([]string{commandUseList})
	if err != nil || list == nil || list.Use != commandUseList {
		t.Fatalf("models list command not registered: child=%v err=%v", list, err)
	}
	for _, flag := range []string{argLimit, keyCursor, argAll} {
		if list.Flags().Lookup(flag) != nil {
			t.Fatalf("models list should not expose --%s", flag)
		}
	}
	if child, _, err := models.Find([]string{commandNameGet, "model-1"}); err == nil && child != nil && child.Use == commandUseGet {
		t.Fatalf("models get command should not be registered")
	}
}

func TestToolsCommandOnlyExposesSupportedVerbs(t *testing.T) {
	tools := newResourceCmd(apiResource(resourceTools))
	for _, args := range [][]string{
		{commandUseList},
		{commandNameGet, testToolID},
		{commandUseCreate},
		{commandNameUpdate, testToolID},
	} {
		if child, _, err := tools.Find(args); err != nil || child == nil {
			t.Fatalf("tools command %v not registered: child=%v err=%v", args, child, err)
		}
	}
	if child, _, err := tools.Find([]string{commandNameDelete, testToolID}); err == nil && child != nil && child.Name() == commandNameDelete {
		t.Fatal("tools delete command should not be registered")
	}

	create, _, err := tools.Find([]string{commandUseCreate})
	if err != nil {
		t.Fatalf("find tools create: %v", err)
	}
	secretIDsFlag := field{name: keySecretIDs}.flagName()
	for _, flag := range []string{keyName, keyDescription, keyScript, secretIDsFlag} {
		if create.Flags().Lookup(flag) == nil {
			t.Fatalf("tools create should expose --%s", flag)
		}
	}
	update, _, err := tools.Find([]string{commandNameUpdate, testToolID})
	if err != nil {
		t.Fatalf("find tools update: %v", err)
	}
	if update.Flags().Lookup("manager-id") == nil {
		t.Fatal("tools update should expose --manager-id")
	}
}

func TestToolUpdateBodyFromFlags(t *testing.T) {
	tools := apiResource(resourceTools)
	cmd := tools.updateCmd()
	mustSet(t, cmd, keyDescription, testUpdatedToolDescription)
	mustSet(t, cmd, field{name: keySecretIDs}.flagName(), "secret-1,"+testSecretID2)

	body, err := tools.body(cmd, false)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	var got struct {
		Description string   `json:"description"`
		SecretIDs   []string `json:"secret_ids"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Description != testUpdatedToolDescription || len(got.SecretIDs) != 2 || got.SecretIDs[1] != testSecretID2 {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestSecretsCommandOnlyExposesSupportedVerbs(t *testing.T) {
	secrets := newResourceCmd(apiResource(resourceSecrets))
	for _, args := range [][]string{{commandUseList}, {commandUseCreate}, {commandNameDelete, "secret-1"}} {
		if child, _, err := secrets.Find(args); err != nil || child == nil {
			t.Fatalf("secrets command %v not registered: child=%v err=%v", args, child, err)
		}
	}
	for _, verb := range []string{commandNameGet, commandNameUpdate} {
		if child, _, err := secrets.Find([]string{verb, "secret-1"}); err == nil && child != nil && child.Name() == verb {
			t.Fatalf("secrets %s command should not be registered", verb)
		}
	}
	create, _, err := secrets.Find([]string{commandUseCreate})
	if err != nil || create.Flags().Lookup(keyName) == nil || create.Flags().Lookup("value") == nil {
		t.Fatalf("secrets create field flags missing: child=%v err=%v", create, err)
	}
}

func TestAgentToolsCommandShapeAndBody(t *testing.T) {
	agents := newAgentsCmd()
	for _, args := range [][]string{{resourceTools, commandUseList, testAgentID}, {resourceTools, "replace", testAgentID}} {
		if child, _, err := agents.Find(args); err != nil || child == nil {
			t.Fatalf("agents command %v not registered: child=%v err=%v", args, child, err)
		}
	}
	replace, _, err := agents.Find([]string{resourceTools, "replace", testAgentID})
	if err != nil {
		t.Fatalf("find agents tools replace: %v", err)
	}
	mustSet(t, replace, agentToolIDsFlag, "tool-1,tool-2")
	body, err := agentToolsBody(replace)
	if err != nil {
		t.Fatalf("agentToolsBody: %v", err)
	}
	if got, want := string(body), `{"tool_ids":["`+testToolID+`","`+testToolID2+`"]}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestSpacesTriggerCommandShape(t *testing.T) {
	spaces := newSpacesCmd()
	for _, args := range [][]string{{commandNameTrigger, commandNameGet, testSpaceID}, {commandNameTrigger, commandNameUpdate, testSpaceID}} {
		child, _, err := spaces.Find(args)
		if err != nil || child == nil {
			t.Fatalf("spaces command %v not registered: child=%v err=%v", args, child, err)
		}
	}
	update, _, err := spaces.Find([]string{commandNameTrigger, commandNameUpdate, testSpaceID})
	if err != nil || update.Flags().Lookup(keyData) == nil || update.Flags().Lookup("file") == nil {
		t.Fatalf("spaces trigger update body flags missing: child=%v err=%v", update, err)
	}
}
