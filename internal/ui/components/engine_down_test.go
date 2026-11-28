package components

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
)

// OC-24 (DEVDESK-822), criteria 1 to 4: EngineDown is the derived state a view holds
// while its engine could not be asked at all. It is not a footer message: it has no
// timer, so a ClearFooterMsg cannot remove it and it stays until the view says the
// engine answered again (Clear), which is what a "Docker unreachable" line needs to
// be true for as long as it is true (Status, DEVDESK-377 and D47).

// unavailable builds the typed error a read returns, with no engine text in it.
func unavailable(reason docker.Unavailability, engineName string) *docker.UnavailableError {
	return &docker.UnavailableError{Reason: reason, Engine: engineName, Label: engineName + " ps"}
}

// The Docker sentences are the ones of the acceptance criteria; the Podman ones are
// the proposals of docker.UnavailableError.UserMessage, still to be validated by the
// techwriter (plan, Q2).
var engineDownSentences = []struct {
	name   string
	reason docker.Unavailability
	engine string
	want   string
}{
	{"docker not found", docker.EngineNotFound, engine.Docker, "Docker not found on PATH — install it or set app.container_engine"},
	{"docker unreachable", docker.EngineUnreachable, engine.Docker, "Cannot reach the Docker daemon — is it running?"},
	{"docker permission", docker.EnginePermissionDenied, engine.Docker, "Permission denied on the Docker socket — add your user to the docker group"},
	{"podman not found", docker.EngineNotFound, engine.Podman, "Podman not found on PATH — install it or set app.container_engine"},
	{"podman unreachable", docker.EngineUnreachable, engine.Podman, "Cannot reach Podman — is the Podman machine or service running?"},
	{"podman permission", docker.EnginePermissionDenied, engine.Podman, "Permission denied on the Podman socket — check that your user may open it"},
}

func TestEngineDownObservesAnUnavailableEngine(t *testing.T) {
	for _, c := range engineDownSentences {
		t.Run(c.name, func(t *testing.T) {
			var down EngineDown
			if down.Active() {
				t.Fatal("the zero value must be an engine that is fine")
			}

			err := fmt.Errorf("listing containers: %w", unavailable(c.reason, c.engine))
			if !down.Observe(err) {
				t.Fatalf("Observe(%v) = false, want true: it holds the fixed sentence of an unavailable engine", err)
			}
			if !down.Active() {
				t.Error("Active() = false after Observe reported true")
			}
			if down.Text() != c.want {
				t.Errorf("Text() = %q, want %q", down.Text(), c.want)
			}
			status := down.Status()
			if status.Text != c.want {
				t.Errorf("Status().Text = %q, want %q", status.Text, c.want)
			}
			if status.Level != LevelError {
				t.Errorf("Status().Level = %d, want LevelError (%d): the engine being down is an error that is true right now", status.Level, LevelError)
			}
			if status.Activity {
				t.Error("Status().Activity is set: a down engine is not work in progress, the dot would blink for nothing")
			}
		})
	}
}

// A second answer replaces the first: the daemon that was unreachable and now
// refuses the socket is described by the latest sentence.
func TestEngineDownFollowsTheLatestObservation(t *testing.T) {
	var down EngineDown
	down.Observe(unavailable(docker.EngineUnreachable, engine.Docker))
	down.Observe(unavailable(docker.EnginePermissionDenied, engine.Docker))

	want := "Permission denied on the Docker socket — add your user to the docker group"
	if down.Text() != want {
		t.Errorf("Text() = %q, want the latest sentence %q", down.Text(), want)
	}
}

// Only a typed unavailability is observed. Everything else keeps the generic
// "... — check logs" of the view, and an unclassified failure never invents a cause.
func TestEngineDownIgnoresAnyOtherError(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"plain error", errors.New("docker ps failed: exit status 1")},
		{"exit error with a stderr nothing classifies", &exec.ExitError{Stderr: []byte("Error: canary exploded\n")}},
		{"exit error whose stderr reads as unreachable but was never classified", &exec.ExitError{Stderr: []byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n")}},
		{"wrapped plain error", fmt.Errorf("list: %w", errors.New("boom"))},
		{"an unavailability with no class", &docker.UnavailableError{Reason: docker.UnavailabilityUnknown, Engine: engine.Docker}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var down EngineDown
			if down.Observe(c.err) {
				t.Errorf("Observe(%v) = true, want false", c.err)
			}
			if down.Active() || down.Text() != "" {
				t.Errorf("state after Observe = (active %v, %q), want an engine that is fine", down.Active(), down.Text())
			}
			if st := down.Status(); st.Text != "" || st.Level != LevelInfo {
				t.Errorf("Status() = %+v, want the zero status", st)
			}
		})
	}
}

