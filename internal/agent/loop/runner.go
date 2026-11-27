package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xeipuuv/gojsonschema"
)

var (
	ErrBudgetExceeded       = errors.New("agent generation budget exhausted")
	ErrInvalidModelResponse = errors.New("model returned an unsupported response")
	ErrModelCallFailed      = errors.New("model request failed")
	ErrToolRegistryInvalid  = errors.New("tool registry is invalid")
	ErrToolExecutionFailed  = errors.New("tool execution failed")
)

// Config bounds all model-controlled and tool-controlled data retained by the
// loop. MaxRetries counts additional attempts and each attempt consumes a model
// call from the job budget.
type Config struct {
	HardLimits            HardLimits
	MaxRetries            int
	RetryBackoff          time.Duration
	MaxToolBatchSize      int
	MaxToolArgumentBytes  int
	MaxToolResultBytes    int
	MaxHistoryBytes       int
	MaxModelResponseBytes int
	MaxActionSummaryBytes int
	Progress              ProgressFunc
}

// DefaultConfig returns conservative bounds for catalog-oriented tool use.
func DefaultConfig() Config {
	return Config{
		HardLimits:            DefaultHardLimits(),
		MaxRetries:            2,
		RetryBackoff:          100 * time.Millisecond,
		MaxToolBatchSize:      8,
		MaxToolArgumentBytes:  32 * 1024,
		MaxToolResultBytes:    4 * 1024,
		MaxHistoryBytes:       1024 * 1024,
		MaxModelResponseBytes: 1024 * 1024,
		MaxActionSummaryBytes: 160,
	}
}

// Runner executes model turns and trusted registered tools. It contains no
// collection-specific behavior and does not log prompts or tool data.
type Runner struct {
	model       ModelClient
	registry    ToolRegistry
	definitions ToolDefinitions
	config      Config
}

// NewRunner validates dependencies and bounds, and snapshots the tool definitions.
func NewRunner(model ModelClient, registry ToolRegistry, config Config) (*Runner, error) {
	if model == nil || registry == nil {
		return nil, errors.New("model client and tool registry are required")
	}
	if config.HardLimits == (HardLimits{}) {
		config.HardLimits = DefaultHardLimits()
	}
	if err := config.HardLimits.validate(); err != nil || config.HardLimits.exceeds(DefaultHardLimits()) {
		return nil, errors.New("agent hard limits are invalid")
	}
	if config.MaxRetries < 0 || config.MaxRetries > 5 || config.RetryBackoff < 0 || config.RetryBackoff > 5*time.Second || config.MaxToolBatchSize <= 0 || config.MaxToolArgumentBytes <= 0 || config.MaxToolResultBytes <= 0 || config.MaxHistoryBytes <= 0 || config.MaxModelResponseBytes <= 0 || config.MaxActionSummaryBytes <= 0 {
		return nil, errors.New("agent loop limits must be positive (retries may be zero)")
	}
	definitions := registry.Definitions()
	if len(definitions) > config.MaxToolBatchSize*16 {
		return nil, errors.New("tool registry exceeds the configured limit")
	}
	definitions = cloneDefinitions(definitions)
	definitionBytes, err := json.Marshal(definitions)
	if err != nil || len(definitionBytes) > config.MaxHistoryBytes {
		return nil, errors.New("tool definitions exceed the configured history limit")
	}
	if err := definitions.validate(); err != nil {
		return nil, ErrToolRegistryInvalid
	}
	return &Runner{model: model, registry: registry, definitions: definitions, config: config}, nil
}

