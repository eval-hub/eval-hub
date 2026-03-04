package messages

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/common"
)

var (
	MethodNotAllowed = createMessage(
		common.HTTPCodeMethodNotAllowed,
		"The HTTP method {{.Method}} is not allowed for the API {{.Api}}.",
		"method_not_allowed",
	)
	UnknownError = createMessage(
		common.HTTPCodeInternalServerError,
		"An unknown error occurred: {{.Error}}.",
		"unknown_error",
	)
)

func MethodNotAllowedMessage() string {
	return fmt.Sprintf("Method not allowed: %s", common.LOG_METHOD)
}

type MessageCode struct {
	status int
	one    string
	code   string
}

func (m *MessageCode) GetStatusCode() int {
	return m.status
}

func (m *MessageCode) GetCode() string {
	return m.code
}

func (m *MessageCode) GetMessage() string {
	return m.one
}

func createMessage(status int, one string, code string) *MessageCode {
	return &MessageCode{
		status,
		one,
		code,
	}
}

func GetErrorMessage(messageCode *MessageCode, messageParams ...any) string {
	msg := messageCode.GetMessage()
	params := make(map[string]any)
	for i := 0; i < len(messageParams); i += 2 {
		param := messageParams[i]
		var paramValue any
		if i+1 < len(messageParams) {
			paramValue = messageParams[i+1]
		} else {
			paramValue = "NOT_DEFINED" // this is a placeholder for a missing parameter value - if you see this value then the code needs to be fixed
		}
		params[param.(string)] = paramValue
	}

	tmpl, _ := template.New("errmfs").Parse(msg)
	out := bytes.NewBuffer(nil)
	err := tmpl.Execute(out, params)
	if err != nil {
		return "INVALID TEMPLATE"
	}
	return out.String()
}
