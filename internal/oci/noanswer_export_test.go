package oci

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// DEVDESK-667 (S-04b): the Docker Hub search lives outside this package, so the
// conversion of a deadline into a *NoAnswerError is exported as NoAnswer. It must be the
// same conversion the registry client applies, not a second one.

func TestNoAnswerMatchesTheInternalConversion(t *testing.T) {
	existing := &NoAnswerError{After: 3}
	other := errors.New("boom")
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"a bare deadline", context.DeadlineExceeded},
		{"a wrapped deadline", fmt.Errorf("hub search: %w", context.DeadlineExceeded)},
		{"an existing no answer", existing},
		{"a cancellation", context.Canceled},
		{"another error", other},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, want := NoAnswer(tt.err), noAnswer(tt.err)
			if (got == nil) != (want == nil) {
				t.Fatalf("NoAnswer(%v) = %v, want %v", tt.err, got, want)
			}
			var gotNA, wantNA *NoAnswerError
			if errors.As(got, &gotNA) != errors.As(want, &wantNA) || (gotNA != nil && *gotNA != *wantNA) {
				t.Errorf("NoAnswer(%v) = %#v, want %#v", tt.err, got, want)
			}
		})
	}
}

func TestNoAnswerTurnsADeadlineIntoTheConstantNoAnswer(t *testing.T) {
	var ne *NoAnswerError
	got := NoAnswer(fmt.Errorf("hub search: %w", context.DeadlineExceeded))
	if !errors.As(got, &ne) || ne.After != RequestTimeout {
		t.Fatalf("NoAnswer(deadline) = %#v, want a *NoAnswerError{After: RequestTimeout}", got)
	}
	if got := NoAnswer(context.Canceled); errors.As(got, &ne) {
		t.Errorf("a cancellation became a no answer: %v", got)
	}
}
