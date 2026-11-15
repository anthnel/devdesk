package wslenv

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

const (
	wslRelease        = "6.18.33.2-microsoft-standard-WSL2\n"
	nativeRelease     = "6.12.0-amd64\n"
	kernelReleasePath = "/proc/sys/kernel/osrelease"
	routePath         = "/proc/net/route"
)

// fakeProbe records every call so a test can assert what was (not) read.
type fakeProbe struct {
	goos      string
	env       map[string]string
	files     map[string][]byte
	fileErr   map[string]error
	ifaces    []string
	ifaceErr  error
	mode      string
	modeErr   error
	reads     []string
	getenvs   []string
	ifaceN    int
	modeCalls int
}

func (f *fakeProbe) probe() Probe {
	return Probe{
		GOOS:   f.goos,
		Getenv: func(k string) string { f.getenvs = append(f.getenvs, k); return f.env[k] },
		ReadFile: func(p string) ([]byte, error) {
			f.reads = append(f.reads, p)
			if err := f.fileErr[p]; err != nil {
				return nil, err
			}
			return f.files[p], nil
		},
		InterfaceNames: func() ([]string, error) { f.ifaceN++; return f.ifaces, f.ifaceErr },
		NetworkingMode: func(ctx context.Context) (string, error) {
			f.modeCalls++
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return f.mode, f.modeErr
		},
	}
}

func (f *fakeProbe) read(path string) bool { return slices.Contains(f.reads, path) }

func wslFake() *fakeProbe {
	return &fakeProbe{
		goos:   "linux",
		env:    map[string]string{"WSL_DISTRO_NAME": "archlinux"},
		files:  map[string][]byte{kernelReleasePath: []byte(wslRelease), routePath: []byte(natRoutes)},
		ifaces: []string{"lo", "eth0"},
		mode:   "nat",
	}
}

func TestGatherNotLinuxProbesNothing(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", ""} {
		f := wslFake()
		f.goos = goos
		got := Gather(context.Background(), f.probe())
		if got.DistroName != "" || got.OSRelease != "" || got.NetworkingMode != "" ||
			got.NetworkingModeErr != nil || len(got.Interfaces) != 0 || len(got.ProcNetRoute) != 0 {
			t.Errorf("GOOS=%q: Facts = %+v, want empty", goos, got)
		}
		if len(f.reads)+len(f.getenvs)+f.ifaceN+f.modeCalls != 0 {
			t.Errorf("GOOS=%q: probe called (reads=%v getenv=%v ifaces=%d mode=%d)", goos, f.reads, f.getenvs, f.ifaceN, f.modeCalls)
		}
	}
}

func TestGatherNativeLinuxSkipsWSLReads(t *testing.T) {
	f := wslFake()
	f.env = map[string]string{}
	f.files[kernelReleasePath] = []byte(nativeRelease)
	got := Gather(context.Background(), f.probe())
	if !f.read(kernelReleasePath) {
		t.Errorf("osrelease not read: reads=%v", f.reads)
	}
	if f.modeCalls != 0 {
		t.Errorf("NetworkingMode called %d times on native Linux", f.modeCalls)
	}
	if f.read(routePath) {
		t.Errorf("%s read on native Linux", routePath)
	}
	if got.NetworkingMode != "" || got.NetworkingModeErr != nil || len(got.ProcNetRoute) != 0 {
		t.Errorf("Facts = %+v, want no WSL facts", got)
	}
	if !strings.Contains(got.OSRelease, "6.12.0-amd64") {
		t.Errorf("OSRelease = %q", got.OSRelease)
	}
	if Classify(got) != (Info{}) {
		t.Errorf("Classify = %+v, want zero", Classify(got))
	}
}

func TestGatherWSLSignals(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		release string
	}{
		{"distro name", map[string]string{"WSL_DISTRO_NAME": "archlinux"}, wslRelease},
		{"osrelease only (env -i)", map[string]string{}, wslRelease},
		{"distro name, plain osrelease", map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, nativeRelease},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := wslFake()
			f.env = tt.env
			f.files[kernelReleasePath] = []byte(tt.release)
			got := Gather(context.Background(), f.probe())
			if f.modeCalls != 1 {
				t.Errorf("NetworkingMode calls = %d, want 1", f.modeCalls)
			}
			if !f.read(kernelReleasePath) || !f.read(routePath) {
				t.Errorf("reads = %v, want osrelease and route", f.reads)
			}
			if f.ifaceN != 1 {
				t.Errorf("InterfaceNames calls = %d, want 1", f.ifaceN)
			}
			if got.DistroName != tt.env["WSL_DISTRO_NAME"] {
				t.Errorf("DistroName = %q", got.DistroName)
			}
			if strings.TrimSpace(got.OSRelease) != strings.TrimSpace(tt.release) {
				t.Errorf("OSRelease = %q", got.OSRelease)
			}
			if got.NetworkingMode != "nat" || got.NetworkingModeErr != nil {
				t.Errorf("mode = %q err = %v", got.NetworkingMode, got.NetworkingModeErr)
			}
			if strings.Join(got.Interfaces, ",") != "lo,eth0" {
				t.Errorf("Interfaces = %v", got.Interfaces)
			}
			if string(got.ProcNetRoute) != natRoutes {
				t.Errorf("ProcNetRoute = %q", got.ProcNetRoute)
			}
		})
	}
}

