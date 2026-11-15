package netdiag

import (
	"net"
	"net/netip"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-329 step 4: the column at the view level (width, scrolling, help).

const (
	longIface = "enp0s31f6-with-a-very-long-predictable-name" // 42 cells
	longHost  = "a-very-long-host-name-for-the-path-column-width-test.example.com"
	manyHops  = 30
)

// longRun returns a full netdiag.Model on the trace sub-level, sized width x height,
// with the 30-hop trace of longHost in the given state: running, done, canceled.
func longRun(t *testing.T, width, height int, state string) *Model {
	t.Helper()
	m := traceReady(t, longHost)
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	m = feed(t, m, testutil.Key("enter"))
	gen := m.pathModel.gen

	route := netcheck.RouteHop{Interface: longIface, Source: net.ParseIP("192.168.1.21"), Gateway: net.ParseIP("192.168.1.1")}
	m = feed(t, m, pathResolvedMsg{gen: gen, addr: netip.MustParseAddr("198.51.100.30"), route: route})

	var hops []nettrace.Hop
	for ttl := 1; ttl <= manyHops; ttl++ {
		h := nettrace.Hop{TTL: ttl, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("2001:db8:85a3:0:0:8a2e:370:7334")}
		switch {
		case ttl == manyHops:
			h = nettrace.Hop{TTL: ttl, Kind: nettrace.HopTarget, Addr: netip.MustParseAddr("198.51.100.30")}
		case ttl%4 == 0:
			h = nettrace.Hop{TTL: ttl, Kind: nettrace.HopSilent}
		case ttl%2 == 0:
			h.Addr = netip.MustParseAddr("10.0.0." + string(rune('0'+ttl%10)))
		}
		hops = append(hops, h)
		m = feed(t, m, pathHopMsg{gen: gen, hop: h})
	}
	switch state {
	case "done":
		m = feed(t, m, pathDoneMsg{gen: gen, result: nettrace.Result{Outcome: nettrace.OutcomeReached, Hops: hops}})
	case "canceled":
		m = feed(t, m, testutil.Key("esc"))
	}
	return m
}

// Criterion 3: no line overflows and no escape sequence is cut. A cut escape
// would eat visible cells, so the exact width is what detects it.
func TestPathColumnFitsTheTerminalWidth(t *testing.T) {
	for _, width := range []int{80, 60} {
		for _, state := range []string{"running", "done", "canceled"} {
			t.Run(strings.Join([]string{state, string(rune('0' + width/10))}, "-"), func(t *testing.T) {
				m := longRun(t, width, 24, state)
				body := m.Render()
				for i, line := range strings.Split(body, "\n") {
					if w := ansi.StringWidth(line); w != width-2 {
						t.Errorf("body line %d is %d cells wide, want %d: %q", i, w, width-2, ansi.Strip(line))
					}
				}
				if got := m.pathModel.table.RenderedWidth(); got != width-2 {
					t.Errorf("RenderedWidth = %d, want %d (Rule 116)", got, width-2)
				}
				plain := ansi.Strip(body)
				if strings.ContainsRune(plain, '\x1b') {
					t.Errorf("an escape sequence survives the strip (cut sequence):\n%q", plain)
				}
				if !strings.Contains(plain, "this machine") {
					t.Errorf("body lacks %q:\n%s", "this machine", plain)
				}
				if !strings.Contains(plain, "…") {
					t.Errorf("the %d-cell interface name is not truncated with a mark:\n%s", len(longIface), plain)
				}
				for i, line := range strings.Split(m.RenderFooter(width), "\n") {
					if w := ansi.StringWidth(line); w > width {
						t.Errorf("footer line %d is %d cells wide, want <= %d", i, w, width)
					}
				}
			})
		}
	}
}

// Criterion 4: more rows than lines, the navigation keys scroll the column in
// every state (the table takes them whatever the state).
func TestPathColumnScrollsWithTheNavigationKeys(t *testing.T) {
	const first, last = "this machine", "198.51.100.30"
	for _, state := range []string{"running", "done", "canceled"} {
		t.Run(state, func(t *testing.T) {
			m := longRun(t, 80, 24, state)
			plain := func() string { return ansi.Strip(m.Render()) }

			if p := plain(); !strings.Contains(p, first) || strings.Contains(p, last) {
				t.Fatalf("initially the body should show the source and not the last hop:\n%s", p)
			}
			m = feed(t, m, testutil.Key("end"))
			if p := plain(); !strings.Contains(p, last) || strings.Contains(p, first) {
				t.Errorf("after end: want the last hop and not the source:\n%s", p)
			}
			m = feed(t, m, testutil.Key("home"))
			if p := plain(); !strings.Contains(p, first) || strings.Contains(p, last) {
				t.Errorf("after home: want the source back:\n%s", p)
			}
			before := m.pathModel.table.Cursor()
			m = feed(t, m, testutil.Key("pgdown"))
			if after := m.pathModel.table.Cursor(); after <= before+1 {
				t.Errorf("pgdown moved the cursor from %d to %d, want more than one row", before, after)
			}
		})
	}
}

// At the path model level esc is not its key (the Model owns it): from done the
// model is untouched, so the route and the local rows stay.
func TestPathModelIgnoresEscInDone(t *testing.T) {
	pm := resolvedWith(t, lanRoute(), nil)
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: nettrace.Result{
		Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{targetHop(1)},
	}})
	pm, _ = pmFeed(t, pm, testutil.Key("esc"))

	if pm.state != pathDone {
		t.Error("esc from done should leave the model in pathDone")
	}
	if pm.route == nil {
		t.Error("route dropped by esc from done")
	}
	steps := pmSteps(pm)
	if len(steps) < 3 || steps[0] != "source" || steps[1] != "interface" || steps[2] != "gateway" {
		t.Errorf("rows after esc = %v, want source, interface, gateway kept first", steps)
	}
}

