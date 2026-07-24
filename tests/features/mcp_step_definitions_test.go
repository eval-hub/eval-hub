package features

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Jeffail/gabs/v2"
	"github.com/PaesslerAG/jsonpath"
	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// InitializeMCPSteps registers MCP tool/resource/prompt step definitions.
func InitializeMCPSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	ctx.Step(`^I call MCP tool "([^"]*)" with arguments "([^"]*)"$`, tc.iCallMCPToolWithArguments)
	ctx.Step(`^I call MCP tool "([^"]*)" with arguments:$`, tc.iCallMCPToolWithInlineArguments)
	ctx.Step(`^the MCP tool call should succeed$`, tc.theMCPToolCallShouldSucceed)
	ctx.Step(`^the MCP tool call should fail$`, tc.theMCPToolCallShouldFail)
	ctx.Step(`^the MCP response should contain "([^"]*)"$`, tc.theMCPResponseShouldContain)
	ctx.Step(`^the MCP response should contain the value "([^"]*)" at path "([^"]*)"$`, tc.theMCPResponseShouldContainValueAtPath)
	ctx.Step(`^the MCP error should contain "([^"]*)"$`, tc.theMCPErrorShouldContain)
	ctx.Step(`^the "([^"]*)" field in the MCP response should be saved as "([^"]*)"$`, tc.theMCPFieldShouldBeSaved)
	ctx.Step(`^the MCP response array at path "([^"]*)" should have length (\d+)$`, tc.theMCPResponseArrayAtPathShouldHaveLength)
	ctx.Step(`^the MCP response array at path "([^"]*)" should have length at least (\d+)$`, tc.theMCPResponseArrayAtPathShouldHaveLengthAtLeast)

	// MCP JSONPath validation steps (with filter expression support)
	ctx.Step(`^the MCP response at JSONPath "(.+?)" should equal "(.+?)"$`, tc.theMCPResponseAtJSONPathShouldEqual)
	ctx.Step(`^the MCP response at JSONPath "(.+?)" should be an array$`, tc.theMCPResponseAtJSONPathShouldBeArray)
	ctx.Step(`^the MCP response at JSONPath "(.+?)" should not be empty$`, tc.theMCPResponseAtJSONPathShouldNotBeEmpty)
	ctx.Step(`^the MCP response at JSONPath "(.+?)" should have at least (\d+) items$`, tc.theMCPResponseAtJSONPathShouldHaveAtLeastNItems)

	// MCP Resource steps
	ctx.Step(`^I read MCP resource "([^"]*)"$`, tc.iReadMCPResource)
	ctx.Step(`^the MCP resource read should succeed$`, tc.theMCPResourceReadShouldSucceed)
	ctx.Step(`^the MCP resource read should fail$`, tc.theMCPResourceReadShouldFail)
	ctx.Step(`^the MCP resource should contain "([^"]*)"$`, tc.theMCPResourceShouldContain)
	ctx.Step(`^the MCP resource should contain the value "([^"]*)" at path "([^"]*)"$`, tc.theMCPResourceShouldContainValueAtPath)
	ctx.Step(`^the MCP resource error should contain "([^"]*)"$`, tc.theMCPResourceErrorShouldContain)

	// MCP Prompt steps
	ctx.Step(`^I get MCP prompt "([^"]*)" with arguments:$`, tc.iGetMCPPrompt)
	ctx.Step(`^the MCP prompt should succeed$`, tc.theMCPPromptShouldSucceed)
	ctx.Step(`^the MCP prompt should fail$`, tc.theMCPPromptShouldFail)
	ctx.Step(`^the MCP prompt should contain "([^"]*)"$`, tc.theMCPPromptShouldContain)
	ctx.Step(`^the MCP prompt error should contain "([^"]*)"$`, tc.theMCPPromptErrorShouldContain)
}

// --- MCP Step Definitions ---

// getMCPResultJSON converts MCP tool result to JSON bytes
func (tc *scenarioConfig) getMCPResultJSON() ([]byte, error) {
	if tc.mcpToolResult == nil {
		return nil, fmt.Errorf("no MCP tool result")
	}
	// For errors, prefer Content (which contains the error text) over StructuredContent
	if tc.mcpToolResult.IsError {
		if len(tc.mcpToolResult.Content) > 0 {
			return json.Marshal(tc.mcpToolResult.Content)
		}
	}
	// For success responses, prefer StructuredContent
	if tc.mcpToolResult.StructuredContent != nil {
		return json.Marshal(tc.mcpToolResult.StructuredContent)
	}
	if len(tc.mcpToolResult.Content) > 0 {
		return json.Marshal(tc.mcpToolResult.Content)
	}
	return nil, fmt.Errorf("MCP tool result has no content")
}