// Run executes one bounded agent session. Budget values should be resolved and
// snapshotted by the trusted job admission path before this call.
func (r *Runner) Run(parent context.Context, input Input) (Result, error) {
	if parent == nil || strings.TrimSpace(input.Model) == "" || strings.TrimSpace(input.SystemPrompt) == "" || strings.TrimSpace(input.UserPrompt) == "" {
		return Result{}, errors.New("context, model, system prompt, and user prompt are required")
	}
	if err := input.Budget.validateAgainst(r.config.HardLimits); err != nil {
		return Result{}, ErrBudgetExceeded
	}
	ctx, cancel := context.WithTimeout(parent, input.Budget.MaxDuration)
	defer cancel()

	definitions := r.definitions
	messages := []Message{
		{Role: RoleSystem, Content: input.SystemPrompt},
		{Role: RoleSystem, Content: budgetNotice(input.Budget.MaxModelCalls, input.Budget.MaxIterations, input.Budget.MaxToolCalls, input.Budget.MaxTotalTokens)},
		{Role: RoleUser, Content: input.UserPrompt},
	}
	if historySize(messages) > r.config.MaxHistoryBytes {
		return Result{}, ErrInvalidModelResponse
	}

	seenCallIDs := make(map[string]struct{})
	result := Result{}
	for iteration := 0; iteration < input.Budget.MaxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return Result{}, budgetOrContextError(parent, err)
		}
		response, promptEstimate, err := r.completeTurn(ctx, input, iteration, definitions, messages, &result)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, budgetOrContextError(parent, ctxErr)
		}
		if err != nil {
			return Result{}, err
		}
		outputEstimate, err := responseTokenEstimate(response)
		if err != nil {
			return Result{}, ErrInvalidModelResponse
		}
		charge, err := accountedUsage(promptEstimate, outputEstimate, response.Usage)
		if err != nil || result.AccountedTokens > input.Budget.MaxTotalTokens-charge {
			return Result{}, ErrBudgetExceeded
		}
		result.AccountedTokens += charge

		if err := response.validate(r.config.MaxModelResponseBytes, r.config.MaxToolBatchSize); err != nil {
			return Result{}, ErrInvalidModelResponse
		}
		assistant := response.Message
		assistant.Role = RoleAssistant
		messages = append(messages, assistant)
		if historySize(messages) > r.config.MaxHistoryBytes {
			return Result{}, ErrInvalidModelResponse
		}

		if len(assistant.ToolCalls) == 0 {
			if strings.TrimSpace(assistant.Content) == "" {
				return Result{}, ErrInvalidModelResponse
			}
			result.Final = assistant.Content
			return result, nil
		}

		registered, err := r.validateToolBatch(assistant.ToolCalls, seenCallIDs)
		if err != nil {
			return Result{}, ErrInvalidModelResponse
		}
		// A tool batch is not dispatched unless its maximum accepted results still
		// leave room for a later model call to produce the required final response.
		if result.ModelCalls >= input.Budget.MaxModelCalls || iteration+1 >= input.Budget.MaxIterations || result.AccountedTokens >= input.Budget.MaxTotalTokens {
			return Result{}, ErrBudgetExceeded
		}
		if result.ToolCalls > input.Budget.MaxToolCalls-len(assistant.ToolCalls) {
			return Result{}, ErrBudgetExceeded
		}
		if !r.canFitFinalTurnAfterTools(input, iteration, definitions, messages, assistant.ToolCalls, result) {
			return Result{}, ErrBudgetExceeded
		}
		for i, call := range assistant.ToolCalls {
			if err := ctx.Err(); err != nil {
				return Result{}, budgetOrContextError(parent, err)
			}
			tool := registered[i]
			label := tool.ProgressLabel
			if label == "" {
				label = "Registered read-only tool"
			}
			r.emit(EventToolCallStarted, result.ModelCalls, tool.Definition.Function.Name, "started", label+" started")
			if err := ctx.Err(); err != nil {
				return Result{}, budgetOrContextError(parent, err)
			}
			toolResult, succeeded, executeErr := executeTool(ctx, tool, call.Function.Arguments, r.config.MaxToolArgumentBytes, r.config.MaxToolResultBytes)
			if executeErr != nil {
				r.emit(EventToolCallCompleted, result.ModelCalls, tool.Definition.Function.Name, "failed", "Registered tool result was rejected")
				return Result{}, ErrToolExecutionFailed
			}
			outcome := "completed"
			content := string(toolResult)
			if !succeeded {
				outcome = "failed"
				content = `{"error":"tool_execution_failed"}`
			}
			messages = append(messages, Message{Role: RoleTool, Name: tool.Definition.Function.Name, ToolCallID: call.ID, Content: content})
			result.ToolCalls++
			seenCallIDs[call.ID] = struct{}{}
			summary := label + " returned"
			if outcome == "failed" {
				summary = label + " failed safely"
			}
			r.emit(EventToolCallCompleted, result.ModelCalls, tool.Definition.Function.Name, outcome, summary)
			if historySize(messages) > r.config.MaxHistoryBytes {
				return Result{}, ErrInvalidModelResponse
			}
		}
	}
	return Result{}, ErrBudgetExceeded
}

