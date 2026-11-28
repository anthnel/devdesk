package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-24 (DEVDESK-822), criteria 1 to 3, 5 and 6, on internal/docker: an engine that is
// absent, stopped or refusing access is a typed error (*UnavailableError), classified
// from the engine's own words, worded by a fixed sentence. The probed answers are in
// unavailable_probe_test.go. The seams (runner, current engine, PATH under fakecli) are
// global: none of these tests calls t.Parallel.

// Sentences the footer will show (stories.md, OC-24). The Podman ones are PROPOSED here
// from the plan, to be validated by the user or the techwriter (decision Q2): a Podman
// local client has no daemon, so these only read for a remote client (--remote, --url,
// CONTAINER_HOST, a stopped machine).
const (
	sentenceDockerNotFound    = "Docker not found on PATH — install it or set app.container_engine"
	sentencePodmanNotFound    = "Podman not found on PATH — install it or set app.container_engine"
	sentenceDockerUnreachable = "Cannot reach the Docker daemon — is it running?"
	sentenceDockerDenied      = "Permission denied on the Docker socket — add your user to the docker group"
	// To validate (techwriter): candidate of the plan, Q2.
	sentencePodmanUnreachable = "Cannot reach Podman — is the Podman machine or service running?"
	// To validate (techwriter): the plan leaves it to be written from the probes (P3: the
	// rootful socket asked by an ordinary user); proposed here, not invented by the code.
	sentencePodmanDenied = "Permission denied on the Podman socket — check that your user may open it"
)

// sentences is the closed set, by class and engine.
var sentences = map[Unavailability]map[string]string{
	EngineNotFound:         {engine.Docker: sentenceDockerNotFound, engine.Podman: sentencePodmanNotFound},
	EngineUnreachable:      {engine.Docker: sentenceDockerUnreachable, engine.Podman: sentencePodmanUnreachable},
	EnginePermissionDenied: {engine.Docker: sentenceDockerDenied, engine.Podman: sentencePodmanDenied},
}

const (
	dkAText   = "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n"
	podmanOff = "Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, " +
		"or try `podman machine init` and `podman machine start` to manage a new Linux VM\n"
)

func exitWith(stderr string) *exec.ExitError { return &exec.ExitError{Stderr: []byte(stderr)} }

// Only the engine's own words count, never DevDesk's label: a label that spells a
// marker does not make "exit status 1" an unreachable daemon (ClassifyRemoval's rule).
func TestClassifyReadsTheEngineTextNotTheLabel(t *testing.T) {
	spells := []string{
		"docker cannot connect to the docker daemon",
		"docker permission denied while trying to connect to the docker API",
		"podman cannot connect to podman no such file or directory",
		"docker failed to connect to the docker API",
	}
	for _, label := range spells {
		for name, err := range map[string]error{
			"mutation, label only":      &engineError{Label: label, Output: "exit status 1", Err: errExit},
			"mutation, empty output":    &engineError{Label: label, Err: errExit},
			"wrapped exit, no stderr":   fmt.Errorf("%s failed: %w", label, exitWith("")),
			"wrapped exit, other text":  fmt.Errorf("%s failed: %w", label, exitWith("Error: boom")),
			"bare error spelling label": errors.New(label + " failed: exit status 1"),
		} {
			if got := ClassifyUnavailable(err); got != UnavailabilityUnknown {
				t.Errorf("%s, label %q: ClassifyUnavailable(%q) = %d, want unknown", name, label, err, got)
			}
		}
	}
}

// A message DevDesk or Go built itself is not the engine's: only Stderr of an
// *exec.ExitError and Output of an *engineError are read, so an error whose text
// merely spells a probed answer stays unknown.
func TestClassifyDoesNotReadAnErrorTextThatIsNotTheEngines(t *testing.T) {
	for _, text := range []string{dkAText, dkC, fmt.Sprintf(pmA, "/run/podman/podman.sock", causeRefused)} {
		if got := ClassifyUnavailable(errors.New(strings.TrimSpace(text))); got != UnavailabilityUnknown {
			t.Errorf("ClassifyUnavailable(errors.New(%q)) = %d, want unknown", text, got)
		}
	}
}

