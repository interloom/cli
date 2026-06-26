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

func TestBodyFlagsAndJSONAreMutuallyExclusive(t *testing.T) {
	cmd := bodyResource.createCmd()
	mustSet(t, cmd, "title", "New case")
	mustSet(t, cmd, "data", `{"title":"x"}`)
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
	if got := q.Get("space_id"); got != "space-1" {
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
