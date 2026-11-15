package netdiag

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-330 step 7: the container, network and trace rows at 80 and 60
// columns, with names that do not fit, wide characters and IPv6.

const (
	// addressCellsAt80 is the Address column at 80 columns (plan H5): 78 minus
	// four paddings of two, minus Hop 3, Step 11 and RTT 9.
	addressCellsAt80 = 47

	longCtrName = "payments-gateway-worker-eu-west-1-blue-green-0123456789" // 55 cells
	longNetName = "customer-facing-backend-overlay-network-01"              // 41 cells
	cjkCtrName  = "日本語のコンテナ名前日本語のコンテナ名前日本語"                                 // 24 wide runes, 48 cells
)

// wholeSGR matches a complete SGR sequence; what an ESC leaves after these are
// removed is a sequence that was cut.
var wholeSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// layoutCase is one container as the engine described it.
type layoutCase struct {
	name string
	ctr  docker.ContainerNetworking
}

func layoutCases() []layoutCase {
	v4 := func(name, net string) docker.ContainerNetworking {
		return docker.ContainerNetworking{
			Name: name, Running: true, Mode: net,
			Networks: []docker.EndpointNetwork{{
				Name: net, IPv4: netip.MustParseAddr("172.18.0.5"), Gateway: netip.MustParseAddr("172.18.0.1"),
			}},
		}
	}
	v6 := v4(longCtrName, longNetName)
	v6.Networks[0].IPv4 = netip.MustParseAddr("2001:db8:85a3:0:0:8a2e:370:7334")
	v6.Networks[0].Gateway = netip.MustParseAddr("2001:db8:85a3:0:0:8a2e:370:1")
	return []layoutCase{
		{"long ascii names", v4(longCtrName, longNetName)},
		{"cjk names", v4(cjkCtrName, "ネットワーク名前ネットワーク名前")},
		{"ipv6", v6},
		{"host mode", docker.ContainerNetworking{Name: longCtrName, Running: true, Mode: "host", Networks: []docker.EndpointNetwork{{Name: "host"}}}},
	}
}

