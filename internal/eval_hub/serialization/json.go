package serialization

import (
	"encoding/json"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	validator "github.com/go-playground/validator/v10"
)

// BeforeValidateFunc runs on the decoded value before struct validation. Pass nil when not needed.
type BeforeValidateFunc func(v any)

// Unmarshal decodes JSON into v, optionally runs beforeValidate, then validates v.
// Pass beforeValidate (e.g. api.NormalizeEvaluationJobConfig) when fields must be normalized
// before struct tags such as k8s_label_value are applied.
func Unmarshal(validate *validator.Validate, executionContext *executioncontext.ExecutionContext, jsonBytes []byte, v any, beforeValidate BeforeValidateFunc) error {
	err := json.Unmarshal(jsonBytes, v)
	if err != nil {
		return serviceerrors.NewServiceError(messages.InvalidJSONRequest, "Error", err.Error())
	}
	if beforeValidate != nil {
		beforeValidate(v)
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
