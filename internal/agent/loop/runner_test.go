package loop

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeModel struct {
	responses []ModelResponse
	errors    []error
	requests  []ModelRequest
	index     int
}

func (m *fakeModel) Complete(_ context.Context, request ModelRequest) (ModelResponse, error) {
	request.Messages = cloneMessages(request.Messages)
	request.Tools = cloneDefinitions(request.Tools)
	m.requests = append(m.requests, request)
	index := m.index
	m.index++
	if index < len(m.errors) && m.errors[index] != nil {
		return ModelResponse{}, m.errors[index]
	}
	if index >= len(m.responses) {
		return ModelResponse{}, errors.New("fake has no response")
	}
	return m.responses[index], nil
}

type retryError struct{}

func (retryError) Error() string   { return "retryable" }
func (retryError) Retryable() bool { return true }

func testBudget() Budget {
	return Budget{
		Limits: Limits{
			MaxIterations: 4,
			MaxModelCalls: 5,
			MaxToolCalls:  8,
			// Match the default standard-effort preset used for normal tool-call tests.
			MaxTotalTokens:  131_072,
			MaxOutputTokens: 2_048,
			MaxDuration:     time.Minute,
		},
	}
}

func finalResponse(content string) ModelResponse {
	return ModelResponse{Message: Message{Role: RoleAssistant, Content: content}, FinishReason: "stop"}
}

func callResponse(calls ...ToolCall) ModelResponse {
	return ModelResponse{Message: Message{Role: RoleAssistant, ToolCalls: calls}, FinishReason: "tool_calls"}
}

func validCall(id, name string) ToolCall {
	return ToolCall{ID: id, Type: "function", Function: FunctionCall{Name: name, Arguments: `{"limit":2}`}}
}

func newTestRunner(t *testing.T, model ModelClient, registry ToolRegistry, cfg Config) *Runner {
	t.Helper()
	runner, err := NewRunner(model, registry, cfg)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func newTestRegistry(t *testing.T, handler ToolHandler) *Registry {
	t.Helper()
	registry, err := NewRegistry(testTool(handler))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func testInput() Input {
	return Input{Model: "catalog-model", SystemPrompt: "You are a catalog assistant.", UserPrompt: "Find matching benchmarks.", Budget: testBudget()}
}

func TestRunnerCompletesWithoutToolsAndAccountsMissingUsage(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{finalResponse(`{"selected_benchmarks":[]}`)}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[]`), nil
	}), DefaultConfig())
	result, err := runner.Run(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final != `{"selected_benchmarks":[]}` || result.ModelCalls != 1 || result.AccountedTokens <= 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(model.requests[0].Messages[1].Content, "model calls remain") || model.requests[0].MaxOutputTokens > testInput().Budget.MaxOutputTokens {
		t.Fatalf("request did not include the advisory budget: %+v", model.requests[0])
	}
}

func TestRunnerRejectsContentFilteredFinalResponse(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{{
		Message:      Message{Role: RoleAssistant, Content: "partially filtered response"},
		FinishReason: "content_filter",
	}}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[]`), nil
	}), DefaultConfig())

	result, err := runner.Run(context.Background(), testInput())
	if !errors.Is(err, ErrInvalidModelResponse) {
		t.Fatalf("Run() error = %v, want %v", err, ErrInvalidModelResponse)
	}
	if result.Final != "" {
		t.Fatalf("Run() returned filtered content as a successful final result: %+v", result)
	}
}

func TestRunnerExecutesToolCallsAndCorrelatesReplies(t *testing.T) {
	var executions int
	registry := newTestRegistry(t, func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		executions++
		if string(args) != `{"limit":2}` {
			t.Fatalf("handler args = %s", args)
		}
		return json.RawMessage(`[{"id":"b1"}]`), nil
	})
	model := &fakeModel{responses: []ModelResponse{
		callResponse(validCall("call-1", "list_catalog")),
		finalResponse("ready"),
	}}
	runner := newTestRunner(t, model, registry, DefaultConfig())
	result, err := runner.Run(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if executions != 1 || result.ToolCalls != 1 || result.ModelCalls != 2 || result.Final != "ready" {
		t.Fatalf("unexpected result: result=%+v executions=%d", result, executions)
	}
	second := model.requests[1].Messages
	if len(second) < 5 || second[len(second)-1].Role != RoleTool || second[len(second)-1].ToolCallID != "call-1" || second[len(second)-1].Name != "list_catalog" {
		t.Fatalf("tool reply did not correlate to its call: %+v", second)
	}
}

