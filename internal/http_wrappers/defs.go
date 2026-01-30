package http_wrappers

// Request sbstraction of undelying HTTP library
type RequestWrapper interface {
	Method() string
	URI() string
	Header(key string) string
	SetHeader(key string, value string)
	Path() string
	Query(key string) map[string][]string
	BodyAsBytes() ([]byte, error)
}

// Response abstraction of underlying HTTP library
type ResponseWrapper interface {
	Error(errorMessage string, code int, requestId string)
	SetHeader(key string, value string)
	DeleteHeader(key string)
	SetStatusCode(code int)
	Write(buf []byte) (n int, err error)
	WriteJSON(v any, code int)
}
