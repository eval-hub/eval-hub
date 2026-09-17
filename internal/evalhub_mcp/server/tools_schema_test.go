package server

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestFixNullableTypeArrays(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type":  []any{"null", "array"},
				"items": map[string]any{"type": "string"},
			},
			"pass_criteria": map[string]any{
				"type": []any{"null", "object"},
				"properties": map[string]any{
					"threshold": map[string]any{
						"type": []any{"null", "number"},
					},
				},
			},
			"name": map[string]any{
				"type": "string",
			},
		},
	}

	fixNullableTypeArrays(input)

	tags := input["properties"].(map[string]any)["tags"].(map[string]any)
	if _, ok := tags["type"]; ok {
		t.Error("tags should not have top-level 'type' after fix")
	}
	anyOf, ok := tags["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("tags.anyOf should have 2 branches, got %v", tags["anyOf"])
	}
	nullBranch := anyOf[0].(map[string]any)
	if nullBranch["type"] != "null" {
		t.Errorf("first branch should be null, got %v", nullBranch["type"])
	}
	arrayBranch := anyOf[1].(map[string]any)
	if arrayBranch["type"] != "array" {
		t.Errorf("second branch type should be 'array', got %v", arrayBranch["type"])
	}
	if _, ok := arrayBranch["items"]; !ok {
		t.Error("array branch should retain items")
	}

	pc := input["properties"].(map[string]any)["pass_criteria"].(map[string]any)
	pcAnyOf, ok := pc["anyOf"].([]any)
	if !ok || len(pcAnyOf) != 2 {
		t.Fatalf("pass_criteria.anyOf should have 2 branches, got %v", pc["anyOf"])
	}
	objBranch := pcAnyOf[1].(map[string]any)
	thresholdProp := objBranch["properties"].(map[string]any)["threshold"].(map[string]any)
	thAnyOf, ok := thresholdProp["anyOf"].([]any)
	if !ok || len(thAnyOf) != 2 {
		t.Fatalf("threshold.anyOf should have 2 branches, got %v", thresholdProp)
	}
	if thAnyOf[1].(map[string]any)["type"] != "number" {
		t.Errorf("threshold non-null branch type should be 'number', got %v", thAnyOf[1].(map[string]any)["type"])
	}

	name := input["properties"].(map[string]any)["name"].(map[string]any)
	if name["type"] != "string" {
		t.Errorf("name type should remain 'string', got %v", name["type"])
	}
}

func TestFixNullableTypeArraysNoOp(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
		"required": []any{"id"},
	}
	original, _ := json.Marshal(input)

	fixNullableTypeArrays(input)

	after, _ := json.Marshal(input)
	if string(original) != string(after) {
		t.Errorf("schema without type arrays should be unchanged\nbefore: %s\nafter:  %s", original, after)
	}
}

// TestMcpToolSchemasHaveNoTypeArrays is a regression guard: it registers all
// MCP tools and asserts that no property in any tool's inputSchema or
// outputSchema uses the array form of "type".
func TestMcpToolSchemasHaveNoTypeArrays(t *testing.T) {
	t.Parallel()

	ctx, cs := connectWithTools(t, &mockToolClient{})
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range result.Tools {
		for _, label := range []string{"inputSchema", "outputSchema"} {
			var schemaVal any
			if label == "inputSchema" {
				schemaVal = tool.InputSchema
			} else {
				schemaVal = tool.OutputSchema
			}
			if schemaVal == nil {
				continue
			}
			data, err := json.Marshal(schemaVal)
			if err != nil {
				t.Errorf("tool %q %s: marshal: %v", tool.Name, label, err)
				continue
			}
			var schema map[string]any
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Errorf("tool %q %s: unmarshal: %v", tool.Name, label, err)
				continue
			}
			if violations := findTypeArrays("", schema); len(violations) > 0 {
				for _, v := range violations {
					t.Errorf("tool %q %s: %s has array-form type (should use anyOf)", tool.Name, label, v)
				}
			}
		}
	}
}

// findTypeArrays walks a JSON Schema map and returns paths where "type" is an array.
func findTypeArrays(path string, node map[string]any) []string {
	var violations []string
	if t, ok := node["type"]; ok {
		if _, isArr := t.([]any); isArr {
			violations = append(violations, fmt.Sprintf("%stype", path))
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for k, v := range props {
			if m, ok := v.(map[string]any); ok {
				violations = append(violations, findTypeArrays(fmt.Sprintf("%sproperties.%s.", path, k), m)...)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		violations = append(violations, findTypeArrays(path+"items.", items)...)
	}
	for _, key := range []string{"anyOf", "allOf", "oneOf"} {
		if arr, ok := node[key].([]any); ok {
			for i, v := range arr {
				if m, ok := v.(map[string]any); ok {
					violations = append(violations, findTypeArrays(fmt.Sprintf("%s%s[%d].", path, key, i), m)...)
				}
			}
		}
	}
	if ap, ok := node["additionalProperties"].(map[string]any); ok {
		violations = append(violations, findTypeArrays(path+"additionalProperties.", ap)...)
	}
	return violations
}