func TestRunnerCanExecuteToolWithLowEffortBudget(t *testing.T) {
	executions := 0
	model := &fakeModel{responses: []ModelResponse{
		callResponse(validCall("call-1", "list_catalog")),
		finalResponse("ready"),
	}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(`null`), nil
	}), DefaultConfig())
	input := testInput()
	input.Budget = DefaultPresets()[EffortLow]
	result, err := runner.Run(context.Background(), input)
	if err != nil || result.Final != "ready" || executions != 1 {
		t.Fatalf("Run() = %+v, %v; handler executions=%d, want a successful low-effort tool call", result, err, executions)
	}
}

func TestWorstCaseToolResultIncludesSmallLimitHandlerFailure(t *testing.T) {
	result := worstCaseToolResult(7)
	if result != toolExecutionFailedResult {
		t.Fatalf("worstCaseToolResult(7) = %q, want fixed handler failure result %q", result, toolExecutionFailedResult)
	}
}

func TestPromptTokenEstimateAfterToolResultsMatchesSerializedMessages(t *testing.T) {
	registry := newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	})
	calls := []ToolCall{validCall("call-1", "list_catalog"), validCall("call-2", "list_catalog")}
	messages := []Message{
		{Role: RoleSystem, Content: "catalog assistant"},
		{Role: RoleUser, Content: "find benchmarks"},
		callResponse(calls...).Message,
	}

	for _, maxResultBytes := range []int{1, 2, 7, 100} {
		t.Run(strconv.Itoa(maxResultBytes)+"_bytes", func(t *testing.T) {
			projected := cloneMessages(messages)
			for _, call := range calls {
				projected = append(projected, Message{
					Role: RoleTool, Name: call.Function.Name, ToolCallID: call.ID,
					Content: worstCaseToolResult(maxResultBytes),
				})
			}
			want, err := promptTokenEstimate(projected, registry.Definitions())
			if err != nil {
				t.Fatal(err)
			}
			got, fits := promptTokenEstimateAfterToolResults(messages, registry.Definitions(), calls, maxResultBytes, math.MaxInt64)
			if !fits || got != want {
				t.Fatalf("projected estimate = %d, fits=%v; want exact serialized estimate %d", got, fits, want)
			}
		})
	}
}

func TestPromptTokenEstimateAfterToolResultsRejectsLargeBatchWithoutMaterializingResults(t *testing.T) {
	registry := newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	})
	calls := make([]ToolCall, 8)
	for i := range calls {
		calls[i] = validCall("call-"+strconv.Itoa(i), "list_catalog")
	}
	messages := []Message{{Role: RoleSystem, Content: "catalog assistant"}, {Role: RoleUser, Content: "find benchmarks"}}
	if _, fits := promptTokenEstimateAfterToolResults(messages, registry.Definitions(), calls, 32<<20, 1_000_000); fits {
		t.Fatal("large projected tool results unexpectedly fit the token budget")
	}
}

func TestRunnerReservesMinimumFinalResponseBeforeDispatch(t *testing.T) {
	var executions int
	config := DefaultConfig()
	config.MaxToolResultBytes = 1
	input := testInput()
	model := modelFunc(func(_ context.Context, request ModelRequest) (ModelResponse, error) {
		response := callResponse(validCall("call-1", "list_catalog"))
		messages := append(cloneMessages(request.Messages), response.Message)
		minimumFinal, err := minimumFinalResponseTokens()
		if err != nil {
			return ModelResponse{}, err
		}

		// Find the stable remaining budget because its decimal representation is
		// included in the next-turn budget notice and therefore in the estimate.
		remainingTokens := int64(0)
		stable := false
		for range 20 {
			projectedMessages := cloneMessages(messages)
			projectedMessages[1].Content = budgetNotice(
				input.Budget.MaxModelCalls-1,
				input.Budget.MaxIterations-1,
				input.Budget.MaxToolCalls-1,
				remainingTokens,
			)
			projectedPrompt, fits := promptTokenEstimateAfterToolResults(
				projectedMessages, request.Tools, response.Message.ToolCalls, config.MaxToolResultBytes, math.MaxInt64,
			)
			if !fits {
				return ModelResponse{}, errors.New("could not estimate projected prompt")
			}
			nextRemaining := projectedPrompt + minimumFinal - 1
			if nextRemaining == remainingTokens {
				stable = true
				break
			}
			remainingTokens = nextRemaining
		}
		if !stable {
			return ModelResponse{}, errors.New("projected prompt budget did not stabilize")
		}
		usage := input.Budget.MaxTotalTokens - remainingTokens
		response.Usage = &TokenUsage{TotalTokens: int(usage), TotalTokensPresent: true}
		return response, nil
	})
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(`null`), nil
	}), config)
	_, err := runner.Run(context.Background(), input)
	if !errors.Is(err, ErrBudgetExceeded) || executions != 0 {
		t.Fatalf("Run() error=%v executions=%d; want budget rejection before dispatch", err, executions)
	}
}

