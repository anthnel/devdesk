package netdiag

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-332 step 2: the engine row. When this machine has no interface for
// the container's network, the bridge lives somewhere this machine cannot see
// (the Docker VM), and the column says so instead of showing a bridge row.

const (
	engineHiddenText     = "internal path not visible (location unknown)"
	engineUnverifiedText = "undetermined (bridge not verified)"
	traceWithEngineText  = "from this machine (host or WSL VM)"
	traceNoEngineText    = "from this machine, not from the container"
	engineAddressMaxCols = 47
)

// engineCtr returns a network-attached container (built the way production
// builds it) after reading the given bridge outcome.
func engineCtr(t *testing.T, b pathBridge) pathContainer {
	t.Helper()
	return bridgeCtr(t, "app_net", true).withBridgeRead(b)
}

func TestPathContainerWithBridgeRead(t *testing.T) {
	attached := bridgeCtr(t, "app_net", true)
	tests := []struct {
		name       string
		ctr        pathContainer
		in         pathBridge
		wantBridge string
		wantEngine engineView
	}{
		{"found sets the bridge", attached, pathBridge{State: bridgeFound, Name: "docker0"}, "docker0", engineNone},
		{"found, user-defined bridge", attached, pathBridge{State: bridgeFound, Name: "br-3f2a1b4c5d6e"}, "br-3f2a1b4c5d6e", engineNone},
		{"absent hides the engine", attached, pathBridge{State: bridgeAbsent}, "", engineHidden},
		{"unverified", attached, pathBridge{State: bridgeUnverified}, "", engineUnverified},
		{"not read changes nothing", attached, pathBridge{State: bridgeNotRead}, "", engineNone},
		{"host mode", bridgeCtr(t, "host", true), pathBridge{State: bridgeAbsent}, "", engineNone},
		{"none mode", bridgeCtr(t, "none", true), pathBridge{State: bridgeUnverified}, "", engineNone},
		{"shared namespace", bridgeCtr(t, "container:abc", true), pathBridge{State: bridgeAbsent}, "", engineNone},
		{"stopped (undetermined)", bridgeCtr(t, "app_net", false), pathBridge{State: bridgeAbsent}, "", engineNone},
		{"host mode, found", bridgeCtr(t, "host", true), pathBridge{State: bridgeFound, Name: "docker0"}, "", engineNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.ctr
			got := tt.ctr.withBridgeRead(tt.in)

			if got.Bridge != tt.wantBridge || got.Engine != tt.wantEngine {
				t.Errorf("Bridge, Engine = %q, %v; want %q, %v", got.Bridge, got.Engine, tt.wantBridge, tt.wantEngine)
			}
			// Everything else is carried over unchanged.
			wantRest := before
			wantRest.Bridge, wantRest.Engine = tt.wantBridge, tt.wantEngine
			if got != wantRest {
				t.Errorf("withBridgeRead changed more than Bridge/Engine: got %+v, want %+v", got, wantRest)
			}
			// The receiver is not modified.
			if tt.ctr != before {
				t.Errorf("withBridgeRead modified its receiver: %+v, was %+v", tt.ctr, before)
			}
		})
	}
}

// Two calls from the same receiver do not leak into each other.
func TestPathContainerWithBridgeReadReturnsACopy(t *testing.T) {
	base := bridgeCtr(t, "app_net", true)
	a := base.withBridgeRead(pathBridge{State: bridgeFound, Name: "docker0"})
	b := base.withBridgeRead(pathBridge{State: bridgeAbsent})
	c := base.withBridgeRead(pathBridge{State: bridgeUnverified})

	if base.Bridge != "" || base.Engine != engineNone {
		t.Errorf("receiver = Bridge %q, Engine %v; want untouched", base.Bridge, base.Engine)
	}
	if a.Bridge != "docker0" || a.Engine != engineNone {
		t.Errorf("found copy = %q, %v", a.Bridge, a.Engine)
	}
	if b.Bridge != "" || b.Engine != engineHidden {
		t.Errorf("absent copy = %q, %v", b.Bridge, b.Engine)
	}
	if c.Bridge != "" || c.Engine != engineUnverified {
		t.Errorf("unverified copy = %q, %v", c.Bridge, c.Engine)
	}
}

