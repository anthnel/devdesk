package docker

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-24 (DEVDESK-822), security review of PR 1: nothing the engine said leaves
// through Error() (an error reaches footers, MCP clients and logs alike); the cause is
// read through LogText(), redacted. Error() stays "<label> failed: ..." for the
// footers and the existing log lines.

// A line of the engine's text, classified or not, never shows in Error().
func TestErrorHoldsNoEngineText(t *testing.T) {
	usingEngine(t, engine.Docker)
	label := cmdLabel("ps")

	for name, stderr := range map[string]string{
		"classified, unreachable": "Cannot connect to the Docker daemon at tcp://u:s3cret@canary.example:2375. Is the docker daemon running?\n",
		"classified, permission":  "permission denied while trying to connect to the docker API at unix:///run/canary.sock\n",
		"unknown":                 "Error: canary exploded at https://canary.example/x\nsecond line\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := readFailure(label, exitWith(stderr))

			text := err.Error()
			for _, fragment := range []string{"canary", "s3cret", "exploded", "://", "second line"} {
				if strings.Contains(text, fragment) {
					t.Errorf("Error() = %q holds %q", text, fragment)
				}
			}
			if !strings.HasPrefix(text, label+" failed") {
				t.Errorf("Error() = %q, want the label first", text)
			}
			if !strings.Contains(LogText(err), "canary") {
				t.Errorf("LogText() = %q, want the engine's line kept for the log", LogText(err))
			}
			if strings.Contains(LogText(err), "s3cret") {
				t.Errorf("LogText() = %q holds the password", LogText(err))
			}
		})
	}
}

// An error that carries no engine line logs its own text.
func TestLogTextOfAnyOtherErrorIsItsText(t *testing.T) {
	if got := LogText(nil); got != "" {
		t.Errorf("LogText(nil) = %q", got)
	}
	if got := LogText(errExit); got != errExit.Error() {
		t.Errorf("LogText(errExit) = %q, want %q", got, errExit.Error())
	}
	wrapped := fmt.Errorf("refresh: %w", readFailure("docker ps", exitWith("Error: odd\n")))
	if got := LogText(wrapped); !strings.Contains(got, "odd") {
		t.Errorf("LogText through a wrapper = %q, want the engine's line", got)
	}
}

// What a log line may not carry, cut from the line the engine wrote: the credentials
// of an URL (a password with "/" or "@" included), its query and fragment, the
// control characters (C1 as well, U+009B is a CSI) and the bidi marks.
func TestRedactCause(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string   // must appear
		missing []string // must not
	}{
		{"password with a slash", "Cannot connect at tcp://admin:pa/ss@10.0.0.1:2375. Is it?", "tcp://***@10.0.0.1:2375.", []string{"admin", "pa/ss", "ss@"}},
		{"password with an at sign", "at ssh://deploy:p@ss@build.example now", "ssh://***@build.example", []string{"deploy", "p@ss", "ss@build"}},
		{"query token", "see https://registry.example/v2/?token=SECRETVALUE&x=1 done", "https://registry.example/v2/ done", []string{"token", "SECRETVALUE"}},
		{"fragment", "at https://h.example/p#frag-secret", "https://h.example/p", []string{"frag-secret"}},
		{"query without userinfo, unix socket", "dial unix:///run/podman/podman.sock?x=y: no", "unix:///run/podman/podman.sock", []string{"x=y"}},
		{"in a variable", "(DOCKER_HOST=tcp://u:s3cret@h:2375, see)", "DOCKER_HOST=tcp://***@h:2375", []string{"s3cret", "u:"}},
		{"C1 control, CSI", "Cannot connect\u009b31m to the daemon", "Cannot connect31m to the daemon", []string{"\u009b"}},
		{"C0 and DEL", "a\x1b[31mb\x7fc\x00d", "a[31mbcd", []string{"\x1b", "\x7f", "\x00"}},
		{"bidi marks", "ab\u202ecd\u202a\u2066ef\u2069gh", "abcdefgh", []string{"\u202e", "\u202a", "\u2066", "\u2069"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactCause(tt.in)

			if !strings.Contains(got, tt.want) {
				t.Errorf("redactCause(%q) = %q, want it to contain %q", tt.in, got, tt.want)
			}
			for _, leak := range tt.missing {
				if strings.Contains(got, leak) {
					t.Errorf("redactCause(%q) = %q, holds %q", tt.in, got, leak)
				}
			}
		})
	}
}

