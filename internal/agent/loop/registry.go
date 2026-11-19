package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var errExternalSchemaDocument = errors.New("external schema documents are not supported")

const toolExecutionFailedResult = `{"error":"tool_execution_failed"}`

// localOnlyJSONLoader keeps schema compilation from using gojsonschema's
// default file and HTTP loaders when a local reference points outside the
// root document.
type localOnlyJSONLoader struct {
	gojsonschema.JSONLoader
}

func (l localOnlyJSONLoader) LoaderFactory() gojsonschema.JSONLoaderFactory {
	return localOnlyJSONLoaderFactory{}
}

type localOnlyJSONLoaderFactory struct{}

func (localOnlyJSONLoaderFactory) New(source string) gojsonschema.JSONLoader {
	return rejectingExternalJSONLoader{localOnlyJSONLoader{gojsonschema.NewReferenceLoader(source)}}
}

type rejectingExternalJSONLoader struct {
	localOnlyJSONLoader
}

func (rejectingExternalJSONLoader) LoadJSON() (interface{}, error) {
	return nil, errExternalSchemaDocument
}

func newLocalOnlySchemaLoader(raw []byte) gojsonschema.JSONLoader {
	return localOnlyJSONLoader{gojsonschema.NewBytesLoader(raw)}
}

// Registry is an immutable set of trusted tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry validates definitions and creates an immutable registry.
func NewRegistry(tools ...Tool) (*Registry, error) {
	registry := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, tool := range tools {
		name := tool.Definition.Function.Name
		if !toolNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid tool name")
		}
		if _, exists := registry.tools[name]; exists {
			return nil, fmt.Errorf("duplicate tool name")
		}
		if tool.Definition.Type != "function" || tool.Handler == nil {
			return nil, fmt.Errorf("tool definition and handler are required")
		}
		if len(tool.Definition.Function.Parameters) == 0 || !json.Valid(tool.Definition.Function.Parameters) {
			return nil, fmt.Errorf("tool parameters must be a valid JSON schema")
		}
		var schema struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(tool.Definition.Function.Parameters, &schema); err != nil || schema.Type != "object" {
			return nil, fmt.Errorf("tool parameters schema must describe an object")
		}
		if hasExternalSchemaReference(tool.Definition.Function.Parameters) {
			return nil, fmt.Errorf("tool parameters schema cannot reference external resources")
		}
		if _, err := gojsonschema.NewSchema(newLocalOnlySchemaLoader(tool.Definition.Function.Parameters)); err != nil {
			return nil, fmt.Errorf("invalid tool parameters schema")
		}
		if len(tool.ProgressLabel) > 80 || strings.ContainsAny(tool.ProgressLabel, "\r\n") {
			return nil, fmt.Errorf("tool progress label must be a short single-line string")
		}
		tool.Definition.Function.Parameters = bytes.Clone(tool.Definition.Function.Parameters)
		registry.tools[name] = tool
	}
	return registry, nil
}

// Definitions returns detached copies so callers cannot mutate the registry.
func (r *Registry) Definitions() ToolDefinitions {
	if r == nil {
		return nil
	}
	out := make(ToolDefinitions, 0, len(r.tools))
	for _, tool := range r.tools {
		definition := tool.Definition
		definition.Function.Parameters = bytes.Clone(definition.Function.Parameters)
		out = append(out, definition)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Function.Name < out[j].Function.Name })
	return out
}

// Lookup returns a registered tool by exact name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	if r == nil {
		return Tool{}, false
	}
	tool, ok := r.tools[name]
	tool.Definition.Function.Parameters = bytes.Clone(tool.Definition.Function.Parameters)
	return tool, ok
}

func (f FunctionMetadata) validateArguments(raw string, maxBytes int) error {
	if maxBytes <= 0 || len(raw) == 0 || len(raw) > maxBytes {
		return errors.New("tool arguments are empty or exceed the configured limit")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return errors.New("tool arguments are not valid JSON")
	}
	if _, ok := value.(map[string]any); !ok {
		return errors.New("tool arguments must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("tool arguments contain trailing data")
	}
	result, err := gojsonschema.Validate(newLocalOnlySchemaLoader(f.Parameters), gojsonschema.NewStringLoader(raw))
	if err != nil || result == nil || !result.Valid() {
		return errors.New("tool arguments do not match the registered schema")
	}
	return nil
}

// hasExternalSchemaReference rejects schema references or identifier scopes
// that could cause the validator to load another document. Tool schemas are
// trusted and fixed; fragment references remain useful when no identifier
// changes their base document.
func hasExternalSchemaReference(raw json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return true
	}
	var visitSchema func(any) bool
	visitSchema = func(node any) bool {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				if key == "id" || key == "$id" {
					if identifierCanLoadExternalDocument(child) {
						return true
					}
				}
				if key == "$ref" {
					reference, ok := child.(string)
					if !ok || !strings.HasPrefix(reference, "#") {
						return true
					}
				}
			}
			for key, child := range typed {
				switch key {
				case "properties", "patternProperties", "definitions", "$defs", "dependencies":
					// These objects map arbitrary property/definition names to
					// schemas. Do not interpret a property literally named "id"
					// as the schema identifier keyword.
					if schemas, ok := child.(map[string]any); ok {
						for _, schema := range schemas {
							// A dependency can also be a list of property names; those
							// strings are harmless to visit and schema-valued
							// dependencies are traversed recursively.
							if visitSchema(schema) {
								return true
							}
						}
					}
				case "items", "additionalItems", "additionalProperties", "not", "allOf", "anyOf", "oneOf", "contains", "propertyNames", "if", "then", "else":
					if visitSchema(child) {
						return true
					}
				}
			}
		case []any:
			for _, child := range typed {
				if visitSchema(child) {
					return true
				}
			}
		}
		return false
	}
	return visitSchema(value)
}

func identifierCanLoadExternalDocument(value any) bool {
	identifier, ok := value.(string)
	if !ok {
		return true
	}
	reference, err := url.Parse(identifier)
	return err != nil || reference.Scheme != "" || reference.Host != "" || reference.Path != "" || reference.RawQuery != "" || reference.Opaque != ""
}

// Execute runs a trusted handler and returns only valid bounded JSON to the
// model. Handler error details are deliberately not returned to the model.
func executeTool(ctx context.Context, tool Tool, arguments string, maxArgumentBytes, maxResultBytes int) (json.RawMessage, bool, error) {
	if err := tool.Definition.Function.validateArguments(arguments, maxArgumentBytes); err != nil {
		return nil, false, err
	}
	result, err := tool.Handler(ctx, json.RawMessage(arguments))
	if err != nil {
		return json.RawMessage(toolExecutionFailedResult), false, nil
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	if len(result) > maxResultBytes {
		return nil, true, errors.New("tool result exceeds the configured limit")
	}
	if !json.Valid(result) {
		return nil, true, errors.New("tool returned invalid JSON")
	}
	return bytes.Clone(result), true, nil
}