func (tc *scenarioConfig) iCallMCPToolWithArguments(toolName, argsJSON string) error {
	if tc.apiFeature.mcpClientSession == nil {
		return tc.logError(fmt.Errorf("MCP client session not initialized"))
	}

	// Substitute values ({{value:key}}) like HTTP steps do
	substitutedArgs, err := tc.substituteValues(argsJSON)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to substitute values in MCP args: %w", err))
	}

	ctx := context.Background()
	result, err := tc.apiFeature.mcpClientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: json.RawMessage(substitutedArgs),
	})

	tc.mcpToolResult = result
	tc.mcpError = err

	tc.logDebug("MCP tool %s called with args %s\n", toolName, substitutedArgs)
	if err != nil {
		tc.logDebug("MCP tool call error: %v\n", err)
	}
	if result != nil && result.IsError {
		tc.logDebug("MCP tool returned error result\n")
	}

	return nil
}

func (tc *scenarioConfig) iCallMCPToolWithInlineArguments(toolName string, argsJSON *godog.DocString) error {
	return tc.iCallMCPToolWithArguments(toolName, argsJSON.Content)
}

func (tc *scenarioConfig) theMCPToolCallShouldSucceed() error {
	if tc.mcpError != nil {
		return tc.logError(fmt.Errorf("expected MCP tool call to succeed but got error: %v", tc.mcpError))
	}
	if tc.mcpToolResult == nil {
		return tc.logError(fmt.Errorf("expected MCP tool result but got nil"))
	}
	if tc.mcpToolResult.IsError {
		// Serialize error content to JSON for better error messages
		errJSON, _ := json.MarshalIndent(tc.mcpToolResult.Content, "", "  ")
		return tc.logError(fmt.Errorf("expected MCP tool call to succeed but got error result: %s", string(errJSON)))
	}
	return nil
}

func (tc *scenarioConfig) theMCPToolCallShouldFail() error {
	if tc.mcpError == nil && (tc.mcpToolResult == nil || !tc.mcpToolResult.IsError) {
		return tc.logError(fmt.Errorf("expected MCP tool call to fail but it succeeded"))
	}

	// Log the error details for debugging
	if tc.mcpToolResult != nil && tc.mcpToolResult.IsError {
		errJSON, _ := json.MarshalIndent(tc.mcpToolResult.Content, "", "  ")
		tc.logDebug("MCP error response: %s\n", string(errJSON))
	}

	return nil
}

func (tc *scenarioConfig) theMCPResponseShouldContain(expected string) error {
	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return tc.logError(err)
	}

	resultStr := string(resultJSON)
	tc.logDebug("MCP response: %s\n", resultStr)

	if !strings.Contains(resultStr, expected) {
		return tc.logError(fmt.Errorf("expected MCP response to contain %q but got: %s", expected, resultStr))
	}
	return nil
}

func (tc *scenarioConfig) theMCPResponseShouldContainValueAtPath(expected, path string) error {
	// Substitute any {{value:key}} patterns in expected value
	expected, _ = tc.substituteValues(expected)

	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return tc.logError(err)
	}

	tc.logDebug("MCP response JSON for path check: %s\n", string(resultJSON))
	tc.logDebug("Looking for path: %s (converted to: %s)\n", path, getJsonPointer(path))

	// Parse JSON and extract value at path using gabs (same as HTTP version)
	jsonParsed, err := gabs.ParseJSON(resultJSON)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse MCP JSON response: %w", err))
	}

	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in MCP response\nJSON: %s", path, string(resultJSON)))
	}

	// Convert to string for comparison
	var actualValue string
	switch v := pathObj.Data().(type) {
	case string:
		actualValue = v
	case float64:
		actualValue = strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		actualValue = strconv.FormatBool(v)
	default:
		// For complex types, marshal to JSON
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return tc.logError(fmt.Errorf("failed to marshal value at path %s: %w", path, err))
		}
		actualValue = string(jsonBytes)
	}

	if actualValue != expected {
		return tc.logError(fmt.Errorf("expected MCP response at path %s to be %q but got %q", path, expected, actualValue))
	}

	return nil
}

