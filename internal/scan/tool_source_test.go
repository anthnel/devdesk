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

	deps := Detect(config.ScanTools{Trivy: config.TrivyConfig{ToolConfig: config.ToolConfig{Binary: custom}}})

	if !deps.Available(ToolTrivy) {
		t.Fatalf("trivy at %s was not found; a configured path is the one thing that must be looked at", custom)
	}
	if deps.Status(ToolTrivy).Source != ToolSourceBinary {
		t.Errorf("TrivySource = %q, want a binary", deps.Status(ToolTrivy).Source)
	}
	if deps.Status(ToolTrivy).Binary != custom {
		t.Errorf("TrivyBinary = %q, want the configured path %q", deps.Status(ToolTrivy).Binary, custom)
	}
}

func TestAConfiguredGitleaksPathIsWhatGetsRun(t *testing.T) {
	installTools(t, "v8.18.0")
	custom := installToolAt(t, t.TempDir(), "gitleaks")

	deps := Detect(config.ScanTools{Gitleaks: config.GitleaksConfig{ToolConfig: config.ToolConfig{Binary: custom}}})

	if !deps.Available(ToolGitleaks) || deps.Status(ToolGitleaks).Binary != custom {
		t.Errorf("gitleaks: available=%v binary=%q, want the configured path %q",
			deps.Available(ToolGitleaks), deps.Status(ToolGitleaks).Binary, custom)
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

	if cmd := gitleaksArgs("/repo", spec, false, "", ""); cmd.Name != "/opt/gitleaks" {
		t.Errorf("command runs %q, want the configured binary", cmd.Name)
	}
}

// "image" must beat a binary that happens to be installed. Under the old
// binary-first resolution this was not expressible at all.
func TestTheImageSourceIgnoresABinaryOnThePath(t *testing.T) {
	installTools(t, "present", "trivy", "docker")

	deps := Detect(config.ScanTools{Trivy: config.TrivyConfig{ToolConfig: config.ToolConfig{Source: config.ToolSourceImage}}})

	if deps.Status(ToolTrivy).Source != ToolSourceContainer {
		t.Errorf("TrivySource = %q with source=image and trivy on PATH, want docker", deps.Status(ToolTrivy).Source)
	}
	if !deps.Available(ToolTrivy) {
		t.Error("TrivyAvailable = false, but docker and the image are both there")
	}
}

// The loud failure. Falling back to Docker is what kept D27 invisible: scans
// kept working, with something other than what was asked for.
func TestTheBinarySourceDoesNotFallBackToDocker(t *testing.T) {
	installTools(t, "present", "docker") // docker and its image, but no trivy

	deps := Detect(config.ScanTools{Trivy: config.TrivyConfig{ToolConfig: config.ToolConfig{Source: config.ToolSourceBinary}}})

	if deps.Available(ToolTrivy) {
		t.Errorf("trivy reported available (source %q) with source=binary and no binary anywhere",
			deps.Status(ToolTrivy).Source)
	}
}

// auto is the default and must behave exactly as the old resolution did, or
// every existing config changes meaning on upgrade.
func TestAutoKeepsTheOldBinaryFirstResolution(t *testing.T) {
	installTools(t, "Version: 0.50.1", "trivy", "gitleaks", "docker")

	for _, source := range []string{"", config.ToolSourceAuto} {
		deps := Detect(config.ScanTools{Trivy: config.TrivyConfig{ToolConfig: config.ToolConfig{Source: source}}})
		if deps.Status(ToolTrivy).Source != ToolSourceBinary {
			t.Errorf("source %q: TrivySource = %q, want the binary to win as before", source, deps.Status(ToolTrivy).Source)
		}
	}
}

// An empty PATH with no docker leaves nothing to run, whatever was asked for.
func TestNothingInstalledIsReportedAsUnavailable(t *testing.T) {
	installTools(t, "")

	deps := Detect(config.ScanTools{})

	if deps.Available(ToolTrivy) || deps.Available(ToolGitleaks) {
		t.Errorf("trivy=%v gitleaks=%v on an empty PATH, want both unavailable",
			deps.Available(ToolTrivy), deps.Available(ToolGitleaks))
	}
}

// ── plumber ──────────────────────────────────────────────────────────────────

// The three preferences behave for plumber exactly as they do for the other
// two: resolveTool is shared, so what is worth checking is that plumber is
// wired to it rather than resolved by a copy of it.
func TestPlumberIsResolvedLikeTheOtherTwo(t *testing.T) {
	t.Run("a configured path is what gets run", func(t *testing.T) {
		installTools(t, "plumber version 0.4.40")
		custom := installToolAt(t, t.TempDir(), "plumber")

		deps := Detect(config.ScanTools{Plumber: config.ToolConfig{Binary: custom}})

		if !deps.Available(ToolPlumber) || deps.Status(ToolPlumber).Binary != custom {
			t.Errorf("plumber: available=%v binary=%q, want the configured path %q",
				deps.Available(ToolPlumber), deps.Status(ToolPlumber).Binary, custom)
		}
	})

	t.Run("binary does not fall back to docker", func(t *testing.T) {
		installTools(t, "present", "docker") // docker and its image, but no plumber

		deps := Detect(config.ScanTools{Plumber: config.ToolConfig{Source: config.ToolSourceBinary}})

		if deps.Available(ToolPlumber) {
			t.Errorf("plumber reported available (source %q) with source=binary and no binary",
				deps.Status(ToolPlumber).Source)
		}
	})

	t.Run("image beats a binary on the path", func(t *testing.T) {
		installTools(t, "present", "plumber", "docker")

		deps := Detect(config.ScanTools{Plumber: config.ToolConfig{Source: config.ToolSourceImage}})

		if deps.Status(ToolPlumber).Source != ToolSourceContainer || !deps.Available(ToolPlumber) {
			t.Errorf("PlumberSource = %q available = %v, want docker and available",
				deps.Status(ToolPlumber).Source, deps.Available(ToolPlumber))
		}
	})
}

// The spec is what a command builder will be handed, and PR 2 depends on it
// carrying all three pieces — the pair (source, image) is what left trivy_path
// unread for so long (D27).
func TestThePlumberSpecCarriesWhatItWasResolvedWith(t *testing.T) {
	deps := Report{Tools: map[ToolID]ToolStatus{ToolPlumber: {
		Available: true,
		Source:    ToolSourceBinary,
		Binary:    "/opt/plumber",
		Image:     "mirror.example/plumber:0.4.40",
	}}}

	spec := deps.Spec(ToolPlumber)

	if spec.Source != ToolSourceBinary || spec.Binary != "/opt/plumber" ||
		spec.Image != "mirror.example/plumber:0.4.40" {
		t.Errorf("Spec(plumber) = %+v, want every field carried through", spec)
	}
}

// An unset image resolves to the default rather than to the empty string, or
// the docker invocation would name no image at all.
func TestAnUnsetPlumberImageResolvesToTheDefault(t *testing.T) {
	installTools(t, "")

	if got := Detect(config.ScanTools{}).Status(ToolPlumber).Image; got != DefaultPlumberImage {
		t.Errorf("plumber image = %q, want %q", got, DefaultPlumberImage)
	}
}