func TestPathStepsEngineOrder(t *testing.T) {
	hidden := engineCtr(t, pathBridge{State: bridgeAbsent})
	unverified := engineCtr(t, pathBridge{State: bridgeUnverified})
	found := engineCtr(t, pathBridge{State: bridgeFound, Name: "docker0"})
	plain := bridgeCtr(t, "app_net", true)
	host := bridgeCtr(t, "host", true).withBridgeRead(pathBridge{State: bridgeAbsent})
	hops := []nettrace.Hop{stepsHop1}

	withEngine := []pathStepKind{stepContainer, stepNetwork, stepEngine, stepSource, stepInterface, stepGateway, stepTrace, stepHop}
	withBridge := []pathStepKind{stepContainer, stepNetwork, stepBridge, stepSource, stepInterface, stepGateway, stepTrace, stepHop}
	neither := []pathStepKind{stepContainer, stepNetwork, stepSource, stepInterface, stepGateway, stepTrace, stepHop}

	tests := []struct {
		name  string
		ctr   pathContainer
		route *pathRoute
		hops  []nettrace.Hop
		want  []pathStepKind
	}{
		{"engine hidden", hidden, stepsRoute, hops, withEngine},
		{"engine unverified", unverified, stepsRoute, hops, withEngine},
		{"engine, no route, no hop", hidden,
			nil, nil, []pathStepKind{stepContainer, stepNetwork, stepEngine, stepSource, stepTrace}},
		{"engine, hop before route", hidden,
			nil, hops, []pathStepKind{stepContainer, stepNetwork, stepEngine, stepSource, stepTrace, stepHop}},
		{"bridge found: bridge row, no engine row", found, stepsRoute, hops, withBridge},
		{"neither bridge nor engine: order unchanged", plain, stepsRoute, hops, neither},
		{"host mode: nothing", host, stepsRoute, hops, neither},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr := tt.ctr
			before := ctr
			got := pathSteps(&ctr, tt.route, tt.hops)
			if kinds := stepKinds(got); !slices.Equal(kinds, tt.want) {
				t.Fatalf("kinds = %v, want %v", kinds, tt.want)
			}
			// Never a bridge row and an engine row together.
			var nBridge, nEngine int
			for _, s := range got {
				switch s.Kind {
				case stepBridge:
					nBridge++
				case stepEngine:
					nEngine++
				}
			}
			if nBridge+nEngine > 1 {
				t.Errorf("%d bridge and %d engine rows, want at most one in total", nBridge, nEngine)
			}
			if ctr != before {
				t.Errorf("pathSteps modified its container input")
			}
		})
	}
}

// A this-machine source (no container) never shows an engine row.
func TestPathStepsEngineAbsentWithoutContainer(t *testing.T) {
	for _, s := range pathSteps(nil, stepsRoute, []nettrace.Hop{stepsHop1}) {
		if s.Kind == stepEngine {
			t.Fatalf("an engine row appeared without a container: %+v", s)
		}
	}
}

// stepEngine sits right after stepBridge in the iota, so the kinds keep the
// order the column shows them in.
func TestStepEngineFollowsStepBridge(t *testing.T) {
	if stepEngine != stepBridge+1 {
		t.Errorf("stepEngine = %d, want stepBridge+1 = %d", stepEngine, stepBridge+1)
	}
	if stepEngine >= stepSource {
		t.Errorf("stepEngine = %d must come before stepSource = %d", stepEngine, stepSource)
	}
}

