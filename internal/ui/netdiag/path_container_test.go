package netdiag

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func prioOf(n int) *int { return &n }

// ep builds an endpoint; gw and ip may be empty (no address).
func ep(name, ip, gw string, prio *int) docker.EndpointNetwork {
	e := docker.EndpointNetwork{Name: name, GwPriority: prio}
	if ip != "" {
		e.IPv4 = netip.MustParseAddr(ip)
	}
	if gw != "" {
		e.Gateway = netip.MustParseAddr(gw)
	}
	return e
}

func TestPathContainerDefaultRouteNetwork(t *testing.T) {
	a0 := ep("a", "172.18.0.5", "172.18.0.1", prioOf(0))
	b10 := ep("b", "172.19.0.5", "172.19.0.1", prioOf(10))
	tests := []struct {
		name   string
		nets   []docker.EndpointNetwork
		want   string // "" means not found
		wantOK bool
	}{
		{"one endpoint with a gateway", []docker.EndpointNetwork{a0}, "a", true},
		{"one endpoint with a gateway and no priority",
			[]docker.EndpointNetwork{ep("a", "172.18.0.5", "172.18.0.1", nil)}, "a", true},
		{"an internal network cannot hold the default route",
			[]docker.EndpointNetwork{a0, ep("b", "10.0.0.2", "", prioOf(50))}, "a", true},
		{"the strictly highest priority wins", []docker.EndpointNetwork{a0, b10}, "b", true},
		{"the strictly highest priority wins whatever the order", []docker.EndpointNetwork{b10, a0}, "b", true},
		{"a tie is never guessed (H8)", []docker.EndpointNetwork{
			ep("a", "172.18.0.5", "172.18.0.1", prioOf(5)),
			ep("b", "172.19.0.5", "172.19.0.1", prioOf(5)),
		}, "", false},
		{"a tie at the top is never guessed", []docker.EndpointNetwork{
			ep("a", "172.18.0.5", "172.18.0.1", prioOf(1)),
			ep("b", "172.19.0.5", "172.19.0.1", prioOf(7)),
			ep("c", "172.20.0.5", "172.20.0.1", prioOf(7)),
		}, "", false},
		{"a priority missing on one candidate is never guessed", []docker.EndpointNetwork{
			ep("a", "172.18.0.5", "172.18.0.1", nil),
			ep("b", "172.19.0.5", "172.19.0.1", prioOf(3)),
		}, "", false},
		{"no priority on any candidate is never guessed", []docker.EndpointNetwork{
			ep("a", "172.18.0.5", "172.18.0.1", nil),
			ep("b", "172.19.0.5", "172.19.0.1", nil),
		}, "", false},
		{"no gateway anywhere", []docker.EndpointNetwork{
			ep("a", "172.18.0.5", "", prioOf(0)),
			ep("b", "172.19.0.5", "", prioOf(1)),
		}, "", false},
		{"no endpoint", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.nets)
			got, ok := defaultRouteNetwork(tt.nets)
			if ok != tt.wantOK || got.Name != tt.want {
				t.Fatalf("defaultRouteNetwork = (%q, %v), want (%q, %v)", got.Name, ok, tt.want, tt.wantOK)
			}
			if !slices.EqualFunc(tt.nets, before, func(x, y docker.EndpointNetwork) bool {
				return x.Name == y.Name && x.IPv4 == y.IPv4 && x.Gateway == y.Gateway
			}) {
				t.Errorf("defaultRouteNetwork modified its input")
			}
		})
	}
}

