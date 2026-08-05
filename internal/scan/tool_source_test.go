package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// D27. scan.trivy_path and scan.gitleaks_path were declared in the schema,
// defaulted, and tilde-expanded on load — and read by nothing. Detection called
// exec.LookPath("trivy") and the command builders hard-coded the name, so
// pointing DevDesk at a tool outside PATH did nothing, silently.
//
// The other half: detection tried the binary first and only reached for Docker
// in the else, so a binary on PATH always won. Asking for the pinned image
// while trivy happened to be installed was not expressible.

// installToolAt writes a fake tool at an explicit path outside PATH, which is
// the situation trivy_path exists for.
func installToolAt(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("reading the test binary: %v", err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatalf("installing %s: %v", path, err)
	}
	return path
}

func TestAConfiguredBinaryPathIsWhatGetsRun(t *testing.T) {
	installTools(t, "Version: 0.50.1") // an empty PATH: no trivy anywhere on it
	custom := installToolAt(t, t.TempDir(), "trivy")

	deps := CheckDependencies(config.ScanConfig{TrivyPath: custom})

	if !deps.TrivyAvailable {
		t.Fatalf("trivy at %s was not found; a configured path is the one thing that must be looked at", custom)
	}
	if deps.TrivySource != ToolSourceBinary {
		t.Errorf("TrivySource = %q, want a binary", deps.TrivySource)
	}
	if deps.TrivyBinary != custom {
		t.Errorf("TrivyBinary = %q, want the configured path %q", deps.TrivyBinary, custom)
	}
}

func TestAConfiguredGitleaksPathIsWhatGetsRun(t *testing.T) {
	installTools(t, "v8.18.0")
	custom := installToolAt(t, t.TempDir(), "gitleaks")

	deps := CheckDependencies(config.ScanConfig{GitleaksPath: custom})

	if !deps.GitleaksAvailable || deps.GitleaksBinary != custom {
		t.Errorf("gitleaks: available=%v binary=%q, want the configured path %q",
			deps.GitleaksAvailable, deps.GitleaksBinary, custom)
	}
}

// The configured path reaches the invocation, not just the detection. A binary
// that is found and then not used is the same defect one step later.
func TestTheConfiguredBinaryReachesTheCommand(t *testing.T) {
	spec := ToolSpec{Source: ToolSourceBinary, Binary: "/opt/trivy/bin/trivy"}

	cmd, err := trivyArgs("/repo", TargetDirectory, false, spec, "", false, false)
	if err != nil {
		t.Fatalf("trivyArgs: %v", err)
	}

	if cmd.Name != "/opt/trivy/bin/trivy" {
		t.Errorf("command runs %q, want the configured binary", cmd.Name)
	}
}

func TestTheConfiguredGitleaksBinaryReachesTheCommand(t *testing.T) {
	spec := ToolSpec{Source: ToolSourceBinary, Binary: "/opt/gitleaks"}

	if cmd := gitleaksArgs("/repo", spec, false, ""); cmd.Name != "/opt/gitleaks" {
		t.Errorf("command runs %q, want the configured binary", cmd.Name)
	}
}

// "image" must beat a binary that happens to be installed. Under the old
// binary-first resolution this was not expressible at all.
func TestTheImageSourceIgnoresABinaryOnThePath(t *testing.T) {
	installTools(t, "present", "trivy", "docker")

	deps := CheckDependencies(config.ScanConfig{TrivySource: config.ToolSourceImage})

	if deps.TrivySource != ToolSourceDocker {
		t.Errorf("TrivySource = %q with source=image and trivy on PATH, want docker", deps.TrivySource)
	}
	if !deps.TrivyAvailable {
		t.Error("TrivyAvailable = false, but docker and the image are both there")
	}
}

// The loud failure. Falling back to Docker is what kept D27 invisible: scans
// kept working, with something other than what was asked for.
func TestTheBinarySourceDoesNotFallBackToDocker(t *testing.T) {
	installTools(t, "present", "docker") // docker and its image, but no trivy

	deps := CheckDependencies(config.ScanConfig{TrivySource: config.ToolSourceBinary})

	if deps.TrivyAvailable {
		t.Errorf("trivy reported available (source %q) with source=binary and no binary anywhere",
			deps.TrivySource)
	}
}

// auto is the default and must behave exactly as the old resolution did, or
// every existing config changes meaning on upgrade.
func TestAutoKeepsTheOldBinaryFirstResolution(t *testing.T) {
	installTools(t, "Version: 0.50.1", "trivy", "gitleaks", "docker")

	for _, source := range []string{"", config.ToolSourceAuto} {
		deps := CheckDependencies(config.ScanConfig{TrivySource: source})
		if deps.TrivySource != ToolSourceBinary {
			t.Errorf("source %q: TrivySource = %q, want the binary to win as before", source, deps.TrivySource)
		}
	}
}

// An empty PATH with no docker leaves nothing to run, whatever was asked for.
func TestNothingInstalledIsReportedAsUnavailable(t *testing.T) {
	installTools(t, "")

	deps := CheckDependencies(config.ScanConfig{})

	if deps.TrivyAvailable || deps.GitleaksAvailable {
		t.Errorf("trivy=%v gitleaks=%v on an empty PATH, want both unavailable",
			deps.TrivyAvailable, deps.GitleaksAvailable)
	}
}