func (r *Runner) canFitFinalTurnAfterTools(input Input, iteration int, definitions ToolDefinitions, messages []Message, calls []ToolCall, result Result) bool {
	remainingTokens := input.Budget.MaxTotalTokens - result.AccountedTokens
	if remainingTokens <= 0 || len(messages) < 2 {
		return false
	}
	projectedMessages := cloneMessages(messages)
	projectedMessages[1].Content = budgetNotice(
		input.Budget.MaxModelCalls-result.ModelCalls,
		input.Budget.MaxIterations-iteration-1,
		input.Budget.MaxToolCalls-result.ToolCalls-len(calls),
		remainingTokens,
	)
	minimumFinalTokens, err := minimumFinalResponseTokens()
	if err != nil || remainingTokens <= minimumFinalTokens {
		return false
	}
	_, fits := promptTokenEstimateAfterToolResults(
		projectedMessages,
		definitions,
		calls,
		r.config.MaxToolResultBytes,
		remainingTokens-minimumFinalTokens,
	)
	return fits
}

// promptTokenEstimateAfterToolResults estimates the serialized prompt after
// appending one worst-case result for each tool call. It adds each message's
// encoded size instead of allocating all synthetic result bodies at once.
func promptTokenEstimateAfterToolResults(messages []Message, tools ToolDefinitions, calls []ToolCall, maxResultBytes int, maxPromptTokens int64) (int64, bool) {
	estimate, err := promptTokenEstimate(messages, tools)
	if err != nil || maxPromptTokens < 0 || estimate > maxPromptTokens {
		return estimate, false
	}

	contentSize := worstCaseToolResultEncodedSize(maxResultBytes)
	messageCount := len(messages)
	for _, call := range calls {
		messageSize, err := projectedToolMessageSize(call, contentSize)
		if err != nil || messageSize > math.MaxInt64-33 {
			return 0, false
		}
		added := messageSize + 32 // promptTokenEstimate's per-message framing allowance
		if messageCount > 0 {
			added++ // comma in the messages JSON array
		}
		if added > maxPromptTokens-estimate {
			return estimate, false
		}
		estimate += added
		messageCount++
	}
	return estimate, true
}

func projectedToolMessageSize(call ToolCall, encodedContentSize int64) (int64, error) {
	message := Message{
		Role: RoleTool, Name: call.Function.Name, ToolCallID: call.ID, Content: "x",
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return 0, err
	}
	// The marker content "x" serializes to three bytes including its quotes.
	const markerSize = int64(3)
	baseSize := int64(len(encoded)) - markerSize
	if encodedContentSize > math.MaxInt64-baseSize {
		return math.MaxInt64, nil
	}
	return baseSize + encodedContentSize, nil
}

// worstCaseToolResultEncodedSize returns the size of the larger JSON-encoded
// content string: a valid JSON result within maxBytes or the fixed safe handler
// failure result. encoding/json escapes each '<' to six bytes when embedding
// the result in a chat message.
func worstCaseToolResultEncodedSize(maxBytes int) int64 {
	failureSize := int64(jsonStringSize(toolExecutionFailedResult))
	var validResultSize int64
	switch {
	case maxBytes == 1:
		// A one-byte JSON scalar such as 0, embedded as a JSON string.
		validResultSize = 3
	case maxBytes >= 2:
		if int64(maxBytes) > math.MaxInt64/6 {
			validResultSize = math.MaxInt64
		} else {
			validResultSize = int64(maxBytes)*6 - 6
		}
	}
	return max(failureSize, validResultSize)
}

func minimumFinalResponseTokens() (int64, error) {
	return responseTokenEstimate(ModelResponse{Message: Message{Role: RoleAssistant, Content: "x"}})
}

// worstCaseToolResult returns the larger serialized form of a valid JSON result
// at the configured byte limit and the fixed handler-failure result. The
// ordinary result uses '<' characters because encoding/json HTML-escapes each
// to six bytes when embedded as a JSON string.
func worstCaseToolResult(maxBytes int) string {
	if maxBytes < 2 {
		return toolExecutionFailedResult
	}
	result := `"` + strings.Repeat("<", maxBytes-2) + `"`
	if jsonStringSize(toolExecutionFailedResult) > jsonStringSize(result) {
		return toolExecutionFailedResult
	}
	return result
}

func jsonStringSize(value string) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return math.MaxInt
	}
	return len(encoded)
}

type retryable interface {
	Retryable() bool
}

