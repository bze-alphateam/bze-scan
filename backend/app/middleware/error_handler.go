// Package middleware holds the HTTP middleware of the API: request id,
// panic recovery and the global error handler with its JSON error envelope.
package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// ErrorBody is the error envelope every failed request answers with:
//
//	{"error": {"code": "not_found", "message": "Not Found"}}
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries a machine-readable code and a human message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorCodes maps HTTP statuses to the envelope's machine-readable code.
var errorCodes = map[int]string{
	http.StatusBadRequest:            "bad_request",
	http.StatusNotFound:              "not_found",
	http.StatusMethodNotAllowed:      "method_not_allowed",
	http.StatusRequestTimeout:        "request_timeout",
	http.StatusRequestEntityTooLarge: "payload_too_large",
	http.StatusTooManyRequests:       "rate_limit_exceeded",
	http.StatusServiceUnavailable:    "service_unavailable",
}

// ErrorHandler is the global echo error handler. An error carrying an HTTP
// status (echo's sentinel errors, *echo.HTTPError) keeps it; anything else is
// a 500 with a generic message, logged with the request id. Internal error
// details are never sent to clients.
func ErrorHandler(c *echo.Context, err error) {
	if resp, _ := echo.UnwrapResponse(c.Response()); resp != nil && resp.Committed {
		return
	}

	status := echo.StatusCode(err)
	if status == 0 || status >= http.StatusInternalServerError {
		if status == 0 {
			status = http.StatusInternalServerError
		}
		reqID, _ := c.Get(RequestIDContextKey).(string)
		log.WithFields(log.Fields{
			"request_id": reqID,
			"method":     c.Request().Method,
			"path":       c.Request().URL.Path,
			"status":     status,
		}).WithError(err).Error("request failed")
	}

	body := ErrorBody{Error: ErrorDetail{Code: codeFor(status), Message: messageFor(status, err)}}

	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(status)
		return
	}
	_ = c.JSON(status, body)
}

func codeFor(status int) string {
	if code, ok := errorCodes[status]; ok {
		return code
	}
	if status >= http.StatusInternalServerError {
		return "internal_error"
	}
	return "http_error"
}

// messageFor keeps the message of a client error raised on purpose
// (*echo.HTTPError) and falls back to the status text otherwise. Server
// errors always get a generic message.
func messageFor(status int, err error) string {
	if status >= http.StatusInternalServerError {
		return "An unexpected error occurred"
	}
	if he, ok := err.(*echo.HTTPError); ok && strings.TrimSpace(he.Message) != "" {
		return he.Message
	}
	return http.StatusText(status)
}