// Permission is tested before unreachable: podman's permission answer also says
// "Cannot connect to Podman", and docker's says "connect".
func TestPermissionIsReadBeforeUnreachable(t *testing.T) {
	for name, text := range map[string]string{
		"podman 6.2.1, remote, permission denied":  fmt.Sprintf(pmA, "/run/podman/podman.sock", causeDenied),
		"podman 5.8.11, remote, permission denied": fmt.Sprintf(pmB, "/run/podman/podman.sock", causeDenied, "/run/podman/podman.sock"),
		"docker 29": dkC,
		"docker 28": fmt.Sprintf(dkD, "Get", "v1.51/containers/json?all=1"),
	} {
		if got := ClassifyUnavailable(exitWith(text)); got != EnginePermissionDenied {
			t.Errorf("%s: ClassifyUnavailable = %d, want EnginePermissionDenied (%d)", name, got, EnginePermissionDenied)
		}
	}
	// The mirror: the same podman line with another cause is unreachable, so
	// "permission denied" is read from the cause and not from anywhere in the text.
	if got := ClassifyUnavailable(exitWith(fmt.Sprintf(pmA, "/run/podman/podman.sock", causeRefused))); got != EngineUnreachable {
		t.Errorf("podman, connection refused: ClassifyUnavailable = %d, want EngineUnreachable (%d)", got, EngineUnreachable)
	}
}

// The text is read case-insensitively and past leading blank lines.
func TestClassifyIgnoresCaseAndLeadingBlankLines(t *testing.T) {
	for name, text := range map[string]string{
		"upper case":    strings.ToUpper(dkAText),
		"blank lines":   "\n\n  \n" + dkAText,
		"indented":      "   " + dkAText,
		"no final feed": strings.TrimSpace(dkAText),
	} {
		if got := ClassifyUnavailable(exitWith(text)); got != EngineUnreachable {
			t.Errorf("%s: ClassifyUnavailable = %d, want EngineUnreachable (%d)", name, got, EngineUnreachable)
		}
	}
}

// Criterion 5: what the classifier does not know stays unknown, and shows the generic
// "check logs". A lone "permission denied", "no such file or directory" or "connection
// refused" is anything (a mkdir, a path, a registry) and is never read as the engine.
func TestAnUnknownFailureStaysUnknown(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"a plain error", errors.New("boom")},
		{"exit status only", errExit},
		{"an exit error with no stderr", exitWith("")},
		{"stderr of blanks", exitWith(" \n\t\n")},
		{"a lone permission denied", exitWith("Error: mkdir /data: permission denied\n")},
		{"a lone no such file", exitWith("open /etc/hosts: no such file or directory\n")},
		{"a lone connection refused", exitWith("dial tcp 10.0.0.1:443: connect: connection refused\n")},
		{"an answer of the daemon about a container", exitWith("Error response from daemon: No such container: " + probedCtrID + "\n")},
		{"a podman object that is absent", exitWith("Error: no such object: \"" + probedCtrID + "\"\n")},
		{"a podman network that is absent", exitWith("Error: network bridge: unable to find network with name or ID bridge: network not found\n")},
		{"a mutation with a daemon answer", &engineError{Label: "docker rm", Output: "Error response from daemon: No such container: x", Err: errExit}},
		{"a mutation with empty output", &engineError{Label: "docker rm", Err: errExit}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyUnavailable(tt.err); got != UnavailabilityUnknown {
				t.Errorf("ClassifyUnavailable(%v) = %d, want unknown", tt.err, got)
			}
		})
	}
}

