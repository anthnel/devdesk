package enginefixture_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b (DEVDESK-942), step 7 of the OC-08 plan: the supported engine versions,
// N and N-1 per tool, read from the one place that owns them.
//
// The API these tests pin down:
//
//	type Supported map[Tool][]string                  // per tool: [N, N-1], newest first
//	func ParseVersions(data []byte) (Supported, error)
//
// ParseVersions reads DOCKER_N_VERSION, DOCKER_N1_VERSION, PODMAN_N_VERSION and
// PODMAN_N1_VERSION from the text of tools/engine-vms/versions.env (bash syntax,
// plain NAME=value, "#" comments, also at the end of a line). The other keys of
// the file (DOCKER_N, PODMAN_N_SHA256, KIND_*, KUBECTL_*, ...) are ignored; kind
// and kubectl join the list with OC-59 and OC-68. A missing key, an empty value
// or a value that is not a dotted version number is an error that names the key.

// versionsEnvPath is the one source of the supported versions (OC-85, OC-01).
var versionsEnvPath = filepath.Join("..", "..", "tools", "engine-vms", "versions.env")

func realVersions(t testing.TB) enginefixture.Supported {
	t.Helper()
	data, err := os.ReadFile(versionsEnvPath)
	if err != nil {
		t.Fatalf("reading the supported versions: %v", err)
	}
	s, err := enginefixture.ParseVersions(data)
	if err != nil {
		t.Fatalf("ParseVersions(%s): %v", versionsEnvPath, err)
	}
	return s
}

const goodVersions = `# a comment
DOCKER_N=5:29.10.1-1~ubuntu.24.04~noble   # released 2026-11-24
DOCKER_N1=5:28.5.2-1~ubuntu.24.04~noble   # latest 28.x
DOCKER_N_VERSION=29.10.1
DOCKER_N1_VERSION=28.5.2

PODMAN_N_VERSION=6.2.1                    # released 2026-11-24
PODMAN_N_SHA256=e95435d3ad37be41ec00a6b10ea5063563c9f8b485da47fbb1cbc21eb102464f
PODMAN_N1_VERSION=5.8.11                  # latest 5.x
PODMAN_N1_SHA256=52803062a9669ac11c2ab4666bc9ec84c53b940e0228c8a651ecf9926ec16486
KIND_N_VERSION=0.33.0
KIND_N1_VERSION=0.32.0
KUBECTL_VERSION=1.37.3
`

// without drops the lines that start with prefix.
func without(text, prefix string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, prefix) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func TestParseVersionsReadsNAndN1PerTool(t *testing.T) {
	got, err := enginefixture.ParseVersions([]byte(goodVersions))
	if err != nil {
		t.Fatalf("ParseVersions: %v", err)
	}
	if want := []string{"29.10.1", "28.5.2"}; !slices.Equal(got[enginefixture.Docker], want) {
		t.Errorf("Docker = %v, want %v (N first, then N-1)", got[enginefixture.Docker], want)
	}
	if want := []string{"6.2.1", "5.8.11"}; !slices.Equal(got[enginefixture.Podman], want) {
		t.Errorf("Podman = %v, want %v", got[enginefixture.Podman], want)
	}
	if len(got) != 2 {
		t.Errorf("%d tools, want docker and podman only (kind and kubectl are OC-59 and OC-68): %v", len(got), got)
	}
}

func TestParseVersionsIgnoresWhatIsNotAVersionKey(t *testing.T) {
	text := goodVersions +
		"# DOCKER_N_VERSION=1.0.0\n" + // a commented line
		"  # DOCKER_N1_VERSION=1.0.1\n" +
		"NOT_DOCKER_N_VERSION=1.0.2\n" + // another key that ends the same way
		"DOCKER_N_VERSION_NOTE=1.0.3\n"
	got, err := enginefixture.ParseVersions([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"29.10.1", "28.5.2"}; !slices.Equal(got[enginefixture.Docker], want) {
		t.Errorf("Docker = %v, want %v: the key is matched whole, at the start of a line, outside a comment", got[enginefixture.Docker], want)
	}
}

func TestParseVersionsErrorsNameTheKey(t *testing.T) {
	keys := []string{"DOCKER_N_VERSION", "DOCKER_N1_VERSION", "PODMAN_N_VERSION", "PODMAN_N1_VERSION"}
	for _, key := range keys {
		t.Run("missing "+key, func(t *testing.T) {
			_, err := enginefixture.ParseVersions([]byte(without(goodVersions, key+"=")))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("err = %v, want one that names %s", err, key)
			}
		})
		t.Run("empty "+key, func(t *testing.T) {
			text := strings.Replace(goodVersions, "\n"+key+"=", "\n"+key+"=   # nothing\n#", 1)
			_, err := enginefixture.ParseVersions([]byte(text))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("err = %v, want one that names %s", err, key)
			}
		})
		t.Run("malformed "+key, func(t *testing.T) {
			for _, bad := range []string{"latest", "29", "v29.10.1", "29.10.1; rm -rf /", "$DOCKER_N"} {
				text := without(goodVersions, key+"=") + "\n" + key + "=" + bad + "\n"
				_, err := enginefixture.ParseVersions([]byte(text))
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Errorf("%s=%q: err = %v, want one that names the key", key, bad, err)
				}
			}
		})
	}
	for name, data := range map[string]string{"empty file": "", "comments only": "# DOCKER_N_VERSION=1.0.0\n"} {
		if _, err := enginefixture.ParseVersions([]byte(data)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// The real file: what the OC-01 measure and the VMs of OC-85 use. A bump of the
// supported versions edits this test, the stale test and the numbers that
// docs/architecture/network.md copies, in the same change.
func TestParseVersionsOnTheRealFile(t *testing.T) {
	got := realVersions(t)
	if want := []string{"29.10.1", "28.5.2"}; !slices.Equal(got[enginefixture.Docker], want) {
		t.Errorf("Docker = %v, want %v", got[enginefixture.Docker], want)
	}
	if want := []string{"6.2.1", "5.8.11"}; !slices.Equal(got[enginefixture.Podman], want) {
		t.Errorf("Podman = %v, want %v", got[enginefixture.Podman], want)
	}
}
