package version

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestShort(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{"a released build is its version", Info{Version: "v0.2.0", Commit: "a1b2c3d"}, "v0.2.0"},
		{"a dev build names the commit instead", Info{Version: DevVersion, Commit: "a1b2c3d"}, "dev+a1b2c3d"},
		{"a dev build with no commit says only that", Info{Version: DevVersion, Commit: Unknown}, "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.Short(); got != tt.want {
				t.Errorf("Short() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReleased(t *testing.T) {
	if (Info{Version: DevVersion}).Released() {
		t.Error("a dev build reports itself as released")
	}
	if !(Info{Version: "v1.0.0"}).Released() {
		t.Error("a tagged build does not report itself as released")
	}
}

// A `-X` is a deliberate assertion, the VCS metadata a fallback. If the
// latter overwrote the former, a release binary built from a tag would
// announce the build's commit rather than the requested version.
func TestBuildInfoNeverOverwritesALdflag(t *testing.T) {
	info := Info{Version: "v0.2.0", Commit: "deadbee", Date: "2026-01-01T00:00:00Z"}
	info.fillFromBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-09-06T00:00:00Z"},
		},
	})

	if info.Version != "v0.2.0" {
		t.Errorf("Version = %q, want the ldflag value v0.2.0", info.Version)
	}
	if info.Commit != "deadbee" {
		t.Errorf("Commit = %q, want the ldflag value deadbee", info.Commit)
	}
	if info.Date != "2026-01-01T00:00:00Z" {
		t.Errorf("Date = %q, want the ldflag value", info.Date)
	}
}

func TestBuildInfoFillsWhatLdflagsLeftEmpty(t *testing.T) {
	info := Info{Version: DevVersion}
	info.fillFromBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-09-06T00:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	})

	// "(devel)" is what Go writes for a local build: it names nothing that
	// DevVersion does not already say.
	if info.Version != DevVersion {
		t.Errorf("Version = %q, want %q — (devel) is not a version", info.Version, DevVersion)
	}
	if info.Commit != "0123456" {
		t.Errorf("Commit = %q, want the revision shortened to 7 characters", info.Commit)
	}
	if info.Date != "2026-09-06T00:00:00Z" {
		t.Errorf("Date = %q, want the VCS time", info.Date)
	}
	if !info.Dirty {
		t.Error("Dirty is false although vcs.modified said true")
	}
}

// `go install pkg@v0.2.0` is the one case where a version exists without any
// ldflag, and nothing else in the chain would find it.
func TestAModuleVersionAnswersForGoInstall(t *testing.T) {
	info := Info{Version: DevVersion}
	info.fillFromBuildInfo(&debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}})

	if info.Version != "v0.2.0" {
		t.Errorf("Version = %q, want the module version v0.2.0", info.Version)
	}
}

// An empty field would be rendered as-is in the About screen, where a missing
// value must read as missing rather than as a blank.
func TestGetLeavesNoFieldEmpty(t *testing.T) {
	info := Get()

	if info.Version == "" {
		t.Error("Version is empty")
	}
	if info.Commit == "" {
		t.Error("Commit is empty, want a SHA or Unknown")
	}
	if info.Date == "" {
		t.Error("Date is empty, want a date or Unknown")
	}
	if info.Go != runtime.Version() {
		t.Errorf("Go = %q, want %q", info.Go, runtime.Version())
	}
	if want := runtime.GOOS + "/" + runtime.GOARCH; info.Platform != want {
		t.Errorf("Platform = %q, want %q", info.Platform, want)
	}
}

func TestShortSHA(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"0123456789abcdef", "0123456"},
		{"0123456", "0123456"},
		{"abc", "abc"},
		{"  0123456789  ", "0123456"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := shortSHA(tt.in); got != tt.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
