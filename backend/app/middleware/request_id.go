package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/labstack/echo/v5"
)

// RequestIDHeader propagates request ids between client and server.
const RequestIDHeader = "X-Request-Id"

// RequestIDContextKey is the echo context key the resolved id is stored under.
const RequestIDContextKey = "request_id"

// maxClientRequestIDLen bounds a client-supplied id so it cannot bloat logs.
const maxClientRequestIDLen = 128

// RequestID assigns every request an id, stores it in the context and echoes
// it in the response header. A client-supplied id is reused when it is
// reasonably short; otherwise a random 128-bit hex id is generated.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			id := c.Request().Header.Get(RequestIDHeader)
			if id == "" || len(id) > maxClientRequestIDLen {
				id = newRequestID()
			}
			c.Set(RequestIDContextKey, id)
			c.Response().Header().Set(RequestIDHeader, id)
			return next(c)
		}
	}
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b[:])
}
