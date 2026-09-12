package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// fakeBinary writes an executable named name into a directory that is put at
// the head of PATH, so exec.LookPath finds it and nothing else does.
func fakeBinary(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
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
			dir := t.TempDir()
			for _, bin := range tc.installed {
				fakeBinary(t, dir, bin)
			}
			onlyPath(t, dir)

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
	dir := t.TempDir()
	fakeBinary(t, dir, "podman")
	onlyPath(t, dir)

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
			dir := t.TempDir()
			fakeBinary(t, dir, tc.installed)
			onlyPath(t, dir)

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
	dir := t.TempDir()
	fakeBinary(t, dir, "podman")
	onlyPath(t, dir)

	path := filepath.Join(dir, "podman")
	if runtime.GOOS == "windows" {
		path += ".bat"
	}

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
	dir := t.TempDir()
	fakeBinary(t, dir, "nerdctl")
	onlyPath(t, dir)

	path := filepath.Join(dir, "nerdctl")
	if runtime.GOOS == "windows" {
		path += ".bat"
	}

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

// The two template sets are identical today and that is the honest state: no
// measurement has been taken against a real podman (§3.67). This test does not
// assert they must stay identical — it fails loudly the day they stop, so the
// divergence is recorded deliberately rather than noticed by a wrong row.
func TestTheTwoTemplateSetsAreStillUnmeasured(t *testing.T) {
	if !reflect.DeepEqual(ShapeFor(Docker).Templates, ShapeFor(Podman).Templates) {
		t.Log("the podman templates now differ from docker's — update §3.67 " +
			"to record what was measured, then delete this test")
		t.Fail()
	}
}

func TestEveryEngineDeclaresItsOwnHelperPrefixAndAuthFile(t *testing.T) {
	for _, name := range Names() {
		shape := ShapeFor(name)
		if !strings.HasPrefix(shape.HelperPrefix, name) {
			t.Errorf("%s: HelperPrefix = %q, want it to start with the engine name",
				name, shape.HelperPrefix)
		}
		if len(shape.AuthPaths) == 0 {
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
func TestAuthPathPrefersTheFileThatExists(t *testing.T) {
	shape := Shape{AuthPaths: []string{"/nowhere/auth.json", "/nowhere/else/config.json"}}
	if got := shape.AuthPath(); got != "/nowhere/auth.json" {
		t.Errorf("AuthPath() = %q with nothing on disk, want the first declared path", got)
	}

	dir := t.TempDir()
	second := filepath.Join(dir, "config.json")
	if err := os.WriteFile(second, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	shape = Shape{AuthPaths: []string{filepath.Join(dir, "missing.json"), second}}
	if got := shape.AuthPath(); got != second {
		t.Errorf("AuthPath() = %q, want the file that exists (%q)", got, second)
	}

	if got := (Shape{}).AuthPath(); got != "" {
		t.Errorf("AuthPath() = %q with no declared path, want \"\"", got)
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