// A binary that is not there is known without reading any text: exec.ErrNotFound (what
// exec.LookPath and exec.Command report) or a file that does not exist (a configured
// path).
func TestAnAbsentBinaryIsEngineNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"exec.LookPath", &exec.Error{Name: "docker", Err: exec.ErrNotFound}},
		{"the bare sentinel", exec.ErrNotFound},
		{"wrapped by a caller", fmt.Errorf("docker ps failed: %w", &exec.Error{Name: "podman", Err: exec.ErrNotFound})},
		{"a configured path that does not exist", &fs.PathError{Op: "fork/exec", Path: "/opt/x/podman-remote", Err: fs.ErrNotExist}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyUnavailable(tt.err); got != EngineNotFound {
				t.Errorf("ClassifyUnavailable(%v) = %d, want EngineNotFound (%d)", tt.err, got, EngineNotFound)
			}
		})
	}
}

// Criterion 1, through the production check: with the binary absent from PATH every
// read says so, names the engine that was asked for, and runs nothing.
func TestLookPathFailureIsEngineNotFound(t *testing.T) {
	reads := []struct {
		name string
		call func() error
	}{
		{"ListContainers", func() error { _, err := ListContainers(true); return err }},
		{"ListImages", func() error { _, err := ListImages(); return err }},
		{"ListVolumes", func() error { _, err := ListVolumes(); return err }},
		{"ListNetworks", func() error { _, err := ListNetworks(); return err }},
		{"InspectNetwork", func() error { _, err := InspectNetwork("bridge"); return err }},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, read := range reads {
			t.Run(read.name, func(t *testing.T) {
				s := stubStrict(t) // no argv is expected: nothing may run once the binary is absent
				s.missing = true

				err := read.call()

				var unavailable *UnavailableError
				if !errors.As(err, &unavailable) {
					t.Fatalf("%s() = %v, want an *UnavailableError", read.name, err)
				}
				if unavailable.Reason != EngineNotFound {
					t.Errorf("Reason = %d, want EngineNotFound (%d)", unavailable.Reason, EngineNotFound)
				}
				if unavailable.Engine != shape.Name {
					t.Errorf("Engine = %q, want %q: the sentence must name the engine that was asked for", unavailable.Engine, shape.Name)
				}
				if !errors.Is(err, ErrEngineNotFound) || errors.Is(err, ErrEngineUnreachable) || errors.Is(err, ErrEnginePermissionDenied) {
					t.Errorf("errors.Is on %v: want ErrEngineNotFound only", err)
				}
				// Logs keep saying which engine, as the existing message does.
				if !strings.HasPrefix(err.Error(), shape.Name+" not found") {
					t.Errorf("Error() = %q, want it to start with %q", err, shape.Name+" not found")
				}
			})
		}
	})
}

// Criterion 1 with the real runner (cliRunner), a PATH that holds only the OTHER
// engine's fake: never a real engine (guard G1 panics). The configured engine is the
// one the sentence names, and the fake that is installed is never started.
func TestAnEngineMissingFromPathIsNamedAfterTheConfiguredOne(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		other := engine.Podman
		if shape.Name == engine.Podman {
			other = engine.Docker
		}
		fake := fakecli.Install(t, fakecli.Scenario{}, other)
		useRealRunner(t)

		for name, call := range map[string]func() error{
			"ListContainers": func() error { _, err := ListContainers(true); return err },
			"ListImages":     func() error { _, err := ListImages(); return err },
			"ListVolumes":    func() error { _, err := ListVolumes(); return err },
			"ListNetworks":   func() error { _, err := ListNetworks(); return err },
			"InspectNetwork": func() error { _, err := InspectNetwork("bridge"); return err },
			// InspectContainer does not call engineAvailable: it meets exec's own
			// "executable file not found" and must classify it all the same.
			"InspectContainer": func() error { _, err := InspectContainer(probedCtrID); return err },
		} {
			err := call()
			var unavailable *UnavailableError
			if !errors.As(err, &unavailable) {
				t.Errorf("%s with only %s on PATH = %v, want an *UnavailableError", name, other, err)
				continue
			}
			if unavailable.Reason != EngineNotFound || unavailable.Engine != shape.Name {
				t.Errorf("%s: Reason, Engine = %d, %q, want EngineNotFound, %q", name, unavailable.Reason, unavailable.Engine, shape.Name)
			}
		}
		if got := fake.Invocations(); len(got) != 0 {
			t.Errorf("the other engine was started: %v", got)
		}
	})
}