func (tc *scenarioConfig) theMCPErrorShouldContain(expected string) error {
	// Check if there was a transport-level error
	if tc.mcpError != nil {
		errorStr := tc.mcpError.Error()
		tc.logDebug("MCP transport error: %s\n", errorStr)
		if !strings.Contains(errorStr, expected) {
			return tc.logError(fmt.Errorf("expected MCP error to contain %q but got: %s", expected, errorStr))
		}
		return nil
	}

	// Check if there was an MCP tool error result
	if tc.mcpToolResult == nil || !tc.mcpToolResult.IsError {
		return tc.logError(fmt.Errorf("expected MCP error result but got success or nil"))
	}

	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return tc.logError(err)
	}

	resultStr := string(resultJSON)
	tc.logDebug("MCP error response: %s\n", resultStr)

	if !strings.Contains(resultStr, expected) {
		return tc.logError(fmt.Errorf("expected MCP error to contain %q but got: %s", expected, resultStr))
	}
	return nil
}

func (tc *scenarioConfig) theMCPFieldShouldBeSaved(path, name string) error {
	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return tc.logError(err)
	}

	// Parse and extract field using gabs (same as HTTP version)
	jsonParsed, err := gabs.ParseJSON(resultJSON)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse MCP JSON response: %w", err))
	}

	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in MCP response", path))
	}

	finalResult, ok := pathObj.Data().(string)
	if !ok {
		if floatResult, ok := pathObj.Data().(float64); ok {
			finalResult = strconv.FormatFloat(floatResult, 'f', -1, 64)
		} else {
			return tc.logError(fmt.Errorf("expected %s to be a string or float64 but got %T", path, pathObj.Data()))
		}
	}

	if strings.HasPrefix(name, valuePrefix) {
		realName := strings.TrimPrefix(name, valuePrefix)
		tc.saveValue(realName, finalResult)

		// If saving job_id, also set lastId for compatibility with wait functions
		if path == "job_id" {
			tc.lastId = finalResult
			tc.values["id"] = finalResult
		}
	} else {
		return tc.logError(fmt.Errorf("unexpected value %s, should start with '%s'", name, valuePrefix))
	}

	return nil
}

func (tc *scenarioConfig) theMCPResponseArrayAtPathShouldHaveLength(jsonPath string, lengthStr string) error {
	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return tc.logError(err)
	}

	jsonParsed, err := gabs.ParseJSON(resultJSON)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse MCP JSON response: %w", err))
	}

	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(jsonPath))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in MCP response", jsonPath))
	}

	arr, ok := pathObj.Data().([]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("value at path %s is not an array in MCP response, got %T", jsonPath, pathObj.Data()))
	}

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer length, got %q: %w", lengthStr, err))
	}

	if len(arr) != length {
		return tc.logError(fmt.Errorf("expected array at path %s to have length %d, got %d", jsonPath, length, len(arr)))
	}
	return nil
}

func (tc *scenarioConfig) theMCPResponseArrayAtPathShouldHaveLengthAtLeast(jsonPath string, minLengthStr string) error {
	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return tc.logError(err)
	}

	jsonParsed, err := gabs.ParseJSON(resultJSON)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse MCP JSON response: %w", err))
	}

	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(jsonPath))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in MCP response", jsonPath))
	}

	arr, ok := pathObj.Data().([]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("value at path %s is not an array in MCP response, got %T", jsonPath, pathObj.Data()))
	}

	minLength, err := strconv.Atoi(minLengthStr)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer length, got %q: %w", minLengthStr, err))
	}

	if len(arr) < minLength {
		return tc.logError(fmt.Errorf("expected array at path %s to have length at least %d, got %d", jsonPath, minLength, len(arr)))
	}
	return nil
}

// MCP JSONPath validation steps (with filter expression support)

// Helper: Check if JSONPath uses filter or wildcard syntax
func jsonPathUsesFilterOrWildcard(jsonPath string) bool {
	return strings.Contains(jsonPath, "[?(") || strings.Contains(jsonPath, "[*]") || strings.Contains(jsonPath, "..")
}

