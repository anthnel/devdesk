package ociresources

import (
	"errors"
	"fmt"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-629 step 4: the assertions that name oci.RequestTimeout and oci.NoAnswerError. They
// do not compile until the step's code exists.

// imageSearchDeadline and the registry client share one constant: they cannot drift.
func TestImageSearchDeadlineIsTheClientTimeout(t *testing.T) {
	if productionSearchDeadline != oci.RequestTimeout {
		t.Errorf("imageSearchDeadline = %v, want oci.RequestTimeout (%v)", productionSearchDeadline, oci.RequestTimeout)
	}
}

func TestRegistryFailureReasonForNoAnswer(t *testing.T) {
	noAnswer := &oci.NoAnswerError{After: oci.RequestTimeout}
	want := noAnswer.Error()
	for name, err := range map[string]error{
		"bare":    noAnswer,
		"wrapped": fmt.Errorf("registry tags: %w", noAnswer),
	} {
		if got := registryFailureReason(err, accessOther); got != want {
			t.Errorf("%s: registryFailureReason = %q, want %q", name, got, want)
		}
	}
	// The error built by the real client is the same one.
	var ne *oci.NoAnswerError
	if err := expiredTagsErr(t); !errors.As(err, &ne) || registryFailureReason(err, accessOther) != want {
		t.Errorf("real client error %v: reason %q, want %q", err, registryFailureReason(err, accessOther), want)
	}
}