func TestRunnerValidatesEntireBatchBeforeDispatch(t *testing.T) {
	executions := 0
	registry := newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(`null`), nil
	})
	model := &fakeModel{responses: []ModelResponse{callResponse(
		validCall("call-1", "list_catalog"),
		validCall("call-2", "not_registered"),
	)}}
	runner := newTestRunner(t, model, registry, DefaultConfig())
	_, err := runner.Run(context.Background(), testInput())
	if !errors.Is(err, ErrInvalidModelResponse) {
		t.Fatalf("Run() error = %v, want invalid response", err)
	}
	if executions != 0 {
		t.Fatalf("executed %d calls from an invalid batch", executions)
	}
}

func TestRunnerRejectsInvalidArgumentsAndDuplicateIDsBeforeDispatch(t *testing.T) {
	tests := []struct {
		name  string
		calls []ToolCall
	}{
		{name: "schema mismatch", calls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "list_catalog", Arguments: `{"limit":"two"}`}}}},
		{name: "duplicate ID", calls: []ToolCall{validCall("same", "list_catalog"), validCall("same", "list_catalog")}},
		{name: "unmatched type", calls: []ToolCall{{ID: "c1", Type: "other", Function: FunctionCall{Name: "list_catalog", Arguments: `{"limit":1}`}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executions := 0
			model := &fakeModel{responses: []ModelResponse{callResponse(tc.calls...)}}
			runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
				executions++
				return json.RawMessage(`null`), nil
			}), DefaultConfig())
			_, err := runner.Run(context.Background(), testInput())
			if !errors.Is(err, ErrInvalidModelResponse) || executions != 0 {
				t.Fatalf("Run() error=%v executions=%d", err, executions)
			}
		})
	}
}

func TestRunnerRepeatedToolCallsAndMultipleCallsInTurn(t *testing.T) {
	var ids []string
	model := &fakeModel{responses: []ModelResponse{
		callResponse(validCall("call-a", "list_catalog"), validCall("call-b", "list_catalog")),
		callResponse(validCall("call-c", "list_catalog")),
		finalResponse("done"),
	}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}), DefaultConfig())
	// Observe IDs through the model history after each turn.
	result, err := runner.Run(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ToolCalls != 3 || result.ModelCalls != 3 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	for _, message := range model.requests[1].Messages {
		if message.Role == RoleTool {
			ids = append(ids, message.ToolCallID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"call-a", "call-b"}) {
		t.Fatalf("correlated tool IDs = %v", ids)
	}
	for _, message := range model.requests[2].Messages {
		if message.Role == RoleTool && message.ToolCallID == "call-c" {
			return
		}
	}
	t.Fatal("third repeated tool call has no correlated result")
}

func TestRunnerDoesNotDispatchWhenNoFinalTurnRemains(t *testing.T) {
	executions := 0
	budget := testBudget()
	budget.MaxModelCalls = 1
	budget.MaxIterations = 1
	model := &fakeModel{responses: []ModelResponse{callResponse(validCall("c1", "list_catalog"))}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(`null`), nil
	}), DefaultConfig())
	input := testInput()
	input.Budget = budget
	_, err := runner.Run(context.Background(), input)
	if !errors.Is(err, ErrBudgetExceeded) || executions != 0 {
		t.Fatalf("Run() error=%v executions=%d", err, executions)
	}
}

func TestRunnerDoesNotDispatchWhenToolResultsLeaveNoFinalTokenBudget(t *testing.T) {
	executions := 0
	budget := testBudget()
	budget.MaxTotalTokens = 3_000
	budget.MaxOutputTokens = 512
	model := &fakeModel{responses: []ModelResponse{callResponse(validCall("c1", "list_catalog"))}}
	config := DefaultConfig()
	config.MaxToolResultBytes = 4_096
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(`null`), nil
	}), config)
	input := testInput()
	input.Budget = budget
	_, err := runner.Run(context.Background(), input)
	if !errors.Is(err, ErrBudgetExceeded) || executions != 0 || len(model.requests) != 1 {
		t.Fatalf("Run() error=%v executions=%d model calls=%d, want budget rejection before tool dispatch", err, executions, len(model.requests))
	}
}

func TestRunnerDoesNotDispatchWhenEscapedToolResultExceedsTokenBudget(t *testing.T) {
	executions := 0
	budget := testBudget()
	budget.MaxTotalTokens = 10_000
	budget.MaxOutputTokens = 512
	config := DefaultConfig()
	config.MaxToolResultBytes = 4_096
	model := &fakeModel{responses: []ModelResponse{callResponse(validCall("c1", "list_catalog"))}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(worstCaseToolResult(config.MaxToolResultBytes)), nil
	}), config)
	input := testInput()
	input.Budget = budget
	_, err := runner.Run(context.Background(), input)
	if !errors.Is(err, ErrBudgetExceeded) || executions != 0 || len(model.requests) != 1 {
		t.Fatalf("Run() error=%v executions=%d model calls=%d, want budget rejection before tool dispatch", err, executions, len(model.requests))
	}
}

