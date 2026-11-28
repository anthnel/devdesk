package docker

import (
	"errors"
	"io/fs"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/engine"
)

// Unavailability is why the engine could not be asked at all (OC-24, DEVDESK-822).
// The zero value is "not one of these": the caller keeps its generic message.
//
// A class is announced only when the engine's own words (or exec's sentinel for a
// missing binary) make it certain. The direction is the opposite of
// notReachableMarkers in removal.go, which is wide on purpose so that a doubtful
// failure is never read as "already gone"; here a doubtful failure stays unknown,
// because a wrong sentence is worse than "check logs".
type Unavailability int

const (
	UnavailabilityUnknown  Unavailability = iota
	EngineNotFound                        // the binary is not on PATH (or the configured path does not exist)
	EngineUnreachable                     // the daemon, service or machine does not answer
	EnginePermissionDenied                // the socket refused this user
)

// Sentinels for errors.Is, one per reason.
var (
	ErrEngineNotFound         = errors.New("container engine not found")
	ErrEngineUnreachable      = errors.New("container engine unreachable")
	ErrEnginePermissionDenied = errors.New("container engine access denied")
)

// UnavailableError is a read that could not reach the engine.
//
// Nothing the engine said leaves through Error(): that text is fixed (the label and
// the class), because an error reaches footers, MCP clients and logs alike. The
// cause, one redacted line of the engine's text, is read through LogText() and
// nowhere else; what a view shows is UserMessage(), a fixed sentence.
type UnavailableError struct {
	Reason Unavailability
	// Engine is engine.Current().Name when the call failed: "docker" or "podman".
	Engine string
	// Label is cmdLabel(...) of the call, e.g. "docker ps".
	Label string
	// ByPath is true when the configured engine is a path (app.container_engine
	// names a file) rather than a bare name; it only changes the "not found" sentence.
	ByPath bool
	// cause is one line of the engine's text, redacted (see redactCause), at most
	// maxCauseBytes. LogText only.
	cause string
	err   error
}

// Error is the fixed text: "<engine> not found: <why exec says so>" for a missing
// binary (Go's words about a path, never the engine's), "<label> failed: <class>"
// otherwise. LogText adds the cause.
func (e *UnavailableError) Error() string {
	if e.Reason == EngineNotFound {
		return e.Engine + " not found: " + e.cause
	}
	return e.Label + " failed: " + e.reasonWords()
}

// LogText is Error() plus the redacted line of the engine's text that decided the
// class, for the log and nowhere else.
func (e *UnavailableError) LogText() string {
	if e.Reason == EngineNotFound || e.cause == "" {
		return e.Error()
	}
	return e.Label + " failed: " + e.cause
}

func (e *UnavailableError) reasonWords() string {
	switch e.Reason {
	case EngineUnreachable:
		return "engine unreachable"
	case EnginePermissionDenied:
		return "engine access denied"
	case EngineNotFound:
		return "engine not found"
	}
	return "engine unavailable"
}

func (e *UnavailableError) Unwrap() error { return e.err }

// Is maps the reason to its sentinel, so errors.Is(err, ErrEngineNotFound) works
// on any wrapping of the error.
func (e *UnavailableError) Is(target error) bool {
	switch target {
	case ErrEngineNotFound:
		return e.Reason == EngineNotFound
	case ErrEngineUnreachable:
		return e.Reason == EngineUnreachable
	case ErrEnginePermissionDenied:
		return e.Reason == EnginePermissionDenied
	}
	return false
}

// UserMessage is the sentence a view shows. The set is closed: one constant per
// class and engine, never built from the engine's text, an URL, a path or a
// variable name (criterion 6).
func (e *UnavailableError) UserMessage() string {
	podman := e.Engine == engine.Podman
	switch e.Reason {
	case EngineNotFound:
		name := "Docker"
		if podman {
			name = "Podman"
		}
		if e.ByPath {
			// A configured path is not looked up on PATH: say where to look instead.
			return name + " not found — check app.container_engine"
		}
		return name + " not found on PATH — install it or set app.container_engine"
	case EngineUnreachable:
		if podman {
			// To be validated by the techwriter (plan, Q2): a local Podman client has no
			// daemon, so this only reads for a remote client (--remote, --url,
			// CONTAINER_HOST, a stopped machine).
			return "Cannot reach Podman — is the Podman machine or service running?"
		}
		return "Cannot reach the Docker daemon — is it running?"
	case EnginePermissionDenied:
		if podman {
			// To be validated by the techwriter (plan, Q2): only the remote-socket case
			// (P3) reads as this; the local permission failures stay unknown.
			return "Permission denied on the Podman socket — check that your user may open it"
		}
		return "Permission denied on the Docker socket — add your user to the docker group"
	}
	return ""
}

