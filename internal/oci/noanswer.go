package oci

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RequestTimeout bounds one registry request, body included. The registry client and
// the image search deadline both read it, so the delay and the text that reports it
// cannot drift apart.
const RequestTimeout = 15 * time.Second

// NoAnswerError is the error of a registry that did not answer within After. It
// unwraps to context.DeadlineExceeded and to nothing else: the request, its URL, its
// credentials and its query never come back with it.
//
// It does not tell the client's own delay from the caller's context deadline; both end
// as the same error, with the same text.
type NoAnswerError struct {
	After time.Duration
}

// Error reads "no answer within N s", N being After in whole seconds.
func (e *NoAnswerError) Error() string {
	return fmt.Sprintf("no answer within %d s", int(e.After/time.Second))
}

// Unwrap returns context.DeadlineExceeded, so a caller testing for a deadline still
// finds one.
func (e *NoAnswerError) Unwrap() error { return context.DeadlineExceeded }

// asNoAnswer returns the *NoAnswerError err is, wraps, or (for a deadline) becomes,
// carrying RequestTimeout; it is nil for any other error, a cancellation included.
func asNoAnswer(err error) *NoAnswerError {
	var na *NoAnswerError
	switch {
	case errors.As(err, &na):
		return na
	case errors.Is(err, context.DeadlineExceeded):
		return &NoAnswerError{After: RequestTimeout}
	}
	return nil
}

// noAnswer is err, or the *NoAnswerError it amounts to when it is a deadline.
func noAnswer(err error) error {
	if na := asNoAnswer(err); na != nil {
		return na
	}
	return err
}

// NoAnswer is err, or the *NoAnswerError it amounts to when it is a deadline, for the
// callers outside this package that run a registry request of their own.
//
// RED-STAGE STUB (DEVDESK-674): it returns err untouched so the tests compile and fail.
func NoAnswer(err error) error { return err }
