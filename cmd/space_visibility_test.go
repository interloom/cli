package cmd

import (
	"encoding/json"
	"testing"
)

const (
	visibilityKey = "is_public"
	trueJSON      = "true"
	falseJSON     = "false"
	nullJSON      = "null"
	booleanType   = "boolean"
)

func TestSpaceVisibilityFlags(t *testing.T) {
	r := apiResource(resourceSpaces)
	for _, value := range []string{"", trueJSON, falseJSON} {
		t.Run(value, func(t *testing.T) {
			cmd := r.createCmd()
			args := []string{"--name", "test"}
			if value != "" {
				args = append(args, "--is-public="+value)
			}
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			body, err := r.body(cmd, true)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			v, present := got[visibilityKey]
			if present != (value != "") || (present && v != (value == trueJSON)) {
				t.Fatalf("unexpected visibility in %s", body)
			}
		})
	}
	if r.updateCmd().Flags().Lookup("is-public") != nil {
		t.Fatal("visibility must be create-only")
	}
}

func TestSpaceVisibilityMCP(t *testing.T) {
	r := apiResource(resourceSpaces)
	props := bodyInputSchema(r, true)["properties"].(map[string]any)
	if props[visibilityKey].(map[string]any)[schemaKeyType] != booleanType {
		t.Fatal("visibility schema must be boolean")
	}
	for _, raw := range []string{trueJSON, falseJSON, `"true"`, "1", nullJSON} {
		got, err := bodyMapFromFieldArgs(toolArgs{visibilityKey: json.RawMessage(raw)}, r.fieldsFor(true))
		valid := raw == trueJSON || raw == falseJSON || raw == nullJSON
		if (err == nil) != valid {
			t.Fatalf("%s: unexpected error %v", raw, err)
		}
		if raw == nullJSON {
			if len(got) != 0 {
				t.Fatal("null optional arguments must be omitted")
			}
		} else if valid && got[visibilityKey] != (raw == trueJSON) {
			t.Fatalf("%s: unexpected body %v", raw, got)
		}
	}
}