// readCall is one read of the package that must come back typed.
type readCall struct {
	name string
	sub  string // the subcommand in the label, "docker ps"
	argv func(engine.Shape) []string
	call func() error
}

var typedReads = []readCall{
	{"ListContainers(true)", "ps", func(s engine.Shape) []string { return []string{"ps", "--all", "--format", s.Templates.PS} },
		func() error { _, err := ListContainers(true); return err }},
	{"ListImages", "image ls", func(s engine.Shape) []string { return []string{"image", "ls", "--format", s.Templates.ImageLS} },
		func() error { _, err := ListImages(); return err }},
	{"ListVolumes", "volume ls", func(s engine.Shape) []string { return []string{"volume", "ls", "--format", s.Templates.VolumeLS} },
		func() error { _, err := ListVolumes(); return err }},
	{"ListNetworks", "network ls", func(s engine.Shape) []string { return []string{"network", "ls", "--format", s.Templates.NetworkLS} },
		func() error { _, err := ListNetworks(); return err }},
	{"InspectNetwork", "network inspect", func(engine.Shape) []string { return []string{"network", "inspect", "bridge"} },
		func() error { _, err := InspectNetwork("bridge"); return err }},
	{"InspectContainer", "inspect", func(engine.Shape) []string { return []string{"inspect", probedCtrID} },
		func() error { _, err := InspectContainer(probedCtrID); return err }},
}

// Answers of each engine, from the probes: a class and the text of one probed case.
var readAnswers = map[string][]struct {
	name   string
	stderr string
	want   Unavailability
}{
	engine.Docker: {
		{"D1 daemon stopped, 28", dkAText, EngineUnreachable},
		{"D1b socket absent, 29", fmt.Sprintf(dkB, "unix:///var/run/docker.sock", "/var/run/docker.sock"), EngineUnreachable},
		{"D4 permission, 29", dkC, EnginePermissionDenied},
		{"D4 permission, 28", fmt.Sprintf(dkD, "Head", "_ping"), EnginePermissionDenied},
	},
	engine.Podman: {
		{"P2a no socket", fmt.Sprintf(pmA, "/run/user/2000/podman/podman.sock", causeNoSocket), EngineUnreachable},
		{"P2e refused, 5.8.11", fmt.Sprintf(pmB, "/run/user/2000/podman/podman.sock", causeRefused, "/run/user/2000/podman/podman.sock"), EngineUnreachable},
		{"P3 permission", fmt.Sprintf(pmA, "/run/podman/podman.sock", causeDenied), EnginePermissionDenied},
	},
}

// Criterion 5, through every read: whatever the verb, the engine's answer comes back as
// a typed error with the class, the engine and the label, and still wraps the exit error.
func TestEveryReadReturnsATypedUnavailability(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, read := range typedReads {
			for _, answer := range readAnswers[shape.Name] {
				t.Run(read.name+"/"+answer.name, func(t *testing.T) {
					stubStrict(t, reply{argv: read.argv(shape), err: exitWith(answer.stderr)})

					err := read.call()

					var unavailable *UnavailableError
					if !errors.As(err, &unavailable) {
						t.Fatalf("%s = %v, want an *UnavailableError", read.name, err)
					}
					if unavailable.Reason != answer.want {
						t.Errorf("Reason = %d, want %d", unavailable.Reason, answer.want)
					}
					if unavailable.Engine != shape.Name {
						t.Errorf("Engine = %q, want %q", unavailable.Engine, shape.Name)
					}
					if want := shape.Name + " " + read.sub; unavailable.Label != want {
						t.Errorf("Label = %q, want %q", unavailable.Label, want)
					}
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) {
						t.Errorf("errors.As(*exec.ExitError) is false: the cause chain is cut")
					}
					if !strings.HasPrefix(err.Error(), unavailable.Label) {
						t.Errorf("Error() = %q, want it to name %q for the logs", err, unavailable.Label)
					}
				})
			}
		}
	})
}

