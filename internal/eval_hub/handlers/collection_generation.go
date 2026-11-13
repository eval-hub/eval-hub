package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// collectionGenerationBasePath is the durable base path for the collection
// generation API.
const collectionGenerationBasePath = "/api/v1/evaluations/collections/generation"

var collectionGenerationRequestAllowedFields = map[string]struct{}{
	"evaluation_goal": {}, "model": {}, "provider_filter": {},
	"max_benchmarks": {}, "strictness": {}, "generation_effort": {},
}

// parseCollectionGenerationRequest parses and validates the durable request
// contract.
func parseCollectionGenerationRequest(body []byte) (api.CollectionGenerationRequest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return api.CollectionGenerationRequest{}, fmt.Errorf("invalid JSON object: %w", err)
	}
	if fields == nil {
		return api.CollectionGenerationRequest{}, errors.New("request body must be a JSON object")
	}
	for key := range fields {
		if _, ok := collectionGenerationRequestAllowedFields[key]; !ok {
			return api.CollectionGenerationRequest{}, fmt.Errorf("field %q is not allowed", key)
		}
	}

	request := api.CollectionGenerationRequest{
		MaxBenchmarks:    api.DefaultCollectionGenerationBenchmarks,
		Strictness:       "moderate",
		GenerationEffort: "standard",
	}
	if raw, ok := fields["evaluation_goal"]; !ok {
		return api.CollectionGenerationRequest{}, errors.New("evaluation_goal is required")
	} else if err := decodeGenerationString(raw, &request.EvaluationGoal); err != nil {
		return api.CollectionGenerationRequest{}, fmt.Errorf("evaluation_goal must be a string: %w", err)
	}
	if strings.TrimSpace(request.EvaluationGoal) == "" || utf8.RuneCountInString(request.EvaluationGoal) > api.MaxCollectionGenerationGoalLength {
		return api.CollectionGenerationRequest{}, fmt.Errorf("evaluation_goal must contain 1 to %d non-whitespace characters", api.MaxCollectionGenerationGoalLength)
	}
	request.EvaluationGoal = strings.TrimSpace(request.EvaluationGoal)
	if raw, ok := fields["model"]; !ok {
		return api.CollectionGenerationRequest{}, errors.New("model is required")
	} else if err := decodeAndValidateGenerationModel(raw, &request.Model); err != nil {
		return api.CollectionGenerationRequest{}, err
	}

	if raw, ok := fields["provider_filter"]; ok {
		if err := decodeGenerationString(raw, &request.ProviderFilter); err != nil {
			return api.CollectionGenerationRequest{}, fmt.Errorf("provider_filter must be a string: %w", err)
		}
		if request.ProviderFilter == "" {
			return api.CollectionGenerationRequest{}, errors.New("provider_filter must not be empty when provided")
		}
		request.ProviderFilter = strings.TrimSpace(request.ProviderFilter)
	}
	if raw, ok := fields["max_benchmarks"]; ok {
		if isJSONNull(raw) {
			return api.CollectionGenerationRequest{}, fmt.Errorf("max_benchmarks must be an integer from 1 to %d", api.MaxCollectionGenerationBenchmarks)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return api.CollectionGenerationRequest{}, fmt.Errorf("max_benchmarks must be an integer from 1 to %d", api.MaxCollectionGenerationBenchmarks)
		}
		value, ok := decoded.(json.Number)
		if !ok {
			return api.CollectionGenerationRequest{}, fmt.Errorf("max_benchmarks must be an integer from 1 to %d", api.MaxCollectionGenerationBenchmarks)
		}
		number, err := strconv.ParseFloat(value.String(), 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || math.Trunc(number) != number || number < 1 || number > api.MaxCollectionGenerationBenchmarks {
			return api.CollectionGenerationRequest{}, fmt.Errorf("max_benchmarks must be an integer from 1 to %d", api.MaxCollectionGenerationBenchmarks)
		}
		request.MaxBenchmarks = int(number)
	}
	if raw, ok := fields["strictness"]; ok {
		if err := decodeGenerationString(raw, &request.Strictness); err != nil {
			return api.CollectionGenerationRequest{}, fmt.Errorf("strictness must be a string: %w", err)
		}
		if request.Strictness != "lenient" && request.Strictness != "moderate" && request.Strictness != "strict" {
			return api.CollectionGenerationRequest{}, errors.New("strictness must be lenient, moderate, or strict")
		}
	}
	if raw, ok := fields["generation_effort"]; ok {
		if err := decodeGenerationString(raw, &request.GenerationEffort); err != nil {
			return api.CollectionGenerationRequest{}, fmt.Errorf("generation_effort must be a string: %w", err)
		}
		if request.GenerationEffort != "low" && request.GenerationEffort != "standard" && request.GenerationEffort != "high" {
			return api.CollectionGenerationRequest{}, errors.New("generation_effort must be low, standard, or high")
		}
	}
	return request, nil
}

func decodeAndValidateGenerationModel(raw json.RawMessage, model *api.ModelRef) error {
	if isJSONNull(raw) {
		return errors.New("model must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("model must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(model); err != nil {
		return errors.New("model must match the evaluation job model shape")
	}
	if strings.TrimSpace(model.Name) == "" {
		return errors.New("model.name is required")
	}
	parsedURL, err := url.Parse(model.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.User != nil {
		return errors.New("model.url must be an HTTP(S) URL without embedded credentials")
	}
	if auth, ok := fields["auth"]; ok {
		if isJSONNull(auth) || model.Auth == nil || strings.TrimSpace(model.Auth.SecretRef) == "" {
			return errors.New("model.auth.secret_ref is required when model.auth is provided")
		}
	}
	return nil
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
