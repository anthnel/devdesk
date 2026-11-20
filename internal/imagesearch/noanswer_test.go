package imagesearch

import (
	"fmt"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-629 step 4 (F5): a registry that did not answer in time is a failure of the
// search, as the bare context deadline it replaces was. This file names oci.NoAnswerError,
// so it does not compile until that type exists.
func TestClassifyNoAnswerIsFailed(t *testing.T) {
	noAnswer := &oci.NoAnswerError{After: oci.RequestTimeout}
	failed := Outcome{Verdict: VerdictFailed}
	for _, tt := range []struct {
		name    string
		err     error
		partial bool
	}{
		{"no answer", noAnswer, false},
		{"no answer, wrapped", fmt.Errorf("catalog walk: %w", noAnswer), false},
		{"no answer on a partial walk", noAnswer, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err, 0, tt.partial); got != failed {
				t.Errorf("Classify(%v) = %+v, want %+v", tt.err, got, failed)
			}
		})
	}
}
