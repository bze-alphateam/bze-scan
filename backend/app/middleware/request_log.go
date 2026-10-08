package middleware

import (
	"time"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// RequestLog writes one info line per request with its method, path, status,
// duration and request id. A request that fails is logged with the status
// the error handler will answer. Install it after RequestID and before
// Recover, so a panic is logged as the 500 it becomes.
func RequestLog(logger log.FieldLogger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			err := next(c)
			_, status := echo.ResolveResponseStatus(c.Response(), err)
			reqID, _ := c.Get(RequestIDContextKey).(string)
			logger.WithFields(log.Fields{
				"method":      c.Request().Method,
				"path":        c.Request().URL.Path,
				"status":      status,
				"duration_ms": float64(time.Since(start).Microseconds()) / 1000,
				"request_id":  reqID,
			}).Info("request")
			return err
		}
	}
}
