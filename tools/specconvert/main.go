// Command specconvert downconverts an OpenAPI 3.1 document to a 3.0-compatible
// one so that oapi-codegen (which does not yet support 3.1) can process it.
//
// It performs the minimal set of transformations our spec needs:
//   - openapi: 3.1.x      -> 3.0.3
//   - anyOf/oneOf:[S,{type:null}] -> S + nullable:true (FastAPI optional fields)
//   - type:[X,"null"]     -> type:X + nullable:true
//   - type:null           -> type:string (3.0 has no null-only equivalent)
//   - contentMediaType    -> format:binary (3.1 binary upload encoding)
//   - discriminator props -> required on each union member schema
//
// Usage: specconvert <in.json> <out.json>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	schemaTypeNull   = "null"
	schemaTypeString = "string"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: specconvert <in.json> <out.json>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		fatal(err)
	}

	if _, ok := doc["openapi"]; ok {
		doc["openapi"] = "3.0.3"
	}
	walk(doc)
	requireDiscriminatorProps(doc)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		fatal(err)
	}
}

// walk recursively normalizes a decoded JSON value in place.
func walk(v any) {
	switch node := v.(type) {
	case map[string]any:
		normalizeMap(node)
		for _, child := range node {
			walk(child)
		}
	case []any:
		for _, child := range node {
			walk(child)
		}
	}
}

func normalizeMap(m map[string]any) {
	for _, comb := range []string{"anyOf", "oneOf"} {
		collapseNullableCombinator(m, comb)
	}
	collapseTypeArray(m)
	rewriteNullType(m)
	rewriteContentMediaType(m)
}

// collapseNullableCombinator turns anyOf/oneOf:[S, {type:null}] into S with
// nullable:true (the FastAPI optional-field pattern).
func collapseNullableCombinator(m map[string]any, comb string) {
	members, ok := m[comb].([]any)
	if !ok {
		return
	}
	var kept []any
	hadNull := false
	for _, member := range members {
		if isNullSchema(member) {
			hadNull = true
			continue
		}
		kept = append(kept, member)
	}
	if hadNull {
		m["nullable"] = true
	}
	switch len(kept) {
	case len(members):
		// no null member; leave as-is
	case 1:
		delete(m, comb)
		mergeInto(m, kept[0])
	default:
		m[comb] = kept
	}
}

// mergeInto copies the keys of a single schema member into m without
// overwriting existing keys.
func mergeInto(m map[string]any, member any) {
	only, ok := member.(map[string]any)
	if !ok {
		return
	}
	for k, val := range only {
		if _, exists := m[k]; !exists {
			m[k] = val
		}
	}
}

// collapseTypeArray turns type:[X,"null"] into type:X + nullable:true.
func collapseTypeArray(m map[string]any) {
	types, ok := m["type"].([]any)
	if !ok {
		return
	}
	var nonNull []any
	for _, t := range types {
		if t == schemaTypeNull {
			m["nullable"] = true
			continue
		}
		nonNull = append(nonNull, t)
	}
	if len(nonNull) > 0 {
		m["type"] = nonNull[0] // OAS 3.0 allows a single type only
	}
}

// rewriteNullType approximates a null-only JSON Schema as a string. OpenAPI 3.0
// has no null-only type, and nullable strings become pointers that oapi-codegen
// cannot use as discriminators. The original 3.1 spec remains authoritative.
func rewriteNullType(m map[string]any) {
	if m["type"] != schemaTypeNull {
		return
	}
	m["type"] = schemaTypeString
}

// rewriteContentMediaType converts the 3.1 binary upload encoding to 3.0's
// format:binary.
func rewriteContentMediaType(m map[string]any) {
	if _, ok := m["contentMediaType"]; !ok {
		return
	}
	delete(m, "contentMediaType")
	if m["type"] != "string" {
		return
	}
	if _, has := m["format"]; !has {
		m["format"] = "binary"
	}
}

func isNullSchema(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) == 1 && m["type"] == schemaTypeNull
}

// requireDiscriminatorProps marks every discriminator property as required on
// its union member schemas. oapi-codegen's generated union helpers assign the
// discriminator value as a plain string (e.g. `v.PayloadType = "message"`),
// which only compiles when the field is a non-pointer — i.e. when the property
// is required. FastAPI emits the discriminator with a const/default but omits
// it from `required`, so it would otherwise be generated as a *string.
func requireDiscriminatorProps(doc map[string]any) {
	defs := schemaDefs(doc)
	if defs == nil {
		return
	}
	visitMaps(doc, func(m map[string]any) {
		disc, ok := m["discriminator"].(map[string]any)
		if !ok {
			return
		}
		prop, ok := disc["propertyName"].(string)
		if !ok || prop == "" {
			return
		}
		for _, ref := range memberRefs(m, disc) {
			name, ok := schemaRefName(ref)
			if !ok {
				continue
			}
			if target, ok := defs[name].(map[string]any); ok {
				addRequired(target, prop)
			}
		}
	})
}

// schemaDefs returns components.schemas, or nil if absent.
func schemaDefs(doc map[string]any) map[string]any {
	comp, _ := doc["components"].(map[string]any)
	if comp == nil {
		return nil
	}
	defs, _ := comp["schemas"].(map[string]any)
	return defs
}

// memberRefs collects the $refs of a discriminated union's members, from the
// sibling oneOf/anyOf list and from the discriminator mapping.
func memberRefs(m, disc map[string]any) []string {
	var refs []string
	for _, key := range []string{"oneOf", "anyOf"} {
		members, _ := m[key].([]any)
		for _, member := range members {
			if mm, ok := member.(map[string]any); ok {
				if ref, ok := mm["$ref"].(string); ok {
					refs = append(refs, ref)
				}
			}
		}
	}
	if mapping, ok := disc["mapping"].(map[string]any); ok {
		for _, v := range mapping {
			if ref, ok := v.(string); ok {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// schemaRefName extracts Name from a "#/components/schemas/Name" reference.
func schemaRefName(ref string) (string, bool) {
	const prefix = "#/components/schemas/"
	if name, ok := strings.CutPrefix(ref, prefix); ok && name != "" {
		return name, true
	}
	return "", false
}

// addRequired appends prop to schema.required if not already present.
func addRequired(schema map[string]any, prop string) {
	req, _ := schema["required"].([]any)
	for _, r := range req {
		if s, ok := r.(string); ok && s == prop {
			return
		}
	}
	schema["required"] = append(req, prop)
}

// visitMaps invokes fn on every map within a decoded JSON value.
func visitMaps(v any, fn func(map[string]any)) {
	switch node := v.(type) {
	case map[string]any:
		fn(node)
		for _, child := range node {
			visitMaps(child, fn)
		}
	case []any:
		for _, child := range node {
			visitMaps(child, fn)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "specconvert:", err)
	os.Exit(1)
}
