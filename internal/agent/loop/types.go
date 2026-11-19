// Package loop provides the transport-independent, bounded agent loop used by
// runner workloads.
package loop

import (
	"context"
	"encoding/json"
	"time"
)

// ModelRequest is one non-streaming chat-completions request. MaxOutputTokens
// is resolved by the runner from the remaining per-job budget.
type ModelRequest struct {
	Model           string
	Messages        []Message
	Tools           ToolDefinitions
	MaxOutputTokens int
	ReasoningEffort string
}

// ModelResponse is the assistant turn returned by a model client.
type ModelResponse struct {
	Message      Message
	FinishReason string
	Usage        *TokenUsage
}

// TokenUsage carries provider-reported token counts. Presence flags distinguish
// omitted values from a reported zero; the loop estimates any omitted counts.
type TokenUsage struct {
	PromptTokens            int
	CompletionTokens        int
	TotalTokens             int
	PromptTokensPresent     bool
	CompletionTokensPresent bool
	TotalTokensPresent      bool
}

// ModelClient makes one model request. Implementations should make one HTTP
// attempt per call and must not add credentials; the runtime sidecar owns auth.
type ModelClient interface {
	Complete(context.Context, ModelRequest) (ModelResponse, error)
}

// Role is a chat message role supported by the tool-calling protocol.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a bounded chat-history item. ToolCallID is set only on tool
// replies; ToolCalls is set only on assistant messages.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is the function-call envelope returned by OpenAI-compatible APIs.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the model-supplied name and raw JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition is the only tool information exposed to the model.
type ToolDefinition struct {
	Type     string           `json:"type"`
	Function FunctionMetadata `json:"function"`
}

// ToolDefinitions is a collection of model-facing tool definitions.
type ToolDefinitions []ToolDefinition

// FunctionMetadata is a trusted tool's public model-facing definition.
type FunctionMetadata struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolHandler executes a validated, schema-conforming JSON object.
type ToolHandler func(context.Context, json.RawMessage) (json.RawMessage, error)

// Tool is trusted code registered with a runner. ProgressLabel is a short,
// non-sensitive description used in lifecycle events; it must not contain
// request, argument, or result data.
type Tool struct {
	Definition    ToolDefinition
	ProgressLabel string
	Handler       ToolHandler
}

// ToolRegistry exposes definitions and trusted handlers to the loop.
// Implementations must expose a consistent, immutable tool set through both
// methods for the lifetime of any Runner constructed with them.
type ToolRegistry interface {
	Definitions() ToolDefinitions
	Lookup(string) (Tool, bool)
}

// EventKind identifies a trusted runner lifecycle event.
type EventKind string

const (
	EventModelCallStarted   EventKind = "model_call_started"
	EventModelCallCompleted EventKind = "model_call_completed"
	EventToolCallStarted    EventKind = "tool_call_started"
	EventToolCallCompleted  EventKind = "tool_call_completed"
)

// ProgressEvent contains bounded, runner-authored progress. It never contains
// model prose, prompts, tool arguments, tool results, URLs, or credentials.
type ProgressEvent struct {
	At       time.Time
	Kind     EventKind
	Call     int
	ToolName string
	Outcome  string
	Summary  string
}

// ProgressFunc receives trusted, bounded lifecycle events.
type ProgressFunc func(ProgressEvent)

// Input is the request-specific context for one loop execution.
type Input struct {
	Model           string
	SystemPrompt    string
	UserPrompt      string
	ReasoningEffort string
	Budget          Budget
}

// Result contains the final model response and accounting metadata. The caller
// is responsible for parsing and validating the domain-specific proposal.
type Result struct {
	Final           string
	ModelCalls      int
	ToolCalls       int
	AccountedTokens int64
}
