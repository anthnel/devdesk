package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-145 step 5, Rule 114: the help describes the activity column.

func helpSection(t *testing.T, title string) string {
	t.Helper()
	for _, s := range New(testConfig(), nil).GetHelpContent().Sections {
		if s.Title == title {
			return s.Body
		}
	}
	t.Fatalf("the help has no %q section", title)
	return ""
}

// The shared legend, unextended: :sec shows running, queued, failed and rest.
func TestHelpCarriesTheActivityLegend(t *testing.T) {
	want := indicator.Legend()
	if got := helpSection(t, want.Title); got != want.Body {
		t.Errorf("the %q section is not indicator.Legend() verbatim:\n got %q\nwant %q", want.Title, got, want.Body)
	}
}

// The Inventory text says what the first column shows, in this view's terms.
func TestHelpInventoryExplainsTheActivityColumn(t *testing.T) {
	body := strings.ToLower(helpSection(t, "Inventory"))
	for _, want := range []string{
		"first column",
		"scanning", // what Scanned reads while a scan runs
		strings.ToLower(indicator.Glyph(indicator.Failed, true)), // the failure mark
		"next attempt", // when it clears
		"footer",       // where the selected row's state is named
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Inventory help does not mention %q:\n%s", want, body)
		}
	}
}
