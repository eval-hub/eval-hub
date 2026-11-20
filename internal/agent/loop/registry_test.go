package loop

import (
	"context"
	"encoding/json"
	"errors"
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
	localReference := testTool(handler)
	localReference.Definition.Function.Parameters = json.RawMessage(`{"type":"object","id":"#scope","definitions":{"value":{"type":"string"}},"properties":{"value":{"$ref":"#/definitions/value"}}}`)
	if _, err := NewRegistry(localReference); err != nil {
		t.Fatalf("NewRegistry() rejected a fragment-only identifier with an in-document reference: %v", err)
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
		{name: "external schema reference in dependency", tools: []Tool{func() Tool {
			v := testTool(handler)
			v.Definition.Function.Parameters = json.RawMessage(`{"type":"object","dependencies":{"value":{"$ref":"https://example.invalid/schema.json"}}}`)
			return v
		}()}},
		{name: "external schema reference reached through local reference", tools: []Tool{func() Tool {
			v := testTool(handler)
			v.Definition.Function.Parameters = json.RawMessage(`{"type":"object","x":{"$ref":"https://example.invalid/schema.json"},"properties":{"value":{"$ref":"#/x"}}}`)
			return v
		}()}},
		{name: "absolute identifier with local reference", tools: []Tool{func() Tool {
			v := testTool(handler)
			v.Definition.Function.Parameters = json.RawMessage(`{"type":"object","id":"https://example.invalid/tool.json#scope","definitions":{"value":{"type":"string"}},"properties":{"value":{"$ref":"#/definitions/value"}}}`)
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
			err := (FunctionMetadata{Parameters: schema}).validateArguments(tc.arg, tc.max)
			if (err == nil) != tc.ok {
				t.Fatalf("FunctionMetadata.validateArguments() error = %v, want success %v", err, tc.ok)
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

func TestRegistryHandlesNilAndUnknownLookups(t *testing.T) {
	var registry *Registry
	if registry.Definitions() != nil {
		t.Fatal("nil registry returned definitions")
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Fatal("nil registry found a tool")
	}
	registry, err := NewRegistry(testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Fatal("registry found an unknown tool")
	}
	definitions := registry.Definitions()
	if len(definitions) != 1 || definitions[0].Function.Name != "list_catalog" {
		t.Fatalf("Definitions() = %+v", definitions)
	}
}

func TestNewRegistryRejectsInvalidTypeSchemaAndProgressLabel(t *testing.T) {
	handler := func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }
	tests := []struct {
		name   string
		mutate func(*Tool)
	}{
		{name: "wrong type", mutate: func(tool *Tool) { tool.Definition.Type = "http" }},
		{name: "invalid schema semantics", mutate: func(tool *Tool) {
			tool.Definition.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"v":{"type":"not-a-json-type"}}}`)
		}},
		{name: "newline in progress label", mutate: func(tool *Tool) { tool.ProgressLabel = "first\nsecond" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool := testTool(handler)
			tc.mutate(&tool)
			if _, err := NewRegistry(tool); err == nil {
				t.Fatal("NewRegistry() accepted an invalid tool")
			}
		})
	}
}

func TestLocalOnlySchemaLoaderRejectsExternalDocuments(t *testing.T) {
	loader := newLocalOnlySchemaLoader([]byte(`{"type":"object"}`))
	externalLoader := loader.LoaderFactory().New("https://example.invalid/schema.json")
	if _, err := externalLoader.LoadJSON(); !errors.Is(err, errExternalSchemaDocument) {
		t.Fatalf("external schema load error = %v, want %v", err, errExternalSchemaDocument)
	}
}

func TestHasExternalSchemaReferenceAllowsLocalRefsAndRejectsMalformedRefs(t *testing.T) {
	if hasExternalSchemaReference(json.RawMessage(`{"type":"object","id":"#scope","properties":{"id":{"type":"string"},"a":{"$ref":"#/definitions/a"}}}`)) {
		t.Fatal("local reference was rejected")
	}
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"type":"object","properties":{"a":{"$ref":12}}}`),
		json.RawMessage(`{"type":"object","properties":{"a":{"$ref":"schema.json"}}}`),
		json.RawMessage(`{"type":"object","dependencies":{"value":{"$ref":"https://example.invalid/schema.json"}}}`),
		json.RawMessage(`{"type":"object","$id":"https://example.invalid/tool.json#scope","properties":{"a":{"$ref":"#/definitions/a"}}}`),
		json.RawMessage(`{`),
	} {
		if !hasExternalSchemaReference(raw) {
			t.Errorf("unsafe or malformed reference accepted: %s", raw)
		}
	}
}

func TestExecuteToolNormalizesEmptyResultAndHidesHandlerErrors(t *testing.T) {
	tool := testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil })
	result, succeeded, err := executeTool(context.Background(), tool, `{"limit":1}`, 100, 100)
	if err != nil || !succeeded || string(result) != "null" {
		t.Fatalf("empty result = %s, succeeded=%v, error=%v", result, succeeded, err)
	}

	tool.Handler = func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("secret handler detail")
	}
	result, succeeded, err = executeTool(context.Background(), tool, `{"limit":1}`, 100, 100)
	if err != nil || succeeded || string(result) != `{"error":"tool_execution_failed"}` {
		t.Fatalf("handler failure = %s, succeeded=%v, error=%v", result, succeeded, err)
	}

	tool.Handler = func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{`), nil }
	if _, _, err := executeTool(context.Background(), tool, `{"limit":1}`, 100, 100); err == nil {
		t.Fatal("executeTool() accepted invalid handler JSON")
	}
}
