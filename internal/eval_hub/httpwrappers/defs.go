package httpwrappers

import "github.com/eval-hub/eval-hub/internal/eval_hub/messages"

// RequestWrapper abstracts the underlying HTTP request.
type RequestWrapper interface {
	Method() string
	URI() string
	Header(key string) string
	SetHeader(key string, value string)
	Path() string
	Query(key string) []string
	BodyAsBytes() ([]byte, error)
	PathValue(name string) string
}

// Response abstraction of underlying HTTP library
type ResponseWrapper interface {
	Error(err error, requestID string)
	ErrorWithMessageCode(requestID string, messageCode *messages.MessageCode, messageParams ...any)
	SetHeader(key string, value string)
	DeleteHeader(key string)
	SetStatusCode(code int)
	Write(buf []byte) (n int, err error)
	WriteJSON(v any, code int, arguments ...any)
}
