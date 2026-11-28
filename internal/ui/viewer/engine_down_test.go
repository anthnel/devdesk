package viewer

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// OC-24 (DEVDESK-822), criterion 7: a load that fails because the engine could not be
// asked (the inspect of a container) shows the fixed sentence the error carries, and
// the viewer learns it without knowing what a docker error is: it reads an optional
// `UserMessage() string` through errors.As, the way the router reads the interfaces of
// a source (messages.go). Any other failure keeps "Failed to open — check logs", and
// the two refusals the viewer names itself (too large, not text) stay.

// userMessageError stands for docker.UnavailableError: its Error() is raw engine text
// that must never be shown, its UserMessage() is the sentence a view may show.
type userMessageError struct {
	raw     string
	message string
}

func (e userMessageError) Error() string       { return e.raw }
func (e userMessageError) UserMessage() string { return e.message }

const (
	rawEngineText = "docker inspect failed: Cannot connect to the Docker daemon at tcp://admin:s3cret@canary.example:2375. Is the docker daemon running?"
	userSentence  = "Cannot reach the Docker daemon — is it running?"
	genericOpen   = "Failed to open — check logs"
)

// loadFailed feeds the viewer the result of a load that failed with err.
func loadFailed(t *testing.T, err error) Model {
	t.Helper()
	return feed(t, jsonModel(t), DocumentLoadedMsg{Name: "inspect · web", Err: err})
}

func TestALoadFailureWithAUserMessageShowsIt(t *testing.T) {
	typed := userMessageError{raw: rawEngineText, message: userSentence}
	for _, c := range []struct {
		name string
		err  error
	}{
		{"as it comes", typed},
		{"wrapped", fmt.Errorf("opening the inspect: %w", typed)},
		{"wrapped twice", fmt.Errorf("a: %w", fmt.Errorf("b: %w", typed))},
		{"joined with another error", errors.Join(errors.New("also failed"), typed)},
		{"as a pointer", &userMessageError{raw: rawEngineText, message: userSentence}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := loadFailed(t, c.err)

			if m.footer.Text() != userSentence || m.footer.Level() != components.LevelError {
				t.Errorf("footer = (%q, level %d), want (%q, LevelError)", m.footer.Text(), m.footer.Level(), userSentence)
			}
			footer := ansi.Strip(m.RenderFooter(120))
			if !strings.Contains(footer, userSentence) {
				t.Errorf("the footer line is %q, want it to say %q", strings.TrimSpace(footer), userSentence)
			}
			if !strings.Contains(m.RenderFooter(120), theme.FooterErrorStyle.Render(userSentence)) {
				t.Error("the sentence is not drawn at level Error")
			}
			// The error's own text never reaches the screen.
			for _, fragment := range []string{"s3cret", "admin", "tcp://", "canary", "2375", "Is the docker daemon"} {
				if strings.Contains(footer, fragment) {
					t.Errorf("the footer holds %q: %q", fragment, footer)
				}
			}
		})
	}
}

func TestAnyOtherLoadFailureKeepsCheckLogs(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"a plain error", errors.New(rawEngineText)},
		{"a wrapped os error", fmt.Errorf("reading: %w", os.ErrPermission)},
		{"an error with no sentence", userMessageError{raw: rawEngineText, message: ""}},
		{"a wrapped error with no sentence", fmt.Errorf("x: %w", userMessageError{raw: rawEngineText})},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := loadFailed(t, c.err)

			if m.footer.Text() != genericOpen || m.footer.Level() != components.LevelError {
				t.Errorf("footer = (%q, level %d), want (%q, LevelError)", m.footer.Text(), m.footer.Level(), genericOpen)
			}
			if footer := ansi.Strip(m.RenderFooter(120)); strings.Contains(footer, "s3cret") || strings.Contains(footer, "tcp://") {
				t.Errorf("the footer shows the error's text: %q", footer)
			}
		})
	}
}

// The two refusals the viewer names itself are not swallowed by the new reading.
func TestTheNamedRefusalsStayNamed(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{viewerpkg.ErrTooLarge, "File is too large to view"},
		{viewerpkg.ErrBinary, "Not a text file"},
	} {
		m := loadFailed(t, c.err)
		if m.footer.Text() != c.want {
			t.Errorf("footer for %v = %q, want %q", c.err, m.footer.Text(), c.want)
		}
	}
}

// The viewer reads the sentence through an interface and imports no engine package: a
// viewer that knew docker.UnavailableError would tie a document viewer to one source.
func TestTheViewerDoesNotImportDocker(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source file found: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		checked++
		for _, imp := range file.Imports {
			if path := strings.Trim(imp.Path.Value, `"`); strings.HasSuffix(path, "/internal/docker") {
				t.Errorf("%s imports %s: read the sentence through `UserMessage() string` and errors.As", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test source file was checked")
	}
}

// The sentence comes from outside the viewer: a terminal sequence or a bidi mark in it
// is never drawn, and a sentence that is nothing but such a sequence is not a sentence.
func TestAUserMessageIsFilteredBeforeItIsShown(t *testing.T) {
	for _, c := range []struct {
		name, message, want string
	}{
		{"an OSC title", "\x1b]0;x\aCannot reach the Docker daemon — is it running?", "Cannot reach the Docker daemon — is it running?"},
		{"a bidi override", "Cannot reach\u202e the daemon", "Cannot reach? the daemon"},
		{"a control character", "Cannot reach\x07 the daemon", "Cannot reach? the daemon"},
		{"nothing but a sequence", "\x1b]0;x\a", genericOpen},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := loadFailed(t, userMessageError{raw: rawEngineText, message: c.message})

			if m.footer.Text() != c.want {
				t.Errorf("footer = %q, want %q", m.footer.Text(), c.want)
			}
			if footer := m.RenderFooter(120); strings.ContainsAny(ansi.Strip(footer), "\x1b\a\u202e") {
				t.Errorf("the footer line holds a control or bidi character: %q", ansi.Strip(footer))
			}
		})
	}
}