func (r *Runner) completeTurn(ctx context.Context, input Input, iteration int, definitions ToolDefinitions, messages []Message, result *Result) (ModelResponse, int64, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return ModelResponse{}, 0, err
		}
		if result.ModelCalls >= input.Budget.MaxModelCalls {
			return ModelResponse{}, 0, ErrBudgetExceeded
		}
		remainingCalls := input.Budget.MaxModelCalls - result.ModelCalls
		remainingIterations := input.Budget.MaxIterations - iteration
		remainingTools := input.Budget.MaxToolCalls - result.ToolCalls
		remainingTokens := input.Budget.MaxTotalTokens - result.AccountedTokens
		outputLimit := min(input.Budget.MaxOutputTokens, int(min(remainingTokens, int64(math.MaxInt))))
		messages[1].Content = budgetNotice(remainingCalls, remainingIterations, remainingTools, remainingTokens)
		promptEstimate, err := promptTokenEstimate(messages, definitions)
		if err != nil || promptEstimate >= remainingTokens {
			return ModelResponse{}, 0, ErrBudgetExceeded
		}
		outputLimit = min(outputLimit, int(min(remainingTokens-promptEstimate, int64(math.MaxInt))))
		if outputLimit <= 0 {
			return ModelResponse{}, 0, ErrBudgetExceeded
		}
		request := ModelRequest{
			Model: input.Model, Messages: cloneMessages(messages), Tools: cloneDefinitions(definitions),
			MaxOutputTokens: outputLimit, ReasoningEffort: input.ReasoningEffort,
		}
		result.ModelCalls++
		r.emit(EventModelCallStarted, result.ModelCalls, "", "started", "Model request started")
		response, callErr := r.model.Complete(ctx, request)
		if callErr == nil {
			r.emit(EventModelCallCompleted, result.ModelCalls, "", "completed", "Model response received")
			return response, promptEstimate, nil
		}
		r.emit(EventModelCallCompleted, result.ModelCalls, "", "failed", "Model request failed")
		if attempt >= r.config.MaxRetries || result.ModelCalls >= input.Budget.MaxModelCalls || !isRetryable(callErr) {
			return ModelResponse{}, 0, ErrModelCallFailed
		}
		// A failed request may still have reached the upstream model and incurred
		// usage. Reserve its full prompt plus output allowance before any retry so
		// repeated failures cannot reuse the same total-token budget.
		result.AccountedTokens += promptEstimate + int64(outputLimit)
		if err := waitContext(ctx, r.config.RetryBackoff*time.Duration(1<<min(attempt, 6))); err != nil {
			return ModelResponse{}, 0, budgetOrContextError(ctx, err)
		}
	}
}

func (r *Runner) validateToolBatch(calls []ToolCall, seen map[string]struct{}) ([]Tool, error) {
	if len(calls) == 0 || len(calls) > r.config.MaxToolBatchSize {
		return nil, ErrInvalidModelResponse
	}
	ids := make(map[string]struct{}, len(calls))
	tools := make([]Tool, len(calls))
	for i, call := range calls {
		if call.Type != "function" || call.ID == "" || len(call.ID) > 256 || call.Function.Name == "" {
			return nil, ErrInvalidModelResponse
		}
		if _, ok := seen[call.ID]; ok {
			return nil, ErrInvalidModelResponse
		}
		if _, ok := ids[call.ID]; ok {
			return nil, ErrInvalidModelResponse
		}
		ids[call.ID] = struct{}{}
		tool, ok := r.registry.Lookup(call.Function.Name)
		if !ok {
			return nil, ErrInvalidModelResponse
		}
		if err := tool.Definition.Function.validateArguments(call.Function.Arguments, r.config.MaxToolArgumentBytes); err != nil {
			return nil, ErrInvalidModelResponse
		}
		tools[i] = tool
	}
	return tools, nil
}

func (r *Runner) emit(kind EventKind, call int, toolName, outcome, summary string) {
	if r.config.Progress == nil {
		return
	}
	if len(summary) > r.config.MaxActionSummaryBytes {
		summary = summary[:r.config.MaxActionSummaryBytes]
	}
	r.config.Progress(ProgressEvent{At: time.Now().UTC(), Kind: kind, Call: call, ToolName: toolName, Outcome: outcome, Summary: summary})
}

func (d ToolDefinition) validate() error {
	name := d.Function.Name
	if d.Type != "function" || !toolNamePattern.MatchString(name) || len(d.Function.Parameters) == 0 || !json.Valid(d.Function.Parameters) {
		return ErrToolRegistryInvalid
	}
	var schema struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(d.Function.Parameters, &schema) != nil || schema.Type != "object" {
		return ErrToolRegistryInvalid
	}
	if hasExternalSchemaReference(d.Function.Parameters) {
		return ErrToolRegistryInvalid
	}
	if _, err := gojsonschema.NewSchema(newLocalOnlySchemaLoader(d.Function.Parameters)); err != nil {
		return ErrToolRegistryInvalid
	}
	return nil
}

