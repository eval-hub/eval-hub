package messages

import (
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/internal/constants"
)

// This package provides all the error messages that should be reported to the user.
// Note that we add a comment with the message parameters so that it is possible
// to see the parameters in the IDE when creating an error message.
var (
	// API errors that are not storage specific

	// MissingPathParameter The path parameter '{{.ParameterName}}' is required.
	MissingPathParameter = createMessage(
		constants.HTTPCodeBadRequest,
		"The path parameter '{{.ParameterName}}' is required.",
		"missing_path_parameter",
	)

	// ResourceNotFound The {{.Type}} resource {{.ResourceId}} was not found.
	ResourceNotFound = createMessage(
		constants.HTTPCodeNotFound,
		"The {{.Type}} resource {{.ResourceId}} was not found.",
		"resource_not_found",
	)

	// QueryParameterRequired The query parameter '{{.ParameterName}}' is required.
	QueryParameterRequired = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is required.",
		"query_parameter_required",
	)
	// QueryParameterInvalid The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.
	QueryParameterInvalid = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.",
		"query_parameter_invalid",
	)

	// JobCanNotBeCancelled The job {{.Id}} can not be cancelled because it is '{{.Status}}'.
	JobCanNotBeCancelled = createMessage(
		constants.HTTPCodeConflict,
		"The job {{.Id}} can not be cancelled because it is '{{.Status}}'.",
		"job_can_not_be_cancelled",
	)

	// InvalidJSONRequest The request JSON is invalid: '{{.Error}}'. Please check the request and try again.
	InvalidJSONRequest = createMessage(
		constants.HTTPCodeBadRequest,
		"The request JSON is invalid: '{{.Error}}'. Please check the request and try again.",
		"invalid_json_request",
	)

	// RequestValidationFailed The request validation failed: '{{.Error}}'. Please check the request and try again.
	RequestValidationFailed = createMessage(
		constants.HTTPCodeBadRequest,
		"The request validation failed: '{{.Error}}'. Please check the request and try again.",
		"request_validation_failed",
	)

	// MLFlowRequiredForExperiment MLflow is required for experiment tracking. Please configure MLflow in the service configuration and try again.
	MLFlowRequiredForExperiment = createMessage(
		constants.HTTPCodeBadRequest,
		"MLflow is required for experiment tracking. Please configure MLflow in the service configuration and try again.",
		"mlflow_required_for_experiment",
	)

	// MLFlowRequestFailed The MLflow request failed: '{{.Error}}'. Please check the MLflow configuration and try again.
	MLFlowRequestFailed = createMessage(
		constants.HTTPCodeBadRequest, // this could be a user error if the MLFlow service details are incorrect
		"The MLflow request failed: '{{.Error}}'. Please check the MLflow configuration and try again.",
		"mlflow_request_failed",
	)

	// Configuration related errors

	// ConfigurationFailed The service startup failed: '{{.Error}}'.
	ConfigurationFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The service startup failed: '{{.Error}}'.",
		"configuration_failed",
	)

	// JSON errors that are not coming from user input

	// JSONUnmarshalFailed The JSON un-marshalling failed for the {{.Type}}: '{{.Error}}'.
	JSONUnmarshalFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The JSON un-marshalling failed for the {{.Type}}: '{{.Error}}'.",
		"json_unmarshalling_failed",
	)

	// Storage related errors

	// DatabaseOperationFailed The request for the {{.Type}} resource {{.ResourceId}} failed: '{{.Error}}'.
	DatabaseOperationFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The request for the {{.Type}} resource {{.ResourceId}} failed: '{{.Error}}'.",
		"database_operation_failed",
	)

	// QueryFailed The request for the {{.Type}} failed: '{{.Error}}'.
	QueryFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The request for the {{.Type}} failed: '{{.Error}}'.",
		"query_failed",
	)

	// InternalServerError An internal server error occurred: '{{.Error}}'.
	InternalServerError = createMessage(
		constants.HTTPCodeInternalServerError,
		"An internal server error occurred: '{{.Error}}'.",
		"internal_server_error",
	)

	// MethodNotAllowed The HTTP method {{.Method}} is not allowed for the API {{.Api}}.
	MethodNotAllowed = createMessage(
		constants.HTTPCodeMethodNotAllowed,
		"The HTTP method {{.Method}} is not allowed for the API {{.Api}}.",
		"method_not_allowed",
	)

	// NotImplemented The API {{.Api}} is not yet implemented.
	NotImplemented = createMessage(
		constants.HTTPCodeNotImplemented,
		"The API {{.Api}} is not yet implemented.",
		"not_implemented",
	)

	// UnknownError An unknown error occurred: '{{.Error}}'. This is a fallback error if the error is not a service error.
	UnknownError = createMessage(
		constants.HTTPCodeInternalServerError,
		"An unknown error occurred: {{.Error}}.",
		"unknown_error",
	)
)

type MessageCode struct {
	status int
	one    string
	code   string
}

func (m *MessageCode) GetStatusCode() int {
	return m.status
}

func (m *MessageCode) GetCode() string {
	return m.code
}

func (m *MessageCode) GetMessage() string {
	return m.one
}

func createMessage(status int, one string, code string) *MessageCode {
	return &MessageCode{
		status,
		one,
		code,
	}
}

func GetErrorMessage(messageCode *MessageCode, messageParams ...any) string {
	msg := messageCode.GetMessage()
	for i := 0; i < len(messageParams); i += 2 {
		param := messageParams[i]
		var paramValue any
		if i+1 < len(messageParams) {
			paramValue = messageParams[i+1]
		} else {
			paramValue = "NOT_DEFINED" // this is a placeholder for a missing parameter value - if you see this value then the code needs to be fixed
		}
		msg = strings.ReplaceAll(msg, fmt.Sprintf("{{.%v}}", param), fmt.Sprintf("%v", paramValue))
	}
	return msg
}
