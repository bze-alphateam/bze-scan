package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v5"
)

// Recover turns a panic in a handler into an error carrying the panic value
// and its stack. The error handler logs it with the request id and answers a
// 500 with the generic envelope, so one bad request never takes the process
// down.
func Recover() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) (err error) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				if r == http.ErrAbortHandler {
					panic(r)
				}
				err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}()
			return next(c)
		}
	}
}
