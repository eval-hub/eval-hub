package serviceerrors

import (
	"github.com/eval-hub/eval-hub/internal/messages"
)

type ServiceError struct {
	messageCode   *messages.MessageCode
	messageParams []any
}

func (e *ServiceError) Error() string {
	return messages.GetErrorMesssage(e.messageCode, e.messageParams...)
}

func (e *ServiceError) MessageCode() *messages.MessageCode {
	return e.messageCode
}

func (e *ServiceError) MessageParams() []any {
	return e.messageParams
}

func NewServiceError(messageCode *messages.MessageCode, messageParams ...any) *ServiceError {
	return &ServiceError{
		messageCode:   messageCode,
		messageParams: messageParams,
	}
}
