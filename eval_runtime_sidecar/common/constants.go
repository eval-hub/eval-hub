package common

const (
	PATH_PARAMETER_JOB_ID       = "job_id"
	EnvVarTerminationFile       = "TERMINATION_FILE"
	TRANSACTION_ID_HEADER       = "X-Global-Transaction-Id"
	USER_HEADER                 = "X-User"
	TENANT_HEADER               = "X-Tenant"
	LOG_REQUEST_ID              = "request_id"
	LOG_METHOD                  = "method"
	LOG_URI                     = "uri"
	LOG_USER                    = "remote_user"
	LOG_REMOTE_ADR              = "remote_addr"
	LOG_RESP_CODE               = "code"
	LOG_ERROR                   = "error"
	LOG_CONTAINER               = "container"
	LOG_RESP_BODY               = "body"
	LOG_REFERER                 = "referer"
	LOG_USER_AGENT              = "user_agent"
	LOG_ELAPSED                 = "elapsed"
	HTTPCodeOK                  = 200
	HTTPCodeCreated             = 201
	HTTPCodeAccepted            = 202
	HTTPCodeNoContent           = 204
	HTTPCodeBadRequest          = 400
	HTTPCodeUnauthorized        = 401
	HTTPCodeForbidden           = 403
	HTTPCodeNotFound            = 404
	HTTPCodeMethodNotAllowed    = 405
	HTTPCodeConflict            = 409
	HTTPCodeInternalServerError = 500
	HTTPCodeNotImplemented      = 501
)