// A failure the classifier does not know is not typed, still wraps the exit error and
// keeps its label, and the log gains the first line of what the engine said.
func TestEveryReadKeepsAnUnknownFailureGeneric(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, read := range typedReads {
			t.Run(read.name, func(t *testing.T) {
				stubStrict(t, reply{argv: read.argv(shape), err: exitWith("Error: the plumbing exploded\nsecond line\n")})

				err := read.call()

				if err == nil {
					t.Fatalf("%s returned nil", read.name)
				}
				var unavailable *UnavailableError
				if errors.As(err, &unavailable) {
					t.Errorf("an unknown failure came back typed: %+v", unavailable)
				}
				if want := shape.Name + " " + read.sub + " failed"; !strings.HasPrefix(err.Error(), want) {
					t.Errorf("Error() = %q, want it to start with %q", err, want)
				}
				if !strings.Contains(LogText(err), "the plumbing exploded") {
					t.Errorf("Error() = %q, want the first line of the engine's text", err)
				}
			})
		}
	})
}

// With the production runner (a fake engine that exits 1 with a probed stderr) the
// text reaches ClassifyUnavailable on a real *exec.ExitError: this is the path that
// carries Stderr in production, and the one a stub could hide.
func TestARealExitErrorCarriesTheProbedAnswer(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, answer := range readAnswers[shape.Name] {
			t.Run(answer.name, func(t *testing.T) {
				fakecli.Install(t, fakecli.Scenario{
					{Tool: shape.Binary, Args: []string{"ps"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: answer.stderr}},
					{Tool: shape.Binary, Args: []string{"inspect"}, Prefix: true, Reply: fakecli.Reply{Exit: 1, Stderr: answer.stderr}},
				}, shape.Binary)
				useRealRunner(t)

				_, listErr := ListContainers(true)
				_, inspectErr := InspectContainer(probedCtrID)

				for name, err := range map[string]error{"ListContainers": listErr, "InspectContainer": inspectErr} {
					var unavailable *UnavailableError
					if !errors.As(err, &unavailable) || unavailable.Reason != answer.want {
						t.Errorf("%s = %v, want an *UnavailableError of reason %d", name, err, answer.want)
					}
				}
			})
		}
	})
}

// Criteria 2 and 3: the sentence of every class and engine, exact. The set is closed:
// nothing else is ever shown.
func TestTheSentenceIsFixed(t *testing.T) {
	for class, byEngine := range sentences {
		for name, want := range byEngine {
			t.Run(fmt.Sprintf("class %d/%s", class, name), func(t *testing.T) {
				e := &UnavailableError{Reason: class, Engine: name, Label: name + " ps"}

				if got := e.UserMessage(); got != want {
					t.Errorf("UserMessage() = %q, want %q", got, want)
				}
				wrapped := fmt.Errorf("refresh: %w", e)
				if got, ok := UnavailableMessage(wrapped); !ok || got != want {
					t.Errorf("UnavailableMessage(wrapped) = %q, %v, want %q, true", got, ok, want)
				}
			})
		}
	}
}

// Any other error carries no sentence: the view keeps its generic message.
func TestUnavailableMessageIsAbsentFromAnyOtherError(t *testing.T) {
	for _, err := range []error{nil, errExit, exitWith(dkAText), fmt.Errorf("x: %w", errExit)} {
		if got, ok := UnavailableMessage(err); ok || got != "" {
			t.Errorf("UnavailableMessage(%v) = %q, %v, want \"\", false", err, got, ok)
		}
	}
}

// The sentinels map the reasons, and only them.
func TestTheSentinelsMapTheReasons(t *testing.T) {
	tests := []struct {
		reason   Unavailability
		sentinel error
	}{
		{EngineNotFound, ErrEngineNotFound},
		{EngineUnreachable, ErrEngineUnreachable},
		{EnginePermissionDenied, ErrEnginePermissionDenied},
	}
	for _, tt := range tests {
		err := fmt.Errorf("wrapped: %w", &UnavailableError{Reason: tt.reason, Engine: engine.Docker, Label: "docker ps", err: errExit})
		for _, other := range tests {
			if got, want := errors.Is(err, other.sentinel), other.reason == tt.reason; got != want {
				t.Errorf("reason %d: errors.Is(err, sentinel of reason %d) = %v, want %v", tt.reason, other.reason, got, want)
			}
		}
		if !errors.Is(err, errExit) {
			t.Errorf("reason %d: the cause chain is cut, errors.Is(err, errExit) is false", tt.reason)
		}
	}
}

