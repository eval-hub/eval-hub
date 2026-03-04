package serialization

import (
	"encoding/json"
	"fmt"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/executioncontext"
	validator "github.com/go-playground/validator/v10"
)

func Unmarshal(validate *validator.Validate, executionContext *executioncontext.ExecutionContext, jsonBytes []byte, v any) error {
	err := json.Unmarshal(jsonBytes, v)
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	// now validate the unmarshalled data
	err = validate.StructCtx(executionContext.Ctx, v)
	if err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			for _, validationError := range validationErrors {
				executionContext.Logger.Info("Validation error", "field", validationError.Field(), "tag", validationError.Tag(), "value", validationError.Value())
			}
		}
		return fmt.Errorf("request validation failed: %w", err)
	}
	// if the validation is successful, return nil
	return nil
}