// At the Model level, esc from a finished trace goes back to the origin screen
// and the finished run (route, local rows) is still what the sub-level holds
// until the next openTrace replaces it.
func TestTraceEscFromDoneGoesBackAndKeepsTheRouteAndTheLocalRows(t *testing.T) {
	m := traceReady(t, "1.1.1.1")
	m = feed(t, m, testutil.Key("enter"))
	gen := m.pathModel.gen
	m = feed(t, m,
		pathResolvedMsg{gen: gen, addr: netip.MustParseAddr("1.1.1.1"), route: lanRoute()},
		pathDoneMsg{gen: gen, result: nettrace.Result{
			Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{targetHop(1)},
		}},
	)
	if m.pathModel.state != pathDone || !m.inTrace() {
		t.Fatalf("precondition: want a finished trace on the sub-level, got state %v inTrace %v", m.pathModel.state, m.inTrace())
	}

	m = feed(t, m, testutil.Key("esc"))

	if m.state != StateInput || m.inTrace() {
		t.Errorf("state = %v after esc from done, want the origin (form)", m.state)
	}
	if m.pathModel.route == nil {
		t.Error("route dropped by esc from done")
	}
	steps := pmSteps(m.pathModel)
	if len(steps) < 3 || steps[0] != "source" || steps[1] != "interface" || steps[2] != "gateway" {
		t.Errorf("rows after esc = %v, want source, interface, gateway kept first", steps)
	}
}

// The help says the column starts on this machine.
func TestPathHelpExplainsTheLocalRows(t *testing.T) {
	for _, s := range New(testConfig()).GetHelpContent().Sections {
		if s.Title != "Diagnostics Tab — Tracing the path" {
			continue
		}
		body := strings.Join(strings.Fields(s.Body), " ")
		for _, want := range []string{"The column starts on this machine", "interface", "source address", "gateway", "routing table"} {
			if !strings.Contains(body, want) {
				t.Errorf("Path help lacks %q", want)
			}
		}
		return
	}
	t.Fatal(`no section "Diagnostics Tab — Tracing the path"`)
}
