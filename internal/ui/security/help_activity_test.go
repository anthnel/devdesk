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

// DEVDESK-750 step 8 and DEVDESK-751, Rule 114: the help says how a finding's
// detail is laid out (Fix first, then Where, Do, Run, Effect, Why, Output, Refs),
// what each of the six fix statuses means, and where the File column is.
func TestHelpDescribesTheFindingDetails(t *testing.T) {
	body := helpSection(t, "Finding details")

	for _, want := range []string{
		"Fix",                      // the first section
		"Where",                    // then where the finding is
		"Do",                       // what to do, in numbered steps
		"Run",                      // the command
		"Effect",                   // what the fix does
		"Why",                      // the reason, after the fix
		"Output",                   // the scanner's message, capped
		"Refs",                     // the references
		"8 lines",                  // how many lines the Output shows
		"110",                      // and how wide
		"relative",                 // paths shown relative to the scanned directory
		"Lines",                    // the scroll position in the footer
		"Description",              // what Why holds
		"Built-in fix available",   // the six statuses, in words
		"Built-in fix unavailable", // a rule that declines this instance
		"Manual",
		"No fix published",
		"Cannot fix here",
		"No guidance available",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Finding details help does not mention %q:\n%s", want, body)
		}
	}
	// The DEVDESK-750 labels are gone: a help that still says them describes a
	// screen that no longer exists.
	for _, old := range []string{"No fix available", "No built-in fix for", "No built-in fix here", "Fix status", "Remediation shows"} {
		if strings.Contains(body, old) {
			t.Errorf("the Finding details help still says %q:\n%s", old, body)
		}
	}
	if results := helpSection(t, "Results"); !strings.Contains(results, "File") {
		t.Errorf("the Results help does not mention the File column of the Misconfigurations tab:\n%s", results)
	}
}