// Helper: Conditionally unwrap single-element arrays from JSONPath filter results
func unwrapIfFilterResult(value interface{}, jsonPath string) interface{} {
	// Only unwrap if the JSONPath uses filter/wildcard syntax
	if !jsonPathUsesFilterOrWildcard(jsonPath) {
		return value
	}

	// Filter expressions return arrays - unwrap single-element results
	if arr, ok := value.([]interface{}); ok && len(arr) == 1 {
		return arr[0]
	}

	return value
}

// Helper: Extract value at JSONPath from MCP response (with substitution, escaping, and unwrapping)
func (tc *scenarioConfig) getMCPValueAtJSONPath(jsonPath string) (interface{}, error) {
	// Substitute any {{value:key}} patterns
	var err error
	jsonPath, err = tc.substituteValues(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("failed to substitute values in JSONPath: %w", err)
	}

	// Unescape quotes in JSONPath (from Gherkin escaping)
	jsonPath = strings.ReplaceAll(jsonPath, `\"`, `"`)

	resultJSON, err := tc.getMCPResultJSON()
	if err != nil {
		return nil, err
	}

	tc.logDebug("MCP response JSON for JSONPath: %s\n", string(resultJSON))
	tc.logDebug("JSONPath: %s\n", jsonPath)

	// Parse JSON to map
	var respMap map[string]interface{}
	if err := json.Unmarshal(resultJSON, &respMap); err != nil {
		return nil, fmt.Errorf("failed to parse MCP JSON response: %w", err)
	}

	// Ensure path starts with $
	path := jsonPath
	if !strings.HasPrefix(path, "$") {
		path = "$." + path
	}

	// Get value at JSONPath
	foundValue, err := jsonpath.Get(path, respMap)
	if err != nil {
		return nil, fmt.Errorf("JSONPath %s does not exist in MCP response: %w\nJSON: %s", jsonPath, err, string(resultJSON))
	}

	return foundValue, nil
}

func (tc *scenarioConfig) theMCPResponseAtJSONPathShouldEqual(jsonPath, expected string) error {
	// Substitute expected value
	var err error
	expected, err = tc.substituteValues(expected)
	if err != nil {
		return tc.logError(fmt.Errorf("failed to substitute values in expected: %w", err))
	}

	// Get value at JSONPath using helper
	foundValue, err := tc.getMCPValueAtJSONPath(jsonPath)
	if err != nil {
		return tc.logError(err)
	}

	// Conditionally unwrap single-element arrays from filter results
	foundValue = unwrapIfFilterResult(foundValue, jsonPath)

	// Convert to string for comparison
	var actualValue string
	switch v := foundValue.(type) {
	case string:
		actualValue = v
	case float64:
		actualValue = strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		actualValue = strconv.FormatBool(v)
	case nil:
		actualValue = ""
	default:
		// For complex types, marshal to JSON
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return tc.logError(fmt.Errorf("failed to marshal value at JSONPath %s: %w", jsonPath, err))
		}
		actualValue = string(jsonBytes)
	}

	if actualValue != expected {
		return tc.logError(fmt.Errorf("expected MCP response at JSONPath %s to equal %q but got %q", jsonPath, expected, actualValue))
	}

	return nil
}

func (tc *scenarioConfig) theMCPResponseAtJSONPathShouldBeArray(jsonPath string) error {
	foundValue, err := tc.getMCPValueAtJSONPath(jsonPath)
	if err != nil {
		return tc.logError(err)
	}

	// Conditionally unwrap single-element arrays from filter results
	foundValue = unwrapIfFilterResult(foundValue, jsonPath)

	if _, ok := foundValue.([]interface{}); !ok {
		return tc.logError(fmt.Errorf("value at JSONPath %s is not an array, got type %T with value: %v", jsonPath, foundValue, foundValue))
	}

	return nil
}

func (tc *scenarioConfig) theMCPResponseAtJSONPathShouldNotBeEmpty(jsonPath string) error {
	foundValue, err := tc.getMCPValueAtJSONPath(jsonPath)
	if err != nil {
		return tc.logError(err)
	}

	// Conditionally unwrap single-element arrays from filter results
	foundValue = unwrapIfFilterResult(foundValue, jsonPath)

	// Check if value is empty based on type
	switch v := foundValue.(type) {
	case string:
		if v == "" {
			return tc.logError(fmt.Errorf("value at JSONPath %s is empty string", jsonPath))
		}
	case []interface{}:
		if len(v) == 0 {
			return tc.logError(fmt.Errorf("array at JSONPath %s is empty", jsonPath))
		}
	case map[string]interface{}:
		if len(v) == 0 {
			return tc.logError(fmt.Errorf("object at JSONPath %s is empty", jsonPath))
		}
	case nil:
		return tc.logError(fmt.Errorf("value at JSONPath %s is null", jsonPath))
	}

	return nil
}