// An error that is not observed leaves what was held as it was: the state ends on a
// successful list (Clear), never because an unrelated failure went by.
func TestEngineDownKeepsItsStateWhenAnotherErrorIsIgnored(t *testing.T) {
	var down EngineDown
	if !down.Observe(unavailable(docker.EngineUnreachable, engine.Docker)) {
		t.Fatal("setup: Observe did not hold the state")
	}
	before := down.Text()

	if down.Observe(errors.New("boom")) {
		t.Fatal("Observe reported true for a plain error")
	}
	if down.Text() != before || !down.Active() {
		t.Errorf("state = (active %v, %q) after an ignored error, want it unchanged (%q)", down.Active(), down.Text(), before)
	}
}

func TestEngineDownClear(t *testing.T) {
	var down EngineDown
	down.Clear() // clearing the zero value is allowed and does nothing
	if down.Active() {
		t.Fatal("Clear on the zero value made the engine down")
	}

	down.Observe(unavailable(docker.EngineNotFound, engine.Docker))
	if !down.Active() {
		t.Fatal("setup: Observe did not hold the state")
	}

	down.Clear()

	if down.Active() || down.Text() != "" {
		t.Errorf("state after Clear = (active %v, %q), want an engine that is fine", down.Active(), down.Text())
	}
	if st := down.Status(); st.Text != "" || st.Level != LevelInfo {
		t.Errorf("Status() after Clear = %+v, want the zero status", st)
	}
}

// The state is derived, not a message: the expiry of a footer message cannot touch
// it, and the footer renders it in the error style, centred, like any other error.
func TestEngineDownIsNotAFooterMessage(t *testing.T) {
	var down EngineDown
	if !down.Observe(unavailable(docker.EngineUnreachable, engine.Docker)) {
		t.Fatal("setup: Observe did not hold the state")
	}
	sentence := down.Text()

	var footer FooterMessage
	cmd := footer.Info("Copied")
	if cmd == nil {
		t.Fatal("setup: a message returned no timer")
	}
	footer.Handle(ClearFooterMsg{ID: footer.ID()})

	if got := down.Text(); got != sentence {
		t.Errorf("Text() = %q after a footer expiry, want %q: the state has no timer", got, sentence)
	}

	const width = 100
	got := footer.View(width, down.Status())
	want := footer.View(width, Status{Text: sentence, Level: LevelError})
	if got != want {
		t.Errorf("the footer renders the state as\n%q\nwant\n%q", got, want)
	}
	if !strings.Contains(ansi.Strip(got), sentence) {
		t.Errorf("the footer line %q does not hold the sentence %q", ansi.Strip(got), sentence)
	}
}

// Rule 128's precedence is untouched: a message the user has not read yet wins over
// the derived state, and the state comes back when the message expires.
func TestAFooterMessageStillWinsOverTheEngineState(t *testing.T) {
	var down EngineDown
	if !down.Observe(unavailable(docker.EngineUnreachable, engine.Docker)) {
		t.Fatal("setup: Observe did not hold the state")
	}

	var footer FooterMessage
	footer.Warn("Scan already in progress")

	shown := ansi.Strip(footer.View(100, down.Status()))
	if !strings.Contains(shown, "Scan already in progress") || strings.Contains(shown, "Cannot reach") {
		t.Errorf("footer = %q, want the timed message first", shown)
	}

	footer.Handle(ClearFooterMsg{ID: footer.ID()})
	if shown = ansi.Strip(footer.View(100, down.Status())); !strings.Contains(shown, "Cannot reach the Docker daemon") {
		t.Errorf("footer after the expiry = %q, want the engine state back", shown)
	}
}
