package filebrowser

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

// This view shows no jobs line of its own, so the footer component is read
// directly: the router's rendered frame must not be stored in it any more.
func TestAFrameNoLongerFeedsTheFooterSpinner(t *testing.T) {
	m := openAt(t, tree(t))

	m, _ = step(t, m, jobs.FrameMsg{RenderedFrame: "<R1>"})

	got := m.footer.View(80, sharedcomponents.Status{Text: "Deleting", Spinner: true})
	if strings.Contains(got, "<R1>") {
		t.Errorf("the footer stored the router frame:\n%s", got)
	}
}