func TestGatherEndToEndClassify(t *testing.T) {
	f := wslFake()
	got := Classify(Gather(context.Background(), f.probe()))
	want := Info{WSL2: true, Mode: ModeNAT, NATInterface: "eth0", NATGateway: netip.MustParseAddr("172.27.128.1")}
	if got != want {
		t.Errorf("Classify(Gather) = %+v, want %+v", got, want)
	}
}

func TestGatherReadErrorsGiveEmptyValues(t *testing.T) {
	boom := errors.New("boom")

	f := wslFake()
	f.fileErr = map[string]error{kernelReleasePath: boom}
	got := Gather(context.Background(), f.probe())
	if got.OSRelease != "" {
		t.Errorf("OSRelease = %q on read error, want empty", got.OSRelease)
	}
	if got.DistroName != "archlinux" {
		t.Errorf("DistroName = %q, want archlinux (env still identifies WSL)", got.DistroName)
	}

	f = wslFake()
	f.fileErr = map[string]error{routePath: boom}
	f.files[routePath] = []byte("partial")
	got = Gather(context.Background(), f.probe())
	if len(got.ProcNetRoute) != 0 {
		t.Errorf("ProcNetRoute = %q on read error, want empty", got.ProcNetRoute)
	}
	if got.NetworkingMode != "nat" {
		t.Errorf("a route read error must not affect the mode: %q", got.NetworkingMode)
	}

	f = wslFake()
	f.ifaceErr = boom
	f.ifaces = []string{"lo"}
	got = Gather(context.Background(), f.probe())
	if len(got.Interfaces) != 0 {
		t.Errorf("Interfaces = %v on error, want empty", got.Interfaces)
	}
}

func TestGatherNetworkingModeError(t *testing.T) {
	f := wslFake()
	f.modeErr = errors.New("exec: wslinfo: not found")
	got := Gather(context.Background(), f.probe())
	if got.NetworkingModeErr == nil {
		t.Fatal("NetworkingModeErr = nil, want the probe error")
	}
	if info := Classify(got); !info.WSL2 || info.Mode != ModeUndetermined {
		t.Errorf("Classify = %+v, want WSL2 with undetermined mode", info)
	}
}

func TestGatherCancelledContext(t *testing.T) {
	f := wslFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := f.probe()
	// A probe that ignores ctx: Gather itself must report the cancellation.
	p.NetworkingMode = func(context.Context) (string, error) { f.modeCalls++; return "nat", nil }
	got := Gather(ctx, p)
	if got.NetworkingModeErr == nil {
		t.Fatal("NetworkingModeErr = nil on a cancelled context")
	}
	if f.modeCalls != 0 {
		t.Errorf("NetworkingMode called %d times on a cancelled context, want 0", f.modeCalls)
	}
	if info := Classify(got); !info.WSL2 || info.Mode != ModeUndetermined {
		t.Errorf("Classify = %+v, want WSL2 with undetermined mode", info)
	}
}

func TestParseWslinfoOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "nat", "nat"},
		{"trailing newline", "mirrored\n", "mirrored"},
		{"crlf", "nat\r\n", "nat"},
		{"surrounding spaces", "  virtioproxy \t\n", "virtioproxy"},
		{"empty", "", ""},
		{"only spaces", " \n\t ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseWslinfoOutput([]byte(tt.in)); got != tt.want {
				t.Errorf("parseWslinfoOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseWslinfoOutputIsBounded(t *testing.T) {
	long := strings.Repeat("a", 500) + "\n"
	got := parseWslinfoOutput([]byte(long))
	if len(got) == 0 || len(got) > 64 {
		t.Fatalf("len = %d, want 1..64", len(got))
	}
	if strings.Trim(got, "a") != "" {
		t.Errorf("output %q is not a prefix of the input", got)
	}
	// Padding must neither push the value out of the window nor survive trimming.
	if got := parseWslinfoOutput([]byte("nat" + strings.Repeat(" ", 200))); got != "nat" {
		t.Errorf("padded = %q, want nat", got)
	}
}
