package serialization

import (
	"encoding/json"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	validator "github.com/go-playground/validator/v10"
)

func Unmarshal(validate *validator.Validate, executionContext *executioncontext.ExecutionContext, jsonBytes []byte, v any) error {
	err := json.Unmarshal(jsonBytes, v)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", err.Error())
	}
	// now validate the unmarshalled data
	err = validate.StructCtx(executionContext.Ctx, v)
	if err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			for _, validationError := range validationErrors {
				executionContext.Logger.Info("Validation error", "field", validationError.Field(), "tag", validationError.Tag(), "value", validationError.Value())
			}
		}
		return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", err.Error())
	}
	// if the validation is successful, return nil
	return nil
}
