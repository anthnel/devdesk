package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// The whole point of §3.67: nothing in this package names an engine any more.
// These tests pin the three places where that could regress — the message when
// the binary is missing, the label a failure is reported under, and the
// templates the parsed calls ask for.

func TestTheMissingBinaryIsNamedAfterTheConfiguredEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		// No argv is expected: nothing may run once the binary is absent.
		s := stubStrict(t)
		s.missing = true

		err := requireEngine()

		if err == nil {
			t.Fatal("requireEngine() returned nil when the engine is absent")
		}
		// Telling someone who pinned podman that docker is missing sends them
		// looking for the wrong thing. The message reaches the footer (Rule 128).
		if want := shape.Name + " not found"; !strings.HasPrefix(err.Error(), want) {
			t.Errorf("requireEngine() = %q, want it to start with %q", err, want)
		}
	})
}

func TestAFailureIsReportedUnderTheConfiguredEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		stubStrict(t, reply{
			argv: []string{"stop", "nope"},
			out:  "Error: no such container\n",
			err:  errExit,
		})

		err := StopContainer("nope")

		if err == nil {
			t.Fatal("StopContainer() returned nil on a failing invocation")
		}
		if want := shape.Name + " stop failed: Error: no such container"; err.Error() != want {
			t.Errorf("StopContainer() = %q, want %q", err, want)
		}
	})
}

// The templates come from the shape rather than from a literal at the call
// site. They are the same on both engines for most outputs, but not for three
// (Info, NetworkLS, SystemDF), so the expected argv is built from the shape of
// the engine under test and the strict stub rejects anything else: a call that
// still carried its own literal would keep docker's template under podman, and
// the test now fails naming that argv instead of passing on the subcommand.
func TestTheParsedCallsAskForTheEnginesOwnTemplates(t *testing.T) {
	tests := []struct {
		name string
		call func()
		// want is every argv the call makes, in the shape of the engine under test.
		want func(engine.Templates) []reply
	}{
		{"ps", func() { _, _ = ListContainers(false) }, func(tp engine.Templates) []reply {
			return []reply{{argv: []string{"ps", "--format", tp.PS}}}
		}},
		{"stats", func() { _, _ = GetContainerMetrics() }, func(tp engine.Templates) []reply {
			return []reply{{argv: []string{"stats", "--no-stream", "--format", tp.Stats}}}
		}},
		{"info", func() { _, _ = FetchCapacity() }, func(tp engine.Templates) []reply {
			return []reply{{argv: []string{"info", "--format", tp.Info}}}
		}},
		{"network ls", func() { _, _ = ListNetworks() }, func(tp engine.Templates) []reply {
			return []reply{{argv: []string{"network", "ls", "--format", tp.NetworkLS}}}
		}},
		{"volume ls", func() { _, _ = ListVolumes() }, func(tp engine.Templates) []reply {
			return []reply{{argv: []string{"volume", "ls", "--format", tp.VolumeLS}}}
		}},
		{"system df", func() { FetchOCIStats() }, func(tp engine.Templates) []reply {
			// FetchOCIStats follows its `system df` with a `network ls` for the
			// count that report has no row for.
			return []reply{
				{argv: []string{"system", "df", "--format", tp.SystemDF}},
				{argv: []string{"network", "ls", "--format", tp.NetworkLS}},
			}
		}},
	}

	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				s := stubStrict(t, tc.want(shape.Templates)...)

				tc.call()

				// stubStrict also fails the test, at cleanup, for any reply above
				// the call never asked for, so the whole argv list is exercised.
				if len(s.calls) == 0 {
					t.Errorf("%s made no call under %s", tc.name, shape.Name)
				}
			})
		}
	})
}

// `system df -v` is the one parsed output with no template: it is a fixed-width
// table read at offsets taken from its header. Under podman the call is not made
// at all, so UniqueSize and Containers stay zero — an absence the table renders
// as a dim "-", rather than numbers sliced out of columns that may not line up.
//
// The strict stub carries the assertion: under podman `system df -v` is not
// declared, so making the call fails the test with its argv.
func TestTheVerboseDiskReportIsNotScrapedUnderPodman(t *testing.T) {
	const imageLS = "sha256:abc\tnginx\tlatest\t140MB\t2026-09-01 10:00:00 +0000 UTC\n"

	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		replies := []reply{
			{argv: []string{"image", "ls", "--format", shape.Templates.ImageLS}, out: imageLS},
			{argv: []string{"image", "inspect", "--format", shape.Templates.ImageInspect, "sha256:abc"}},
		}
		if shape.ParsesSystemDFVerbose() {
			replies = append(replies, reply{argv: []string{"system", "df", "-v"}})
		}
		s := stubStrict(t, replies...)

		if _, err := ListImages(); err != nil {
			t.Fatalf("ListImages() = %v", err)
		}
		if got := ranSubcommand(s, "system"); got != shape.ParsesSystemDFVerbose() {
			t.Errorf("%s: `system df -v` ran = %v, want %v", shape.Name, got, shape.ParsesSystemDFVerbose())
		}
	})
}

func allArgs(s *stubRunner) [][]string {
	var out [][]string
	for _, c := range s.calls {
		out = append(out, c.Args)
	}
	return out
}

func ranSubcommand(s *stubRunner, subcommand string) bool {
	for _, c := range s.calls {
		if len(c.Args) > 0 && c.Args[0] == subcommand {
			return true
		}
	}
	return false
}

// Both engines run docker-credential-<name>: podman reuses docker's helpers
// rather than naming its own (measured, see credentialHelperPrefix). The real
// runner is used, against a helper script on PATH — a podman-credential-probe
// beside it must never be the one called.
func TestTheCredentialHelperIsDockersUnderEveryEngine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the helper stand-ins are shell scripts")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("docker-credential-probe", `echo '{"Username":"alice","Secret":"s3cret"}'`)
	write("podman-credential-probe", `exit 1`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			stub(t, &stubRunner{}) // restores the runner and the engine
			runner = cliRunner{}
			useEngine(t, name)

			if _, secret, ok := getCredsFromHelper("probe", "registry.example.com"); !ok || secret != "s3cret" {
				t.Errorf("getCredsFromHelper() = %q, %v — want docker-credential-probe's answer", secret, ok)
			}
		})
	}
}
