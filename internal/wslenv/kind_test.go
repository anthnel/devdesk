package wslenv

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyKind(t *testing.T) {
	tests := []struct {
		name string
		in   Facts
		want Kind
	}{
		{"wsl1 with distro name", Facts{DistroName: "Ubuntu", OSRelease: "4.4.0-19041-Microsoft"}, KindWSL1},
		{"wsl1 without distro name (env -i)", Facts{OSRelease: "4.4.0-19041-Microsoft"}, KindWSL1},
		{"wsl1 with trailing newline", Facts{OSRelease: "4.4.0-19041-Microsoft\n"}, KindWSL1},
		{"wsl2 modern kernel", Facts{DistroName: "archlinux", OSRelease: "5.15.167.4-microsoft-standard-WSL2"}, KindWSL2},
		{"wsl2 modern kernel without distro name", Facts{OSRelease: "5.15.167.4-microsoft-standard-WSL2"}, KindWSL2},
		{"wsl2 kernel without the WSL2 suffix", Facts{OSRelease: "4.19.104-microsoft-standard"}, KindWSL2},
		{"wsl2 custom kernel, distro name", Facts{DistroName: "Ubuntu", OSRelease: "6.12.0-custom"}, KindWSL2},
		{"native linux", Facts{OSRelease: "6.12.0-amd64"}, KindNone},
		{"empty facts", Facts{}, KindNone},
		{"osrelease unreadable, distro name", Facts{DistroName: "Ubuntu"}, KindWSL2},
		{"osrelease unreadable, no distro name", Facts{}, KindNone},
		// Other facts must not influence the kind.
		{"networking facts ignored", Facts{
			OSRelease: "4.4.0-19041-Microsoft", NetworkingMode: "mirrored",
			NetworkingModeErr: errors.New("boom"), Interfaces: []string{"loopback0"},
			ProcNetRoute: []byte(natRoutes),
		}, KindWSL1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyKind(tt.in); got != tt.want {
				t.Errorf("ClassifyKind(%+v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// The WSL1 marker is a capital M; a lower-case "microsoft" is the WSL2 kernel.
func TestClassifyKindMicrosoftIsCaseSensitive(t *testing.T) {
	if got := ClassifyKind(Facts{OSRelease: "4.4.0-19041-microsoft"}); got != KindWSL2 {
		t.Errorf("lower-case microsoft = %d, want KindWSL2", got)
	}
	if got := ClassifyKind(Facts{OSRelease: "4.4.0-19041-MICROSOFT"}); got != KindWSL2 {
		t.Errorf("upper-case MICROSOFT = %d, want KindWSL2", got)
	}
}

func TestKindZeroValueIsNone(t *testing.T) {
	var k Kind
	if k != KindNone {
		t.Errorf("zero Kind = %d, want KindNone", k)
	}
	if KindNone == KindWSL1 || KindWSL1 == KindWSL2 || KindNone == KindWSL2 {
		t.Error("Kind values must be distinct")
	}
}

func TestGatherKernel(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		env         map[string]string
		release     string
		releaseErr  error
		want        Facts
		wantRead    bool
		wantGetenv  bool
		wantNothing bool
	}{
		{name: "wsl1", goos: "linux", env: map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, release: "4.4.0-19041-Microsoft\n",
			want: Facts{DistroName: "Ubuntu", OSRelease: "4.4.0-19041-Microsoft\n"}, wantRead: true, wantGetenv: true},
		{name: "wsl2 without distro name", goos: "linux", release: wslRelease,
			want: Facts{OSRelease: wslRelease}, wantRead: true, wantGetenv: true},
		{name: "native linux", goos: "linux", release: nativeRelease,
			want: Facts{OSRelease: nativeRelease}, wantRead: true, wantGetenv: true},
		{name: "osrelease unreadable", goos: "linux", env: map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, releaseErr: errors.New("boom"),
			want: Facts{DistroName: "Ubuntu"}, wantRead: true, wantGetenv: true},
		{name: "darwin reads nothing", goos: "darwin", env: map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, release: wslRelease, wantNothing: true},
		{name: "windows reads nothing", goos: "windows", release: wslRelease, wantNothing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := wslFake()
			f.goos = tt.goos
			f.env = tt.env
			f.files[kernelReleasePath] = []byte(tt.release)
			if tt.releaseErr != nil {
				f.fileErr = map[string]error{kernelReleasePath: tt.releaseErr}
			}
			got := GatherKernel(f.probe())
			if got.DistroName != tt.want.DistroName || got.OSRelease != tt.want.OSRelease {
				t.Errorf("GatherKernel = %+v, want %+v", got, tt.want)
			}
			if got.NetworkingMode != "" || got.NetworkingModeErr != nil || len(got.Interfaces) != 0 || len(got.ProcNetRoute) != 0 {
				t.Errorf("GatherKernel filled a non-kernel fact: %+v", got)
			}
			if tt.wantNothing && len(f.reads)+len(f.getenvs) != 0 {
				t.Errorf("probe used off Linux: reads=%v getenv=%v", f.reads, f.getenvs)
			}
			if tt.wantRead && (len(f.reads) != 1 || !f.read(kernelReleasePath)) {
				t.Errorf("reads = %v, want exactly %s", f.reads, kernelReleasePath)
			}
			if tt.wantGetenv && (len(f.getenvs) != 1 || f.getenvs[0] != "WSL_DISTRO_NAME") {
				t.Errorf("getenv calls = %v, want exactly WSL_DISTRO_NAME", f.getenvs)
			}
		})
	}
}

// GatherKernel is the cheap path: it must not run wslinfo, list interfaces or
// read the route table, even under WSL2 where Gather does all three.
func TestGatherKernelCallsNoOtherProbe(t *testing.T) {
	f := wslFake()
	p := f.probe()
	p.NetworkingMode = func(context.Context) (string, error) {
		t.Error("NetworkingMode called by GatherKernel")
		return "nat", nil
	}
	p.InterfaceNames = func() ([]string, error) {
		t.Error("InterfaceNames called by GatherKernel")
		return []string{"eth0"}, nil
	}
	got := GatherKernel(p)
	if f.read(routePath) {
		t.Errorf("%s read by GatherKernel", routePath)
	}
	for _, r := range f.reads {
		if r != kernelReleasePath {
			t.Errorf("unexpected read %q", r)
		}
	}
	if f.modeCalls != 0 || f.ifaceN != 0 {
		t.Errorf("modeCalls=%d ifaceN=%d, want 0", f.modeCalls, f.ifaceN)
	}
	if ClassifyKind(got) != KindWSL2 {
		t.Errorf("ClassifyKind = %d, want KindWSL2", ClassifyKind(got))
	}
}

// A nil probe field the kernel path does not need must not be dereferenced.
func TestGatherKernelNeedsOnlyItsThreeProbeFields(t *testing.T) {
	f := wslFake()
	p := Probe{GOOS: f.goos, Getenv: f.probe().Getenv, ReadFile: f.probe().ReadFile}
	if got := ClassifyKind(GatherKernel(p)); got != KindWSL2 {
		t.Errorf("ClassifyKind(GatherKernel) = %d, want KindWSL2", got)
	}
}

func TestGatherKernelEndToEnd(t *testing.T) {
	f := wslFake()
	f.files[kernelReleasePath] = []byte("4.4.0-19041-Microsoft\n")
	if got := ClassifyKind(GatherKernel(f.probe())); got != KindWSL1 {
		t.Errorf("WSL1 = %d", got)
	}
	f = wslFake()
	f.env = map[string]string{}
	f.files[kernelReleasePath] = []byte(nativeRelease)
	if got := ClassifyKind(GatherKernel(f.probe())); got != KindNone {
		t.Errorf("native = %d", got)
	}
}
