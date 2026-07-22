package features

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/server"
	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var discardLogger = slog.New(slog.DiscardHandler)

type mcpTestContext struct {
	srv           *mcp.Server
	clientSession *mcp.ClientSession
	ctx           context.Context
	cancel        context.CancelFunc
	mockServer    *httptest.Server

	initResult *mcp.InitializeResult

	toolsResult     *mcp.ListToolsResult
	resourcesResult *mcp.ListResourcesResult
	promptsResult   *mcp.ListPromptsResult

	toolCallResult *mcp.CallToolResult
	toolCallErr    error

	resourceReadResult *mcp.ReadResourceResult
	resourceReadErr    error

	promptResult *mcp.GetPromptResult
	promptErr    error
}

func (tc *mcpTestContext) reset() {
	if tc.cancel != nil {
		tc.cancel()
	}
	if tc.mockServer != nil {
		tc.mockServer.Close()
	}
	*tc = mcpTestContext{}
}

func (tc *mcpTestContext) cleanup() {
	if tc.clientSession != nil {
		_ = tc.clientSession.Close()
	}
	if tc.cancel != nil {
		tc.cancel()
	}
	if tc.mockServer != nil {
		tc.mockServer.Close()
	}
}

func (tc *mcpTestContext) connect() error {
	tc.ctx, tc.cancel = context.WithTimeout(context.Background(), 10*time.Second)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := tc.srv.Connect(tc.ctx, serverTransport, nil)
	if err != nil {
		return fmt.Errorf("server.Connect failed: %w", err)
	}
	_ = serverSession

	client := mcp.NewClient(&mcp.Implementation{Name: "godog-test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(tc.ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("client.Connect failed: %w", err)
	}
	tc.clientSession = cs
	tc.initResult = cs.InitializeResult()
	return nil
}

// --- step definitions ---

func (tc *mcpTestContext) anMCPServerIsRunning() error {
	info := &server.ServerInfo{Build: "test-version", BuildDate: "2026-01-01", GitHash: "abc123"}
	tc.srv = server.New(info, discardLogger, nil)
	if err := server.RegisterHandlers(tc.srv, nil, info, discardLogger, evalhubclient.DefaultListPageLimit); err != nil {
		return fmt.Errorf("RegisterHandlers: %w", err)
	}
	return nil
}

func (tc *mcpTestContext) anMCPServerIsRunningWithABackend() error {
	tc.mockServer = httptest.NewServer(newMockEvalHubHandler())
	info := &server.ServerInfo{Build: "test-version", BuildDate: "2026-01-01", GitHash: "abc123"}
	client := evalhubclient.NewClient(tc.mockServer.URL)
	tc.srv = server.New(info, discardLogger, server.CompletionHandlerOption(client, discardLogger, evalhubclient.DefaultListPageLimit))
	if err := server.RegisterHandlers(tc.srv, client, info, discardLogger, evalhubclient.DefaultListPageLimit); err != nil {
		return fmt.Errorf("RegisterHandlers: %w", err)
	}
	return nil
}

func (tc *mcpTestContext) iConnectToTheMCPServer() error {
	return tc.connect()
}

func (tc *mcpTestContext) theServerNameShouldBe(expected string) error {
	if tc.initResult == nil {
		return fmt.Errorf("not connected to MCP server")
	}
	if tc.initResult.ServerInfo.Name != expected {
		return fmt.Errorf("server name = %q, want %q", tc.initResult.ServerInfo.Name, expected)
	}
	return nil
}

func (tc *mcpTestContext) theServerVersionShouldNotBeEmpty() error {
	if tc.initResult == nil {
		return fmt.Errorf("not connected to MCP server")
	}
	if tc.initResult.ServerInfo.Version == "" {
		return fmt.Errorf("server version is empty")
	}
	return nil
}

func (tc *mcpTestContext) theServerShouldAdvertiseCapability(capability string) error {
	if tc.initResult == nil {
		return fmt.Errorf("not connected to MCP server")
	}
	caps := tc.initResult.Capabilities
	switch capability {
	case "tools":
		if caps.Tools == nil {
			return fmt.Errorf("tools capability not advertised")
		}
	case "resources":
		if caps.Resources == nil {
			return fmt.Errorf("resources capability not advertised")
		}
	case "prompts":
		if caps.Prompts == nil {
			return fmt.Errorf("prompts capability not advertised")
		}
	case "logging":
		if caps.Logging == nil {
			return fmt.Errorf("logging capability not advertised")
		}
	default:
		return fmt.Errorf("unknown capability: %s", capability)
	}
	return nil
}

func (tc *mcpTestContext) ensureConnected() error {
	if tc.clientSession == nil {
		return tc.connect()
	}
	return nil
}

func (tc *mcpTestContext) iListTools() error {
	if err := tc.ensureConnected(); err != nil {
		return err
	}
	var err error
	tc.toolsResult, err = tc.clientSession.ListTools(tc.ctx, nil)
	if err != nil {
		return fmt.Errorf("ListTools failed: %w", err)
	}
	return nil
}

func (tc *mcpTestContext) theToolsListShouldContain(toolName string) error {
	if tc.toolsResult == nil {
		return fmt.Errorf("tools not listed yet")
	}
	for _, t := range tc.toolsResult.Tools {
		if t.Name == toolName {
			return nil
		}
	}
	return fmt.Errorf("tool %q not found in tools list", toolName)
}

func (tc *mcpTestContext) theToolsListShouldHaveLength(length int) error {
	if tc.toolsResult == nil {
		return fmt.Errorf("tools not listed yet")
	}
	if len(tc.toolsResult.Tools) != length {
		return fmt.Errorf("expected %d tools, got %d", length, len(tc.toolsResult.Tools))
	}
	return nil
}

func (tc *mcpTestContext) iListResources() error {
	if err := tc.ensureConnected(); err != nil {
		return err
	}
	var err error
	tc.resourcesResult, err = tc.clientSession.ListResources(tc.ctx, nil)
	if err != nil {
		return fmt.Errorf("ListResources failed: %w", err)
	}
	return nil
}

func (tc *mcpTestContext) theResourcesListShouldContainURI(uri string) error {
	if tc.resourcesResult == nil {
		return fmt.Errorf("resources not listed yet")
	}
	for _, r := range tc.resourcesResult.Resources {
		if r.URI == uri {
			return nil
		}
	}
	return fmt.Errorf("resource URI %q not found in resources list", uri)
}

func (tc *mcpTestContext) theResourcesListShouldHaveLength(length int) error {
	if tc.resourcesResult == nil {
		return fmt.Errorf("resources not listed yet")
	}
	if len(tc.resourcesResult.Resources) != length {
		return fmt.Errorf("expected %d resources, got %d", length, len(tc.resourcesResult.Resources))
	}
	return nil
}

func (tc *mcpTestContext) iListPrompts() error {
	if err := tc.ensureConnected(); err != nil {
		return err
	}
	var err error
	tc.promptsResult, err = tc.clientSession.ListPrompts(tc.ctx, nil)
	if err != nil {
		return fmt.Errorf("ListPrompts failed: %w", err)
	}
	return nil
}

func (tc *mcpTestContext) thePromptsListShouldContain(promptName string) error {
	if tc.promptsResult == nil {
		return fmt.Errorf("prompts not listed yet")
	}
	for _, p := range tc.promptsResult.Prompts {
		if p.Name == promptName {
			return nil
		}
	}
	return fmt.Errorf("prompt %q not found in prompts list", promptName)
}

func (tc *mcpTestContext) thePromptsListShouldHaveLength(length int) error {
	if tc.promptsResult == nil {
		return fmt.Errorf("prompts not listed yet")
	}
	if len(tc.promptsResult.Prompts) != length {
		return fmt.Errorf("expected %d prompts, got %d", length, len(tc.promptsResult.Prompts))
	}
	return nil
}

// --- tool steps ---

func (tc *mcpTestContext) iCallTheToolWithArguments(toolName string, body *godog.DocString) error {
	if err := tc.ensureConnected(); err != nil {
		return err
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(body.Content), &args); err != nil {
		return fmt.Errorf("invalid JSON arguments: %w", err)
	}
	tc.toolCallResult, tc.toolCallErr = tc.clientSession.CallTool(tc.ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	return nil
}

func (tc *mcpTestContext) theToolCallShouldSucceed() error {
	if tc.toolCallErr != nil {
		return fmt.Errorf("tool call failed: %w", tc.toolCallErr)
	}
	if tc.toolCallResult != nil && tc.toolCallResult.IsError {
		text := extractToolResultText(tc.toolCallResult)
		return fmt.Errorf("tool call returned error: %s", text)
	}
	return nil
}

func (tc *mcpTestContext) theToolCallShouldReturnAnError() error {
	if tc.toolCallErr == nil && (tc.toolCallResult == nil || !tc.toolCallResult.IsError) {
		return fmt.Errorf("expected tool call to return an error, but it succeeded")
	}
	return nil
}

func (tc *mcpTestContext) theToolResultShouldContain(expected string) error {
	text := extractToolResultText(tc.toolCallResult)
	if !strings.Contains(text, expected) {
		return fmt.Errorf("tool result does not contain %q, got: %s", expected, text)
	}
	return nil
}

// --- resource steps ---

func (tc *mcpTestContext) iReadTheResource(uri string) error {
	if err := tc.ensureConnected(); err != nil {
		return err
	}
	tc.resourceReadResult, tc.resourceReadErr = tc.clientSession.ReadResource(tc.ctx, &mcp.ReadResourceParams{URI: uri})
	return nil
}

func (tc *mcpTestContext) theResourceReadShouldSucceed() error {
	if tc.resourceReadErr != nil {
		return fmt.Errorf("resource read failed: %w", tc.resourceReadErr)
	}
	return nil
}

func (tc *mcpTestContext) theResourceReadShouldFail() error {
	if tc.resourceReadErr == nil {
		return fmt.Errorf("expected resource read to fail, but it succeeded")
	}
	return nil
}

func (tc *mcpTestContext) theResourceResponseShouldContain(expected string) error {
	if tc.resourceReadResult == nil || len(tc.resourceReadResult.Contents) == 0 {
		return fmt.Errorf("no resource contents returned")
	}
	text := tc.resourceReadResult.Contents[0].Text
	if !strings.Contains(text, expected) {
		return fmt.Errorf("resource response does not contain %q, got: %s", expected, text)
	}
	return nil
}

func (tc *mcpTestContext) theResourceResponseShouldBeAJSONArrayWithAtLeastNItems(minItems int) error {
	if tc.resourceReadResult == nil || len(tc.resourceReadResult.Contents) == 0 {
		return fmt.Errorf("no resource contents returned")
	}
	text := tc.resourceReadResult.Contents[0].Text
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(text), &arr); err != nil {
		return fmt.Errorf("resource response is not a JSON array: %w", err)
	}
	if len(arr) < minItems {
		return fmt.Errorf("expected at least %d items, got %d", minItems, len(arr))
	}
	return nil
}

// --- prompt steps ---

func (tc *mcpTestContext) iGetThePromptWithArguments(promptName string, table *godog.Table) error {
	if err := tc.ensureConnected(); err != nil {
		return err
	}
	args := make(map[string]string)
	for _, row := range table.Rows[1:] {
		key := row.Cells[0].Value
		value := row.Cells[1].Value
		args[key] = value
	}
	tc.promptResult, tc.promptErr = tc.clientSession.GetPrompt(tc.ctx, &mcp.GetPromptParams{
		Name:      promptName,
		Arguments: args,
	})
	return nil
}

func (tc *mcpTestContext) thePromptShouldSucceed() error {
	if tc.promptErr != nil {
		return fmt.Errorf("prompt failed: %w", tc.promptErr)
	}
	return nil
}

func (tc *mcpTestContext) thePromptShouldFail() error {
	if tc.promptErr == nil {
		return fmt.Errorf("expected prompt to fail, but it succeeded")
	}
	return nil
}

func (tc *mcpTestContext) thePromptResultShouldHaveAtLeastNMessages(minMessages int) error {
	if tc.promptResult == nil {
		return fmt.Errorf("no prompt result")
	}
	if len(tc.promptResult.Messages) < minMessages {
		return fmt.Errorf("expected at least %d messages, got %d", minMessages, len(tc.promptResult.Messages))
	}
	return nil
}

// --- helpers ---

func extractToolResultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// --- scenario initializer ---

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := &mcpTestContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tc.reset()
		return ctx, nil
	})

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		tc.cleanup()
		return ctx, nil
	})

	// Server setup
	ctx.Step(`^an MCP server is running$`, tc.anMCPServerIsRunning)
	ctx.Step(`^an MCP server is running with a backend$`, tc.anMCPServerIsRunningWithABackend)
	ctx.Step(`^I connect to the MCP server$`, tc.iConnectToTheMCPServer)

	// Server metadata
	ctx.Step(`^the server name should be "([^"]*)"$`, tc.theServerNameShouldBe)
	ctx.Step(`^the server version should not be empty$`, tc.theServerVersionShouldNotBeEmpty)
	ctx.Step(`^the server should advertise (\w+) capability$`, tc.theServerShouldAdvertiseCapability)

	// List capabilities
	ctx.Step(`^I list tools$`, tc.iListTools)
	ctx.Step(`^the tools list should contain "([^"]*)"$`, tc.theToolsListShouldContain)
	ctx.Step(`^the tools list should have length (\d+)$`, tc.theToolsListShouldHaveLength)

	ctx.Step(`^I list resources$`, tc.iListResources)
	ctx.Step(`^the resources list should contain URI "([^"]*)"$`, tc.theResourcesListShouldContainURI)
	ctx.Step(`^the resources list should have length (\d+)$`, tc.theResourcesListShouldHaveLength)

	ctx.Step(`^I list prompts$`, tc.iListPrompts)
	ctx.Step(`^the prompts list should contain "([^"]*)"$`, tc.thePromptsListShouldContain)
	ctx.Step(`^the prompts list should have length (\d+)$`, tc.thePromptsListShouldHaveLength)

	// Tool calls
	ctx.Step(`^I call the tool "([^"]*)" with arguments:$`, tc.iCallTheToolWithArguments)
	ctx.Step(`^the tool call should succeed$`, tc.theToolCallShouldSucceed)
	ctx.Step(`^the tool call should return an error$`, tc.theToolCallShouldReturnAnError)
	ctx.Step(`^the tool result should contain "([^"]*)"$`, tc.theToolResultShouldContain)

	// Resource reads
	ctx.Step(`^I read the resource "([^"]*)"$`, tc.iReadTheResource)
	ctx.Step(`^the resource read should succeed$`, tc.theResourceReadShouldSucceed)
	ctx.Step(`^the resource read should fail$`, tc.theResourceReadShouldFail)
	ctx.Step(`^the resource response should contain "([^"]*)"$`, tc.theResourceResponseShouldContain)
	ctx.Step(`^the resource response should be a JSON array with at least (\d+) item$`, tc.theResourceResponseShouldBeAJSONArrayWithAtLeastNItems)

	// Prompts
	ctx.Step(`^I get the prompt "([^"]*)" with arguments:$`, tc.iGetThePromptWithArguments)
	ctx.Step(`^the prompt should succeed$`, tc.thePromptShouldSucceed)
	ctx.Step(`^the prompt should fail$`, tc.thePromptShouldFail)
	ctx.Step(`^the prompt result should have at least (\d+) message$`, tc.thePromptResultShouldHaveAtLeastNMessages)
}
