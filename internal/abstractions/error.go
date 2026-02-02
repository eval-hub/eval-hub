package abstractions

import "github.com/eval-hub/eval-hub/internal/messages"

// ServiceError is an interface that represents an error in the service.
// It is used to return errors from the service to the caller.
// Error() can be used to log the error in the service,
// MessageCode() and MessageParams() can be used to return the error to the caller.
// The generation of the error message for the caller is done in the
// top level of the service where i18n can be implemented if required.
type ServiceError interface {
	Error() string                      // This allows this to be used with the error interface
	MessageCode() *messages.MessageCode // The message code to return to the caller
	MessageParams() []any               // The parameters to the message code
}
