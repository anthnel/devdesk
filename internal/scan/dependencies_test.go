package scan

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// Detection probes the machine it runs on, which is exactly what a test must
// not do. Instead the tools are installed on a PATH of the test's own making,
// by the shared fake binary of internal/testutil/fakecli (OC-02): the test
// binary started under the tool's name, answering a declared scenario.

// installTools puts the named tools on an otherwise empty PATH and makes each of
// them answer every argv with output. An empty output is a tool that prints
// nothing, which is how "docker has no such image" is expressed. These tests are
// about what detection concludes from the answers, not about the argv it sends,
// so no argv is declared: a test that cares installs its own scenario.
func installTools(t *testing.T, output string, names ...string) *fakecli.Fake {
	t.Helper()
	return installToolsRefusing(t, output, "", names...)
}

// installToolsRefusing is installTools, except that a tool started with refused
// as its first argument fails the way an unreachable daemon does. One binary is
// asked several different things (`docker images` and `docker run` reach the
// same fake), so failing is selected per subcommand. An empty refused refuses
// nothing.
func installToolsRefusing(t *testing.T, output, refused string, names ...string) *fakecli.Fake {
	t.Helper()
	var sc fakecli.Scenario
	for _, name := range names {
		if refused != "" {
			sc = append(sc, fakecli.Exchange{
				Tool: name, Args: []string{refused}, Prefix: true,
				Reply: fakecli.Reply{Stderr: "cannot connect to the daemon\n", Exit: 1},
			})
		}
		sc = append(sc, fakecli.Exchange{Tool: name, Prefix: true, Reply: fakecli.Reply{Stdout: output}})
	}
	return fakecli.Install(t, sc, names...)
}

// A tool installed natively is preferred over the container: it starts faster
// and needs no daemon.
func TestAToolOnThePathIsUsedDirectly(t *testing.T) {
	installTools(t, "Version: 0.50.1", "trivy", "gitleaks")

	deps := Detect(config.ScanTools{})

	if !deps.Available(ToolTrivy) || deps.Status(ToolTrivy).Source != ToolSourceBinary {
		t.Errorf("trivy: available=%v source=%q, want an installed binary", deps.Available(ToolTrivy), deps.Status(ToolTrivy).Source)
	}
	if !deps.Available(ToolGitleaks) || deps.Status(ToolGitleaks).Source != ToolSourceBinary {
		t.Errorf("gitleaks: available=%v source=%q, want an installed binary", deps.Available(ToolGitleaks), deps.Status(ToolGitleaks).Source)
	}
	if !strings.Contains(deps.Status(ToolTrivy).Version, "0.50.1") || !strings.Contains(deps.Status(ToolGitleaks).Version, "0.50.1") {
		t.Errorf("versions = %q / %q, want what the tools reported", deps.Status(ToolTrivy).Version, deps.Status(ToolGitleaks).Version)
	}
	// Nothing else is on this PATH.
	if deps.EngineAvailable {
		t.Error("docker was reported as available although it is not installed")
	}
}

// With no binary, the image is the fallback — but only if it has been pulled,
// since scanning must not silently trigger a download.
func TestWithNoBinaryTheDockerImageIsTheFallback(t *testing.T) {
	installTools(t, "sha256:2b1cbf1a4f0e", "docker")

	deps := Detect(config.ScanTools{})

	if !deps.Available(ToolTrivy) || deps.Status(ToolTrivy).Source != ToolSourceContainer {
		t.Errorf("trivy: available=%v source=%q, want the image", deps.Available(ToolTrivy), deps.Status(ToolTrivy).Source)
	}
	if !deps.Available(ToolGitleaks) || deps.Status(ToolGitleaks).Source != ToolSourceContainer {
		t.Errorf("gitleaks: available=%v source=%q, want the image", deps.Available(ToolGitleaks), deps.Status(ToolGitleaks).Source)
	}
	if !deps.EngineAvailable {
		t.Error("docker is on the PATH but was not reported as available")
	}
	if !strings.HasPrefix(deps.Status(ToolTrivy).Version, "docker:") {
		t.Errorf("TrivyVersion = %q, want it marked as coming from the image", deps.Status(ToolTrivy).Version)
	}
}

// `docker images -q` printing nothing is how Docker says the image is not
// there. A scan that assumed otherwise would fail at the worst moment.
func TestAnImageThatWasNeverPulledIsNotAvailable(t *testing.T) {
	installTools(t, "", "docker")

	deps := Detect(config.ScanTools{})

	if !deps.EngineAvailable {
		t.Error("docker is on the PATH but was not reported as available")
	}
	if deps.Available(ToolTrivy) || deps.Available(ToolGitleaks) {
		t.Errorf("a tool was reported available with neither a binary nor an image: %+v", deps)
	}
	if deps.Status(ToolTrivy).Source != ToolSourceNone || deps.Status(ToolGitleaks).Source != ToolSourceNone {
		t.Errorf("sources = %q / %q, want none", deps.Status(ToolTrivy).Source, deps.Status(ToolGitleaks).Source)
	}
}

