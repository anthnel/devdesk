package app

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
)

// DEVDESK-328 step 5: the Path tab's model and target parsing sit under the
// activity-column guard (no spinner, glyph literal or Frame field). path_run.go
// holds the I/O and stays out, like ports_commands.go.
func TestActivityGuardScansThePathTabSources(t *testing.T) {
	got := migratedSources[command.ViewNetdiag]
	for _, want := range []string{"ui/netdiag/path_model.go", "ui/netdiag/path_target.go"} {
		if !slices.Contains(got, want) {
			t.Errorf("migratedSources[%q] lacks %q", command.ViewNetdiag, want)
		}
	}
	if slices.Contains(got, "ui/netdiag/path_run.go") {
		t.Error("path_run.go holds the I/O and is not scanned, like ports_commands.go")
	}
}