func TestPathContainerNew(t *testing.T) {
	src := pathSource{ID: "abc123", Name: "web-1", Image: "nginx"}
	app := ep("app_net", "172.18.0.5", "172.18.0.1", prioOf(0))
	tests := []struct {
		name        string
		n           docker.ContainerNetworking
		err         error
		attach      containerAttach
		known       bool
		wantCont    string
		wantNetwork string
		wantTrace   string
	}{
		{
			name: "inspection failed", err: errors.New("boom"),
			attach: attachUndetermined, known: false,
			wantCont: "web-1", wantNetwork: "undetermined",
			wantTrace: "from this machine, not from the container",
		},
		{
			name:   "one network",
			n:      docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "app_net", Networks: []docker.EndpointNetwork{app}},
			attach: attachNetwork, known: true,
			wantCont: "web-1 (172.18.0.5)", wantNetwork: "app_net via 172.18.0.1",
			wantTrace: "from this machine, not from the container",
		},
		{
			name:   "default bridge",
			n:      docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "bridge", Networks: []docker.EndpointNetwork{ep("bridge", "172.17.0.3", "172.17.0.1", prioOf(0))}},
			attach: attachNetwork, known: true,
			wantCont: "web-1 (172.17.0.3)", wantNetwork: "bridge via 172.17.0.1",
			wantTrace: "from this machine, not from the container",
		},
		{
			name:   "not running",
			n:      docker.ContainerNetworking{Name: "web-1", Running: false, Mode: "app_net", Networks: []docker.EndpointNetwork{app}},
			attach: attachUndetermined, known: true,
			wantCont: "web-1 (not running)", wantNetwork: "undetermined",
			wantTrace: "from this machine, not from the container",
		},
		{
			name: "two networks, higher priority wins",
			n: docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "app_net", Networks: []docker.EndpointNetwork{
				ep("a", "172.18.0.5", "172.18.0.1", prioOf(0)),
				ep("b", "172.19.0.5", "172.19.0.1", prioOf(10)),
			}},
			attach: attachNetwork, known: true,
			wantCont: "web-1 (172.19.0.5)", wantNetwork: "b via 172.19.0.1",
			wantTrace: "from this machine, not from the container",
		},
		{
			name: "two networks, a tie is undetermined",
			n: docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "a", Networks: []docker.EndpointNetwork{
				ep("a", "172.18.0.5", "172.18.0.1", prioOf(0)),
				ep("b", "172.19.0.5", "172.19.0.1", prioOf(0)),
			}},
			attach: attachUndetermined, known: true,
			wantCont: "web-1", wantNetwork: "undetermined (2 networks)",
			wantTrace: "from this machine, not from the container",
		},
		{
			name: "one network without a gateway is undetermined, no count",
			n: docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "app_net",
				Networks: []docker.EndpointNetwork{ep("app_net", "172.18.0.5", "", prioOf(0))}},
			attach: attachUndetermined, known: true,
			wantCont: "web-1", wantNetwork: "undetermined",
			wantTrace: "from this machine, not from the container",
		},
		{
			name: "a stopped container with one network is undetermined, no count",
			n: docker.ContainerNetworking{Name: "web-1", Running: false, Mode: "app_net",
				Networks: []docker.EndpointNetwork{ep("app_net", "172.18.0.5", "172.18.0.1", prioOf(0))}},
			attach: attachUndetermined, known: true,
			wantCont: "web-1 (not running)", wantNetwork: "undetermined",
			wantTrace: "from this machine, not from the container",
		},
		{
			name: "host mode",
			n: docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "host",
				Networks: []docker.EndpointNetwork{ep("host", "", "", nil)}},
			attach: attachHost, known: true,
			wantCont: "web-1", wantNetwork: "host (shares this machine's stack)",
			wantTrace: "from this machine (shared with the container)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newPathContainer(src, tt.n, tt.err)
			if got.Known != tt.known || got.Attach != tt.attach {
				t.Fatalf("Known/Attach = %v/%v, want %v/%v", got.Known, got.Attach, tt.known, tt.attach)
			}
			if got.containerText() != tt.wantCont {
				t.Errorf("containerText = %q, want %q", got.containerText(), tt.wantCont)
			}
			if got.networkText() != tt.wantNetwork {
				t.Errorf("networkText = %q, want %q", got.networkText(), tt.wantNetwork)
			}
			if got.traceText() != tt.wantTrace {
				t.Errorf("traceText = %q, want %q", got.traceText(), tt.wantTrace)
			}
		})
	}
}