func TestRunnerCanReturnFinalAtLastAllowedCall(t *testing.T) {
	budget := testBudget()
	budget.MaxModelCalls = 1
	budget.MaxIterations = 1
	model := &fakeModel{responses: []ModelResponse{finalResponse("complete")}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}), DefaultConfig())
	input := testInput()
	input.Budget = budget
	result, err := runner.Run(context.Background(), input)
	if err != nil || result.Final != "complete" {
		t.Fatalf("Run() = %+v, %v", result, err)
	}
}

func TestRunnerRetriesOnlyRetryableModelErrorsAndChargesAttempts(t *testing.T) {
	model := &fakeModel{errors: []error{retryError{}}, responses: []ModelResponse{{}, finalResponse("done")}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}), DefaultConfig())
	result, err := runner.Run(context.Background(), testInput())
	if err != nil || result.ModelCalls != 2 {
		t.Fatalf("Run() = %+v, %v", result, err)
	}
	if model.requests[0].Model != model.requests[1].Model {
		t.Fatal("retry changed the model")
	}
	if result.AccountedTokens <= 0 {
		t.Fatal("retry run reported no accounted token usage")
	}
}

func TestRunnerReservesTokenBudgetForEachRetry(t *testing.T) {
	budget := testBudget()
	budget.MaxModelCalls = 3
	budget.MaxIterations = 3
	budget.MaxTotalTokens = 10_000
	budget.MaxOutputTokens = 256
	model := &fakeModel{
		errors:    []error{retryError{}, retryError{}, nil},
		responses: []ModelResponse{{}, {}, finalResponse("done")},
	}
	config := DefaultConfig()
	config.RetryBackoff = 0
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}), config)
	input := testInput()
	input.Budget = budget
	result, err := runner.Run(context.Background(), input)
	if err != nil || result.ModelCalls != 3 {
		t.Fatalf("Run() = %+v, %v", result, err)
	}
	var expected int64
	for i := 0; i < 2; i++ {
		prompt, estimateErr := promptTokenEstimate(model.requests[i].Messages, model.requests[i].Tools)
		if estimateErr != nil {
			t.Fatal(estimateErr)
		}
		expected += prompt + int64(model.requests[i].MaxOutputTokens)
	}
	lastPrompt, err := promptTokenEstimate(model.requests[2].Messages, model.requests[2].Tools)
	if err != nil {
		t.Fatal(err)
	}
	lastOutput, err := responseTokenEstimate(finalResponse("done"))
	if err != nil {
		t.Fatal(err)
	}
	lastCharge, err := accountedUsage(lastPrompt, lastOutput, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected += lastCharge
	if result.AccountedTokens != expected {
		t.Fatalf("accounted tokens = %d, want failed-call reservations plus final usage %d", result.AccountedTokens, expected)
	}
}

func TestRunnerHidesToolErrorsAndBoundsProgress(t *testing.T) {
	var events []ProgressEvent
	config := DefaultConfig()
	config.Progress = func(event ProgressEvent) { events = append(events, event) }
	model := &fakeModel{responses: []ModelResponse{callResponse(validCall("c1", "list_catalog")), finalResponse("done")}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("secret-token and raw user prompt")
	}), config)
	if _, err := runner.Run(context.Background(), testInput()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	toolReply := model.requests[1].Messages[len(model.requests[1].Messages)-1].Content
	if toolReply != `{"error":"tool_execution_failed"}` {
		t.Fatalf("tool error leaked: %s", toolReply)
	}
	for _, event := range events {
		if len(event.Summary) > config.MaxActionSummaryBytes || strings.Contains(event.Summary, "secret-token") || strings.Contains(event.Summary, "raw user prompt") {
			t.Fatalf("unsafe progress event: %+v", event)
		}
	}
}

