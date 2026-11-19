package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	"collection_name": {}, "collection_description": {},
}

var collectionGenerationModelAllowedFields = map[string]struct{}{
	"url": {}, "name": {}, "auth": {}, "parameters": {}, "card_url": {},
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
	if raw, ok := fields["collection_name"]; ok {
		if err := decodeGenerationString(raw, &request.CollectionName); err != nil {
			return api.CollectionGenerationRequest{}, fmt.Errorf("collection_name must be a string: %w", err)
		}
		request.CollectionName = strings.TrimSpace(request.CollectionName)
		if request.CollectionName == "" {
			return api.CollectionGenerationRequest{}, errors.New("collection_name must not be blank when provided")
		}
	}
	if raw, ok := fields["collection_description"]; ok {
		if err := decodeGenerationString(raw, &request.CollectionDescription); err != nil {
			return api.CollectionGenerationRequest{}, fmt.Errorf("collection_description must be a string: %w", err)
		}
		request.CollectionDescription = strings.TrimSpace(request.CollectionDescription)
		if request.CollectionDescription == "" || utf8.RuneCountInString(request.CollectionDescription) > 1024 {
			return api.CollectionGenerationRequest{}, errors.New("collection_description must contain 1 to 1024 non-whitespace characters")
		}
	}
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
		number, ok := parseBoundedJSONInteger(value.String(), 1, api.MaxCollectionGenerationBenchmarks)
		if !ok {
			return api.CollectionGenerationRequest{}, fmt.Errorf("max_benchmarks must be an integer from 1 to %d", api.MaxCollectionGenerationBenchmarks)
		}
		request.MaxBenchmarks = number
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

// parseBoundedJSONInteger validates the decimal value without converting
// through float64, which can round fractional or out-of-range JSON numbers.
func parseBoundedJSONInteger(value string, min, max int) (int, bool) {
	if value == "" || value[0] == '-' {
		return 0, false
	}

	mantissa := value
	exponent := int64(0)
	if exponentAt := strings.IndexAny(value, "eE"); exponentAt >= 0 {
		parsedExponent, err := strconv.ParseInt(value[exponentAt+1:], 10, 64)
		if err != nil {
			return 0, false
		}
		exponent = parsedExponent
		mantissa = value[:exponentAt]
	}

	fractionDigits := int64(0)
	if decimalAt := strings.IndexByte(mantissa, '.'); decimalAt >= 0 {
		fractionDigits = int64(len(mantissa) - decimalAt - 1)
		mantissa = mantissa[:decimalAt] + mantissa[decimalAt+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		return 0, false
	}

	const minInt64 = -1 << 63
	if exponent < minInt64+fractionDigits {
		return 0, false
	}
	scale := exponent - fractionDigits
	maxDigits := len(strconv.Itoa(max))
	if scale >= 0 {
		if scale > int64(maxDigits) || int64(len(digits))+scale > int64(maxDigits) {
			return 0, false
		}
		digits += strings.Repeat("0", int(scale))
	} else {
		if scale == minInt64 {
			return 0, false
		}
		trailingZeros := -scale
		if trailingZeros > int64(len(digits)) {
			return 0, false
		}
		integerEnd := len(digits) - int(trailingZeros)
		if strings.Trim(digits[integerEnd:], "0") != "" {
			return 0, false
		}
		digits = digits[:integerEnd]
	}
	if digits == "" || len(digits) > maxDigits {
		return 0, false
	}

	integer, err := strconv.Atoi(digits)
	if err != nil || integer < min || integer > max {
		return 0, false
	}
	return integer, true
}

func decodeAndValidateGenerationModel(raw json.RawMessage, model *api.ModelRef) error {
	if isJSONNull(raw) {
		return errors.New("model must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("model must be an object")
	}
	for key := range fields {
		if _, ok := collectionGenerationModelAllowedFields[key]; !ok {
			return fmt.Errorf("model field %q is not allowed", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(model); err != nil {
		return errors.New("model must match the evaluation job model shape")
	}
	if modelParametersContainCredentials(model.Parameters) {
		return errors.New("model.parameters must not contain credentials; use model.auth.secret_ref")
	}
	if strings.TrimSpace(model.Name) == "" {
		return errors.New("model.name is required")
	}
	parsedURL, err := url.Parse(model.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.User != nil {
		return errors.New("model.url must be an HTTP(S) URL without embedded credentials")
	}
	query, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		return errors.New("model.url must contain valid query parameters")
	}
	for key := range query {
		if isCredentialModelParameterKey(normalizeModelParameterKey(key)) {
			return errors.New("model.url query parameters must not contain credentials; use model.auth.secret_ref")
		}
	}
	if auth, ok := fields["auth"]; ok {
		if isJSONNull(auth) {
			return errors.New("model.auth must be an object")
		}
		var authFields map[string]json.RawMessage
		if err := json.Unmarshal(auth, &authFields); err != nil || authFields == nil {
			return errors.New("model.auth must be an object")
		}
		for key := range authFields {
			if key != "secret_ref" {
				return fmt.Errorf("model.auth field %q is not allowed", key)
			}
		}
		if model.Auth == nil || strings.TrimSpace(model.Auth.SecretRef) == "" {
			return errors.New("model.auth.secret_ref is required when model.auth is provided")
		}
	}
	return nil
}

func modelParametersContainCredentials(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalizedKey := normalizeModelParameterKey(key)
			if isCredentialModelParameterKey(normalizedKey) {
				return true
			}
			if modelParametersContainCredentials(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if modelParametersContainCredentials(nested) {
				return true
			}
		}
	}
	return false
}

func normalizeModelParameterKey(key string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
}

func isCredentialModelParameterKey(normalizedKey string) bool {
	return normalizedKey == "auth" ||
		normalizedKey == "authorization" ||
		normalizedKey == "cookie" ||
		normalizedKey == "key" ||
		normalizedKey == "proxyauthorization" ||
		normalizedKey == "sig" ||
		strings.Contains(normalizedKey, "apikey") ||
		strings.Contains(normalizedKey, "accesskey") ||
		strings.Contains(normalizedKey, "privatekey") ||
		strings.Contains(normalizedKey, "signature") ||
		strings.HasSuffix(normalizedKey, "token") ||
		strings.Contains(normalizedKey, "secret") ||
		strings.Contains(normalizedKey, "password") ||
		strings.Contains(normalizedKey, "credential")
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
