package filebrowser

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-341 step 2g: this view's status never sets Spinner, so the router frame
// it used to store in its footer was a dead write. A frame must leave the footer
// untouched: it holds no spinner frame, and starts no timer.

func TestAFrameLeavesTheFooterWithoutASpinnerFrame(t *testing.T) {
	m := openAt(t, tree(t))
	before := m.RenderFooter(80)

	m, cmd := step(t, m, jobs.FrameMsg{Blink: true})

	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
	if got := m.RenderFooter(80); got != before {
		t.Errorf("a frame changed the rendered footer:\n got: %q\nwant: %q", got, before)
	}
	// Read the component directly: a status that asked for a spinner would show
	// the frame the message brought.
	got := m.footer.View(80, sharedcomponents.Status{Text: "Deleting", Spinner: true})
	if strings.Contains(got, "⠹") {
		t.Errorf("the router frame was stored in the footer (dead write):\n%s", got)
	}
}
