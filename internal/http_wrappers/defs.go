package http_wrappers

import "github.com/eval-hub/eval-hub/internal/messages"

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
	Error(err error, requestId string)
	ErrorWithMessageCode(requestId string, messageCode *messages.MessageCode, messageParams ...any)
	SetHeader(key string, value string)
	DeleteHeader(key string)
	SetStatusCode(code int)
	Write(buf []byte) (n int, err error)
	WriteJSON(v any, code int)
}
