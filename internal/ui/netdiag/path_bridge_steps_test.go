package netdiag

import (
	"errors"
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/nettrace"
)

// DEVDESK-331 step 3: the bridge row of the container source. Bridge is the
// name of the interface read on this machine; "" means there is no bridge in
// the path.

// bridgeCtr returns a container built the way production builds it, in the given
// mode. A stopped container carries no endpoint, as the engine reports it.
func bridgeCtr(t *testing.T, mode string, running bool) pathContainer {
	t.Helper()
	n := docker.ContainerNetworking{
		Name: "web-1", Running: running, Mode: mode,
		Networks: []docker.EndpointNetwork{ep("app_net", "172.18.0.5", "172.18.0.1", prioOf(0))},
	}
	return newPathContainer(pathSource{ID: "abc123", Name: "web-1"}, n, nil)
}

// A stopped container is represented by Running == false, and newPathContainer
// then leaves Attach at attachUndetermined, so withBridge's Attach check covers
// it; the "stopped" case below goes through the real constructor to prove it.
func TestPathContainerWithBridge(t *testing.T) {
	failed := newPathContainer(pathSource{ID: "abc123", Name: "web-1"}, docker.ContainerNetworking{}, errors.New("boom"))
	// two endpoints, no gateway priority: the default-route network is ambiguous
	ambiguous := newPathContainer(pathSource{ID: "abc123", Name: "web-1"}, docker.ContainerNetworking{
		Name: "web-1", Running: true, Mode: "app_net",
		Networks: []docker.EndpointNetwork{
			ep("a_net", "172.18.0.5", "172.18.0.1", nil),
			ep("b_net", "172.19.0.5", "172.19.0.1", nil),
		},
	}, nil)

	tests := []struct {
		name string
		ctr  pathContainer
		in   string
		want string
	}{
		{"attached to a network", bridgeCtr(t, "app_net", true), "docker0", "docker0"},
		{"user-defined bridge name", bridgeCtr(t, "app_net", true), "br-3f2a1b4c5d6e", "br-3f2a1b4c5d6e"},
		{"empty name sets nothing", bridgeCtr(t, "app_net", true), "", ""},
		{"host mode", bridgeCtr(t, "host", true), "docker0", ""},
		{"none mode", bridgeCtr(t, "none", true), "docker0", ""},
		{"shared network namespace", bridgeCtr(t, "container:abc", true), "docker0", ""},
		{"undetermined: ambiguous default route", ambiguous, "docker0", ""},
		{"undetermined: failed inspection", failed, "docker0", ""},
		{"stopped container", bridgeCtr(t, "app_net", false), "docker0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.ctr
			got := tt.ctr.withBridge(tt.in)

			if got.Bridge != tt.want {
				t.Errorf("withBridge(%q).Bridge = %q, want %q", tt.in, got.Bridge, tt.want)
			}
			// Everything else is carried over unchanged.
			wantRest := before
			wantRest.Bridge = tt.want
			if got != wantRest {
				t.Errorf("withBridge changed more than Bridge: got %+v, want %+v", got, wantRest)
			}
			// The receiver is not modified.
			if tt.ctr != before {
				t.Errorf("withBridge modified its receiver: %+v, was %+v", tt.ctr, before)
			}
		})
	}
}

// Two calls from the same receiver do not leak into each other.
func TestPathContainerWithBridgeReturnsACopy(t *testing.T) {
	base := bridgeCtr(t, "app_net", true)
	a := base.withBridge("docker0")
	b := base.withBridge("br-abc")
	if base.Bridge != "" {
		t.Errorf("receiver Bridge = %q, want empty", base.Bridge)
	}
	if a.Bridge != "docker0" || b.Bridge != "br-abc" {
		t.Errorf("copies = %q and %q, want docker0 and br-abc", a.Bridge, b.Bridge)
	}
}

// Criterion 1: container, network, bridge, source, interface, gateway, trace, hops.
// Criterion 2: without a bridge, no bridge row, and the existing order holds.
func TestPathStepsBridgeOrder(t *testing.T) {
	withBr := bridgeCtr(t, "app_net", true).withBridge("docker0")
	noBr := bridgeCtr(t, "app_net", true)
	host := bridgeCtr(t, "host", true).withBridge("docker0")
	hops := []nettrace.Hop{stepsHop1}

	tests := []struct {
		name  string
		ctr   pathContainer
		route *pathRoute
		hops  []nettrace.Hop
		want  []pathStepKind
	}{
		{"criterion 1: bridge between network and source", withBr, stepsRoute, hops,
			[]pathStepKind{stepContainer, stepNetwork, stepBridge, stepSource, stepInterface, stepGateway, stepTrace, stepHop}},
		{"bridge, no route, no hop", withBr, nil, nil,
			[]pathStepKind{stepContainer, stepNetwork, stepBridge, stepSource, stepTrace}},
		{"bridge, hop before route", withBr, nil, hops,
			[]pathStepKind{stepContainer, stepNetwork, stepBridge, stepSource, stepTrace, stepHop}},
		{"criterion 2: no bridge, no bridge row", noBr, stepsRoute, hops,
			[]pathStepKind{stepContainer, stepNetwork, stepSource, stepInterface, stepGateway, stepTrace, stepHop}},
		{"host mode never gets one", host, stepsRoute, hops,
			[]pathStepKind{stepContainer, stepNetwork, stepSource, stepInterface, stepGateway, stepTrace, stepHop}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr := tt.ctr
			before := ctr
			got := pathSteps(&ctr, tt.route, tt.hops)
			if kinds := stepKinds(got); !slices.Equal(kinds, tt.want) {
				t.Fatalf("kinds = %v, want %v", kinds, tt.want)
			}
			if ctr != before {
				t.Errorf("pathSteps modified its container input")
			}
		})
	}
}

// A this-machine source with no container never shows a bridge row.
func TestPathStepsBridgeAbsentWithoutContainer(t *testing.T) {
	for _, s := range pathSteps(nil, stepsRoute, []nettrace.Hop{stepsHop1}) {
		if s.Kind == stepBridge {
			t.Fatalf("a bridge row appeared without a container: %+v", s)
		}
	}
}

func TestPathStepsBridgeCells(t *testing.T) {
	ctr := bridgeCtr(t, "app_net", true).withBridge("docker0")
	long := bridgeCtr(t, "app_net", true).withBridge("br-3f2a1b4c5d6e")
	tests := []struct {
		name string
		step pathStep
		want cells
	}{
		{"docker0", pathStep{Kind: stepBridge, Container: ctr},
			cells{"", "bridge", "docker0", ""}},
		{"user-defined bridge", pathStep{Kind: stepBridge, Container: long},
			cells{"", "bridge", "br-3f2a1b4c5d6e", ""}},
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
		})
	}
}

// pathSteps hands the bridge row the container, so its Address is the name.
func TestPathStepsBridgeRowCarriesTheName(t *testing.T) {
	ctr := bridgeCtr(t, "app_net", true).withBridge("docker0")
	for _, s := range pathSteps(&ctr, stepsRoute, nil) {
		if s.Kind != stepBridge {
			continue
		}
		if s.Container.Bridge != "docker0" || s.addressCell() != "docker0" {
			t.Errorf("bridge row = %+v, address %q", s, s.addressCell())
		}
		return
	}
	t.Fatal("no bridge row")
}
