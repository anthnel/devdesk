package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// TestMain lets the test binary answer as a fake tool when fakecli starts it
// under a tool name; without it Install refuses to run.
func TestMain(m *testing.M) { os.Exit(fakecli.Main(m)) }

// installEngines puts executables named names alone on PATH, so exec.LookPath
// finds them and nothing else does. They come from the shared fake binary of
// internal/testutil/fakecli (OC-02) and are only looked up here, never run.
func installEngines(t *testing.T, names ...string) {
	t.Helper()
	fakecli.Install(t, nil, names...)
}

// pathOf is where the installed tool name resolves on PATH.
func pathOf(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is not on PATH: %v", name, err)
	}
	return path
}

// onlyPath replaces PATH with dir for the duration of the test.
func onlyPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir)
}

func TestAutoPrefersDockerThenPodman(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		want      string
	}{
		{"both installed", []string{"docker", "podman"}, Docker},
		{"only podman", []string{"podman"}, Podman},
		{"only docker", []string{"docker"}, Docker},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installEngines(t, tc.installed...)

			shape, err := Resolve(Auto)
			if err != nil {
				t.Fatalf("Resolve(auto) = %v, want a shape", err)
			}
			if shape.Name != tc.want {
				t.Errorf("Resolve(auto) picked %q, want %q", shape.Name, tc.want)
			}
			if shape.Binary != tc.want {
				t.Errorf("Binary = %q, want %q", shape.Binary, tc.want)
			}
		})
	}
}

// An empty preference is what a config file written before the setting existed
// carries, and it has to mean the same thing as "auto" — the default is written
// on load for the configuration view's sake, not because anything downstream
// needs it.
func TestAnEmptyPreferenceMeansAuto(t *testing.T) {
	installEngines(t, "podman")

	shape, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") = %v, want a shape", err)
	}
	if shape.Name != Podman {
		t.Errorf("Resolve(\"\") picked %q, want %q", shape.Name, Podman)
	}
}

func TestAutoFailsWhenNeitherEngineIsInstalled(t *testing.T) {
	onlyPath(t, t.TempDir())

	_, err := Resolve(Auto)
	if err == nil {
		t.Fatal("Resolve(auto) returned no error with no engine on PATH")
	}
	// The message has to name both, since the user picked neither.
	for _, want := range []string{Docker, Podman} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// A pinned engine that is absent is an error, never the other engine. The two
// do not hold the same containers, so a silent substitution would answer a
// question nobody asked — credentials.Select's rule (§3.9).
func TestAPinnedEngineNeverFallsBackToTheOther(t *testing.T) {
	for _, tc := range []struct{ pinned, installed string }{
		{Podman, "docker"},
		{Docker, "podman"},
	} {
		t.Run(tc.pinned+" pinned, "+tc.installed+" installed", func(t *testing.T) {
			installEngines(t, tc.installed)

			shape, err := Resolve(tc.pinned)
			if err == nil {
				t.Fatalf("Resolve(%q) returned %q, want an error", tc.pinned, shape.Name)
			}
			if !strings.Contains(err.Error(), tc.pinned) {
				t.Errorf("error %q does not name the engine that was asked for", err)
			}
		})
	}
}

func TestAConfiguredPathIsRunAsGiven(t *testing.T) {
	installEngines(t, "podman")
	path := pathOf(t, "podman")

	shape, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want a shape", path, err)
	}
	if shape.Binary != path {
		t.Errorf("Binary = %q, want the configured path %q", shape.Binary, path)
	}
	// The name is read off the path, which is what decides the auth file.
	if shape.Name != Podman {
		t.Errorf("Name = %q, want %q for a podman path", shape.Name, Podman)
	}
}

// A path that names nothing recognisable is treated as docker, which is what a
// docker-compatible CLI behaves like. Getting this wrong costs the auth file
// location, not the invocation — so the fallback is the compatible one.
func TestAnUnrecognisedPathIsTreatedAsDocker(t *testing.T) {
	installEngines(t, "nerdctl")
	path := pathOf(t, "nerdctl")

	shape, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want a shape", path, err)
	}
	if shape.Name != Docker {
		t.Errorf("Name = %q, want %q", shape.Name, Docker)
	}
	if shape.Binary != path {
		t.Errorf("Binary = %q, want %q", shape.Binary, path)
	}
}

func TestAMissingConfiguredPathIsRefused(t *testing.T) {
	onlyPath(t, t.TempDir())

	_, err := Resolve("/nowhere/in/particular/podman")
	if err == nil {
		t.Fatal("Resolve() accepted a path that does not exist")
	}
}

// Every output this application parses needs a template on both engines. An
// empty one would send `--format ""` and produce blank rows, which is exactly
// the silent failure the per-engine table exists to make fixable.
func TestEveryParsedOutputHasATemplatePerEngine(t *testing.T) {
	for _, name := range Names() {
		shape := ShapeFor(name)
		v := reflect.ValueOf(shape.Templates)
		typ := v.Type()
		for i := range v.NumField() {
			if v.Field(i).String() == "" {
				t.Errorf("%s: Templates.%s is empty", name, typ.Field(i).Name)
			}
		}
	}
}

// Measured against a real podman (§3.67, "La première mesure"): `podman
// info` nests CPU count and total memory under `.Host` and has no top-level
// NCPU/MemTotal at all, so docker's Info template doesn't just misparse
// against it — it fails the template outright.
func TestPodmanInfoTemplateUsesTheHostNestedFields(t *testing.T) {
	got := ShapeFor(Podman).Templates.Info
	if want := "{{.Host.CPUs}}\t{{.Host.MemTotal}}"; got != want {
		t.Errorf("podman Info template = %q, want %q", got, want)
	}
	if got == ShapeFor(Docker).Templates.Info {
		t.Error("podman's Info template must not equal docker's — that was exactly " +
			"the bug: a genuinely running podman reported as unreachable")
	}
}

