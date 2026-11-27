package docker

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-19 (S3): a published port is reachable from this machine only, unless the
// user wrote an address themselves. The default is applied once, here, so the
// form's preview, its submit and every other caller of buildLaunchArgs agree.
//
// Expected API:
//
//	LoopbackPort(spec string) string           // the -p value with 127.0.0.1 added when spec has no address
//	PortExposedOnAllInterfaces(spec string) bool // true when spec itself names 0.0.0.0 or ::

// portFlagValues returns the value of every -p flag of an argv, in order.
func portFlagValues(args []string) []string {
	var out []string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-p" {
			out = append(out, args[i+1])
		}
	}
	return out
}

// portSpecCases covers the -p syntax: host:container, ip:host:container,
// protocols, ranges, IPv6 in brackets, a lone container port, and a spec whose
// host port is left to the engine.
var portSpecCases = []struct {
	name string
	spec string
	want string // what must reach the engine
}{
	// No address: 127.0.0.1 is added (criterion 1, criterion 4).
	{"host:container", "8080:80", "127.0.0.1:8080:80"},
	{"same port", "80:80", "127.0.0.1:80:80"},
	{"with tcp", "8080:80/tcp", "127.0.0.1:8080:80/tcp"},
	{"with udp", "8080:80/udp", "127.0.0.1:8080:80/udp"},
	{"range to range", "8000-8010:8000-8010", "127.0.0.1:8000-8010:8000-8010"},
	{"container port only", "80", "127.0.0.1::80"},
	{"container port only with protocol", "53/udp", "127.0.0.1::53/udp"},
	{"container range only", "8000-8010", "127.0.0.1::8000-8010"},
	// An address the user wrote is never rewritten (criterion 3).
	{"explicit loopback", "127.0.0.1:8080:80", "127.0.0.1:8080:80"},
	{"explicit all interfaces", "0.0.0.0:8080:80", "0.0.0.0:8080:80"},
	{"explicit LAN address", "192.168.1.10:8080:80", "192.168.1.10:8080:80"},
	{"explicit address, engine-chosen host port", "127.0.0.1::80", "127.0.0.1::80"},
	{"explicit address with protocol", "0.0.0.0:8080:80/udp", "0.0.0.0:8080:80/udp"},
	{"explicit address with ranges", "0.0.0.0:8000-8010:8000-8010", "0.0.0.0:8000-8010:8000-8010"},
	{"IPv6 loopback", "[::1]:8080:80", "[::1]:8080:80"},
	{"IPv6 loopback, same port", "[::1]:80:80", "[::1]:80:80"},
	{"IPv6 any", "[::]:8080:80", "[::]:8080:80"},
	{"IPv6 loopback, engine-chosen host port", "[::1]::80", "[::1]::80"},
}

func TestLoopbackPortAddsTheLoopbackAddressOnlyWhenThereIsNone(t *testing.T) {
	for _, tt := range portSpecCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := LoopbackPort(tt.spec); got != tt.want {
				t.Errorf("LoopbackPort(%q) = %q, want %q", tt.spec, got, tt.want)
			}
			// Applying it twice must not stack addresses: the form previews the
			// result and the launcher applies it again.
			if got := LoopbackPort(LoopbackPort(tt.spec)); got != tt.want {
				t.Errorf("LoopbackPort is not idempotent for %q: got %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestBuildLaunchArgsPublishesEveryPortOnLoopbackByDefault(t *testing.T) {
	for _, tt := range portSpecCases {
		t.Run(tt.name, func(t *testing.T) {
			args := buildLaunchArgs(ContainerLaunchOptions{Image: "img", Ports: []string{tt.spec}})

			got := portFlagValues(args)
			if !slices.Equal(got, []string{tt.want}) {
				t.Errorf("buildLaunchArgs(%q) -p values = %q, want %q (argv %q)", tt.spec, got, []string{tt.want}, args)
			}
			if args[len(args)-1] != "img" {
				t.Errorf("the image is not last in %q", args)
			}
		})
	}
}

// A bare mapping must never reach the engine: it would bind 0.0.0.0.
func TestBuildLaunchArgsNeverEmitsABareMapping(t *testing.T) {
	args := buildLaunchArgs(ContainerLaunchOptions{
		Image: "img",
		Ports: []string{"8080:80", "443", "9000-9010:9000-9010/udp", "", "0.0.0.0:8081:81"},
	})

	got := portFlagValues(args)
	want := []string{"127.0.0.1:8080:80", "127.0.0.1::443", "127.0.0.1:9000-9010:9000-9010/udp", "0.0.0.0:8081:81"}
	if !slices.Equal(got, want) {
		t.Errorf("-p values = %q, want %q (argv %q)", got, want, args)
	}
	for _, bare := range []string{"8080:80", "443", "9000-9010:9000-9010/udp"} {
		if slices.Contains(args, bare) {
			t.Errorf("argv %q contains the bare mapping %q", args, bare)
		}
	}
}

func TestPortExposedOnAllInterfaces(t *testing.T) {
	tests := []struct {
		spec string
		want bool
	}{
		{"0.0.0.0:8080:80", true},
		{"0.0.0.0:8080:80/udp", true},
		{"0.0.0.0::80", true},
		{"[::]:8080:80", true},
		{"8080:80", false}, // no address: the default makes it loopback
		{"80", false},
		{"8080:80/tcp", false},
		{"8000-8010:8000-8010", false},
		{"127.0.0.1:8080:80", false},
		{"[::1]:8080:80", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			if got := PortExposedOnAllInterfaces(tt.spec); got != tt.want {
				t.Errorf("PortExposedOnAllInterfaces(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

// Criterion 5: Docker and Podman (rootless included) get the same -p value. The
// strict stub fails on any other argv, so a bare "-p 8080:80" is caught for both.
func TestLaunchPublishesOnLoopbackOnEveryEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		argv := []string{"run", "--pull=never", "-d", "-p", "127.0.0.1:8080:80", "-p", "127.0.0.1::443", "-p", "0.0.0.0:9090:90", "img"}
		stubStrict(t, reply{argv: argv, out: "9f2e1c8b7a6d\n"})
		opts := ContainerLaunchOptions{
			Image:  "img",
			Detach: true,
			Ports:  []string{"8080:80", "443", "0.0.0.0:9090:90"},
		}

		id, err := LaunchContainer(opts)
		if err != nil {
			t.Fatalf("LaunchContainer() error = %v", err)
		}
		if id != "9f2e1c8b7a6d" {
			t.Errorf("LaunchContainer() = %q, want the container ID", id)
		}

		cmd, err := BuildLaunchCmd(opts)
		if err != nil {
			t.Fatalf("BuildLaunchCmd() error = %v", err)
		}
		if got := cmd.Args[1:]; !slices.Equal(got, argv) {
			t.Errorf("BuildLaunchCmd() argv = %q, want %q", got, argv)
		}
	})
}
