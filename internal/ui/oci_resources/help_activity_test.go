package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-146 step 4, Rule 114: the help carries the shared activity legend.
func TestHelpCarriesTheActivityLegend(t *testing.T) {
	want := indicator.Legend()
	for _, s := range loadedModel(t).GetHelpContent().Sections {
		if s.Title == want.Title {
			if s.Body != want.Body {
				t.Errorf("the %q section is not indicator.Legend() verbatim:\n got %q\nwant %q", want.Title, s.Body, want.Body)
			}
			return
		}
	}
	t.Fatalf("the help has no %q section", want.Title)
}
