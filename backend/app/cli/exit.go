package cli

import "errors"

// ExitError is an error with the process exit code it calls for.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode is the process exit code of a command's error: 0 for nil, the
// code of an ExitError, 1 for anything else.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := errors.AsType[*ExitError](err); ok {
		return e.Code
	}
	return 1
}