// containerRun is longRun with a container source: the 30-hop trace of longHost
// from the container described by lc, in the given state. With picker set, the
// selector is open over a 40-entry list afterwards.
func containerRun(t *testing.T, width, height int, state string, lc layoutCase, picker bool) *Model {
	t.Helper()
	captureLog(t)
	m := traceReady(t, longHost)
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	src := pathSource{ID: idWeb, Name: lc.ctr.Name}
	m.pathModel.source = src
	m = feed(t, m, testutil.Key("enter"))
	gen := m.pathModel.gen

	route := netcheck.RouteHop{Interface: longIface, Source: net.ParseIP("192.168.1.21"), Gateway: net.ParseIP("192.168.1.1")}
	m = feed(t, m, pathResolvedMsg{gen: gen, addr: netip.MustParseAddr("198.51.100.30"), route: route, source: src, ctr: lc.ctr})
	var hops []nettrace.Hop
	for ttl := 1; ttl <= manyHops; ttl++ {
		h := nettrace.Hop{TTL: ttl, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("2001:db8:85a3:0:0:8a2e:370:7334")}
		if ttl == manyHops {
			h = nettrace.Hop{TTL: ttl, Kind: nettrace.HopTarget, Addr: netip.MustParseAddr("198.51.100.30")}
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
	if picker {
		var rows []docker.Container
		for i := range 40 {
			rows = append(rows, docker.Container{
				ID: fmt.Sprintf("%012x", i+1), Name: fmt.Sprintf("container-%02d-%s", i, strings.Repeat("日", 20)),
				Image: "registry.example.com/team/image", State: "running",
			})
		}
		m.pathModel.engine = (&fakeEngine{psRows: rows}).engine()
		m, cmd := step(t, m, testutil.Key("ctrl+o"))
		m = feed(t, m, testutil.Msg(cmd))
		if !m.pathModel.pickerOpen() {
			t.Fatalf("state %s: the selector did not open", state)
		}
		return m
	}
	return m
}

// Every body line is exactly width-2 cells, no escape sequence is cut, with and
// without the selector. The selector cannot open while a trace runs (refused,
// see TestPathSourceKeyIsRefusedWithAReason), so running is covered without it.
func TestPathContainerRowsFitTheTerminalWidth(t *testing.T) {
	for _, lc := range layoutCases() {
		for _, width := range []int{80, 60} {
			for _, state := range []string{"running", "done", "canceled"} {
				for _, picker := range []bool{false, true} {
					if picker && state == "running" {
						continue
					}
					name := fmt.Sprintf("%s/%d/%s/picker=%v", lc.name, width, state, picker)
					t.Run(name, func(t *testing.T) {
						m := containerRun(t, width, 24, state, lc, picker)
						body := m.Render()
						for i, line := range strings.Split(body, "\n") {
							if w := ansi.StringWidth(line); w != width-2 {
								t.Errorf("body line %d is %d cells wide, want %d: %q", i, w, width-2, ansi.Strip(line))
							}
						}
						if got := m.pathModel.table.RenderedWidth(); got != width-2 {
							t.Errorf("RenderedWidth = %d, want %d (Rule 116)", got, width-2)
						}
						// Checked on the RAW text: ansi.Strip swallows a cut sequence,
						// so every ESC must open a whole SGR sequence instead.
						if rest := wholeSGR.ReplaceAllString(body, ""); strings.ContainsRune(rest, '\x1b') {
							t.Errorf("an escape sequence is not a whole SGR sequence (cut?):\n%q", rest)
						}
						plain := ansi.Strip(body)
						if picker {
							return // the selector covers the first rows by design
						}
						if !strings.Contains(plain, "container") || !strings.Contains(plain, "network") {
							t.Errorf("the container and network rows are missing:\n%s", plain)
						}
						if !strings.Contains(plain, "…") {
							t.Errorf("no truncation mark although names exceed the column:\n%s", plain)
						}
					})
				}
			}
		}
	}
}

// The trace row, in full at 80 columns, truncated at 60 (the Address column is
// 27 cells there).
func TestPathTraceRowAtTheTwoWidths(t *testing.T) {
	// The bridge variant says textTraceFromHost, the host-network variant
	// textTraceSharedHost (45 cells: whole in the 47 of the Address column at
	// 80, cut in the 27 at 60).
	if got := ansi.StringWidth(textTraceSharedHost); got > addressCellsAt80 || got <= 27 {
		t.Fatalf("textTraceSharedHost is %d cells, the test needs 28..%d", got, addressCellsAt80)
	}
	variants := []struct{ name, text string }{
		{"long ascii names", textTraceFromHost},
		{"host mode", textTraceSharedHost},
	}
	for _, v := range variants {
		var lc layoutCase
		for _, c := range layoutCases() {
			if c.name == v.name {
				lc = c
			}
		}
		if lc.name == "" {
			t.Fatalf("no layout case %q", v.name)
		}
		for _, state := range []string{"running", "done", "canceled"} {
			t.Run(v.name+"/"+state, func(t *testing.T) {
				at80 := ansi.Strip(containerRun(t, 80, 24, state, lc, false).Render())
				if !strings.Contains(at80, v.text) {
					t.Errorf("80 columns: %q is not whole:\n%s", v.text, at80)
				}
				at60 := ansi.Strip(containerRun(t, 60, 24, state, lc, false).Render())
				if strings.Contains(at60, v.text) || !strings.Contains(at60, "from this machine") || !strings.Contains(at60, "…") {
					t.Errorf("60 columns: want the trace text cut with a mark:\n%s", at60)
				}
			})
		}
	}
}

// The Address column is 47 cells at 80 columns: a 47-cell name is whole, a
// 48-cell one is cut. Wide runes count two cells.
func TestPathAddressColumnIs47CellsAt80(t *testing.T) {
	cases := []struct {
		name  string
		cells string
		whole bool
	}{
		{"ascii 47", strings.Repeat("a", addressCellsAt80), true},
		{"ascii 48", strings.Repeat("a", addressCellsAt80+1), false},
		{"cjk 46", strings.Repeat("日", 23), true},
		{"cjk 48", strings.Repeat("日", 24), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// An inspection that failed leaves the bare name in the container row.
			m := traceReady(t, "example.org")
			m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
			captureLog(t)
			src := pathSource{ID: idWeb, Name: tc.cells}
			m.pathModel.source = src
			m = feed(t, m, testutil.Key("enter"))
			m = feed(t, m, pathResolvedMsg{gen: m.pathModel.gen, addr: netip.MustParseAddr("1.1.1.1"), route: defaultRouteHop(), source: src, ctrErr: errors.New("x")})

			plain := ansi.Strip(m.Render())
			if got := strings.Contains(plain, tc.cells); got != tc.whole {
				t.Errorf("%q whole = %v, want %v:\n%s", tc.cells, got, tc.whole, plain)
			}
			if !tc.whole && !strings.Contains(plain, "…") {
				t.Errorf("a cut name carries no mark:\n%s", plain)
			}
		})
	}
}

// 36 rows in a short body: end shows the last hop, home the container row again.
func TestPathContainerColumnScrolls(t *testing.T) {
	const last = "198.51.100.30"
	for _, state := range []string{"running", "done", "canceled"} {
		t.Run(state, func(t *testing.T) {
			m := containerRun(t, 80, 24, state, layoutCases()[2], false)
			if rows := len(m.pathModel.table.Items()); rows != 6+manyHops {
				t.Fatalf("rows = %d, want %d (container, network, source, interface, gateway, trace, 30 hops)", rows, 6+manyHops)
			}
			if p := ansi.Strip(m.Render()); !strings.Contains(p, "container") || strings.Contains(p, last) {
				t.Fatalf("initially: want the container row and not the last hop:\n%s", p)
			}
			m = feed(t, m, testutil.Key("end"))
			if p := ansi.Strip(m.Render()); !strings.Contains(p, last) || strings.Contains(p, textTraceFromHost) {
				t.Errorf("after end: want the last hop and not the trace row:\n%s", p)
			}
			m = feed(t, m, testutil.Key("home"))
			if p := ansi.Strip(m.Render()); !strings.Contains(p, "container") || strings.Contains(p, last) {
				t.Errorf("after home: want the container row back:\n%s", p)
			}
		})
	}
}
