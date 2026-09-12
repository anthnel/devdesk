package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// The whole point of §3.67: nothing in this package names an engine any more.
// These tests pin the three places where that could regress — the message when
// the binary is missing, the label a failure is reported under, and the
// templates the parsed calls ask for.

func TestTheMissingBinaryIsNamedAfterTheConfiguredEngine(t *testing.T) {
	stub(t, &stubRunner{missing: true})
	useEngine(t, engine.Podman)

	err := requireEngine()

	if err == nil {
		t.Fatal("requireEngine() returned nil when the engine is absent")
	}
	// Telling someone who pinned podman that docker is missing sends them
	// looking for the wrong thing. The message reaches the footer (Rule 128).
	if !strings.HasPrefix(err.Error(), "podman not found") {
		t.Errorf("requireEngine() = %q, want it to name podman", err)
	}
}

func TestAFailureIsReportedUnderTheConfiguredEngine(t *testing.T) {
	stub(t, &stubRunner{
		output: map[string][]byte{"stop": []byte("Error: no such container\n")},
		err:    map[string]error{"stop": errExit},
	})
	useEngine(t, engine.Podman)

	err := StopContainer("nope")

	if err == nil {
		t.Fatal("StopContainer() returned nil on a failing invocation")
	}
	if want := "podman stop failed: Error: no such container"; err.Error() != want {
		t.Errorf("StopContainer() = %q, want %q", err, want)
	}
}

// The templates come from the shape rather than from a literal at the call
// site. They are the same on both engines today — no measurement has been taken
// (§3.67) — so this asserts the wiring, not the contents: a call that still
// carried its own literal would keep docker's template under podman and nothing
// on screen would say so.
func TestTheParsedCallsAskForTheEnginesOwnTemplates(t *testing.T) {
	tests := []struct {
		name       string
		subcommand string
		call       func()
		want       func(engine.Templates) string
	}{
		{"ps", "ps", func() { _, _ = ListContainers(false) }, func(t engine.Templates) string { return t.PS }},
		{"stats", "stats", func() { _, _ = GetContainerMetrics() }, func(t engine.Templates) string { return t.Stats }},
		{"info", "info", func() { _, _ = FetchCapacity() }, func(t engine.Templates) string { return t.Info }},
		{"network ls", "network", func() { _, _ = ListNetworks() }, func(t engine.Templates) string { return t.NetworkLS }},
		{"volume ls", "volume", func() { _, _ = ListVolumes() }, func(t engine.Templates) string { return t.VolumeLS }},
		{"system df", "system", func() { FetchOCIStats() }, func(t engine.Templates) string { return t.SystemDF }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stub(t, &stubRunner{output: map[string][]byte{tc.subcommand: []byte("")}})
			useEngine(t, engine.Podman)

			tc.call()

			want := tc.want(engine.ShapeFor(engine.Podman).Templates)
			// Every call the function made, not just the last: FetchOCIStats
			// follows its `system df` with a `network ls` for the count that
			// report has no row for.
			if !anyCallCarries(s, want) {
				t.Errorf("%s made %v, want one of them to carry the podman template %q",
					tc.name, allArgs(s), want)
			}
		})
	}
}

func anyCallCarries(s *stubRunner, want string) bool {
	for _, c := range s.calls {
		for _, a := range c.Args {
			if a == want {
				return true
			}
		}
	}
	return false
}

func allArgs(s *stubRunner) [][]string {
	var out [][]string
	for _, c := range s.calls {
		out = append(out, c.Args)
	}
	return out
}

// `system df -v` is the one parsed output with no template: it is a fixed-width
// table read at offsets taken from its header. Under podman the call is not made
// at all, so UniqueSize and Containers stay zero — an absence the table renders
// as a dim "-", rather than numbers sliced out of columns that may not line up.
func TestTheVerboseDiskReportIsNotScrapedUnderPodman(t *testing.T) {
	const imageLS = "sha256:abc\tnginx\tlatest\t140MB\t2026-09-01 10:00:00 +0000 UTC\n"

	t.Run("docker scrapes it", func(t *testing.T) {
		s := stub(t, &stubRunner{output: map[string][]byte{
			"image":  []byte(imageLS),
			"system": []byte(""),
		}})
		useEngine(t, engine.Docker)

		if _, err := ListImages(); err != nil {
			t.Fatalf("ListImages() = %v", err)
		}
		if !ranSubcommand(s, "system") {
			t.Error("docker: `system df -v` was not run")
		}
	})

	t.Run("podman does not", func(t *testing.T) {
		s := stub(t, &stubRunner{output: map[string][]byte{
			"image":  []byte(imageLS),
			"system": []byte(""),
		}})
		useEngine(t, engine.Podman)

		if _, err := ListImages(); err != nil {
			t.Fatalf("ListImages() = %v", err)
		}
		if ranSubcommand(s, "system") {
			t.Error("podman: `system df -v` was run; its table is not known to match docker's")
		}
	})
}

func ranSubcommand(s *stubRunner, subcommand string) bool {
	for _, c := range s.calls {
		if len(c.Args) > 0 && c.Args[0] == subcommand {
			return true
		}
	}
	return false
}

// The credential helper is invoked as `<engine>-credential-<name>`, and the
// prefix is the engine's own. A podman setup naming "pass" as its helper runs
// podman-credential-pass, which is a different binary from docker's.
func TestTheCredentialHelperCarriesTheEnginesPrefix(t *testing.T) {
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			stub(t, &stubRunner{})
			useEngine(t, name)

			if got := engine.Current().HelperPrefix; !strings.HasPrefix(got, name) {
				t.Errorf("HelperPrefix = %q, want it to start with %q", got, name)
			}
		})
	}
}
