package server

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// mcpToolSchema generates a JSON Schema for the Go type T and rewrites any
// nullable type arrays (e.g. ["null","object"]) into the equivalent anyOf
// form that all MCP clients accept.
//
// The MCP Go SDK's jsonschema inference represents Go pointers and slices as
// type arrays (["null", base]), which is valid JSON Schema but rejected by
// clients that expect type to be a single string.
func mcpToolSchema[T any]() (any, error) {
	rt := reflect.TypeFor[T]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	schema, err := jsonschema.ForType(rt, &jsonschema.ForOptions{})
	if err != nil {
		return nil, fmt.Errorf("infer schema for %v: %w", rt, err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal schema for %v: %w", rt, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal schema for %v: %w", rt, err)
	}
	fixNullableTypeArrays(raw)
	return raw, nil
}

// fixNullableTypeArrays recursively walks a JSON Schema map and converts any
// "type": ["null", X] into "anyOf": [{"type":"null"}, {type: X, ...}].
func fixNullableTypeArrays(node map[string]any) {
	if convertTypeArray(node) {
		return
	}

	if props, ok := node["properties"].(map[string]any); ok {
		for _, v := range props {
			if m, ok := v.(map[string]any); ok {
				fixNullableTypeArrays(m)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		fixNullableTypeArrays(items)
	}
	for _, key := range []string{"anyOf", "allOf", "oneOf", "prefixItems"} {
		if arr, ok := node[key].([]any); ok {
			for _, v := range arr {
				if m, ok := v.(map[string]any); ok {
					fixNullableTypeArrays(m)
				}
			}
		}
	}
	if ap, ok := node["additionalProperties"].(map[string]any); ok {
		fixNullableTypeArrays(ap)
	}
	if defs, ok := node["$defs"].(map[string]any); ok {
		for _, v := range defs {
			if m, ok := v.(map[string]any); ok {
				fixNullableTypeArrays(m)
			}
		}
	}
}

// convertTypeArray checks if node has "type" as an array containing "null"
// and one other type. If so it rewrites the node in-place to use anyOf and
// returns true. The caller should skip further recursion on the original node
// because it has been restructured; the function recurses into the non-null
// branch itself.
func convertTypeArray(node map[string]any) bool {
	typeVal, ok := node["type"]
	if !ok {
		return false
	}
	arr, ok := typeVal.([]any)
	if !ok || len(arr) != 2 {
		return false
	}

	var nullType, otherType string
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			return false
		}
		if s == "null" {
			nullType = s
		} else {
			otherType = s
		}
	}
	if nullType == "" || otherType == "" {
		return false
	}

	branch := make(map[string]any, len(node))
	for k, v := range node {
		if k != "type" {
			branch[k] = v
		}
	}
	branch["type"] = otherType

	for k := range node {
		delete(node, k)
	}
	node["anyOf"] = []any{
		map[string]any{"type": "null"},
		branch,
	}

	fixNullableTypeArrays(branch)
	return true
}