func (tc *scenarioConfig) theMCPResponseAtJSONPathShouldHaveAtLeastNItems(jsonPath string, minCountStr string) error {
	foundValue, err := tc.getMCPValueAtJSONPath(jsonPath)
	if err != nil {
		return tc.logError(err)
	}

	// Conditionally unwrap single-element arrays from filter results
	foundValue = unwrapIfFilterResult(foundValue, jsonPath)

	arr, ok := foundValue.([]interface{})
	if !ok {
		return tc.logError(fmt.Errorf("value at JSONPath %s is not an array, got type %T", jsonPath, foundValue))
	}

	minCount, err := strconv.Atoi(minCountStr)
	if err != nil {
		return tc.logError(fmt.Errorf("expected integer count, got %q: %w", minCountStr, err))
	}

	if len(arr) < minCount {
		return tc.logError(fmt.Errorf("array at JSONPath %s has %d items, expected at least %d", jsonPath, len(arr), minCount))
	}

	return nil
}

// MCP Resource steps
func (tc *scenarioConfig) iReadMCPResource(uri string) error {
	if tc.apiFeature.mcpClientSession == nil {
		return tc.logError(fmt.Errorf("MCP client session not initialized"))
	}

	// Substitute any {{value:key}} or {{env:VAR|default}} patterns in URI
	uri, _ = tc.substituteValues(uri)

	ctx := context.Background()
	result, err := tc.apiFeature.mcpClientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})

	tc.mcpResourceError = err
	if err == nil && len(result.Contents) > 0 {
		// Combine all text content from the resource
		var textParts []string
		for _, content := range result.Contents {
			textParts = append(textParts, content.Text)
		}
		tc.mcpResourceText = strings.Join(textParts, "\n")
		tc.logDebug("MCP resource content: %s\n", tc.mcpResourceText)
	}

	return nil
}

func (tc *scenarioConfig) theMCPResourceReadShouldSucceed() error {
	if tc.mcpResourceError != nil {
		return tc.logError(fmt.Errorf("expected MCP resource read to succeed but got error: %w", tc.mcpResourceError))
	}
	if tc.mcpResourceText == "" {
		return tc.logError(fmt.Errorf("expected MCP resource to have content but got empty text"))
	}
	return nil
}

func (tc *scenarioConfig) theMCPResourceReadShouldFail() error {
	if tc.mcpResourceError == nil {
		return tc.logError(fmt.Errorf("expected MCP resource read to fail but it succeeded"))
	}
	tc.logDebug("MCP resource error: %s\n", tc.mcpResourceError.Error())
	return nil
}

func (tc *scenarioConfig) theMCPResourceShouldContain(expected string) error {
	// Substitute any {{value:key}} patterns in expected value
	expected, _ = tc.substituteValues(expected)

	if tc.mcpResourceText == "" {
		return tc.logError(fmt.Errorf("no MCP resource text to check"))
	}
	if !strings.Contains(tc.mcpResourceText, expected) {
		return tc.logError(fmt.Errorf("expected MCP resource to contain %q but got: %s", expected, tc.mcpResourceText))
	}
	return nil
}

func (tc *scenarioConfig) theMCPResourceShouldContainValueAtPath(expected, path string) error {
	// Substitute any {{value:key}} patterns in expected value
	expected, _ = tc.substituteValues(expected)

	if tc.mcpResourceText == "" {
		return tc.logError(fmt.Errorf("no MCP resource text to check"))
	}

	tc.logDebug("MCP resource JSON for path check: %s\n", tc.mcpResourceText)
	tc.logDebug("Looking for path: %s (converted to: %s)\n", path, getJsonPointer(path))

	// Parse JSON and extract value at path using gabs
	jsonParsed, err := gabs.ParseJSON([]byte(tc.mcpResourceText))
	if err != nil {
		return tc.logError(fmt.Errorf("failed to parse MCP resource JSON: %w", err))
	}

	pathObj, err := jsonParsed.JSONPointer(getJsonPointer(path))
	if err != nil {
		return tc.logError(fmt.Errorf("path %v does not exist in MCP resource\nJSON: %s", path, tc.mcpResourceText))
	}

	// Convert to string for comparison
	var actualValue string
	switch v := pathObj.Data().(type) {
	case string:
		actualValue = v
	case float64:
		actualValue = strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		actualValue = strconv.FormatBool(v)
	default:
		// For complex types, marshal to JSON
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return tc.logError(fmt.Errorf("failed to marshal value at path %s: %w", path, err))
		}
		actualValue = string(jsonBytes)
	}

	if actualValue != expected {
		return tc.logError(fmt.Errorf("expected MCP resource at path %s to be %q but got %q", path, expected, actualValue))
	}

	return nil
}

