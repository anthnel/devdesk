package enginecontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a2 hardening (review of DEVDESK-943), findings B3 and B4 and the minor of
// the lab message: a contract refuses an environment that points the engine
// elsewhere, accepts only an image pinned by digest, and its lab refusal cites the
// path it actually read.

// clearEngineEnv unsets, for the length of the test, the variables that would send
// the CLI to another engine, so a developer's shell does not decide a test.
func clearEngineEnv(t *testing.T) {
	t.Helper()
	for _, name := range enginecontract.EngineEnvVars {
		t.Setenv(name, "")
	}
}

func TestCheckEngineEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string // substrings of the error; nil for none
	}{
		{name: "nothing set"},
		{name: "empty values count as unset", env: map[string]string{"DOCKER_HOST": "", "CONTAINER_HOST": ""}},
		{name: "DOCKER_HOST", env: map[string]string{"DOCKER_HOST": "tcp://other:2375"}, want: []string{"DOCKER_HOST", "is set"}},
		{name: "DOCKER_CONTEXT", env: map[string]string{"DOCKER_CONTEXT": "prod"}, want: []string{"DOCKER_CONTEXT"}},
		{name: "CONTAINER_HOST", env: map[string]string{"CONTAINER_HOST": "ssh://u@h/run/podman.sock"}, want: []string{"CONTAINER_HOST"}},
		{name: "CONTAINER_CONNECTION", env: map[string]string{"CONTAINER_CONNECTION": "prod"}, want: []string{"CONTAINER_CONNECTION"}},
		{name: "several", env: map[string]string{"DOCKER_HOST": "x", "CONTAINER_CONNECTION": "y"},
			want: []string{"DOCKER_HOST", "CONTAINER_CONNECTION", "are set"}},
		{name: "the limit is said", env: map[string]string{"DOCKER_HOST": "x"}, want: []string{"currentContext", "system connections"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enginecontract.CheckEngineEnv(func(k string) string { return tt.env[k] })

			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("CheckEngineEnv() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckEngineEnv() = nil, want a refusal naming %q", tt.want)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("CheckEngineEnv() = %q, want it to contain %q", err, w)
				}
			}
		})
	}
}

func TestEngineEnvVarsAreTheFourOfTheReview(t *testing.T) {
	want := []string{"DOCKER_HOST", "DOCKER_CONTEXT", "CONTAINER_HOST", "CONTAINER_CONNECTION"}
	for _, name := range want {
		found := false
		for _, have := range enginecontract.EngineEnvVars {
			found = found || have == name
		}
		if !found {
			t.Errorf("EngineEnvVars = %q, lacks %s", enginecontract.EngineEnvVars, name)
		}
	}
}

// Require refuses before the probe: the probe is the first thing that would talk to
// the engine the variable points at. Both tools, each variable.
func TestRequireRefusesAnEngineEnvironmentAndNeverProbes(t *testing.T) {
	for _, tool := range []enginefixture.Tool{enginefixture.Docker, enginefixture.Podman} {
		for _, name := range enginecontract.EngineEnvVars {
			t.Run(string(tool)+"/"+name, func(t *testing.T) {
				openGate(t, tool)
				t.Setenv(name, "somewhere-else")

				got := callRequire(t, podmanN, nil)

				if !got.c.failed || got.c.skipped || got.session != nil {
					t.Fatalf("Require failed = %v, skipped = %v, session = %v, want a refusal", got.c.failed, got.c.skipped, got.session)
				}
				if !strings.Contains(got.c.msg, name) {
					t.Errorf("fatal message = %q, want it to name %s", got.c.msg, name)
				}
				if len(got.probed) != 0 {
					t.Errorf("the probe was called %d time(s): the refusal comes before any engine call", len(got.probed))
				}
			})
		}
	}
}

// The control of the refusal: the same environment, the variables cleared, goes through.
func TestRequireGoesThroughWithACleanEngineEnvironment(t *testing.T) {
	for _, tool := range []enginefixture.Tool{enginefixture.Docker, enginefixture.Podman} {
		t.Run(string(tool), func(t *testing.T) {
			openGate(t, tool)

			got := callRequire(t, podmanN, nil)

			if got.c.failed || got.c.skipped || got.session == nil {
				t.Fatalf("Require failed = %v, skipped = %v (%q), want it to go through", got.c.failed, got.c.skipped, got.c.msg)
			}
		})
	}
}

func TestValidImage(t *testing.T) {
	sum := strings.Repeat("a1", 32)
	tests := []struct {
		ref  string
		want bool
	}{
		{"docker.io/library/alpine@sha256:" + sum, true},
		{"registry.invalid/ddc/sleeper@sha256:" + strings.Repeat("0", 64), true},
		{"alpine@sha256:" + sum, true},
		{"", false},
		{"alpine", false},
		{"alpine:3.24", false},
		{"docker.io/library/alpine:3.24@sha256:" + sum, false},
		{"alpine@sha256:" + sum[:63], false},
		{"alpine@sha256:" + sum + "0", false},
		{"alpine@sha256:" + strings.ToUpper(sum), false},
		{"Alpine@sha256:" + sum, false},
		{"alpine@sha512:" + sum, false},
		{"@sha256:" + sum, false},
		{"-v/:/h@sha256:" + sum, false},
		{"alpine@sha256:" + sum + " --privileged", false},
		{"alpine@sha256:" + sum + "\n", false},
	}
	for _, tt := range tests {
		if got := enginecontract.ValidImage(tt.ref); got != tt.want {
			t.Errorf("ValidImage(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

// versions.env's own CONTRACT_IMAGE is accepted by the Go shape, so the value
// contract.sh pins is one the contracts will take.
func TestTheImageOfVersionsEnvHasTheShapeTheContractsAccept(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "engine-vms", "versions.env"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		if value, ok := strings.CutPrefix(line, "CONTRACT_IMAGE="); ok {
			value, _, _ = strings.Cut(value, "#")
			if image := strings.TrimSpace(value); !enginecontract.ValidImage(image) {
				t.Errorf("CONTRACT_IMAGE = %q is refused by enginecontract.ValidImage", image)
			}
			return
		}
	}
	t.Error("versions.env has no CONTRACT_IMAGE")
}

// The refusal of RequireLab cites the path it read. In a test that moved the marker
// that is the temporary path, and the production path is named too, so the message
// still says what a VM has.
func TestRequireLabCitesThePathItRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "elsewhere", "current")
	enginecontract.SetLabMarkerForTest(t, path)
	t.Setenv(enginecontract.RunEnv, labRun)
	s := &enginecontract.Session{RunID: labRun, Mode: enginecontract.Verify}

	got := callRequireLab(t, s)

	if !got.failed {
		t.Fatal("RequireLab accepted a marker that does not exist")
	}
	for _, w := range []string{path, enginecontract.LabMarkerFile} {
		if !strings.Contains(got.msg, w) {
			t.Errorf("fatal message = %q, want it to contain %q", got.msg, w)
		}
	}
}

// With the production path nothing is said twice.
func TestCheckLabNamesTheProductionPathOnce(t *testing.T) {
	err := enginecontract.CheckLab(func(string) string { return labRun }, func(string) ([]byte, error) { return []byte("x\n"), nil })

	if err == nil {
		t.Fatal("CheckLab accepted a marker that holds x")
	}
	if n := strings.Count(err.Error(), enginecontract.LabMarkerFile); n != 1 {
		t.Errorf("CheckLab() = %q names %s %d times, want once", err, enginecontract.LabMarkerFile, n)
	}
}
