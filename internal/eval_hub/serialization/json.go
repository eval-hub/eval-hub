package serialization

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
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
			return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", formatValidationError(validationErrors))
		}
		return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", err.Error())
	}
	// if the validation is successful, return nil
	return nil
}

func formatValidationError(errs validator.ValidationErrors) string {
	if len(errs) == 0 {
		return ""
	}
	e := errs[0]
	switch e.Tag() {
	case "required":
		if e.Field() == "operations" || e.Field() == "confidence_interval" {
			return "operations must contain at least one operation"
		}
		if e.Field() == "results_data_ref" {
			return "results_data_ref is required"
		}
	case "operation_order_matches_operations":
		return "operation_order must list each configured operation exactly once"
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", e.Field(), strings.ReplaceAll(e.Param(), " ", ", "))
	case "excluded_with":
		if refName := postProcessingDataRefName(e); refName != "" {
			if refName == "calibration_data_ref" {
				return "calibration_data_ref: exactly one of s3, pvc, git, or hf must be set"
			}
			return fmt.Sprintf("%s: exactly one data source must be set", refName)
		}
		if isTestDataRefSourceField(e.Field()) && strings.Contains(e.StructNamespace(), "TestDataRef.") {
			return "test_data_ref: exactly one of s3, pvc, git, or hf must be set"
		}
		if e.Field() == "primary_score" {
			return "primary_score is not allowed when results_data_ref.eval_job is set"
		}
	case "required_without_all":
		if refName := postProcessingDataRefName(e); refName != "" {
			if refName == "calibration_data_ref" {
				return "calibration_data_ref: one of s3, pvc, git, or hf must be set"
			}
			return fmt.Sprintf("%s: exactly one data source must be set", refName)
		}
		if isTestDataRefSourceField(e.Field()) && strings.Contains(e.StructNamespace(), "TestDataRef.") {
			return "test_data_ref: one of s3, pvc, git, or hf must be set"
		}
	case "required_without":
		if e.Field() == "primary_score" {
			return "primary_score is required when results_data_ref.eval_job is not set"
		}
	case "category_or_domains":
		return "either category or a non-empty domains array must be provided"
	case "git_http_with_secret":
		if param := e.Param(); param != "" {
			return param
		}
		return "git url with credentials must use https scheme"
	case "hardware_config_exclusive":
		if param := e.Param(); param != "" {
			return fmt.Sprintf("hardware_config: %s", param)
		}
	}
	return errs.Error()
}

func postProcessingDataRefName(e validator.FieldError) string {
	namespace := e.StructNamespace()
	switch {
	case strings.Contains(namespace, ".ResultsDataRef."):
		return "results_data_ref"
	case strings.Contains(namespace, ".CalibrationDataRef["):
		return "calibration_data_ref"
	default:
		return ""
	}
}

func isTestDataRefSourceField(field string) bool {
	switch field {
	case "s3", "pvc", "git", "hf":
		return true
	default:
		return false
	}
}
