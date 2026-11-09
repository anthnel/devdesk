package templates

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-149 criterion 5: the help carries the activity legend, once, in
// normal and in selection mode.
func TestHelpCarriesTheActivityLegend(t *testing.T) {
	want := indicator.Legend()
	for name, m := range map[string]Model{
		"normal":    NewWithPath(config.Default(), nil, t.TempDir()+"/templates.yaml"),
		"selection": NewForSelection(nil, nil, "Pick a template"),
	} {
		count := 0
		for _, s := range m.GetHelpContent().Sections {
			if s.Title != want.Title {
				continue
			}
			count++
			if s.Body != want.Body {
				t.Errorf("%s mode: the legend was altered:\n%s", name, s.Body)
			}
		}
		if count != 1 {
			t.Errorf("%s mode: help has %d %q sections, want 1", name, count, want.Title)
		}
	}
}

// DEVDESK-149 Step 4: the Scanning and Syncing sections say that the first
// column blinks during the operation, that a failure leaves the failed glyph on
// the row until the next scan or sync, and that the footer gives the cause.
func TestHelpDescribesRowActivityInScanAndSync(t *testing.T) {
	running := indicator.Glyph(indicator.Running, true)
	failed := indicator.Glyph(indicator.Failed, false)
	for name, m := range map[string]Model{
		"normal":    NewWithPath(config.Default(), nil, t.TempDir()+"/templates.yaml"),
		"selection": NewForSelection(nil, nil, "Pick a template"),
	} {
		for title, op := range map[string]string{"Scanning": "scan", "Syncing": "sync"} {
			var body string
			for _, s := range m.GetHelpContent().Sections {
				if s.Title == title {
					body = s.Body
				}
			}
			if body == "" {
				t.Fatalf("%s mode: no %q section", name, title)
			}
			for _, want := range []string{
				"first column blinks " + running + " while the " + op + " runs",
				"failed " + op + " leaves " + failed + " on the row until the next scan or sync",
				"the footer gives the cause when the row is selected",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s mode, %s: missing %q", name, title, want)
				}
			}
		}
	}
}