// Docker installed is not Docker running. When the daemon cannot be reached the
// image lookup fails outright, which is not the same as the image being absent
// — but it has the same consequence, and reporting the tool as available would
// mean failing later instead of now.
func TestADaemonThatCannotBeReachedLeavesTheToolsUnavailable(t *testing.T) {
	installToolsRefusing(t, "sha256:2b1cbf1a4f0e", "images", "docker")

	deps := Detect(config.ScanTools{})

	if !deps.EngineAvailable {
		t.Error("the docker binary is installed and should be reported as such")
	}
	if deps.Available(ToolTrivy) || deps.Available(ToolGitleaks) {
		t.Errorf("a tool was reported available although the daemon did not answer: %+v", deps)
	}
}

// Failing to read a version is not failing to scan, so the tool stays usable
// and the version simply says where it comes from.
func TestAnImageThatWillNotReportItsVersionIsStillUsable(t *testing.T) {
	installToolsRefusing(t, "sha256:2b1cbf1a4f0e", "run", "docker")

	deps := Detect(config.ScanTools{})

	if !deps.Available(ToolTrivy) || deps.Status(ToolTrivy).Source != ToolSourceContainer {
		t.Errorf("trivy: available=%v source=%q, want the image", deps.Available(ToolTrivy), deps.Status(ToolTrivy).Source)
	}
	if deps.Status(ToolTrivy).Version != "docker" || deps.Status(ToolGitleaks).Version != "docker" {
		t.Errorf("versions = %q / %q, want them to fall back to naming the source",
			deps.Status(ToolTrivy).Version, deps.Status(ToolGitleaks).Version)
	}
}

func TestWithNothingInstalledNothingIsAvailable(t *testing.T) {
	installTools(t, "")

	deps := Detect(config.ScanTools{})

	if deps.Available(ToolTrivy) || deps.Available(ToolGitleaks) || deps.EngineAvailable {
		t.Errorf("something was reported available on an empty PATH: %+v", deps)
	}
	// The images are still named, because the dashboard shows which one a scan
	// would use once Docker is there.
	if deps.Status(ToolTrivy).Image != DefaultTrivyImage || deps.Status(ToolGitleaks).Image != DefaultGitleaksImage {
		t.Errorf("images = %q / %q, want the defaults filled in", deps.Status(ToolTrivy).Image, deps.Status(ToolGitleaks).Image)
	}
}

func TestConfiguredImagesReplaceTheDefaults(t *testing.T) {
	installTools(t, "")

	var tools config.ScanTools
	tools.Trivy.Image = "mirror.local/trivy:0.50"
	tools.Gitleaks.Image = "mirror.local/gitleaks:8.18"
	deps := Detect(tools)

	if deps.Status(ToolTrivy).Image != "mirror.local/trivy:0.50" || deps.Status(ToolGitleaks).Image != "mirror.local/gitleaks:8.18" {
		t.Errorf("images = %q / %q, want the configured ones", deps.Status(ToolTrivy).Image, deps.Status(ToolGitleaks).Image)
	}
}

// NewScanner is the constructor the application uses, and the only difference
// from the one the tests use is that it probes the machine.
func TestNewScannerDetectsWhatIsInstalled(t *testing.T) {
	installTools(t, "Version: 0.50.1", "trivy", "gitleaks")

	s := NewScanner(scanFor(CategoryIDVuln))

	if !s.deps.Available(ToolTrivy) || s.deps.Status(ToolTrivy).Source != ToolSourceBinary {
		t.Errorf("the constructor did not pick up the installed trivy: %+v", s.deps)
	}
	if !s.options.Categories.Vuln.Enabled {
		t.Error("the options were not carried onto the scanner")
	}
}

// Every tool's source setting has to reach detection through NewScanner. The
// constructor used to forward Trivy's and Gitleaks' only, so a scan resolved
// plumber with its defaults whatever the context said — only the dashboard,
// which passes the whole config, honoured plumber_source. It walks the tool
// table, so a tool added to it is covered without being named here.
func TestNewScannerHonoursEveryToolsSource(t *testing.T) {
	var names []string
	for _, tool := range Tools() {
		names = append(names, tool.Binary)
	}
	installTools(t, "present", append(names, "docker")...)

	cfg := config.Default()
	for _, tool := range Tools() {
		set := cfg.Scan.Tools.Tool(string(tool.ID))
		set.Source = config.ToolSourceImage
		set.Image = "mirror.example/" + tool.Binary + ":1"
	}
	s := NewScanner(OptionsFromConfig(cfg))

	for _, tool := range Tools() {
		spec := s.deps.Spec(tool.ID)
		if want := "mirror.example/" + tool.Binary + ":1"; spec.Source != ToolSourceContainer || spec.Image != want {
			t.Errorf("%s resolved as %q with image %q, want the configured image source %q",
				tool.Name, spec.Source, spec.Image, want)
		}
	}
}