// Modes the plan leaves without a fixed sentence: only the attachment, the
// absence of a bridge line and the plain text are pinned.
func TestPathContainerNewOtherModes(t *testing.T) {
	src := pathSource{ID: "abc123", Name: "web-1"}
	tests := []struct {
		name   string
		mode   string
		attach containerAttach
		want   string
	}{
		{"none", "none", attachNone, "none (no network)"},
		{"shared with another container", "container:abc", attachShared, "shares another container's network"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newPathContainer(src, docker.ContainerNetworking{Name: "web-1", Running: true, Mode: tt.mode}, nil)
			if got.Attach != tt.attach {
				t.Fatalf("Attach = %v, want %v", got.Attach, tt.attach)
			}
			if got.networkText() == "" || strings.Contains(got.networkText(), " via ") {
				t.Errorf("networkText = %q, want a sentence with no gateway", got.networkText())
			}
			if got.networkText() != tt.want {
				t.Errorf("networkText = %q, want %q", got.networkText(), tt.want)
			}
		})
	}
}

// H12: the engine's own text reaches the cells already sanitized.
func TestPathContainerNewSanitizesEngineText(t *testing.T) {
	src := pathSource{ID: "abc123", Name: "web-1"}
	n := docker.ContainerNetworking{
		Name: "we\x1b[31mb-1\u202e", Running: true, Mode: "evil\x1b]0;pwn\a",
		Networks: []docker.EndpointNetwork{ep("ev\x1b[1mil\u200b_net", "172.18.0.5", "172.18.0.1", prioOf(0))},
	}
	got := newPathContainer(src, n, nil)
	for _, c := range []string{got.containerText(), got.networkText(), got.traceText()} {
		if ansi.Strip(c) != c || strings.ContainsAny(c, "\x1b\a\u202e\u200b") {
			t.Errorf("cell %q is not display-safe", c)
		}
	}
	if got.networkText() != "evil_net via 172.18.0.1" {
		t.Errorf("networkText = %q, want %q", got.networkText(), "evil_net via 172.18.0.1")
	}
}

func TestPathContainerNewDoesNotMutateItsInput(t *testing.T) {
	nets := []docker.EndpointNetwork{
		ep("b", "172.19.0.5", "172.19.0.1", prioOf(1)),
		ep("a", "172.18.0.5", "172.18.0.1", prioOf(0)),
	}
	n := docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "a", Networks: nets}
	_ = newPathContainer(pathSource{ID: "abc123", Name: "web-1"}, n, nil)
	if nets[0].Name != "b" || nets[1].Name != "a" || n.Name != "web-1" || len(n.Networks) != 2 {
		t.Errorf("newPathContainer modified its input: %+v", n)
	}
}

// Dim rows: undetermined, none and the trace row; the others take the default colour.
func TestPathColumnContainerRowStyles(t *testing.T) {
	src := pathSource{ID: "abc123", Name: "web-1"}
	app := ep("app_net", "172.18.0.5", "172.18.0.1", prioOf(0))
	ok := newPathContainer(src, docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "app_net", Networks: []docker.EndpointNetwork{app}}, nil)
	bad := newPathContainer(src, docker.ContainerNetworking{}, errors.New("boom"))
	none := newPathContainer(src, docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "none"}, nil)
	tests := []struct {
		name string
		step pathStep
		dim  bool
	}{
		{"none network row", pathStep{Kind: stepNetwork, Container: none}, true},
		{"container row", pathStep{Kind: stepContainer, Container: ok}, false},
		{"network row", pathStep{Kind: stepNetwork, Container: ok}, false},
		{"undetermined network row", pathStep{Kind: stepNetwork, Container: bad}, true},
		{"trace row", pathStep{Kind: stepTrace, Container: ok}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathAddressStyle(tt.step).Render("x")
			isDim := got == theme.DimStyle.Render("x")
			if isDim != tt.dim {
				t.Errorf("dim = %v, want %v (rendered %q)", isDim, tt.dim, got)
			}
		})
	}
}
