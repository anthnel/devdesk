package explorer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-147 / DEVDESK-225 step 5, help half (Rule 114): the `?` overlay carries
// the shared legend and no longer talks about a spinner in the first column.

func TestHelpCarriesTheActivityLegend(t *testing.T) {
	want := indicator.Legend(indicator.CloneLegend...)
	var body string
	n := 0
	for _, s := range loadedModel(t).GetHelpContent().Sections {
		if s.Title == want.Title {
			body = s.Body
			n++
		}
	}
	if n != 1 {
		t.Fatalf("help has %d %q sections, want exactly 1", n, want.Title)
	}
	if body == "" {
		t.Fatalf("help has no %q section", want.Title)
	}
	if body != want.Body {
		t.Errorf("the legend was altered: the explorer shows running, queued, failed and idle, the clone list adds cloned and skipped\n%s", body)
	}
}

func TestHelpNoLongerMentionsASpinner(t *testing.T) {
	c := loadedModel(t).GetHelpContent()
	for _, kb := range c.KeyBindings {
		if strings.Contains(strings.ToLower(kb.Description), "spinner") {
			t.Errorf("stale help for %q: %s", kb.Key, kb.Description)
		}
	}
	for _, s := range c.Sections {
		if strings.Contains(strings.ToLower(s.Body), "spinner") {
			t.Errorf("section %q still mentions a spinner:\n%s", s.Title, s.Body)
		}
	}
}

// What the help says about a create or a delete in flight must match what the
// row does: a blinking dot, no way to stop it, K greyed in :jobs.
func TestHelpDescribesTheBlinkingDotAndTheFailedDelete(t *testing.T) {
	sections := map[string]string{}
	for _, s := range loadedModel(t).GetHelpContent().Sections {
		sections[s.Title] = s.Body
	}
	tests := []struct{ section, want string }{
		{"Row Icons", "blinking dot"},
		{"Row Icons", indicator.Glyph(indicator.Failed, false)},
		{"Creating Groups and Projects", "blinking dot"},
		{"Deleting", "cannot be stopped"},
		{"Deleting", "K"},
	}
	for _, tt := range tests {
		body, ok := sections[tt.section]
		if !ok {
			t.Fatalf("help has no %q section", tt.section)
		}
		if !strings.Contains(body, tt.want) {
			t.Errorf("section %q does not say %q:\n%s", tt.section, tt.want, body)
		}
	}
}

// DEVDESK-148: the Cloning section describes the clone list's first column.
func TestHelpCloningDescribesTheFirstColumn(t *testing.T) {
	var body string
	for _, s := range loadedModel(t).GetHelpContent().Sections {
		if s.Title == "Cloning" {
			body = s.Body
		}
	}
	if body == "" {
		t.Fatal("help has no Cloning section")
	}
	if strings.Contains(body, "each row showing what it is doing") {
		t.Errorf("Cloning still has the vague wording:\n%s", body)
	}
	for name, want := range map[string]string{
		"queued":  indicator.Glyph(indicator.Queued, false),
		"done":    indicator.Glyph(indicator.Done, false),
		"skipped": indicator.Glyph(indicator.Skipped, false),
		"failed":  indicator.Glyph(indicator.Failed, false),
		"dot":     "blinking dot",
		"footer":  "footer",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Cloning does not mention %s (%q):\n%s", name, want, body)
		}
	}
}
