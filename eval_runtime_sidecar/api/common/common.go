package common

import "github.com/eval-hub/eval-hub/eval_runtime_sidecar/messages"

type Tenant string

type User string

type ServiceError interface {
	Error() string                      // This allows this to be used with the error interface
	MessageCode() *messages.MessageCode // The message code to return to the caller
	MessageParams() []any               // The parameters to the message code
	ShouldRollback() bool               // Whether the transaction should be rolled back due to this error
}

// Error represents an error response
type Error struct {
	MessageCode string `json:"message_code"`
	Message     string `json:"message"`
	Trace       string `json:"trace"`
}
