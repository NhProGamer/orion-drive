// Package serializer defines the uniform JSON envelope used by the API.
package serializer

import "net/http"

// Response is the standard API envelope: {code, data, msg}. Code 0 means success.
type Response struct {
	Code int    `json:"code"`
	Data any    `json:"data,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

// Application error codes.
const (
	CodeOK           = 0
	CodeBadRequest   = 40000
	CodeUnauthorized = 40100
	CodeForbidden    = 40300
	CodeNotFound     = 40400
	CodeConflict     = 40900
	CodeInternal     = 50000
)

// OK wraps a successful payload.
func OK(data any) Response { return Response{Code: CodeOK, Data: data} }

// Err builds an error response.
func Err(code int, msg string) Response { return Response{Code: code, Msg: msg} }

// HTTPStatus maps an application code to an HTTP status code.
func (r Response) HTTPStatus() int {
	switch r.Code {
	case CodeOK:
		return http.StatusOK
	case CodeBadRequest:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