// The cut at 200 bytes comes after the redaction: no half of a password is left.
func TestRedactCauseCutsAfterRedacting(t *testing.T) {
	line := strings.Repeat("y", 190) + " tcp://u:s3cret@host:2375"

	got := redactCause(line)

	if strings.Contains(got, "s3cret") || strings.Contains(got, "s3c") {
		t.Errorf("redactCause = %q, holds a piece of the password", got)
	}
	if len(got) > maxCauseBytes {
		t.Errorf("len = %d, want at most %d", len(got), maxCauseBytes)
	}
}

// A configured path is not looked up on PATH: the sentence says where to look. A bare
// name keeps the sentence criterion 1 imposes.
func TestANotFoundEngineConfiguredByPathSaysSo(t *testing.T) {
	missing := &exec.Error{Name: "/opt/x/podman-remote", Err: exec.ErrNotFound}
	tests := []struct {
		name       string
		preference string
		want       string
	}{
		{"a podman path", "/opt/x/podman-remote", "Podman not found — check app.container_engine"},
		{"a docker path", "/opt/x/nerdctl", "Docker not found — check app.container_engine"},
		{"a bare podman", engine.Podman, sentencePodmanNotFound},
		{"a bare docker", engine.Docker, sentenceDockerNotFound},
		{"auto", engine.Auto, sentenceDockerNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := engine.Current()
			t.Cleanup(func() { engine.SetCurrent(previous) })
			engine.SetCurrent(engine.Unresolved(tt.preference))

			err := readFailure(cmdLabel("ps"), missing)

			if got, ok := UnavailableMessage(err); !ok || got != tt.want {
				t.Errorf("UnavailableMessage = %q, %v, want %q", got, ok, tt.want)
			}
		})
	}
}

// The same through the production check, with the stub that makes LookPath fail.
func TestEngineAvailableNamesAConfiguredPath(t *testing.T) {
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.Unresolved("/opt/x/podman-remote"))
	stub(t, &stubRunner{missing: true})

	err := engineAvailable()

	if got, _ := UnavailableMessage(err); got != "Podman not found — check app.container_engine" {
		t.Errorf("sentence = %q", got)
	}
}

// An unavailable engine is never "already gone", whatever the text: the probed answers,
// in the form a mutation gives them (the engine's combined output in an *engineError),
// are refused with an unknown reason.
func TestClassifyRemovalNeverReadsAProbedUnavailableAnswerAsGone(t *testing.T) {
	for _, p := range probedAnswers {
		if p.want == UnavailabilityUnknown {
			continue
		}
		t.Run(p.String(), func(t *testing.T) {
			err := &engineError{Label: "docker rm", Output: strings.TrimSpace(p.stderr), Err: errExit}

			status, why := ClassifyRemoval(err)

			if status != RemovalRefused || why != ReasonOther {
				t.Errorf("ClassifyRemoval = (%q, %q), want (%q, %q)", status, why, RemovalRefused, ReasonOther)
			}
		})
	}
	// The engine text alone, as a bare error that still reads as the engine's: not "gone".
	if status, _ := ClassifyRemoval(&engineError{Label: "docker rm", Output: dkAText + "Error response from daemon: No such container: abc", Err: errors.New("x")}); status == RemovalGone {
		t.Errorf("an engine that answered DK-A was read as gone")
	}
}