// UnavailableMessage returns the fixed sentence of an *UnavailableError in err's
// chain.
func UnavailableMessage(err error) (string, bool) {
	var unavailable *UnavailableError
	if errors.As(err, &unavailable) {
		if message := unavailable.UserMessage(); message != "" {
			return message, true
		}
	}
	return "", false
}

// The markers below are the engine's own words, each read on one line of the
// text, lower-cased, and anchored at its start. Sources: the probes of
// docs/measurements/2026-11-28-engine-unavailable.md (docker 29.10.1 and 28.5.2
// rootful, podman 6.2.1 and 5.8.11 rootless and rootful, Ubuntu 24.04). They are
// client texts and can change with a version: a text that moves falls back to the
// generic message, which is the safe side.
const (
	// dockerPermissionPrefix: DK-C (docker 29, "...to the docker API at") and DK-D
	// (docker 28, "...to the Docker daemon socket at"), case D4.
	dockerPermissionPrefix = "permission denied while trying to connect to the docker "
	// podmanLinePrefix starts the second line of a remote client's failure (PM-A and
	// PM-B, cases P2a to P2e, P3, P3c); the cause follows the first ": connect: ".
	podmanLinePrefix  = "error: unable to connect to podman socket:"
	podmanDialMarker  = ": connect: "
	podmanDeniedCause = "permission denied"
)

var (
	// dockerUnreachablePrefixes: DK-A (29 and 28, cases D1, D2, D5, and D1b/D2c on
	// 28) and DK-B (29 only, D1b and D2c).
	dockerUnreachablePrefixes = []string{
		"cannot connect to the docker daemon at ",
		"failed to connect to the docker api at ",
	}
	// podmanUnreachableCauses are the two non-permission causes measured after
	// "connect: ". The 5.8.11 text adds ": unix://<path>" after the cause, so they
	// are matched as prefixes. Any other cause stays unknown.
	podmanUnreachableCauses = []string{"no such file or directory", "connection refused"}
)

// maxScannedLines bounds how many non-empty lines are read: the engine's diagnostic
// is on the first one or two (podman puts its cause on the second); a mutation's
// combined output may hold more, and later lines are never the engine's own.
const maxScannedLines = 3

// maxCauseBytes caps the cause kept for the logs.
const maxCauseBytes = 200

// ClassifyUnavailable says whether err is an engine that could not be asked, and
// why. It reads only the engine's own words — the Stderr of an *exec.ExitError and
// the Output of an *engineError — and exec's sentinel for a missing binary, never
// DevDesk's label and never an error built from text. Permission is read before
// unreachable (podman's permission answer also says "Cannot connect to Podman").
// It is called on a failed invocation only: a command that exits 0 is never an
// error here, whatever it printed (podman's subuid warnings, P5). It is for
// reads only, never for the combined output of run, exec or logs: a docker:cli
// container started without the socket prints exactly DK-A, and that is the
// container's answer, not the engine's.
func ClassifyUnavailable(err error) Unavailability {
	reason, _ := classify(err)
	return reason
}

// classify is ClassifyUnavailable plus the engine line that decided it ("" for a
// missing binary).
func classify(err error) (Unavailability, string) {
	if err == nil {
		return UnavailabilityUnknown, ""
	}
	var typed *UnavailableError
	if errors.As(err, &typed) {
		return typed.Reason, ""
	}
	if binaryMissing(err) {
		return EngineNotFound, ""
	}
	return classifyText(engineOutput(err))
}

// binaryMissing reports a binary that exec could not find or start because it does
// not exist: exec.ErrNotFound (LookPath, exec.Command) or a configured path whose
// fork/exec says the file is absent.
func binaryMissing(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var pathErr *fs.PathError
	return errors.As(err, &pathErr) && pathErr.Op == "fork/exec" && errors.Is(pathErr.Err, fs.ErrNotExist)
}

// engineOutput returns what the engine printed: the combined output of a mutation,
// else the stderr a read left on its *exec.ExitError.
func engineOutput(err error) string {
	var mutation *engineError
	if errors.As(err, &mutation) && strings.TrimSpace(mutation.Output) != "" {
		return mutation.Output
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(exitErr.Stderr)
	}
	return ""
}

// firstLines returns up to n trimmed non-empty lines of text.
func firstLines(text string, n int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
			if len(lines) == n {
				break
			}
		}
	}
	return lines
}

func classifyText(text string) (Unavailability, string) {
	unreachable, unreachableLine := UnavailabilityUnknown, ""
	for _, line := range firstLines(text, maxScannedLines) {
		switch reason := classifyLine(strings.ToLower(line)); reason {
		case EnginePermissionDenied:
			return reason, line // permission before unreachable
		case EngineUnreachable:
			if unreachable == UnavailabilityUnknown {
				unreachable, unreachableLine = reason, line
			}
		}
	}
	return unreachable, unreachableLine
}