func TestPathStepsEngineCells(t *testing.T) {
	hidden := engineCtr(t, pathBridge{State: bridgeAbsent})
	unverified := engineCtr(t, pathBridge{State: bridgeUnverified})
	tests := []struct {
		name string
		step pathStep
		want cells
	}{
		{"hidden", pathStep{Kind: stepEngine, Container: hidden},
			cells{"", "engine", engineHiddenText, ""}},
		{"unverified", pathStep{Kind: stepEngine, Container: unverified},
			cells{"", "engine", engineUnverifiedText, ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cellsOf(tt.step)
			if got != tt.want {
				t.Errorf("cells = %+v, want %+v", got, tt.want)
			}
			for _, c := range []string{got.hop, got.step, got.address, got.rtt} {
				if ansi.Strip(c) != c {
					t.Errorf("cell %q carries an escape sequence (Rule 122)", c)
				}
			}
			// 47 cells at most, so nothing is truncated at 80 columns.
			if w := ansi.StringWidth(got.address); w > engineAddressMaxCols {
				t.Errorf("address is %d cells wide, want at most %d", w, engineAddressMaxCols)
			}
		})
	}
}

// The engine row is grey, like the trace row.
func TestPathAddressStyleEngineIsDim(t *testing.T) {
	for name, ctr := range map[string]pathContainer{
		"hidden":     engineCtr(t, pathBridge{State: bridgeAbsent}),
		"unverified": engineCtr(t, pathBridge{State: bridgeUnverified}),
	} {
		t.Run(name, func(t *testing.T) {
			got := pathAddressStyle(pathStep{Kind: stepEngine, Container: ctr}).Render("x")
			if got != theme.DimStyle.Render("x") {
				t.Errorf("engine row not dim: rendered %q", got)
			}
		})
	}
}

func TestPathTraceTextWithEngineRow(t *testing.T) {
	tests := []struct {
		name string
		ctr  pathContainer
		want string
	}{
		{"engine hidden", engineCtr(t, pathBridge{State: bridgeAbsent}), traceWithEngineText},
		{"engine unverified", engineCtr(t, pathBridge{State: bridgeUnverified}), traceWithEngineText},
		{"bridge found: unchanged", engineCtr(t, pathBridge{State: bridgeFound, Name: "docker0"}), traceNoEngineText},
		{"no bridge, no engine: unchanged", bridgeCtr(t, "app_net", true), traceNoEngineText},
		{"host mode: unchanged", bridgeCtr(t, "host", true), textTraceSharedHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := pathStep{Kind: stepTrace, Container: tt.ctr}
			if got := step.addressCell(); got != tt.want {
				t.Errorf("trace address = %q, want %q", got, tt.want)
			}
			if got := cellsOf(step); got.step != "trace" || got.hop != "" || got.rtt != "" {
				t.Errorf("trace cells = %+v", got)
			}
		})
	}
}

// Neither the engine row nor the trace row (with an engine row present) names
// an IP address or a bridge: the whole point is that the location is unknown.
func TestPathEngineTextsLeakNothing(t *testing.T) {
	for _, state := range []bridgeState{bridgeAbsent, bridgeUnverified} {
		ctr := engineCtr(t, pathBridge{State: state})
		for _, kind := range []pathStepKind{stepEngine, stepTrace} {
			text := pathStep{Kind: kind, Container: ctr}.addressCell()
			for _, word := range strings.Fields(text) {
				word = strings.Trim(word, "(),.:;")
				if _, err := netip.ParseAddr(word); err == nil {
					t.Errorf("state %v kind %v: %q contains the address %q", state, kind, text, word)
				}
			}
			lower := strings.ToLower(text)
			for _, bad := range []string{"docker0", "br-", ctr.Addr.String(), ctr.Gateway.String()} {
				if strings.Contains(lower, bad) {
					t.Errorf("state %v kind %v: %q contains %q", state, kind, text, bad)
				}
			}
		}
	}
}