func TestEveryEngineDeclaresItsOwnAuthFile(t *testing.T) {
	for _, name := range Names() {
		shape := ShapeFor(name)
		if len(shape.AuthPaths()) == 0 {
			t.Errorf("%s: declares no auth path", name)
		}
	}
}

// Only docker prints the fixed-width table internal/docker scrapes for
// per-image unique size. Podman leaves those columns empty rather than filled
// from offsets taken on a header that may not be the same one.
func TestOnlyDockerParsesTheVerboseDiskReport(t *testing.T) {
	if !ShapeFor(Docker).ParsesSystemDFVerbose() {
		t.Error("docker: ParsesSystemDFVerbose() = false, want true")
	}
	if ShapeFor(Podman).ParsesSystemDFVerbose() {
		t.Error("podman: ParsesSystemDFVerbose() = true, want false")
	}
}

// AuthPath names the file a login would write when none exists yet — the first
// declared one — and the one that is actually there when one is.
//
// Podman is the engine with two, so it is the one this can be stated on.
func TestAuthPathPrefersTheFileThatExists(t *testing.T) {
	home := t.TempDir()
	runtimeDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	shape := ShapeFor(Podman)
	paths := shape.AuthPaths()
	if len(paths) != 2 {
		t.Fatalf("podman declares %d auth paths (%v), want the runtime one and the config one", len(paths), paths)
	}

	// Nothing on disk: the answer is where a login would write.
	if got := shape.AuthPath(); got != paths[0] {
		t.Errorf("AuthPath() = %q with nothing on disk, want the first declared path %q", got, paths[0])
	}

	// The second exists and the first does not: the existing one wins, which is
	// what makes a podman that never had $XDG_RUNTIME_DIR set still readable.
	if err := os.MkdirAll(filepath.Dir(paths[1]), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[1], []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := shape.AuthPath(); got != paths[1] {
		t.Errorf("AuthPath() = %q, want the file that exists (%q)", got, paths[1])
	}
}

// The paths are resolved when they are asked for, not when the shape is built.
// A shape is built at package initialisation, so a $HOME read there would be
// frozen before anything else in the process runs.
func TestTheAuthPathFollowsTheEnvironment(t *testing.T) {
	shape := ShapeFor(Docker)

	first := t.TempDir()
	t.Setenv("HOME", first)
	if got := shape.AuthPath(); !strings.HasPrefix(got, first) {
		t.Fatalf("AuthPath() = %q, want it under %q", got, first)
	}

	second := t.TempDir()
	t.Setenv("HOME", second)
	if got := shape.AuthPath(); !strings.HasPrefix(got, second) {
		t.Errorf("AuthPath() = %q after HOME moved, want it under %q — the path was cached", got, second)
	}
}

// The default is docker so that every caller has a usable answer before the
// router has read a configuration — which is what the application did before
// this package existed.
func TestCurrentStartsOnDocker(t *testing.T) {
	if got := Current().Name; got != Docker {
		t.Errorf("Current().Name = %q before any resolution, want %q", got, Docker)
	}
}

func TestSetCurrentIsWhatCurrentReports(t *testing.T) {
	before := Current()
	t.Cleanup(func() { SetCurrent(before) })

	SetCurrent(ShapeFor(Podman))
	if got := Current().Name; got != Podman {
		t.Errorf("Current().Name = %q after SetCurrent(podman), want %q", got, Podman)
	}
}

// OC-24 (DEVDESK-822), step 2. When the configured engine is absent, Resolve fails and
// the router keeps the previous shape, so the views would say "Docker not found" to
// someone who pinned podman. Unresolved builds the shape of the engine the preference
// NAMES, without looking anything up: the router installs it when Resolve fails.
func TestUnresolvedNamesTheConfiguredEngine(t *testing.T) {
	tests := []struct {
		preference string
		wantName   string
		wantBinary string
	}{
		{"podman", Podman, "podman"},
		{"docker", Docker, "docker"},
		{"", Docker, "docker"},
		{Auto, Docker, "docker"},
		// A path is read like Resolve reads it (engineOfPath) and run as given.
		{"/opt/x/podman-remote", Podman, "/opt/x/podman-remote"},
		{"/opt/x/nerdctl", Docker, "/opt/x/nerdctl"},
	}
	for _, tt := range tests {
		t.Run("preference "+tt.preference, func(t *testing.T) {
			// Nothing is installed and nothing may be looked up: PATH holds an empty directory.
			onlyPath(t, t.TempDir())
			before := Current()

			got := Unresolved(tt.preference)

			if got.Name != tt.wantName {
				t.Errorf("Unresolved(%q).Name = %q, want %q", tt.preference, got.Name, tt.wantName)
			}
			if got.Binary != tt.wantBinary {
				t.Errorf("Unresolved(%q).Binary = %q, want %q", tt.preference, got.Binary, tt.wantBinary)
			}
			if want := ShapeFor(tt.wantName).Templates; got.Templates != want {
				t.Errorf("Unresolved(%q).Templates are not the %s templates", tt.preference, tt.wantName)
			}
			if Current() != before {
				t.Errorf("Unresolved(%q) changed the current engine: it builds a shape, the caller installs it", tt.preference)
			}
		})
	}
}
