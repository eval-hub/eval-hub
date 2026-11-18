package loop

import (
	"context"
	"encoding/json"
	"testing"
)

func testTool(handler ToolHandler) Tool {
	return Tool{
		Definition: ToolDefinition{Type: "function", Function: FunctionMetadata{
			Name: "list_catalog", Description: "List visible catalog entries.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["limit"],"additionalProperties":false}`),
		}},
		ProgressLabel: "Catalog lookup", Handler: handler,
	}
}

func TestNewRegistryValidatesDefinitions(t *testing.T) {
	handler := func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`[]`), nil }
	if _, err := NewRegistry(testTool(handler)); err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	cases := []struct {
		name  string
		tools []Tool
	}{
		{name: "invalid name", tools: []Tool{func() Tool { v := testTool(handler); v.Definition.Function.Name = "bad name"; return v }()}},
		{name: "duplicate", tools: []Tool{testTool(handler), testTool(handler)}},
		{name: "missing handler", tools: []Tool{testTool(nil)}},
		{name: "invalid schema", tools: []Tool{func() Tool { v := testTool(handler); v.Definition.Function.Parameters = json.RawMessage(`{`); return v }()}},
		{name: "non-object schema", tools: []Tool{func() Tool {
			v := testTool(handler)
			v.Definition.Function.Parameters = json.RawMessage(`{"type":"string"}`)
			return v
		}()}},
		{name: "external schema reference", tools: []Tool{func() Tool {
			v := testTool(handler)
			v.Definition.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"value":{"$ref":"https://example.invalid/schema.json"}}}`)
			return v
		}()}},
		{name: "long progress label", tools: []Tool{func() Tool { v := testTool(handler); v.ProgressLabel = string(make([]byte, 81)); return v }()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRegistry(tc.tools...); err == nil {
				t.Fatal("NewRegistry() succeeded")
			}
		})
	}
}

func TestValidateArgumentsRequiresObjectAndSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1}},"required":["limit"],"additionalProperties":false}`)
	tests := []struct {
		name string
		arg  string
		max  int
		ok   bool
	}{
		{name: "valid", arg: `{"limit":2}`, max: 100, ok: true},
		{name: "wrong type", arg: `{"limit":"2"}`, max: 100},
		{name: "unknown property", arg: `{"limit":2,"other":true}`, max: 100},
		{name: "array", arg: `[]`, max: 100},
		{name: "trailing JSON", arg: `{"limit":2} {}`, max: 100},
		{name: "too large", arg: `{"limit":2}`, max: 2},
		{name: "empty", arg: "", max: 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArguments(schema, tc.arg, tc.max)
			if (err == nil) != tc.ok {
				t.Fatalf("validateArguments() error = %v, want success %v", err, tc.ok)
			}
		})
	}
}

func TestRegistryReturnsDetachedDefinitions(t *testing.T) {
	registry, err := NewRegistry(testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	definitions[0].Function.Parameters[0] = 'x'
	tool, ok := registry.Lookup("list_catalog")
	if !ok || tool.Definition.Function.Parameters[0] == 'x' {
		t.Fatal("caller mutation changed the registered definition")
	}
	tool.Definition.Function.Parameters[1] = 'y'
	toolAgain, ok := registry.Lookup("list_catalog")
	if !ok || toolAgain.Definition.Function.Parameters[1] == 'y' {
		t.Fatal("Lookup() exposed mutable registry schema bytes")
	}
}
