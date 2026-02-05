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
	)

	// ResourceNotFound The {{.Type}} resource {{.ResourceId}} was not found.
	ResourceNotFound = createMessage(
		constants.HTTPCodeNotFound,
		"The {{.Type}} resource {{.ResourceId}} was not found.",
	)

	// QueryParameterRequired The query parameter '{{.ParameterName}}' is required.
	QueryParameterRequired = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is required.",
	)
	// QueryParameterInvalid The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.
	QueryParameterInvalid = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.",
	)

	// InvalidJSONRequest The request JSON is invalid: '{{.Error}}'. Please check the request and try again.
	InvalidJSONRequest = createMessage(
		constants.HTTPCodeBadRequest,
		"The request JSON is invalid: '{{.Error}}'. Please check the request and try again.",
	)

	// RequestValidationFailed The request validation failed: '{{.Error}}'. Please check the request and try again.
	RequestValidationFailed = createMessage(
		constants.HTTPCodeBadRequest,
		"The request validation failed: '{{.Error}}'. Please check the request and try again.",
	)

	// MLFlowRequiredForExperiment MLflow is required for experiment tracking. Please configure MLflow in the service configuration and try again.
	MLFlowRequiredForExperiment = createMessage(
		constants.HTTPCodeBadRequest,
		"MLflow is required for experiment tracking. Please configure MLflow in the service configuration and try again.",
	)

	// MLFlowRequestFailed The MLflow request failed: '{{.Error}}'. Please check the MLflow configuration and try again.
	MLFlowRequestFailed = createMessage(
		constants.HTTPCodeInternalServerError, // this could be a user errir if the MLFlow service details are incorrect
		"The MLflow request failed: '{{.Error}}'. Please check the MLflow configuration and try again.",
	)

	// Configurastion related errors

	// ConfigurationFailed The service startup failed: '{{.Error}}'.
	ConfigurationFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The service startup failed: '{{.Error}}'.",
	)

	// JSON errors that are not coming from user input

	// JSONUnmarshalFailed The JSON unmarshalling failed for the {{.Type}}: '{{.Error}}'.
	JSONUnmarshalFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The JSON unmarshalling failed for the {{.Type}}: '{{.Error}}'.",
	)

	// Storage related errors

	// DatabaseOperationFailed The request for the {{.Type}} resource {{.ResourceId}} failed: '{{.Error}}'.
	DatabaseOperationFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The request for the {{.Type}} resource {{.ResourceId}} failed: '{{.Error}}'.",
	)
	// QueryFailed The request for the {{.Type}} failed: '{{.Error}}'.
	QueryFailed = createMessage(
		constants.HTTPCodeInternalServerError,
		"The request for the {{.Type}} failed: '{{.Error}}'.",
	)

	// InternalServerError An internal server error occurred: '{{.Error}}'.
	InternalServerError = createMessage(
		constants.HTTPCodeInternalServerError,
		"An internal server error occurred: '{{.Error}}'.",
	)

	// MethodNotAllowed The HTTP method {{.Method}} is not allowed for the API {{.Api}}.
	MethodNotAllowed = createMessage(
		constants.HTTPCodeMethodNotAllowed,
		"The HTTP method {{.Method}} is not allowed for the API {{.Api}}.",
	)

	// NotImplemented The API {{.Api}} is not yet implemented.
	NotImplemented = createMessage(
		constants.HTTPCodeNotImplemented,
		"The API {{.Api}} is not yet implemented.",
	)

	// UnknownError An unknown error occurred: '{{.Error}}'. This is a fallback error if the error is not a service error.
	UnknownError = createMessage(
		constants.HTTPCodeInternalServerError,
		"An unknown error occurred: {{.Error}}.",
	)
)

type MessageCode struct {
	status int
	one    string
}

func (m *MessageCode) GetCode() int {
	return m.status
}

func (m *MessageCode) GetMessage() string {
	return m.one
}

func createMessage(status int, one string) *MessageCode {
	return &MessageCode{
		status,
		one,
	}
}

func GetErrorMesssage(messageCode *MessageCode, messageParams ...any) string {
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