// Criterion 6: the sentence is a constant of the closed set. With a stderr stuffed with
// a socket path, a URL with userinfo and variable names, none of it reaches the sentence.
func TestNoEngineTextReachesTheSentence(t *testing.T) {
	const secret = "s3cret"
	canaries := []string{"canary", "DOCKER_HOST", "CONTAINER_HOST", "://", "$", secret}

	cases := []struct {
		name   string
		engine string
		err    error
		want   Unavailability
	}{
		{"docker unreachable", engine.Docker, exitWith(
			"Cannot connect to the Docker daemon at tcp://u:" + secret + "@canary.example:2375 (DOCKER_HOST=unix:///run/canary.sock, see https://canary.example). Is the docker daemon running?\n"), EngineUnreachable},
		{"docker permission", engine.Docker, exitWith(
			"permission denied while trying to connect to the docker API at unix:///run/canary.sock (DOCKER_HOST) tcp://u:" + secret + "@canary.example https://canary.example\n"), EnginePermissionDenied},
		{"podman unreachable", engine.Podman, exitWith(
			podmanOff + "Error: unable to connect to Podman socket: Get \"http://d/v6.2.1/libpod/_ping\": dial unix /run/canary.sock: connect: connection refused: CONTAINER_HOST=ssh://u:" + secret + "@canary.example\n"), EngineUnreachable},
		{"podman permission", engine.Podman, exitWith(
			podmanOff + "Error: unable to connect to Podman socket: Get \"http://d/v6.2.1/libpod/_ping\": dial unix /run/canary.sock: connect: permission denied: CONTAINER_HOST=ssh://u:" + secret + "@canary.example\n"), EnginePermissionDenied},
		{"docker, a configured path", engine.Docker, &exec.Error{Name: "/opt/canary/docker", Err: exec.ErrNotFound}, EngineNotFound},
		{"podman, a configured path", engine.Podman, &exec.Error{Name: "/opt/canary/podman-remote", Err: exec.ErrNotFound}, EngineNotFound},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			usingEngine(t, tt.engine)

			err := readFailure(cmdLabel("ps"), tt.err)

			message, ok := UnavailableMessage(err)
			if !ok {
				t.Fatalf("readFailure(%v) = %v, want an *UnavailableError", tt.err, err)
			}
			if want := sentences[tt.want][tt.engine]; message != want {
				t.Errorf("sentence = %q, want %q", message, want)
			}
			for _, fragment := range canaries {
				if strings.Contains(message, fragment) {
					t.Errorf("the sentence %q holds %q", message, fragment)
				}
			}
		})
	}
}

// The log (LogText) keeps the cause, so a failure can be diagnosed, but never a credential: the
// userinfo of an URL is replaced, only the first line stays, and the cause is capped.
func TestUnavailableErrorLogHasNoUserinfo(t *testing.T) {
	usingEngine(t, engine.Docker)
	label := cmdLabel("ps")

	t.Run("userinfo of an URL", func(t *testing.T) {
		err := readFailure(label, exitWith("Cannot connect to the Docker daemon at tcp://admin:s3cret@10.0.0.1:2375. Is the docker daemon running?\n"))

		text := LogText(err)
		if strings.Contains(text, "s3cret") || strings.Contains(text, "admin") {
			t.Errorf("Error() = %q, holds the userinfo", text)
		}
		if !strings.Contains(text, "tcp://***@10.0.0.1:2375") {
			t.Errorf("Error() = %q, want the URL kept with its userinfo replaced (tcp://***@10.0.0.1:2375)", text)
		}
	})
	t.Run("a user without a password", func(t *testing.T) {
		err := readFailure(label, exitWith("Cannot connect to the Docker daemon at ssh://deploy@build.example. Is the docker daemon running?\n"))

		if text := LogText(err); strings.Contains(text, "deploy") || !strings.Contains(text, "ssh://***@build.example") {
			t.Errorf("Error() = %q, want ssh://***@build.example and no user", text)
		}
	})
	t.Run("first line only", func(t *testing.T) {
		err := readFailure(label, exitWith(dkAText+"SECOND-LINE-MARKER\n"))

		if text := LogText(err); strings.Contains(text, "SECOND-LINE-MARKER") {
			t.Errorf("Error() = %q, holds the second line", text)
		}
	})
	t.Run("capped at 200 bytes of cause", func(t *testing.T) {
		long := "Cannot connect to the Docker daemon at " + strings.Repeat("x", 500) + ". Is the docker daemon running?\n"

		err := readFailure(label, exitWith(long))

		text := LogText(err)
		if !strings.Contains(text, strings.Repeat("x", 100)) {
			t.Errorf("Error() = %q, want the beginning of the cause kept", text)
		}
		if strings.Contains(text, strings.Repeat("x", 200)) {
			t.Errorf("Error() holds %d bytes of cause, want at most 200", len(text))
		}
	})
}

