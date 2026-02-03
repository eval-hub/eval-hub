package messages

import (
	"fmt"
	"net/http"
	"strings"
)

// This package provides all the error messages that should be reported to the user.
// Note that we add a comment with the message parameters so that it is possible
// to see the parameters in the IDE when creating an error message.
var (
	// API errors that are not storage specific

	// MissingPathParameter The path parameter '{{.ParameterName}}' is required.
	MissingPathParameter = createMessage(
		http.StatusNotFound,
		"The path parameter '{{.ParameterName}}' is required.",
	)

	// ResourceNotFound The {{.Type}} resource {{.ResourceId}} was not found.
	ResourceNotFound = createMessage(
		http.StatusNotFound,
		"The {{.Type}} resource {{.ResourceId}} was not found.",
	)

	// QueryParameterRequired The query parameter '{{.ParameterName}}' is required.
	QueryParameterRequired = createMessage(
		http.StatusBadRequest,
		"The query parameter '{{.ParameterName}}' is required.",
	)
	// QueryParameterInvalid The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.
	QueryParameterInvalid = createMessage(
		http.StatusBadRequest,
		"The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.",
	)

	// Configurastion related errors

	// ConfigurationFailed The service startup failed: '{{.Error}}'.
	ConfigurationFailed = createMessage(
		http.StatusInternalServerError,
		"The service startup failed: '{{.Error}}'.",
	)

	// JSON errors that are not coming from user input

	// JSONUnmarshalFailed The JSON unmarshalling failed for the {{.Type}}: '{{.Error}}'.
	JSONUnmarshalFailed = createMessage(
		http.StatusInternalServerError,
		"The JSON unmarshalling failed for the {{.Type}}: '{{.Error}}'.",
	)

	// Storage related errors

	// DatabaseOperationFailed The request for the {{.Type}} resource {{.ResourceId}} failed: '{{.Error}}'.
	DatabaseOperationFailed = createMessage(
		http.StatusInternalServerError,
		"The request for the {{.Type}} resource {{.ResourceId}} failed: '{{.Error}}'.",
	)
	// QueryFailed The request for the {{.Type}} failed: '{{.Error}}'.
	QueryFailed = createMessage(
		http.StatusInternalServerError,
		"The request for the {{.Type}} failed: '{{.Error}}'.",
	)

	// InternalServerError An internal server error occurred: '{{.Error}}'.
	InternalServerError = createMessage(
		http.StatusInternalServerError,
		"An internal server error occurred: '{{.Error}}'.",
	)

	// MethodNotAllowed The HTTP method {{.Method}} is not allowed for the API {{.Api}}.
	MethodNotAllowed = createMessage(
		http.StatusMethodNotAllowed,
		"The HTTP method {{.Method}} is not allowed for the API {{.Api}}.",
	)

	// NotImplemented The API {{.Api}} is not yet implemented.
	NotImplemented = createMessage(
		http.StatusNotImplemented,
		"The API {{.Api}} is not yet implemented.",
	)

	// UnknownError An unknown error occurred: '{{.Error}}'. This is a fallback error if the error is not a service error.
	UnknownError = createMessage(
		http.StatusInternalServerError,
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
