package cmd

import "testing"

func TestResourceFilterFlagsUseKebabCaseAndQuerySnakeCase(t *testing.T) {
	r := resource{name: "cases", singular: "case", filters: []filter{
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
