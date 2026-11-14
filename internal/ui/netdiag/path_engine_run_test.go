package netdiag

import (
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-332 step 3: the engine row, end to end through the producer and the
// model. The engine and the interface list are always fakes.

var ipv4Re = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)

// feedAll feeds the model what the launch Cmd produces, then every message the
// producer sends until it closes.
func feedAll(t *testing.T, pm *PathModel, cmd tea.Cmd) *PathModel {
	t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		pm, _ = pmFeed(t, pm, msg)
	}
	for msg := range pm.msgs {
		pm, _ = pmFeed(t, pm, msg)
	}
	return pm
}

// engineRows returns the address cell of every engine row.
func engineRows(pm *PathModel) []string {
	var out []string
	for _, s := range pm.table.Items() {
		if s.Kind == stepEngine {
			out = append(out, s.addressCell())
		}
	}
	return out
}

func requireEngine(t *testing.T, pm *PathModel, want string) {
	t.Helper()
	got := engineRows(pm)
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("engine rows = %v, want none (rows %v)", got, rowKinds(pm))
		}
		return
	}
	if !slices.Equal(got, []string{want}) {
		t.Fatalf("engine rows = %v, want [%s] (rows %v)", got, want, rowKinds(pm))
	}
}

func traceRow(pm *PathModel) string {
	for _, s := range pm.table.Items() {
		if s.Kind == stepTrace {
			return s.addressCell()
		}
	}
	return "<no trace row>"
}

// screenLinesOf returns the rendered lines (escape sequences stripped) that
// carry kind in their step column.
func screenLinesOf(pm *PathModel, kind string) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(pm.view()), "\n") {
		if slices.Contains(strings.Fields(l), kind) {
			out = append(out, l)
		}
	}
	return out
}

// Criteria 1 and 5 to 7: every outcome that is not "the bridge was found" and
// not "nothing to read".
func TestPathEngineRowOutcomes(t *testing.T) {
	boom := errors.New("engine-boom-raw")
	tests := []struct {
		name      string
		fake      func() *fakeEngine
		want      string
		wantLog   string
		wantTrace string
	}{
		{"no interface of that name", func() *fakeEngine {
			return bridgeFake("docker0", ifaceWith("eth0", "192.168.1.21/24"))
		}, engineHiddenText, "", traceWithEngineText},
		{"interface without the gateway", func() *fakeEngine {
			return bridgeFake("docker0", ifaceWith("docker0", "10.9.0.1/16"))
		}, engineUnverifiedText, "", traceWithEngineText},
		{"macvlan (empty name)", func() *fakeEngine {
			f := bridgeFake("", ifaceWith("docker0", testGateway+"/16"))
			f.networkVal.Driver = "macvlan"
			return f
		}, engineUnverifiedText, "", traceWithEngineText},
		{"network read fails", func() *fakeEngine {
			f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
			f.networkErr = boom
			return f
		}, engineUnverifiedText, "engine-boom-raw", traceWithEngineText},
		{"interface list fails", func() *fakeEngine {
			f := bridgeFake("docker0")
			f.ifacesErr = boom
			return f
		}, engineUnverifiedText, "engine-boom-raw", traceWithEngineText},
		{"bridge found: bridge row, no engine row", func() *fakeEngine {
			return bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
		}, "", "", traceNoEngineText},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake()
			logs := captureLog(t)
			pm := pmWith(t, f)
			pm.source = webSource
			pm, cmd := launched(t, pm, "1.1.1.1")
			pm = feedAll(t, pm, cmd)

			requireEngine(t, pm, tc.want)
			if tc.want == "" {
				requireBridge(t, pm, "docker0")
			} else {
				requireBridge(t, pm, "")
			}
			if got := traceRow(pm); got != tc.wantTrace {
				t.Errorf("trace row = %q, want %q", got, tc.wantTrace)
			}
			if !slices.Equal(pmHopTTLs(pm), []int{1}) || pm.state != pathDone {
				t.Fatalf("hops %v, state %v: the hops must show and the run be done", pmHopTTLs(pm), pm.state)
			}
			if tc.wantLog == "" {
				if strings.Contains(logs.String(), "ERROR [netdiag/path]") {
					t.Fatalf("unexpected error log %q", logs.String())
				}
				return
			}
			if !strings.Contains(logs.String(), "ERROR [netdiag/path] ") || !strings.Contains(logs.String(), tc.wantLog) {
				t.Fatalf("log %q lacks an ERROR [netdiag/path] line with %q", logs.String(), tc.wantLog)
			}
			screen := ansi.Strip(pm.view()) + pm.footer.Text() + pm.statusLine().Text + pm.errText
			if strings.Contains(screen, tc.wantLog) {
				t.Fatalf("the raw error reached the screen: %q", screen)
			}
		})
	}
}

