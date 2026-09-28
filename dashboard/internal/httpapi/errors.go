package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
)

// errorBody is the JSON shape of every API error. code and message have been
// the contract since v1; the other fields are additive and let the dashboard
// translate: message_key/params name a catalog text, detail marks message as
// a passed-through technical error the UI appends to the translated code text.
type errorBody struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	MessageKey string         `json:"message_key,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	ToolOutput string         `json:"tool_output,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, errorBody{Code: code, Message: message})
}

// writeErrorKey is writeError for a code whose text has several variants:
// key is the variant's catalog key, message its German fallback.
func writeErrorKey(w http.ResponseWriter, status int, code, key string, params map[string]any, message string) {
	writeJSONStatus(w, status, errorBody{Code: code, Message: message, MessageKey: key, Params: params})
}

// writeErrorDetail reports err under code. A *uierror.Error in the chain
// supplies the catalog key, any other error travels as detail.
func writeErrorDetail(w http.ResponseWriter, status int, code string, err error) {
	writeJSONStatus(w, status, errorBodyFor(code, err))
}

func writeTinyTuyaError(w http.ResponseWriter, status int, code string, err error) {
	body := errorBodyFor(code, err)
	body.ToolOutput = err.Error()
	writeJSONStatus(w, status, body)
}

func errorBodyFor(code string, err error) errorBody {
	body := errorBody{Code: code, Message: err.Error()}
	if typed, ok := uierror.From(err); ok {
		body.MessageKey, body.Params = typed.Key, typed.Params
	} else {
		body.Detail = body.Message
	}
	return body
}