// classifyLine reads one lower-cased line.
func classifyLine(line string) Unavailability {
	if strings.HasPrefix(line, dockerPermissionPrefix) {
		return EnginePermissionDenied
	}
	for _, prefix := range dockerUnreachablePrefixes {
		if strings.HasPrefix(line, prefix) {
			// DK-B with a permission cause was not measured (docker 29 words it as
			// DK-C): not claimed.
			if strings.HasSuffix(line, "permission denied") {
				return UnavailabilityUnknown
			}
			return EngineUnreachable
		}
	}
	if strings.HasPrefix(line, podmanLinePrefix) {
		if i := strings.Index(line, podmanDialMarker); i >= 0 {
			cause := line[i+len(podmanDialMarker):]
			if strings.HasPrefix(cause, podmanDeniedCause) {
				return EnginePermissionDenied
			}
			for _, known := range podmanUnreachableCauses {
				if strings.HasPrefix(cause, known) {
					return EngineUnreachable
				}
			}
		}
	}
	return UnavailabilityUnknown
}

// urlPattern finds the URLs of a line: a scheme, "://", then everything up to a
// space, a quote or a bracket.
var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>()\[\]{}]*`)

// redactURL keeps the scheme and the host of an URL and drops what may carry a
// secret: the whole userinfo (up to the LAST "@", so a password holding "/" or "@"
// goes too; the name is replaced as well) and the query and fragment. It cuts by
// hand rather than through url.Parse because a log line is not always a valid URL
// ("tcp://h:2375." with its full stop, a password with "/") and a failed parse
// must not leave the text as it was.
func redactURL(raw string) string {
	trimmed := strings.TrimRight(raw, ".,;:!")
	suffix := raw[len(trimmed):]
	scheme, rest, _ := strings.Cut(trimmed, "://")
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = "***@" + rest[i+1:]
	}
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	return scheme + "://" + rest + suffix
}

// isInvisible reports a character a log line must not carry: a control character
// (C0, DEL and C1, so U+009B CSI too) or a bidirectional mark that could reorder
// what a terminal shows.
func isInvisible(r rune) bool {
	return unicode.IsControl(r) ||
		(r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// redactCause makes one line of engine text fit for the log: URLs reduced by
// redactURL, invisible characters dropped, at most maxCauseBytes. The redaction
// comes before the cut so that a cut never leaves half a secret.
func redactCause(line string) string {
	line = urlPattern.ReplaceAllStringFunc(line, redactURL)
	line = strings.Map(func(r rune) rune {
		if isInvisible(r) {
			return -1
		}
		return r
	}, line)
	if len(line) > maxCauseBytes {
		line = line[:maxCauseBytes]
		for !utf8.ValidString(line) {
			line = line[:len(line)-1]
		}
	}
	return strings.TrimSpace(line)
}

// firstEngineLine is the first non-empty line of the engine's text, redacted.
func firstEngineLine(err error) string {
	lines := firstLines(engineOutput(err), 1)
	if len(lines) == 0 {
		return ""
	}
	return redactCause(lines[0])
}

// readError is a failed read whose class is unknown. Its text is what wrapErr
// always gave ("<label> failed: <err>"); the first line of the engine's stderr,
// redacted, is kept apart for LogText.
type readError struct {
	label string
	err   error
	cause string
}

func (e *readError) Error() string { return e.label + " failed: " + e.err.Error() }

func (e *readError) Unwrap() error { return e.err }

// LogText is Error() plus the redacted first line of the engine's stderr.
func (e *readError) LogText() string { return e.Error() + " (" + e.cause + ")" }

// LogText is the text of err for the log: the redacted line of the engine's words
// when the error holds one (an *UnavailableError, or an unknown read failure that
// came with stderr), err.Error() otherwise.
func LogText(err error) string {
	if err == nil {
		return ""
	}
	var logged interface{ LogText() string }
	if errors.As(err, &logged) {
		return logged.LogText()
	}
	return err.Error()
}

// readFailure replaces wrapErr in the read functions. A failure the engine's words
// classify comes back as an *UnavailableError (still wrapping err, so errors.As to
// *exec.ExitError keeps working); any other keeps the text it always had. Neither
// puts a word of the engine in Error(); LogText has the first line, redacted.
func readFailure(label string, err error) error {
	if err == nil {
		return nil
	}
	reason, line := classify(err)
	if reason != UnavailabilityUnknown {
		cause := redactCause(line)
		if cause == "" {
			cause = redactCause(firstLine(err.Error()))
		}
		return &UnavailableError{
			Reason: reason, Engine: engine.Current().Name, Label: label,
			ByPath: configuredByPath(), cause: cause, err: err,
		}
	}
	if first := firstEngineLine(err); first != "" {
		return &readError{label: label, err: err, cause: first}
	}
	return wrapErr(label, err)
}

// firstLine is the first line of s.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
