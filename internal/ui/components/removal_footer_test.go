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

// The confirmation names what goes and never says "all".
func TestRemovalPreviewListsTheFrozenItems(t *testing.T) {
	var names []string
	for i := range 10 {
		names = append(names, "item"+string(rune('a'+i)))
	}

	got := RemovalPreview("stopped containers", names)

	for _, want := range []string{"Remove these 10 stopped containers?", "itema", "itemh", "and 2 more", "Only the items listed here are removed."} {
		if !strings.Contains(got, want) {
			t.Errorf("preview = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "itemi") || strings.Contains(strings.ToLower(got), "remove all") {
		t.Errorf("preview = %q lists more than the first eight or says all", got)
	}
}
