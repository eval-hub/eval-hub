package serviceerrors

import (
	"github.com/eval-hub/eval-hub/internal/messages"
)

type ServiceError struct {
	messageCode   *messages.MessageCode
	messageParams []any
	rollback      bool
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
	return &ServiceError{
		messageCode:   e.messageCode,
		messageParams: e.messageParams,
		rollback:      true,
	}
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
