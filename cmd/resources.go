package cmd

// apiResources is the shared catalog for REST-backed resources. Keep resource
// shape here so Cobra commands and MCP tools do not drift.
func apiResources() []resource {
	return []resource{
		{name: "spaces", singular: "space", fields: []field{
			{name: "name", usage: "Space name", onCreate: true, onUpdate: true, required: true},
			{name: keyDescription, usage: "Space description", onCreate: true, onUpdate: true},
		}},
		{name: "cases", singular: "case", filters: []filter{
			filterSpaceID,
			{name: keyParentCaseID, usage: "filter by parent Case ID"},
			{name: keyAssigneeID, usage: "filter by assignee User ID"},
			{name: keyStatus, usage: "filter by status (repeatable): open, started, completed, cancelled, blocked", multi: true},
			{name: "sort", usage: "sort field: position, created_at, or updated_at"},
			filterDirection,
		}, fields: []field{
			{name: keyTitle, usage: "Case title", onCreate: true, onUpdate: true, required: true},
			{name: keyDescription, usage: "Case description", onCreate: true, onUpdate: true},
			{name: keySpaceID, usage: "owning Space ID (exactly one of space-id or parent-case-id)", onCreate: true, onUpdate: true},
			{name: keyParentCaseID, usage: "parent Case ID (exactly one of space-id or parent-case-id)", onCreate: true, onUpdate: true},
			{name: keyAssigneeID, usage: "assignee User ID", onCreate: true, onUpdate: true},
			{name: "due_at", usage: "due date (RFC3339 timestamp)", onCreate: true, onUpdate: true},
			{name: "external_id", usage: "external identifier", onCreate: true},
			{name: keyStatus, usage: "status: open, started, completed, cancelled, blocked", onUpdate: true},
			fieldTags,
			{name: "attached_file_ids", usage: "attached File IDs (repeatable)", multi: true, onCreate: true},
		}},
		{name: "notes", singular: "note", filters: []filter{
			filterSpaceID,
			filterCaseID,
			{name: "thread_id", usage: "filter by thread ID"},
			filterSort,
			filterDirection,
		}, fields: []field{
			{name: keyTitle, usage: "Note title", onCreate: true, onUpdate: true, required: true},
			{name: "body", usage: "Note body", onCreate: true, onUpdate: true},
			{name: keySpaceID, usage: "owning Space ID (exactly one of space-id or case-id)", onCreate: true, onUpdate: true},
			{name: keyCaseID, usage: "owning Case ID (exactly one of space-id or case-id)", onCreate: true, onUpdate: true},
			fieldTags,
		}},
		{name: "procedures", singular: "procedure", filters: []filter{
			filterSpaceID,
		}, fields: []field{
			{name: keyTitle, usage: "Procedure name", onCreate: true, onUpdate: true, required: true},
			{name: keySpaceID, usage: "owning Space ID", onCreate: true, required: true},
		}},
		{name: "agents", singular: "agent", noDelete: true, fields: []field{
			{name: "name", usage: "Agent name", onCreate: true, onUpdate: true, required: true},
			{name: "job_description", usage: "Agent job description", onCreate: true, onUpdate: true},
			{name: "model", usage: "model the agent uses", onCreate: true, onUpdate: true},
		}},
		{name: "files", singular: "file", noCreate: true, filters: []filter{
			filterSpaceID,
			filterCaseID,
			filterSort,
			filterDirection,
		}, fields: []field{
			fieldSpaceID,
			fieldCaseID,
			fieldTags,
		}},
		{name: "users", singular: "user", readOnly: true},
	}
}

func apiResource(name string) resource {
	for _, r := range apiResources() {
		if r.name == name {
			return r
		}
	}
	panic("unknown API resource: " + name)
}
