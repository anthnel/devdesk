package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// Detection probes the machine it runs on, which is exactly what a test must
// not do. Instead the tools are installed on a PATH of the test's own making:
// each one is a copy of the test binary, which already knows how to behave as a
// scanner (see run_test.go).

// installTools puts the named tools on an otherwise empty PATH and makes them
// answer with output. An empty output is a tool that prints nothing, which is
// how "docker has no such image" is expressed.
func installTools(t *testing.T, output string, names ...string) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("reading the test binary: %v", err)
	}

	dir := t.TempDir()
	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o755); err != nil {
			t.Fatalf("installing %s: %v", name, err)
		}
	}

	t.Setenv("PATH", dir)
	t.Setenv(helperMode, "1")
	t.Setenv(helperStdout, output)
	t.Setenv(helperStderr, "")
	t.Setenv(helperExit, "0")
}

// A tool installed natively is preferred over the container: it starts faster
// and needs no daemon.
func TestAToolOnThePathIsUsedDirectly(t *testing.T) {
	installTools(t, "Version: 0.50.1", "trivy", "gitleaks")

	deps := CheckDependencies(config.ScanConfig{})

	if !deps.TrivyAvailable || deps.TrivySource != ToolSourceBinary {
		t.Errorf("trivy: available=%v source=%q, want an installed binary", deps.TrivyAvailable, deps.TrivySource)
	}
	if !deps.GitleaksAvailable || deps.GitleaksSource != ToolSourceBinary {
		t.Errorf("gitleaks: available=%v source=%q, want an installed binary", deps.GitleaksAvailable, deps.GitleaksSource)
	}
	if !strings.Contains(deps.TrivyVersion, "0.50.1") || !strings.Contains(deps.GitleaksVersion, "0.50.1") {
		t.Errorf("versions = %q / %q, want what the tools reported", deps.TrivyVersion, deps.GitleaksVersion)
	}
	// Nothing else is on this PATH.
	if deps.DockerAvailable {
		t.Error("docker was reported as available although it is not installed")
	}
}

// With no binary, the image is the fallback — but only if it has been pulled,
// since scanning must not silently trigger a download.
func TestWithNoBinaryTheDockerImageIsTheFallback(t *testing.T) {
	installTools(t, "sha256:2b1cbf1a4f0e", "docker")

	deps := CheckDependencies(config.ScanConfig{})

	if !deps.TrivyAvailable || deps.TrivySource != ToolSourceDocker {
		t.Errorf("trivy: available=%v source=%q, want the image", deps.TrivyAvailable, deps.TrivySource)
	}
	if !deps.GitleaksAvailable || deps.GitleaksSource != ToolSourceDocker {
		t.Errorf("gitleaks: available=%v source=%q, want the image", deps.GitleaksAvailable, deps.GitleaksSource)
	}
	if !deps.DockerAvailable {
		t.Error("docker is on the PATH but was not reported as available")
	}
	if !strings.HasPrefix(deps.TrivyVersion, "docker:") {
		t.Errorf("TrivyVersion = %q, want it marked as coming from the image", deps.TrivyVersion)
	}
}

// `docker images -q` printing nothing is how Docker says the image is not
// there. A scan that assumed otherwise would fail at the worst moment.
func TestAnImageThatWasNeverPulledIsNotAvailable(t *testing.T) {
	installTools(t, "", "docker")

	deps := CheckDependencies(config.ScanConfig{})

	if !deps.DockerAvailable {
		t.Error("docker is on the PATH but was not reported as available")
	}
	if deps.TrivyAvailable || deps.GitleaksAvailable {
		t.Errorf("a tool was reported available with neither a binary nor an image: %+v", deps)
	}
	if deps.TrivySource != ToolSourceNone || deps.GitleaksSource != ToolSourceNone {
		t.Errorf("sources = %q / %q, want none", deps.TrivySource, deps.GitleaksSource)
	}
}

// Docker installed is not Docker running. When the daemon cannot be reached the
// image lookup fails outright, which is not the same as the image being absent
// — but it has the same consequence, and reporting the tool as available would
// mean failing later instead of now.
func TestADaemonThatCannotBeReachedLeavesTheToolsUnavailable(t *testing.T) {
	installTools(t, "sha256:2b1cbf1a4f0e", "docker")
	t.Setenv(helperFailOn, "images")

	deps := CheckDependencies(config.ScanConfig{})

	if !deps.DockerAvailable {
		t.Error("the docker binary is installed and should be reported as such")
	}
	if deps.TrivyAvailable || deps.GitleaksAvailable {
		t.Errorf("a tool was reported available although the daemon did not answer: %+v", deps)
	}
}

// Failing to read a version is not failing to scan, so the tool stays usable
// and the version simply says where it comes from.
func TestAnImageThatWillNotReportItsVersionIsStillUsable(t *testing.T) {
	installTools(t, "sha256:2b1cbf1a4f0e", "docker")
	t.Setenv(helperFailOn, "run")

	deps := CheckDependencies(config.ScanConfig{})

	if !deps.TrivyAvailable || deps.TrivySource != ToolSourceDocker {
		t.Errorf("trivy: available=%v source=%q, want the image", deps.TrivyAvailable, deps.TrivySource)
	}
	if deps.TrivyVersion != "docker" || deps.GitleaksVersion != "docker" {
		t.Errorf("versions = %q / %q, want them to fall back to naming the source",
			deps.TrivyVersion, deps.GitleaksVersion)
	}
}

func TestWithNothingInstalledNothingIsAvailable(t *testing.T) {
	installTools(t, "")

	deps := CheckDependencies(config.ScanConfig{})

	if deps.TrivyAvailable || deps.GitleaksAvailable || deps.DockerAvailable {
		t.Errorf("something was reported available on an empty PATH: %+v", deps)
	}
	// The images are still named, because the dashboard shows which one a scan
	// would use once Docker is there.
	if deps.TrivyImage != DefaultTrivyImage || deps.GitleaksImage != DefaultGitleaksImage {
		t.Errorf("images = %q / %q, want the defaults filled in", deps.TrivyImage, deps.GitleaksImage)
	}
}

func TestConfiguredImagesReplaceTheDefaults(t *testing.T) {
	installTools(t, "")

	deps := CheckDependencies(config.ScanConfig{
		TrivyImage:    "mirror.local/trivy:0.50",
		GitleaksImage: "mirror.local/gitleaks:8.18",
	})

	if deps.TrivyImage != "mirror.local/trivy:0.50" || deps.GitleaksImage != "mirror.local/gitleaks:8.18" {
		t.Errorf("images = %q / %q, want the configured ones", deps.TrivyImage, deps.GitleaksImage)
	}
}

// NewScanner is the constructor the application uses, and the only difference
// from the one the tests use is that it probes the machine.
func TestNewScannerDetectsWhatIsInstalled(t *testing.T) {
	installTools(t, "Version: 0.50.1", "trivy", "gitleaks")

	s := NewScanner(ScanOptions{EnableVuln: true})

	if !s.deps.TrivyAvailable || s.deps.TrivySource != ToolSourceBinary {
		t.Errorf("the constructor did not pick up the installed trivy: %+v", s.deps)
	}
	if !s.options.EnableVuln {
		t.Error("the options were not carried onto the scanner")
	}
}
