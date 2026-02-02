package http_wrappers

import "github.com/eval-hub/eval-hub/internal/messages"

// Request sbstraction of undelying HTTP library
type RequestWrapper interface {
	Method() string
	URI() string
	Header(key string) string
	SetHeader(key string, value string)
	Path() string
	Query(key string) []string
	BodyAsBytes() ([]byte, error)
}

// Response abstraction of underlying HTTP library
type ResponseWrapper interface {
	Error(errorMessage string, code int, requestId string) // TODO this will be removed as soon as all errors are changed to use one of the methods below
	ErrorWithError(err error, requestId string)
	ErrorWithMessageCode(requestId string, messageCode *messages.MessageCode, messageParams ...any)
	SetHeader(key string, value string)
	DeleteHeader(key string)
	SetStatusCode(code int)
	Write(buf []byte) (n int, err error)
	WriteJSON(v any, code int)
}
