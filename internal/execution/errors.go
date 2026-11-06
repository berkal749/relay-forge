package execution

import "errors"

// permanentError marks a failure that must not be retried, such as a malformed
// payload or a 4xx response from an upstream service.
type permanentError struct {
	err error
}

func (e *permanentError) Error() string { return e.err.Error() }

func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the execution service records a terminal failure
// instead of scheduling another attempt.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err was wrapped with Permanent.
func IsPermanent(err error) bool {
	var target *permanentError
	return errors.As(err, &target)
}
