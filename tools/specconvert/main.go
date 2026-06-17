// Command specconvert downconverts an OpenAPI 3.1 document to a 3.0-compatible
// one so that oapi-codegen (which does not yet support 3.1) can process it.
//
// It performs the minimal set of transformations our spec needs:
//   - openapi: 3.1.x      -> 3.0.3
//   - anyOf/oneOf:[S,{type:null}] -> S + nullable:true (FastAPI optional fields)
//   - type:[X,"null"]     -> type:X + nullable:true
//   - contentMediaType    -> format:binary (3.1 binary upload encoding)
//
// Usage: specconvert <in.json> <out.json>
package main

import (
	"encoding/json"
	"fmt"
	"os"
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
		if t == "null" {
			m["nullable"] = true
			continue
		}
		nonNull = append(nonNull, t)
	}
	if len(nonNull) > 0 {
		m["type"] = nonNull[0] // OAS 3.0 allows a single type only
	}
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
	return ok && len(m) == 1 && m["type"] == "null"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "specconvert:", err)
	os.Exit(1)
}