func TestRunnerRejectsOversizedToolResult(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{callResponse(validCall("c1", "list_catalog"))}}
	config := DefaultConfig()
	config.MaxToolResultBytes = 4
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"long":true}`), nil
	}), config)
	if _, err := runner.Run(context.Background(), testInput()); !errors.Is(err, ErrToolExecutionFailed) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerEnforcesHistoryLimitBeforeCall(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{finalResponse("no")}}
	config := DefaultConfig()
	config.MaxHistoryBytes = 10
	if _, err := NewRunner(model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}), config); err == nil {
		t.Fatal("NewRunner() accepted tool schema beyond history limit")
	}
}

func TestRunnerPropagatesCancellationAndBudgetDeadline(t *testing.T) {
	model := modelFunc(func(ctx context.Context, _ ModelRequest) (ModelResponse, error) {
		<-ctx.Done()
		return ModelResponse{}, ctx.Err()
	})
	registry := newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil })
	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner := newTestRunner(t, model, registry, DefaultConfig())
		if _, err := runner.Run(ctx, testInput()); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	})
	t.Run("budget deadline", func(t *testing.T) {
		input := testInput()
		input.Budget.MaxDuration = 10 * time.Millisecond
		runner := newTestRunner(t, model, registry, DefaultConfig())
		if _, err := runner.Run(context.Background(), input); !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("Run() error = %v", err)
		}
	})
}

func TestRunnerStopsWhenProgressCallbackCancelsAfterModelResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := DefaultConfig()
	config.Progress = func(event ProgressEvent) {
		if event.Kind == EventModelCallCompleted {
			cancel()
		}
	}
	model := &fakeModel{responses: []ModelResponse{finalResponse("complete")}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}), config)
	if _, err := runner.Run(ctx, testInput()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want caller cancellation", err)
	}
}

func TestRunnerStopsWhenProgressCallbackCancelsBeforeToolExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var executions int
	config := DefaultConfig()
	config.Progress = func(event ProgressEvent) {
		if event.Kind == EventToolCallStarted {
			cancel()
		}
	}
	model := &fakeModel{responses: []ModelResponse{
		callResponse(validCall("call-1", "list_catalog")),
		finalResponse("complete"),
	}}
	runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executions++
		return json.RawMessage(`null`), nil
	}), config)
	if _, err := runner.Run(ctx, testInput()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want caller cancellation", err)
	}
	if executions != 0 {
		t.Fatalf("executed %d tools after cancellation", executions)
	}
}

func TestAccountedUsageConservativelyUsesReportedOrEstimatedCounts(t *testing.T) {
	usage := &TokenUsage{PromptTokens: 20, CompletionTokens: 12, PromptTokensPresent: true, CompletionTokensPresent: true}
	got, err := accountedUsage(100, 40, usage)
	if err != nil || got != 140 {
		t.Fatalf("accountedUsage() = %d, %v, want 140", got, err)
	}
	usage = &TokenUsage{TotalTokens: 200, TotalTokensPresent: true}
	got, err = accountedUsage(100, 40, usage)
	if err != nil || got != 200 {
		t.Fatalf("accountedUsage() = %d, %v, want 200", got, err)
	}
}

type registryStub struct {
	definitions      ToolDefinitions
	definitionsCalls *int
}

func (r registryStub) Definitions() ToolDefinitions {
	if r.definitionsCalls != nil {
		(*r.definitionsCalls)++
	}
	return r.definitions
}

func (r registryStub) Lookup(name string) (Tool, bool) {
	for _, definition := range r.definitions {
		if definition.Function.Name == name {
			return Tool{Definition: definition}, true
		}
	}
	return Tool{}, false
}

func TestNewRunnerRejectsInvalidDependenciesConfigurationAndRegistrySize(t *testing.T) {
	validRegistry := registryStub{definitions: ToolDefinitions{testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	}).Definition}}
	validModel := &fakeModel{}
	if _, err := NewRunner(nil, validRegistry, DefaultConfig()); err == nil {
		t.Fatal("NewRunner() accepted a nil model")
	}
	if _, err := NewRunner(validModel, nil, DefaultConfig()); err == nil {
		t.Fatal("NewRunner() accepted a nil registry")
	}

	tests := []struct {
		name   string
		config Config
	}{
		{name: "invalid hard limits", config: func() Config { c := DefaultConfig(); c.HardLimits.MaxModelCalls = 0; return c }()},
		{name: "hard limits exceed service cap", config: func() Config { c := DefaultConfig(); c.HardLimits.MaxModelCalls++; return c }()},
		{name: "negative retries", config: func() Config { c := DefaultConfig(); c.MaxRetries = -1; return c }()},
		{name: "excessive retries", config: func() Config { c := DefaultConfig(); c.MaxRetries = 6; return c }()},
		{name: "negative retry delay", config: func() Config { c := DefaultConfig(); c.RetryBackoff = -1; return c }()},
		{name: "excessive retry delay", config: func() Config { c := DefaultConfig(); c.RetryBackoff = 6 * time.Second; return c }()},
		{name: "zero tool batch", config: func() Config { c := DefaultConfig(); c.MaxToolBatchSize = 0; return c }()},
		{name: "zero argument limit", config: func() Config { c := DefaultConfig(); c.MaxToolArgumentBytes = 0; return c }()},
		{name: "zero result limit", config: func() Config { c := DefaultConfig(); c.MaxToolResultBytes = 0; return c }()},
		{name: "zero history limit", config: func() Config { c := DefaultConfig(); c.MaxHistoryBytes = 0; return c }()},
		{name: "zero response limit", config: func() Config { c := DefaultConfig(); c.MaxModelResponseBytes = 0; return c }()},
		{name: "zero progress summary limit", config: func() Config { c := DefaultConfig(); c.MaxActionSummaryBytes = 0; return c }()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRunner(validModel, validRegistry, tc.config); err == nil {
				t.Fatal("NewRunner() accepted invalid configuration")
			}
		})
	}

	config := DefaultConfig()
	tooMany := make(ToolDefinitions, config.MaxToolBatchSize*16+1)
	for i := range tooMany {
		tooMany[i] = validRegistry.definitions[0]
	}
	if _, err := NewRunner(validModel, registryStub{definitions: tooMany}, config); err == nil {
		t.Fatal("NewRunner() accepted a tool registry over its count limit")
	}
	config.MaxHistoryBytes = 1
	if _, err := NewRunner(validModel, registryStub{}, config); err == nil {
		t.Fatal("NewRunner() accepted definitions over the history byte limit")
	}
}

func TestRunnerSnapshotsDefinitionsAtConstruction(t *testing.T) {
	definitionsCalls := 0
	registry := registryStub{
		definitions:      ToolDefinitions{testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }).Definition},
		definitionsCalls: &definitionsCalls,
	}
	expectedParameters := string(registry.definitions[0].Function.Parameters)
	model := &fakeModel{responses: []ModelResponse{finalResponse("done"), finalResponse("done")}}
	runner := newTestRunner(t, model, registry, DefaultConfig())
	if definitionsCalls != 1 {
		t.Fatalf("Definitions() calls after NewRunner() = %d, want 1", definitionsCalls)
	}
	registry.definitions[0].Function.Parameters[0] = '!'
	for range 2 {
		if _, err := runner.Run(context.Background(), testInput()); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	}
	if definitionsCalls != 1 {
		t.Fatalf("Definitions() calls after runs = %d, want 1", definitionsCalls)
	}
	for i, request := range model.requests {
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "list_catalog" || string(request.Tools[0].Function.Parameters) != expectedParameters {
			t.Fatalf("request %d tools = %+v, want cached list_catalog definition", i, request.Tools)
		}
	}
}

func TestRunnerRejectsInvalidInputsAndBudgets(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{finalResponse("unused")}}
	registry := newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	})
	runner := newTestRunner(t, model, registry, DefaultConfig())
	tests := []struct {
		name  string
		ctx   context.Context
		input Input
	}{
		{name: "nil context", input: testInput()},
		{name: "blank model", ctx: context.Background(), input: func() Input { v := testInput(); v.Model = "  "; return v }()},
		{name: "blank system prompt", ctx: context.Background(), input: func() Input { v := testInput(); v.SystemPrompt = ""; return v }()},
		{name: "blank user prompt", ctx: context.Background(), input: func() Input { v := testInput(); v.UserPrompt = "\n "; return v }()},
		{name: "invalid budget", ctx: context.Background(), input: func() Input { v := testInput(); v.Budget.MaxModelCalls = 0; return v }()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runner.Run(tc.ctx, tc.input); err == nil {
				t.Fatal("Run() accepted invalid input")
			}
		})
	}
	if model.index != 0 {
		t.Fatalf("invalid requests made %d model calls", model.index)
	}
}

func TestValidateDefinitionsRejectsMalformedAndUnsafeTools(t *testing.T) {
	base := testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }).Definition
	tests := []struct {
		name        string
		definitions ToolDefinitions
	}{
		{name: "wrong type", definitions: ToolDefinitions{func() ToolDefinition { d := base; d.Type = "other"; return d }()}},
		{name: "invalid name", definitions: ToolDefinitions{func() ToolDefinition { d := base; d.Function.Name = "bad name"; return d }()}},
		{name: "missing schema", definitions: ToolDefinitions{func() ToolDefinition { d := base; d.Function.Parameters = nil; return d }()}},
		{name: "malformed schema", definitions: ToolDefinitions{func() ToolDefinition { d := base; d.Function.Parameters = json.RawMessage(`{`); return d }()}},
		{name: "non-object schema", definitions: ToolDefinitions{func() ToolDefinition {
			d := base
			d.Function.Parameters = json.RawMessage(`{"type":"string"}`)
			return d
		}()}},
		{name: "invalid schema keyword", definitions: ToolDefinitions{func() ToolDefinition {
			d := base
			d.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"v":{"type":"not-a-json-type"}}}`)
			return d
		}()}},
		{name: "external schema ref", definitions: ToolDefinitions{func() ToolDefinition {
			d := base
			d.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"v":{"$ref":"https://example.invalid/schema"}}}`)
			return d
		}()}},
		{name: "external schema ref reached through local reference", definitions: ToolDefinitions{func() ToolDefinition {
			d := base
			d.Function.Parameters = json.RawMessage(`{"type":"object","x":{"$ref":"https://example.invalid/schema"},"properties":{"v":{"$ref":"#/x"}}}`)
			return d
		}()}},
		{name: "duplicate names", definitions: ToolDefinitions{base, base}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.definitions.validate(); !errors.Is(err, ErrToolRegistryInvalid) {
				t.Fatalf("ToolDefinitions.validate() error = %v", err)
			}
		})
	}
	if err := ToolDefinitions(nil).validate(); err != nil {
		t.Fatalf("ToolDefinitions.validate(empty) error = %v", err)
	}
}

func TestNewRunnerRejectsInvalidRegistry(t *testing.T) {
	model := &fakeModel{}
	definitions := testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil }).Definition
	definitions.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"v":{"$ref":"https://example.invalid/schema"}}}`)
	if _, err := NewRunner(model, registryStub{definitions: ToolDefinitions{definitions}}, DefaultConfig()); !errors.Is(err, ErrToolRegistryInvalid) {
		t.Fatalf("NewRunner() error = %v, want invalid registry", err)
	}
	if model.index != 0 {
		t.Fatalf("invalid registry made %d model calls", model.index)
	}
}

