package messages

import (
	"bytes"
	"text/template"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
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
		"The {{.Type}} resource '{{.ResourceId}}' was not found.",
		"resource_not_found",
	)

	// QueryParameterRequired The query parameter '{{.ParameterName}}' is required.
	QueryParameterRequired = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is required.",
		"query_parameter_required",
	)
	// QueryParameterValueInvalid The query parameter '{{.ParameterName}}' is not valid. Allowed values are: '{{.AllowedValues}}'.
	QueryParameterValueInvalid = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is not valid. Allowed values are: '{{.AllowedValues}}'.",
		"query_parameter_value_invalid",
	)
	// QueryParameterInvalid The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.
	QueryParameterInvalid = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameter '{{.ParameterName}}' is not a valid {{.Type}}: '{{.Value}}'.",
		"query_parameter_invalid",
	)
	// QueryBadParameter The parameter '{{.ParameterName}}' is not a valid query parameter. Allowed parameters are: {{.AllowedParameters}}.
	QueryBadParameter = createMessage(
		constants.HTTPCodeBadRequest,
		"The parameter '{{.ParameterName}}' is not a valid query parameter. Allowed parameters are: {{.AllowedParameters}}.",
		"query_bad_parameter",
	)
	// QueryParameterMismatch The query parameters '{{.ParameterNames}}' are mutually exclusive.
	QueryParameterMismatch = createMessage(
		constants.HTTPCodeBadRequest,
		"The query parameters '{{.ParameterNames}}' are mutually exclusive.",
		"query_parameter_mismatch",
	)

	// JobCanNotBeUpdated The job {{.Id}} can not be {{.NewStatus}} because it is '{{.Status}}'.
	JobCanNotBeUpdated = createMessage(
		constants.HTTPCodeConflict,
		"The job {{.Id}} can not be {{.NewStatus}} because it is '{{.Status}}'.",
		"job_can_not_be_updated",
	)

	// InvalidJSONRequest The request JSON is invalid: '{{.Error}}'. Please check the request and try again.
	InvalidJSONRequest = createMessage(
		constants.HTTPCodeBadRequest,
		"The request JSON is invalid: '{{.Error}}'. Please check the request and try again.",
		"invalid_json_request",
	)

	// InvalidPatchOperation The patch operation '{{.Operation}}' is not valid. Allowed operations are: {{.AllowedOperations}}.
	InvalidPatchOperation = createMessage(
		constants.HTTPCodeBadRequest,
		"The patch operation '{{.Operation}}' is not valid. Allowed operations are: {{.AllowedOperations}}.",
		"invalid_patch_operation",
	)

	// UnallowedPatch The operation '{{.Operation}}' is not allowed for the path '{{.Path}}'.
	UnallowedPatch = createMessage(
		constants.HTTPCodeBadRequest,
		"The operation '{{.Operation}}' is not allowed for the path '{{.Path}}'.",
		"unallowed_patch",
	)

	// RequestValidationFailed The request validation failed: '{{.Error}}'. Please check the request and try again.
	RequestValidationFailed = createMessage(
		constants.HTTPCodeBadRequest,
		"The request validation failed: '{{.Error}}'. Please check the request and try again.",
		"request_validation_failed",
	)

	// ResourceDoesNotExist The {{.Type}} with id '{{.ResourceID}}' does not exist.
	ResourceDoesNotExist = createMessage(
		constants.HTTPCodeBadRequest,
		"The {{.Type}} with id '{{.ResourceID}}' does not exist.",
		"resource_does_not_exist",
	)

	// LocalRuntimeNotEnabled Local runtime is not enabled for provider '{{.ProviderID}}'. Please configure a local runtime command for this provider and try again.
	LocalRuntimeNotEnabled = createMessage(
		constants.HTTPCodeBadRequest,
		"Local runtime is not enabled for provider '{{.ProviderID}}'. Please configure a local runtime command for this provider and try again.",
		"local_runtime_not_enabled",
	)

	// ProviderIDNotUnique The provider ID '{{.ProviderID}}' is not unique.
	ProviderIDNotUnique = createMessage(
		constants.HTTPCodeBadRequest,
		"The provider ID '{{.ProviderID}}' is not unique.",
		"provider_id_not_unique",
	)

	// ReadOnlyProvider Provider '{{.ProviderID}}' cannot be modified or deleted.
	ReadOnlyProvider = createMessage(
		constants.HTTPCodeBadRequest,
		"Provider '{{.ProviderID}}' cannot be modified or deleted.",
		"read_only_provider",
	)

	// ReadOnlyCollection Collection '{{.CollectionID}}' cannot be modified or deleted.
	ReadOnlyCollection = createMessage(
		constants.HTTPCodeBadRequest,
		"Collection '{{.CollectionID}}' cannot be modified or deleted.",
		"read_only_collection",
	)

	// QueueNotFound The queue '{{.QueueName}}' was not found in namespace '{{.Namespace}}'.
	QueueNotFound = createMessage(
		constants.HTTPCodeBadRequest,
		"The queue '{{.QueueName}}' was not found in namespace '{{.Namespace}}'.",
		"queue_not_found",
	)

	// QueueValidationFailed Failed to validate queue '{{.QueueName}}': '{{.Error}}'.
	QueueValidationFailed = createMessage(
		constants.HTTPCodeBadRequest,
		"Failed to validate queue '{{.QueueName}}': '{{.Error}}'.",
		"queue_validation_failed",
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

	// CollectionEmpty The collection {{.CollectionID}} does not have any benchmarks.
	CollectionEmpty = createMessage(
		constants.HTTPCodeBadRequest,
		"The collection {{.CollectionID}} does not have any benchmarks.",
		"collection_empty",
	)

	// EvaluationJobEmpty The evaluation job {{.EvaluationJobID}} does not have any benchmarks.
	EvaluationJobEmpty = createMessage(
		constants.HTTPCodeBadRequest,
		"The evaluation job {{.EvaluationJobID}} does not have any benchmarks.",
		"evaluation_job_empty",
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

	// Forbidden The request is not authorized.
	Forbidden = createMessage(
		constants.HTTPCodeForbidden,
		"The request is not authorized.",
		"forbidden",
	)

	// BadAuthorizationRequest Bad request: '{{.Error}}'.
	BadAuthorizationRequest = createMessage(
		constants.HTTPCodeBadRequest,
		"Bad request: '{{.Error}}'.",
		"unable_to_authorize_request",
	)

	// Unauthorized The request is not authenticated: '{{.Error}}'.
	Unauthorized = createMessage(
		constants.HTTPCodeUnauthorized,
		"The request is not authenticated.",
		"unauthorized",
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
	params := make(map[string]any)
	for i := 0; i < len(messageParams); i += 2 {
		param := messageParams[i]
		var paramValue any
		if i+1 < len(messageParams) {
			paramValue = messageParams[i+1]
		} else {
			paramValue = "NOT_DEFINED" // this is a placeholder for a missing parameter value - if you see this value then the code needs to be fixed
		}
		params[param.(string)] = paramValue
	}

	tmpl, _ := template.New("errmfs").Parse(msg)
	out := bytes.NewBuffer(nil)
	err := tmpl.Execute(out, params)
	if err != nil {
		return "INVALID TEMPLATE"
	}
	return out.String()
}
