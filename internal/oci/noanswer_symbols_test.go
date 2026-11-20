package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// DEVDESK-629 step 4: the assertions that name the symbols of the step, RequestTimeout
// and NoAnswerError. They do not compile until noanswer.go exists.

func TestRequestTimeoutIsUnchanged(t *testing.T) {
	if RequestTimeout != 15*time.Second {
		t.Errorf("RequestTimeout = %v, want the 15 s the client has always had", RequestTimeout)
	}
	if registryHTTPClient.Timeout != RequestTimeout {
		t.Errorf("registryHTTPClient.Timeout = %v, want RequestTimeout (%v)", registryHTTPClient.Timeout, RequestTimeout)
	}
	// The text comes from the constant, not from a literal.
	want := fmt.Sprintf("no answer within %d s", int(RequestTimeout/time.Second))
	if got := (&NoAnswerError{After: RequestTimeout}).Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNoAnswerErrorShape(t *testing.T) {
	err := &NoAnswerError{After: 3 * time.Second}
	if got, want := err.Error(), "no answer within 3 s"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got := errors.Unwrap(err); got != context.DeadlineExceeded {
		t.Errorf("Unwrap() = %v, want context.DeadlineExceeded and nothing else", got)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is(err, context.DeadlineExceeded) = false")
	}
	if errors.Is(err, context.Canceled) {
		t.Error("errors.Is(err, context.Canceled) = true")
	}
	var ne *NoAnswerError
	if !errors.As(fmt.Errorf("registry tags: %w", err), &ne) || ne.After != 3*time.Second {
		t.Errorf("errors.As through a wrap failed: %v", ne)
	}
}

// The error of a real slow registry carries the constant, not the shrunk delay the test
// gave the client.
func TestSlowRegistryNoAnswerCarriesTheConstant(t *testing.T) {
	withClientTimeout(t, shortClientTimeout)
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) { blockUntilGone(r) })

	_, err := ListRegistryTagsContext(context.Background(), srv.URL, "library/nginx", "", "")

	var ne *NoAnswerError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %v (%T), want a *NoAnswerError", err, err)
	}
	if ne.After != RequestTimeout {
		t.Errorf("After = %v, want RequestTimeout (%v)", ne.After, RequestTimeout)
	}
	if want := (&NoAnswerError{After: RequestTimeout}).Error(); err.Error() != want {
		t.Errorf("err text = %q, want exactly %q: nothing else comes with it", err.Error(), want)
	}
}