func TestValidateAssistantResponseRejectsUnsupportedTurns(t *testing.T) {
	validToolCall := validCall("call-1", "list_catalog")
	tests := []struct {
		name     string
		response ModelResponse
		maxBytes int
		maxBatch int
	}{
		{name: "wrong role", response: ModelResponse{Message: Message{Role: RoleUser, Content: "x"}}},
		{name: "oversized message", response: finalResponse("response too long"), maxBytes: 1},
		{name: "too many calls", response: callResponse(validToolCall, validCall("call-2", "list_catalog")), maxBatch: 1},
		{name: "truncated completion", response: ModelResponse{Message: Message{Role: RoleAssistant, Content: "x"}, FinishReason: "length"}},
		{name: "tool calls with non-tool finish", response: ModelResponse{Message: Message{Role: RoleAssistant, ToolCalls: []ToolCall{validToolCall}}, FinishReason: "stop"}},
		{name: "no tool calls with tool finish", response: ModelResponse{Message: Message{Role: RoleAssistant, Content: "x"}, FinishReason: "tool_calls"}},
		{name: "oversized argument", response: callResponse(func() ToolCall { c := validToolCall; c.Function.Arguments = strings.Repeat("x", 20); return c }()), maxBytes: 10},
		{name: "oversized call id", response: callResponse(func() ToolCall { c := validToolCall; c.ID = strings.Repeat("i", 257); return c }())},
		{name: "oversized function name", response: callResponse(func() ToolCall { c := validToolCall; c.Function.Name = strings.Repeat("n", 65); return c }())},
		{name: "negative prompt usage", response: ModelResponse{Message: Message{Role: RoleAssistant, Content: "x"}, Usage: &TokenUsage{PromptTokens: -1, PromptTokensPresent: true}}},
		{name: "negative completion usage", response: ModelResponse{Message: Message{Role: RoleAssistant, Content: "x"}, Usage: &TokenUsage{CompletionTokens: -1, CompletionTokensPresent: true}}},
		{name: "negative total usage", response: ModelResponse{Message: Message{Role: RoleAssistant, Content: "x"}, Usage: &TokenUsage{TotalTokens: -1, TotalTokensPresent: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.maxBytes == 0 {
				tc.maxBytes = 1 << 20
			}
			if tc.maxBatch == 0 {
				tc.maxBatch = 8
			}
			if err := tc.response.validate(tc.maxBytes, tc.maxBatch); !errors.Is(err, ErrInvalidModelResponse) {
				t.Fatalf("ModelResponse.validate() error = %v", err)
			}
		})
	}
}

func TestRunnerRejectsBudgetOverrunsAndEmptyFinalText(t *testing.T) {
	t.Run("reported usage exceeds budget", func(t *testing.T) {
		input := testInput()
		input.Budget.MaxTotalTokens = 10_000
		input.Budget.MaxOutputTokens = 2_048
		model := &fakeModel{responses: []ModelResponse{{Message: Message{Role: RoleAssistant, Content: "done"}, Usage: &TokenUsage{TotalTokens: 20_000, TotalTokensPresent: true}}}}
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), DefaultConfig())
		if _, err := runner.Run(context.Background(), input); !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("Run() error = %v, want budget exceeded", err)
		}
	})
	t.Run("empty final response", func(t *testing.T) {
		model := &fakeModel{responses: []ModelResponse{finalResponse(" \n")}}
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), DefaultConfig())
		if _, err := runner.Run(context.Background(), testInput()); !errors.Is(err, ErrInvalidModelResponse) {
			t.Fatalf("Run() error = %v, want invalid response", err)
		}
	})
	t.Run("tool batch exceeds budget", func(t *testing.T) {
		model := &fakeModel{responses: []ModelResponse{callResponse(validCall("a", "list_catalog"), validCall("b", "list_catalog"))}}
		input := testInput()
		input.Budget.MaxToolCalls = 1
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), DefaultConfig())
		if _, err := runner.Run(context.Background(), input); !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("Run() error = %v, want budget exceeded", err)
		}
	})
}

