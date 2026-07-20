package main

import "testing"

func TestNormalizeMapRewritesNullOnlyType(t *testing.T) {
	schema := map[string]any{"type": schemaTypeNull, "description": "disabled"}

	normalizeMap(schema)

	if got := schema["type"]; got != schemaTypeString {
		t.Fatalf("type = %v, want string", got)
	}
	if _, ok := schema["nullable"]; ok {
		t.Fatalf("nullable should be absent: %v", schema)
	}
	if got := schema["description"]; got != "disabled" {
		t.Fatalf("description = %v, want disabled", got)
	}
}
