// Package validate holds the error types services use to reject input, so the HTTP
// layer can map them the same way for every module: a FieldError per bad field (422)
// and RateLimitError (429).
package validate

import (
	"errors"
	"fmt"
	"time"
)

// FieldError rejects one input field; the API reports it as a validation error.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// FieldErrors returns every FieldError in err (validation joins one per bad field).
func FieldErrors(err error) []*FieldError {
	var out []*FieldError
	var walk func(error)
	walk = func(e error) {
		var fe *FieldError
		switch x := e.(type) {
		case nil:
		case interface{ Unwrap() []error }:
			for _, inner := range x.Unwrap() {
				walk(inner)
			}
		default:
			if errors.As(e, &fe) {
				out = append(out, fe)
			}
		}
	}
	walk(err)
	return out
}

// RateLimitError means too many attempts; retry after RetryAfter.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("too many attempts, retry in %s", e.RetryAfter.Round(time.Second))
}