func (definitions ToolDefinitions) validate() error {
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if err := definition.validate(); err != nil {
			return err
		}
		name := definition.Function.Name
		if _, ok := seen[name]; ok {
			return ErrToolRegistryInvalid
		}
		seen[name] = struct{}{}
	}
	return nil
}

func (response ModelResponse) validate(maxBytes, maxBatch int) error {
	if response.Message.Role != "" && response.Message.Role != RoleAssistant {
		return ErrInvalidModelResponse
	}
	messageBytes, err := json.Marshal(response.Message)
	if err != nil || len(messageBytes) > maxBytes || len(response.Message.ToolCalls) > maxBatch {
		return ErrInvalidModelResponse
	}
	if response.FinishReason == "length" || response.FinishReason == "content_filter" || (len(response.Message.ToolCalls) > 0 && response.FinishReason != "" && response.FinishReason != "tool_calls") || (len(response.Message.ToolCalls) == 0 && response.FinishReason == "tool_calls") {
		return ErrInvalidModelResponse
	}
	for _, call := range response.Message.ToolCalls {
		if len(call.Function.Arguments) > maxBytes || len(call.ID) > 256 || len(call.Function.Name) > 64 {
			return ErrInvalidModelResponse
		}
	}
	if response.Usage != nil && ((response.Usage.PromptTokensPresent && response.Usage.PromptTokens < 0) || (response.Usage.CompletionTokensPresent && response.Usage.CompletionTokens < 0) || (response.Usage.TotalTokensPresent && response.Usage.TotalTokens < 0)) {
		return ErrInvalidModelResponse
	}
	return nil
}

func promptTokenEstimate(messages []Message, tools ToolDefinitions) (int64, error) {
	wire := struct {
		Messages []Message       `json:"messages"`
		Tools    ToolDefinitions `json:"tools,omitempty"`
	}{messages, tools}
	data, err := json.Marshal(wire)
	if err != nil {
		return 0, err
	}
	// Byte length is a conservative lower-level bound for text tokenizers. The
	// fixed allowance accounts for message/tool framing tokens.
	framing := int64(len(messages)+len(tools)+1) * 32
	return int64(len(data)) + framing, nil
}

func responseTokenEstimate(response ModelResponse) (int64, error) {
	data, err := json.Marshal(response.Message)
	if err != nil {
		return 0, err
	}
	return int64(len(data)) + 32, nil
}

func accountedUsage(inputEstimate, outputEstimate int64, usage *TokenUsage) (int64, error) {
	if inputEstimate < 0 || outputEstimate < 0 {
		return 0, ErrInvalidModelResponse
	}
	input, output := inputEstimate, outputEstimate
	if usage != nil {
		if usage.PromptTokensPresent {
			input = max(input, int64(usage.PromptTokens))
		}
		if usage.CompletionTokensPresent {
			output = max(output, int64(usage.CompletionTokens))
		}
	}
	total := input + output
	if usage != nil && usage.TotalTokensPresent {
		total = max(total, int64(usage.TotalTokens))
	}
	if total < input || total < output {
		return 0, fmt.Errorf("token accounting overflow")
	}
	return total, nil
}

func budgetNotice(remainingCalls, remainingIterations, remainingTools int, remainingTokens int64) string {
	return fmt.Sprintf("Runner budget guidance (runner-enforced): at most %d model calls remain, %d loop turns remain, and %d tool calls remain; estimated token budget remaining is %d. These limits are advisory to you but are enforced by the runner. Prefer a concise final proposal as soon as you have enough evidence.", remainingCalls, remainingIterations, remainingTools, remainingTokens)
}

func historySize(messages []Message) int {
	data, err := json.Marshal(messages)
	if err != nil {
		return math.MaxInt
	}
	return len(data)
}

func cloneMessages(in []Message) []Message {
	out := make([]Message, len(in))
	copy(out, in)
	for i := range out {
		out[i].ToolCalls = append([]ToolCall(nil), in[i].ToolCalls...)
	}
	return out
}

func cloneDefinitions(in ToolDefinitions) ToolDefinitions {
	out := make(ToolDefinitions, len(in))
	copy(out, in)
	for i := range out {
		out[i].Function.Parameters = append([]byte(nil), in[i].Function.Parameters...)
	}
	return out
}

func isRetryable(err error) bool {
	var retryErr retryable
	return errors.As(err, &retryErr) && retryErr.Retryable()
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func budgetOrContextError(parent context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
		return ErrBudgetExceeded
	}
	return err
}
