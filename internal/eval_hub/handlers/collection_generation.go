package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"go.yaml.in/yaml/v4"
)

const (
	collectionGenerationMockID      = "550e8400-e29b-41d4-a716-446655440099"
	collectionGenerationMockModelID = "mock-generator"
	collectionGenerationMockPath    = "/api/v1/evaluations/collections/generation"
)

var errInvalidCollectionGenerationModel = errors.New("generator model is not available")

type collectionGenerationOptions struct {
	EvaluationGoal   string
	ProviderFilter   string
	MaxBenchmarks    int
	Strictness       string
	GeneratorModelID string
	GenerationEffort string
}

// HandleListCollectionGenerationModels returns the single fixture profile when
// explicitly requested. The real profile discovery endpoint is implemented by
// the collection-generation service work.
func (h *Handlers) HandleListCollectionGenerationModels(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, resp httpwrappers.ResponseWrapper) {
	if !collectionGenerationMockRequested(ctx, req, resp) {
		return
	}
	setCollectionGenerationMockHeaders(resp)
	resp.WriteJSON(map[string]any{
		"items": []map[string]any{{
			"id":           collectionGenerationMockModelID,
			"display_name": "Mock generator (fixture)",
			"is_default":   true,
		}},
	}, 200)
}

// HandleCreateCollectionGeneration validates a request and returns a stateless
// pending fixture resource. It does not store or start an evaluation job.
func (h *Handlers) HandleCreateCollectionGeneration(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, resp httpwrappers.ResponseWrapper) {
	if !collectionGenerationMockRequested(ctx, req, resp) {
		return
	}

	body, err := req.BodyAsBytes()
	if err != nil {
		resp.Error(err, ctx.RequestID)
		return
	}
	options, err := parseCollectionGenerationOptions(body)
	if err != nil {
		if errors.Is(err, errInvalidCollectionGenerationModel) {
			resp.ErrorWithMessageCode(ctx.RequestID, messages.InvalidGeneratorModel)
			return
		}
		resp.ErrorWithMessageCode(ctx.RequestID, messages.RequestValidationFailed, "Error", err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	request := normalizedCollectionGenerationRequest(options)
	resource := map[string]any{
		"id":         collectionGenerationMockID,
		"tenant":     generationIdentity(ctx.Tenant.String(), "local"),
		"owner":      generationIdentity(ctx.User.String(), "local"),
		"created_at": now,
		"updated_at": now,
	}
	setCollectionGenerationMockHeaders(resp)
	resp.SetHeader("Location", collectionGenerationMockPath+"/"+collectionGenerationMockID+"?mock=true")
	resp.WriteJSON(map[string]any{
		"resource": resource,
		"request":  request,
		"status":   map[string]any{"state": "pending"},
	}, 202)
}

// HandleGetCollectionGeneration returns the same completed fixture for its
// fixed ID. The request envelope uses the current caller's identity; no POST
// state, owner continuity, or tenant continuity is retained.
func (h *Handlers) HandleGetCollectionGeneration(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, resp httpwrappers.ResponseWrapper) {
	if !collectionGenerationMockRequested(ctx, req, resp) {
		return
	}
	if req.PathValue("id") != collectionGenerationMockID {
		resp.ErrorWithMessageCode(ctx.RequestID, messages.ResourceNotFound, "Type", "collection generation", "ResourceId", req.PathValue("id"))
		return
	}

	const createdAt = "2026-10-29T12:00:00Z"
	const completedAt = "2026-10-29T12:00:30Z"
	fixture := map[string]any{
		"resource": map[string]any{
			"id":         collectionGenerationMockID,
			"tenant":     generationIdentity(ctx.Tenant.String(), "local"),
			"owner":      generationIdentity(ctx.User.String(), "local"),
			"created_at": createdAt,
			"updated_at": completedAt,
		},
		"request": map[string]any{
			"evaluation_goal":    "Assess model safety and reasoning",
			"max_benchmarks":     12,
			"strictness":         "moderate",
			"generator_model_id": collectionGenerationMockModelID,
			"generation_effort":  "standard",
		},
		"status": map[string]any{
			"state":        "completed",
			"started_at":   "2026-10-29T12:00:01Z",
			"completed_at": completedAt,
		},
	}
	result, err := collectionGenerationMockResult()
	if err != nil {
		resp.Error(fmt.Errorf("build collection generation fixture: %w", err), ctx.RequestID)
		return
	}
	fixture["result"] = result
	setCollectionGenerationMockHeaders(resp)
	resp.WriteJSON(fixture, 200)
}

// HandleDeleteCollectionGeneration is an intentional repeatable no-op. The
// temporary fixture has no retained or active resource to cancel.
func (h *Handlers) HandleDeleteCollectionGeneration(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, resp httpwrappers.ResponseWrapper) {
	if !collectionGenerationMockRequested(ctx, req, resp) {
		return
	}
	if req.PathValue("id") != collectionGenerationMockID {
		resp.ErrorWithMessageCode(ctx.RequestID, messages.ResourceNotFound, "Type", "collection generation", "ResourceId", req.PathValue("id"))
		return
	}
	setCollectionGenerationMockHeaders(resp)
	resp.SetStatusCode(204)
}

func collectionGenerationMockRequested(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, resp httpwrappers.ResponseWrapper) bool {
	values := req.Query("mock")
	if len(values) == 1 && values[0] == "true" {
		return true
	}
	if len(values) == 0 {
		resp.ErrorWithMessageCode(ctx.RequestID, messages.CollectionGenerationNotConfigured)
		return false
	}
	resp.ErrorWithMessageCode(ctx.RequestID, messages.QueryParameterValueInvalid, "ParameterName", "mock", "AllowedValues", "true")
	return false
}

func setCollectionGenerationMockHeaders(resp httpwrappers.ResponseWrapper) {
	resp.SetHeader("X-EvalHub-Mock", "true")
	resp.SetHeader("Cache-Control", "no-store")
}

func generationIdentity(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parseCollectionGenerationOptions(body []byte) (collectionGenerationOptions, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return collectionGenerationOptions{}, fmt.Errorf("invalid JSON object: %w", err)
	}
	if fields == nil {
		return collectionGenerationOptions{}, errors.New("request body must be a JSON object")
	}
	allowed := map[string]struct{}{
		"evaluation_goal": {}, "provider_filter": {}, "max_benchmarks": {},
		"strictness": {}, "generator_model_id": {}, "generation_effort": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return collectionGenerationOptions{}, fmt.Errorf("field %q is not allowed", key)
		}
	}

	options := collectionGenerationOptions{
		MaxBenchmarks:    12,
		Strictness:       "moderate",
		GeneratorModelID: collectionGenerationMockModelID,
		GenerationEffort: "standard",
	}
	if raw, ok := fields["evaluation_goal"]; !ok {
		return collectionGenerationOptions{}, errors.New("evaluation_goal is required")
	} else if err := decodeGenerationString(raw, &options.EvaluationGoal); err != nil {
		return collectionGenerationOptions{}, fmt.Errorf("evaluation_goal must be a string: %w", err)
	}
	if strings.TrimSpace(options.EvaluationGoal) == "" || utf8.RuneCountInString(options.EvaluationGoal) > 2000 {
		return collectionGenerationOptions{}, errors.New("evaluation_goal must contain 1 to 2000 non-whitespace characters")
	}
	options.EvaluationGoal = strings.TrimSpace(options.EvaluationGoal)

	if raw, ok := fields["provider_filter"]; ok {
		if err := decodeGenerationString(raw, &options.ProviderFilter); err != nil {
			return collectionGenerationOptions{}, fmt.Errorf("provider_filter must be a string: %w", err)
		}
		if options.ProviderFilter == "" {
			return collectionGenerationOptions{}, errors.New("provider_filter must not be empty when provided")
		}
		options.ProviderFilter = strings.TrimSpace(options.ProviderFilter)
	}
	if raw, ok := fields["max_benchmarks"]; ok {
		if isJSONNull(raw) {
			return collectionGenerationOptions{}, errors.New("max_benchmarks must be an integer from 1 to 50")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return collectionGenerationOptions{}, errors.New("max_benchmarks must be an integer from 1 to 50")
		}
		value, ok := decoded.(json.Number)
		if !ok {
			return collectionGenerationOptions{}, errors.New("max_benchmarks must be an integer from 1 to 50")
		}
		number, err := strconv.ParseFloat(value.String(), 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || math.Trunc(number) != number || number < 1 || number > 50 {
			return collectionGenerationOptions{}, errors.New("max_benchmarks must be an integer from 1 to 50")
		}
		options.MaxBenchmarks = int(number)
	}
	if raw, ok := fields["strictness"]; ok {
		if err := decodeGenerationString(raw, &options.Strictness); err != nil {
			return collectionGenerationOptions{}, fmt.Errorf("strictness must be a string: %w", err)
		}
		if options.Strictness != "lenient" && options.Strictness != "moderate" && options.Strictness != "strict" {
			return collectionGenerationOptions{}, errors.New("strictness must be lenient, moderate, or strict")
		}
	}
	if raw, ok := fields["generator_model_id"]; ok {
		if err := decodeGenerationString(raw, &options.GeneratorModelID); err != nil {
			return collectionGenerationOptions{}, fmt.Errorf("generator_model_id must be a string: %w", err)
		}
		if strings.TrimSpace(options.GeneratorModelID) == "" || utf8.RuneCountInString(options.GeneratorModelID) > 128 {
			return collectionGenerationOptions{}, errors.New("generator_model_id must contain 1 to 128 non-whitespace characters")
		}
		options.GeneratorModelID = strings.TrimSpace(options.GeneratorModelID)
		if options.GeneratorModelID != collectionGenerationMockModelID {
			return collectionGenerationOptions{}, errInvalidCollectionGenerationModel
		}
	}
	if raw, ok := fields["generation_effort"]; ok {
		if err := decodeGenerationString(raw, &options.GenerationEffort); err != nil {
			return collectionGenerationOptions{}, fmt.Errorf("generation_effort must be a string: %w", err)
		}
		if options.GenerationEffort != "low" && options.GenerationEffort != "standard" && options.GenerationEffort != "high" {
			return collectionGenerationOptions{}, errors.New("generation_effort must be low, standard, or high")
		}
	}
	return options, nil
}

func decodeGenerationString(raw json.RawMessage, target *string) error {
	if isJSONNull(raw) {
		return errors.New("must not be null")
	}
	return json.Unmarshal(raw, target)
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func normalizedCollectionGenerationRequest(options collectionGenerationOptions) map[string]any {
	request := map[string]any{
		"evaluation_goal":    options.EvaluationGoal,
		"max_benchmarks":     options.MaxBenchmarks,
		"strictness":         options.Strictness,
		"generator_model_id": options.GeneratorModelID,
		"generation_effort":  options.GenerationEffort,
	}
	if options.ProviderFilter != "" {
		request["provider_filter"] = options.ProviderFilter
	}
	return request
}

func collectionGenerationMockResult() (map[string]any, error) {
	collection := map[string]any{
		"name":        "Mock generated collection",
		"description": "Temporary stateless collection-generation fixture.",
		"domains":     []string{"knowledge_and_reasoning"},
		"benchmarks": []map[string]any{{
			"id":          "arc_easy",
			"provider_id": "lm_evaluation_harness",
		}},
	}
	yamlBytes, err := yaml.Marshal(collection)
	if err != nil {
		return nil, fmt.Errorf("marshal collection YAML: %w", err)
	}
	return map[string]any{
		"collection": collection,
		"yaml":       string(yamlBytes),
		"rationale":  "The fixture includes a catalog benchmark relevant to the requested reasoning goal.",
		"explanation": map[string]any{
			"selected_benchmarks": []map[string]any{{
				"provider_id": "lm_evaluation_harness",
				"id":          "arc_easy",
				"reason":      "Provides a simple knowledge-reasoning benchmark for the requested evaluation goal.",
			}},
			"rejected_benchmarks": []map[string]any{},
		},
		"actions": []map[string]any{{
			"name":    "search_benchmarks",
			"outcome": "success",
			"summary": "Searched the visible catalog and selected one fixture benchmark.",
		}},
	}, nil
}