func (tc *scenarioConfig) theMCPResourceErrorShouldContain(expected string) error {
	if tc.mcpResourceError == nil {
		return tc.logError(fmt.Errorf("expected MCP resource error but got success"))
	}
	errorStr := tc.mcpResourceError.Error()
	if !strings.Contains(errorStr, expected) {
		return tc.logError(fmt.Errorf("expected MCP resource error to contain %q but got: %s", expected, errorStr))
	}
	return nil
}

// MCP Prompt steps
func (tc *scenarioConfig) iGetMCPPrompt(name, argsJSON string) error {
	if tc.apiFeature.mcpClientSession == nil {
		return tc.logError(fmt.Errorf("MCP client session not initialized"))
	}

	// Parse arguments
	var args map[string]string
	if argsJSON != "" {
		argsJSON, _ = tc.substituteValues(argsJSON)
		var rawArgs map[string]interface{}
		if err := json.Unmarshal([]byte(argsJSON), &rawArgs); err != nil {
			return tc.logError(fmt.Errorf("failed to parse MCP prompt arguments JSON: %w", err))
		}
		// Convert to map[string]string as expected by GetPromptParams
		args = make(map[string]string)
		for k, v := range rawArgs {
			args[k] = fmt.Sprintf("%v", v)
		}
	}

	ctx := context.Background()
	result, err := tc.apiFeature.mcpClientSession.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      name,
		Arguments: args,
	})

	tc.mcpPromptError = err
	tc.mcpPromptResult = result

	if err != nil {
		tc.logDebug("MCP prompt error: %s\n", err.Error())
	} else if result != nil {
		resultJSON, _ := json.MarshalIndent(result, "", "  ")
		tc.logDebug("MCP prompt result: %s\n", string(resultJSON))
	}

	return nil
}

func (tc *scenarioConfig) theMCPPromptShouldSucceed() error {
	if tc.mcpPromptError != nil {
		return tc.logError(fmt.Errorf("expected MCP prompt to succeed but got error: %w", tc.mcpPromptError))
	}
	if tc.mcpPromptResult == nil {
		return tc.logError(fmt.Errorf("expected MCP prompt result but got nil"))
	}
	return nil
}

func (tc *scenarioConfig) theMCPPromptShouldFail() error {
	if tc.mcpPromptError == nil {
		return tc.logError(fmt.Errorf("expected MCP prompt to fail but it succeeded"))
	}
	tc.logDebug("MCP prompt error: %s\n", tc.mcpPromptError.Error())
	return nil
}

func (tc *scenarioConfig) theMCPPromptShouldContain(expected string) error {
	if tc.mcpPromptResult == nil {
		return tc.logError(fmt.Errorf("no MCP prompt result to check"))
	}

	// Combine all message content to search
	var allText []string
	for _, msg := range tc.mcpPromptResult.Messages {
		if textContent, ok := msg.Content.(*mcp.TextContent); ok {
			allText = append(allText, textContent.Text)
		}
	}
	fullText := strings.Join(allText, "\n")

	if !strings.Contains(fullText, expected) {
		return tc.logError(fmt.Errorf("expected MCP prompt to contain %q but got: %s", expected, fullText))
	}
	return nil
}

func (tc *scenarioConfig) theMCPPromptErrorShouldContain(expected string) error {
	if tc.mcpPromptError == nil {
		return tc.logError(fmt.Errorf("expected MCP prompt error but got success"))
	}
	errorStr := tc.mcpPromptError.Error()
	if !strings.Contains(errorStr, expected) {
		return tc.logError(fmt.Errorf("expected MCP prompt error to contain %q but got: %s", expected, errorStr))
	}
	return nil
}
