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

func TestFixNullableTypeArraysAllOfOneOf(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"allOf": []any{
			map[string]any{
				"type": []any{"null", "object"},
				"properties": map[string]any{
					"x": map[string]any{"type": "string"},
				},
			},
		},
		"oneOf": []any{
			map[string]any{
				"type": []any{"null", "number"},
			},
		},
	}

	fixNullableTypeArrays(input)

	allOf := input["allOf"].([]any)
	branch := allOf[0].(map[string]any)
	if _, ok := branch["anyOf"]; !ok {
		t.Error("allOf branch should be converted to anyOf")
	}

	oneOf := input["oneOf"].([]any)
	branch2 := oneOf[0].(map[string]any)
	if _, ok := branch2["anyOf"]; !ok {
		t.Error("oneOf branch should be converted to anyOf")
	}
}

func TestFixNullableTypeArraysPrefixItems(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": "array",
		"prefixItems": []any{
			map[string]any{
				"type": []any{"null", "integer"},
			},
		},
	}

	fixNullableTypeArrays(input)

	pi := input["prefixItems"].([]any)
	branch := pi[0].(map[string]any)
	anyOf, ok := branch["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("prefixItems branch should be converted to anyOf, got %v", branch)
	}
	if anyOf[1].(map[string]any)["type"] != "integer" {
		t.Errorf("prefixItems non-null branch should be integer, got %v", anyOf[1])
	}
}

func TestFixNullableTypeArraysAdditionalProperties(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"type": []any{"null", "string"},
		},
	}

	fixNullableTypeArrays(input)

	ap := input["additionalProperties"].(map[string]any)
	anyOf, ok := ap["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("additionalProperties should be converted to anyOf, got %v", ap)
	}
	if anyOf[1].(map[string]any)["type"] != "string" {
		t.Errorf("non-null branch should be string, got %v", anyOf[1])
	}
}

func TestFixNullableTypeArraysDefs(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"type": "object",
		"$defs": map[string]any{
			"Score": map[string]any{
				"type": []any{"null", "object"},
				"properties": map[string]any{
					"value": map[string]any{"type": "number"},
				},
			},
		},
	}

	fixNullableTypeArrays(input)

	score := input["$defs"].(map[string]any)["Score"].(map[string]any)
	anyOf, ok := score["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("$defs entry should be converted to anyOf, got %v", score)
	}
	objBranch := anyOf[1].(map[string]any)
	if objBranch["type"] != "object" {
		t.Errorf("non-null branch type should be 'object', got %v", objBranch["type"])
	}
	if _, ok := objBranch["properties"]; !ok {
		t.Error("non-null branch should retain properties")
	}
}

func TestConvertTypeArrayEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("no type key", func(t *testing.T) {
		node := map[string]any{"description": "no type here"}
		if convertTypeArray(node) {
			t.Error("should return false when no type key")
		}
	})

	t.Run("single string type", func(t *testing.T) {
		node := map[string]any{"type": "string"}
		if convertTypeArray(node) {
			t.Error("should return false for single string type")
		}
	})

	t.Run("three element array", func(t *testing.T) {
		node := map[string]any{"type": []any{"null", "string", "number"}}
		if convertTypeArray(node) {
			t.Error("should return false for 3-element type array")
		}
	})

	t.Run("non-string element in array", func(t *testing.T) {
		node := map[string]any{"type": []any{"null", 42}}
		if convertTypeArray(node) {
			t.Error("should return false for non-string element")
		}
	})

	t.Run("both elements are null", func(t *testing.T) {
		node := map[string]any{"type": []any{"null", "null"}}
		if convertTypeArray(node) {
			t.Error("should return false when otherType is empty")
		}
	})

	t.Run("neither element is null", func(t *testing.T) {
		node := map[string]any{"type": []any{"string", "number"}}
		if convertTypeArray(node) {
			t.Error("should return false when nullType is empty")
		}
	})
}

func TestMcpToolSchemaProducesValidSchema(t *testing.T) {
	t.Parallel()

	schema, err := mcpToolSchema[SubmitEvaluationInput]()
	if err != nil {
		t.Fatalf("mcpToolSchema: %v", err)
	}
	m, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", schema)
	}
	if _, ok := m["properties"]; !ok {
		t.Error("schema should have properties")
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
	for _, key := range []string{"anyOf", "allOf", "oneOf", "prefixItems"} {
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
	if defs, ok := node["$defs"].(map[string]any); ok {
		for k, v := range defs {
			if m, ok := v.(map[string]any); ok {
				violations = append(violations, findTypeArrays(fmt.Sprintf("%s$defs.%s.", path, k), m)...)
			}
		}
	}
	return violations
}
