package netdiag

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The height a WindowSizeMsg carries is the viewport's content height: the
// router has already taken the header, the title line, the border and the
// footer off it (Rule 124). The Diagnostics table and the details pane must
// fill exactly that, as the Ports, Interfaces and Forward tables do. They used
// to take 13 more lines off it, a leftover of the days the view drew its own
// header and footer, so the table filled about half the viewport and its last
// rows had to be scrolled to while the rest stayed blank (DEVDESK-744).

// contentHeights are the viewport content heights the router hands netdiag
// for terminals of 24, 30, 38 and 60 lines (header 7, blank 1, title 1,
// border 1, footer 3 with the tab row).
var contentHeights = []int{11, 17, 25, 47}

func TestTheChecksTableFillsTheViewportHeight(t *testing.T) {
	states := []struct {
		name  string
		model func(t *testing.T) *Model
	}{
		{"running, nothing answered yet", func(t *testing.T) *Model { return runningModel(t, "example.com") }},
		{"results", resultsModel},
		{"results, every row of the pipeline", func(t *testing.T) *Model {
			return deliver(t, runningModel(t, "example.com"), manyChecks()...)
		}},
		{"results, problems filter on", func(t *testing.T) *Model {
			return feed(t, resultsModel(t), testutil.Key("p"))
		}},
	}
	for _, s := range states {
		for _, h := range contentHeights {
			for _, w := range []int{80, 120, 200} {
				t.Run(fmt.Sprintf("%s/%dx%d", s.name, w, h), func(t *testing.T) {
					m := feed(t, s.model(t), tea.WindowSizeMsg{Width: w, Height: h})
					if got := lipgloss.Height(m.Render()); got != h {
						t.Errorf("the checks table is %d lines tall in a %d-line viewport", got, h)
					}
				})
			}
		}
	}
}

// TestTheChecksTableFillsTheViewportAfterAResize covers the order the router
// actually uses: the size arrives first, the run lands afterwards, then the
// terminal is resized while the results are on screen.
func TestTheChecksTableFillsTheViewportAfterAResize(t *testing.T) {
	m := deliver(t, runningModel(t, "example.com"), manyChecks()...)
	for _, h := range []int{25, 11, 47} {
		m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: h})
		if got := lipgloss.Height(m.Render()); got != h {
			t.Errorf("after a resize to %d lines the checks table is %d lines tall", h, got)
		}
	}
}

// TestTheDetailsPaneFillsTheViewportHeight: the details pane of one check had
// the same 13 lines taken off it.
func TestTheDetailsPaneFillsTheViewportHeight(t *testing.T) {
	for _, h := range contentHeights {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			m := feed(t, resultsModel(t), tea.WindowSizeMsg{Width: 120, Height: h}, testutil.Key("enter"))
			if m.state != StateDetails {
				t.Fatalf("enter did not open the details, state = %v", m.state)
			}
			if got := m.detailsViewport.Height(); got != h {
				t.Errorf("the details pane is %d lines tall in a %d-line viewport", got, h)
			}
		})
	}
}

// manyChecks is one row per check of a full run, more than a short viewport holds.
func manyChecks() []netcheck.Check {
	ids := []netcheck.CheckID{
		netcheck.CheckResolve, netcheck.CheckTCP, netcheck.CheckICMP, netcheck.CheckTLSChain,
	}
	var out []netcheck.Check
	for i := range 12 {
		c := check(ids[i%len(ids)], netcheck.OK, fmt.Sprintf("observation %d", i))
		c.ID = netcheck.CheckID(fmt.Sprintf("%s-%d", c.ID, i))
		out = append(out, c)
	}
	return out
}