// No engine row, and no network or interface read, when there is nothing to
// read or the container is not the source.
func TestPathEngineRowAbsentWhenNothingIsRead(t *testing.T) {
	ambiguous := netCtr("app_net", testNetID)
	ambiguous.Networks = append(ambiguous.Networks, docker.EndpointNetwork{
		Name: "b_net", NetworkID: testNetID, Gateway: netip.MustParseAddr("172.19.0.1"),
	})
	stopped := netCtr("app_net", testNetID)
	stopped.Running = false
	tests := []struct {
		name string
		ctr  docker.ContainerNetworking
	}{
		{"host", netCtr("host", testNetID)},
		{"none", netCtr("none", testNetID)},
		{"shared namespace", netCtr("container:abc", testNetID)},
		{"stopped", stopped},
		{"ambiguous route", ambiguous},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Absent interface: would be "engine hidden" if a read happened.
			f := bridgeFake("docker0", ifaceWith("eth0", "192.168.1.21/24"))
			f.inspectNet = tc.ctr
			pm := bridgeRun(t, f)

			requireEngine(t, pm, "")
			if f.networkCalls != 0 || f.ifacesCalls != 0 {
				t.Fatalf("network calls %d, interfaces calls %d, want 0 and 0", f.networkCalls, f.ifacesCalls)
			}
			if pm.state != pathDone {
				t.Fatalf("state = %v, want pathDone", pm.state)
			}
		})
	}

	t.Run("source is this machine", func(t *testing.T) {
		f := bridgeFake("docker0", ifaceWith("eth0", "192.168.1.21/24"))
		captureLog(t)
		pm := pmWith(t, f)
		pm, cmd := launched(t, pm, "1.1.1.1")
		pm = feedAll(t, pm, cmd)
		requireEngine(t, pm, "")
		if f.networkCalls != 0 || f.ifacesCalls != 0 || f.inspectCalls != 0 {
			t.Fatalf("network %d, interfaces %d, inspect %d calls from this machine, want none", f.networkCalls, f.ifacesCalls, f.inspectCalls)
		}
	})
}

// Criteria 2 to 4 and the 80-column width, on the rendered screen.
func TestPathEngineRowRenderedScreen(t *testing.T) {
	tests := []struct {
		name string
		fake func() *fakeEngine
		text string
	}{
		{"hidden", func() *fakeEngine {
			return bridgeFake("docker0", ifaceWith("eth0", "192.168.1.21/24"))
		}, engineHiddenText},
		{"unverified", func() *fakeEngine {
			return bridgeFake("docker0", ifaceWith("docker0", "10.9.0.1/16"))
		}, engineUnverifiedText},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captureLog(t)
			pm := pmWith(t, tc.fake())
			pm.resize(80, 30)
			pm.source = webSource
			pm, cmd := launched(t, pm, "1.1.1.1")
			pm = feedAll(t, pm, cmd)

			screen := ansi.Strip(pm.view())
			engine := screenLinesOf(pm, "engine")
			trace := screenLinesOf(pm, "trace")
			if len(engine) != 1 || len(trace) != 1 {
				t.Fatalf("engine lines %q, trace lines %q, want one of each in:\n%s", engine, trace, screen)
			}
			// 80 columns: the engine text is not truncated.
			if !strings.Contains(engine[0], tc.text) {
				t.Errorf("engine line %q lacks %q at 80 columns", engine[0], tc.text)
			}
			// Criterion 3: the probes are labeled as leaving this machine.
			if !strings.Contains(trace[0], "from this machine") {
				t.Errorf("trace line %q is not labeled \"from this machine\"", trace[0])
			}
			// Criterion 2: no IP and no bridge name on those lines.
			for _, l := range append(engine, trace...) {
				if ip := ipv4Re.FindString(l); ip != "" {
					t.Errorf("line %q names the address %s", l, ip)
				}
				for _, bad := range []string{"docker0", "br-", testGateway} {
					if strings.Contains(l, bad) {
						t.Errorf("line %q names %q", l, bad)
					}
				}
			}
			// Criterion 4: doubt is shown as doubt, never as a VM.
			lower := strings.ToLower(screen)
			for _, bad := range []string{"vm de l'engine", "engine vm"} {
				if strings.Contains(lower, bad) {
					t.Errorf("screen asserts %q:\n%s", bad, screen)
				}
			}
			if n := strings.Count(screen, "VM"); n != 1 || !strings.Contains(trace[0], "WSL VM") {
				t.Errorf("\"VM\" occurs %d times, want only \"WSL VM\" in the trace line:\n%s", n, screen)
			}
		})
	}
}