func TestRunnerHandlesModelErrorsAndRetryCancellation(t *testing.T) {
	t.Run("non-retryable failure", func(t *testing.T) {
		model := &fakeModel{errors: []error{errors.New("private failure")}}
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), DefaultConfig())
		if _, err := runner.Run(context.Background(), testInput()); !errors.Is(err, ErrModelCallFailed) {
			t.Fatalf("Run() error = %v, want model call failed", err)
		}
	})
	t.Run("retry limit", func(t *testing.T) {
		model := &fakeModel{errors: []error{retryError{}, retryError{}, retryError{}}}
		config := DefaultConfig()
		config.RetryBackoff = 0
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), config)
		if _, err := runner.Run(context.Background(), testInput()); !errors.Is(err, ErrModelCallFailed) {
			t.Fatalf("Run() error = %v, want model call failed after retries", err)
		}
	})
	t.Run("canceled during retry delay", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		model := &fakeModel{errors: []error{retryError{}}}
		config := DefaultConfig()
		config.RetryBackoff = time.Second
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), config)
		if _, err := runner.Run(ctx, testInput()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v, want caller deadline", err)
		}
	})
}

func TestRunnerEnforcesHistoryLimitsAfterResponseAndTool(t *testing.T) {
	t.Run("assistant response", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxHistoryBytes = 2_000
		model := &fakeModel{responses: []ModelResponse{finalResponse(strings.Repeat("x", 2_000))}}
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }), config)
		if _, err := runner.Run(context.Background(), testInput()); !errors.Is(err, ErrInvalidModelResponse) {
			t.Fatalf("Run() error = %v, want invalid response", err)
		}
	})
	t.Run("tool reply", func(t *testing.T) {
		input := testInput()
		messages := []Message{
			{Role: RoleSystem, Content: input.SystemPrompt},
			{Role: RoleSystem, Content: budgetNotice(input.Budget.MaxModelCalls, input.Budget.MaxIterations, input.Budget.MaxToolCalls, input.Budget.MaxTotalTokens)},
			{Role: RoleUser, Content: input.UserPrompt},
		}
		assistant := callResponse(validCall("call-1", "list_catalog")).Message
		limit := historySize(append(cloneMessages(messages), assistant)) + 50
		config := DefaultConfig()
		config.MaxHistoryBytes = limit
		model := &fakeModel{responses: []ModelResponse{callResponse(validCall("call-1", "list_catalog"))}}
		runner := newTestRunner(t, model, newTestRegistry(t, func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"` + strings.Repeat("x", 200) + `"`), nil
		}), config)
		if _, err := runner.Run(context.Background(), input); !errors.Is(err, ErrInvalidModelResponse) {
			t.Fatalf("Run() error = %v, want history limit error", err)
		}
	})
}

func TestRunnerBoundsProgressSummary(t *testing.T) {
	var events []ProgressEvent
	config := DefaultConfig()
	config.MaxActionSummaryBytes = 4
	config.Progress = func(event ProgressEvent) { events = append(events, event) }
	model := &fakeModel{responses: []ModelResponse{finalResponse("done")}}
	registry, err := NewRegistry(Tool{
		Definition:    testTool(func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil }).Definition,
		ProgressLabel: strings.Repeat("L", 80),
		Handler:       func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`null`), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, model, registry, config)
	if _, err := runner.Run(context.Background(), testInput()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 2 || len(events[0].Summary) != config.MaxActionSummaryBytes {
		t.Fatalf("progress summaries were not truncated: %+v", events)
	}
}

func TestTokenAccountingAndWaitHelpersRejectInvalidValues(t *testing.T) {
	if _, err := promptTokenEstimate(nil, ToolDefinitions{{Function: FunctionMetadata{Parameters: json.RawMessage("{")}}}); err == nil {
		t.Fatal("promptTokenEstimate() accepted invalid raw JSON")
	}
	if _, err := accountedUsage(-1, 1, nil); !errors.Is(err, ErrInvalidModelResponse) {
		t.Fatalf("accountedUsage(negative) error = %v", err)
	}
	if _, err := accountedUsage(int64(^uint64(0)>>1), 1, nil); err == nil {
		t.Fatal("accountedUsage() accepted overflow")
	}
	if err := waitContext(context.Background(), 0); err != nil {
		t.Fatalf("waitContext(zero) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitContext(canceled) error = %v", err)
	}
}

type modelFunc func(context.Context, ModelRequest) (ModelResponse, error)

func (f modelFunc) Complete(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	return f(ctx, request)
}
