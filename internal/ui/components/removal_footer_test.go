package components

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
)

// The level of the bilan follows what happened (OC-22, Rule 128): removed or gone is
// Info, a refusal the engine explained is a Warning, one it did not is an Error.
func TestRemovalFooterLevels(t *testing.T) {
	removed := docker.RemovalResult{Status: docker.RemovalRemoved}
	gone := docker.RemovalResult{Status: docker.RemovalGone}
	running := docker.RemovalResult{Status: docker.RemovalRefused, Reason: docker.ReasonRunning}
	other := docker.RemovalResult{Status: docker.RemovalRefused, Reason: docker.ReasonOther}
	tests := []struct {
		name   string
		report docker.RemovalReport
		want   Level
	}{
		{"removed", docker.RemovalReport{removed}, LevelInfo},
		{"already gone", docker.RemovalReport{removed, gone}, LevelInfo},
		{"explained refusal", docker.RemovalReport{removed, running}, LevelWarning},
		{"unexplained refusal", docker.RemovalReport{running, other}, LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f FooterMessage

			if cmd := f.Removal(tt.report); cmd == nil {
				t.Error("the message has no expiry timer")
			}

			if f.Level() != tt.want || f.Text() != tt.report.Summary() {
				t.Errorf("footer = %q at %v, want %q at %v", f.Text(), f.Level(), tt.report.Summary(), tt.want)
			}
		})
	}
}

// The confirmation names what goes, says that the items it does not show go too, and
// never says "all".
func TestRemovalPreviewListsTheFrozenItems(t *testing.T) {
	var names []string
	for i := range 10 {
		names = append(names, "item"+string(rune('a'+i)))
	}

	got := RemovalPreview("stopped containers", names)

	for _, want := range []string{"Remove these 10 stopped containers?", "itema", "itemh", "and 2 more (not shown)", "These 10 will be removed, including the 2 not shown."} {
		if !strings.Contains(got, want) {
			t.Errorf("preview = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "itemi") || strings.Contains(strings.ToLower(got), "all") {
		t.Errorf("preview = %q lists more than the first eight or says all", got)
	}

	short := RemovalPreview("unused volumes", []string{"a", "b"})
	if !strings.Contains(short, "Exactly the items listed above will be removed.") || strings.Contains(short, "not shown") {
		t.Errorf("preview = %q, want the plain statement when nothing is hidden", short)
	}
}

// What the engine named is drawn as text: no escape sequence, no control or
// bidirectional character.
func TestPrintableDrawsNothingTheEngineWrote(t *testing.T) {
	tests := map[string]string{
		"a color sequence":     "api\x1b[31mred",
		"a title sequence":     "api\x1b]0;owned\x07",
		"a bidirectional mark": "api\u202egpj",
		"a newline":            "api\nweb",
		"a carriage return":    "api\rweb",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			got := Printable(in)
			for _, r := range got {
				if r < ' ' || r == 0x7f || r == '\u202e' {
					t.Errorf("Printable(%q) = %q still holds %U", in, got, r)
				}
			}
			if !strings.HasPrefix(got, "api") {
				t.Errorf("Printable(%q) = %q lost the visible text", in, got)
			}
		})
	}
}

// A list that reloads drops the error of the previous load, not the bilan of a
// removal: that one leaves with its timer, or when something else replaces it.
func TestClearUnlessRemoval(t *testing.T) {
	removed := docker.RemovalReport{{Status: docker.RemovalRemoved}}

	var bilan FooterMessage
	bilan.Removal(removed)
	bilan.ClearUnlessRemoval()
	if bilan.Text() != "1 removed" {
		t.Errorf("the bilan = %q after a refresh, want it kept", bilan.Text())
	}
	id := bilan.ID()
	if !bilan.Handle(ClearFooterMsg{ID: id}) || bilan.IsSet() {
		t.Error("the bilan did not leave with its own timer")
	}

	var oldError FooterMessage
	oldError.Error("Failed to load — check logs")
	oldError.ClearUnlessRemoval()
	if oldError.IsSet() {
		t.Error("a refresh kept the error of the previous load")
	}

	var replaced FooterMessage
	replaced.Removal(removed)
	replaced.Error("Failed to load — check logs")
	replaced.ClearUnlessRemoval()
	if replaced.IsSet() {
		t.Error("a message that replaced the bilan was still protected as one")
	}
}
