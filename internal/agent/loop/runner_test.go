package loop

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
		MaxIterations:   4,
		MaxModelCalls:   5,
		MaxToolCalls:    8,
		MaxTotalTokens:  100_000,
		MaxOutputTokens: 2_048,
		Deadline:        time.Minute,
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
		input.Budget.Deadline = 10 * time.Millisecond
		runner := newTestRunner(t, model, registry, DefaultConfig())
		if _, err := runner.Run(context.Background(), input); !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("Run() error = %v", err)
		}
	})
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

type modelFunc func(context.Context, ModelRequest) (ModelResponse, error)

func (f modelFunc) Complete(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	return f(ctx, request)
}