// An unknown failure is logged better than before: the label and the exit error stay,
// the first line of stderr is added with the same redaction, and errors.As to
// *exec.ExitError still works. Nothing changes when stderr is empty.
func TestAnUnknownReadFailureLogsTheFirstStderrLine(t *testing.T) {
	usingEngine(t, engine.Docker)
	label := cmdLabel("ps")

	t.Run("first line, redacted", func(t *testing.T) {
		cause := exitWith("Error: something odd happened at tcp://u:s3cret@h:2375\nsecond line\n")

		err := readFailure(label, cause)

		text := LogText(err)
		if !strings.HasPrefix(text, label+" failed: ") {
			t.Errorf("Error() = %q, want the label first", text)
		}
		if !strings.Contains(text, "something odd happened at tcp://***@h:2375") {
			t.Errorf("Error() = %q, want the first stderr line with its userinfo replaced", text)
		}
		for _, leak := range []string{"s3cret", "second line"} {
			if strings.Contains(text, leak) {
				t.Errorf("Error() = %q, holds %q", text, leak)
			}
		}
		// The engine's line is for the log (LogText), never for Error() (security review M1).
		if got := err.Error(); strings.Contains(got, "something odd happened") {
			t.Errorf("Error() = %q, holds the engine's line", got)
		}
		var unavailable *UnavailableError
		if errors.As(err, &unavailable) {
			t.Errorf("an unknown failure came back typed: %+v", unavailable)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr != cause {
			t.Errorf("errors.As(*exec.ExitError) = %v, want the very error given", exitErr)
		}
	})
	t.Run("no stderr, unchanged", func(t *testing.T) {
		err := readFailure(label, errExit)

		if want := label + " failed: exit status 1"; err.Error() != want {
			t.Errorf("Error() = %q, want %q", err, want)
		}
		if !errors.Is(err, errExit) {
			t.Error("the cause is no longer wrapped")
		}
	})
}

// ClassifyRemoval treats an unavailable engine as a refusal of unknown reason, whatever
// its text holds: nothing was looked up, so "no such container" in a cause says nothing
// about the target (OC-22's rule, now explicit).
func TestClassifyRemovalReadsAnUnavailableEngineAsOther(t *testing.T) {
	for _, reason := range []Unavailability{EngineNotFound, EngineUnreachable, EnginePermissionDenied} {
		for _, cause := range []string{"", "Error response from daemon: No such container: abc", "image not known", "volume is in use", "container is running"} {
			err := fmt.Errorf("remove: %w", &UnavailableError{
				Reason: reason, Engine: engine.Docker, Label: "docker rm", cause: cause, err: errExit,
			})

			status, why := ClassifyRemoval(err)

			if status != RemovalRefused || why != ReasonOther {
				t.Errorf("reason %d, cause %q: ClassifyRemoval = (%q, %q), want (%q, %q)", reason, cause, status, why, RemovalRefused, ReasonOther)
			}
		}
	}
}
