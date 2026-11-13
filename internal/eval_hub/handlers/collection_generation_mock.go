package handlers

// This file contains the temporary stateless mock implementation only:
// fixture handlers, fixed data, and mock-specific request/response behavior.
// Request parsing and validation live in collection_generation.go and define
// the contract intended to remain when the real generator is connected.

import (
	"fmt"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/httpwrappers"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.yaml.in/yaml/v4"
)

const collectionGenerationMockID = "550e8400-e29b-41d4-a716-446655440099"

// HandleCreateCollectionGeneration is part of the temporary mock. It validates
// the durable request contract, then returns a stateless pending fixture.
func (h *Handlers) HandleCreateCollectionGeneration(ctx *executioncontext.ExecutionContext, req httpwrappers.RequestWrapper, resp httpwrappers.ResponseWrapper) {
	if !collectionGenerationMockRequested(ctx, req, resp) {
		return
	}

	body, err := req.BodyAsBytes()
	if err != nil {
		resp.Error(err, ctx.RequestID)
		return
	}
	request, err := parseCollectionGenerationRequest(body)
	if err != nil {
		resp.ErrorWithMessageCode(ctx.RequestID, messages.RequestValidationFailed, "Error", err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	resource := map[string]any{
		"id":         collectionGenerationMockID,
		"tenant":     generationIdentity(ctx.Tenant.String(), "local"),
		"owner":      generationIdentity(ctx.User.String(), "local"),
		"created_at": now,
		"updated_at": now,
	}
	setCollectionGenerationMockHeaders(resp)
	resp.SetHeader("Location", collectionGenerationBasePath+"/"+collectionGenerationMockID+"?mock=true")
	resp.WriteJSON(map[string]any{
		"resource": resource,
		"request":  request,
		"status":   map[string]any{"state": "pending"},
	}, 202)
}

// HandleGetCollectionGeneration returns a temporary fixed fixture. The request
// envelope uses the current caller's identity; no POST state, owner continuity,
// or tenant continuity is retained.
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
		"request": api.CollectionGenerationRequest{
			EvaluationGoal:   "Assess model safety and reasoning",
			Model:            api.ModelRef{Name: "fixture-generator", URL: "https://generator.example/v1"},
			MaxBenchmarks:    12,
			Strictness:       "moderate",
			GenerationEffort: "standard",
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

// HandleDeleteCollectionGeneration is a temporary repeatable no-op. The mock
// fixture has no retained or active resource to cancel.
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
