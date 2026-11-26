package serviceerrors

import (
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
)

type ServiceError struct {
	messageCode   *messages.MessageCode
	messageParams []any
	rollback      bool
	cause         error
}

func (e *ServiceError) Error() string {
	return messages.GetErrorMessage(e.messageCode, e.messageParams...)
}

func (e *ServiceError) MessageCode() *messages.MessageCode {
	return e.messageCode
}

func (e *ServiceError) MessageParams() []any {
	return e.messageParams
}

func (e *ServiceError) ShouldRollback() bool {
	return e.rollback
}

func NewServiceError(messageCode *messages.MessageCode, messageParams ...any) *ServiceError {
	return &ServiceError{
		messageCode:   messageCode,
		messageParams: messageParams,
		rollback:      false, // the default is to commit the transaction
	}
}

func (e *ServiceError) WithRollback() *ServiceError {
	result := *e
	result.rollback = true
	return &result
}

// WithCause retains the original error without changing the public message.
func (e *ServiceError) WithCause(cause error) *ServiceError {
	result := *e
	result.cause = cause
	return &result
}

func (e *ServiceError) Unwrap() error {
	return e.cause
}

func WithRollback(err error) *ServiceError {
	if se, ok := err.(*ServiceError); ok {
		return se.WithRollback()
	}
	return &ServiceError{
		messageCode:   messages.InternalServerError,
		messageParams: []any{"Error", err.Error()},
		rollback:      true,
	}
}
