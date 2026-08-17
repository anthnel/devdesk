package viewer

import (
	"io"
	"log"
	"os"
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/config"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// The view logs every load failure, and several fixtures are failures on
// purpose.
//
// The colour profile is forced for the whole package: `go test` has no terminal,
// so lipgloss resolves to Ascii and every style renders as bare text — which
// would make the colour and background assertions pass by rendering nothing at
// all.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	lipgloss.SetColorProfile(termenv.TrueColor)

	code := m.Run()

	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// ── Fixture sources ──────────────────────────────────────────────────────────

// fakeSource is a source with no I/O: Load hands back what the test put in it.
type fakeSource struct {
	name    string
	kind    viewerpkg.Kind
	content string
	err     error
}

func (s fakeSource) Name() string          { return s.name }
func (s fakeSource) Kind() viewerpkg.Kind  { return s.kind }
func (s fakeSource) Load() ([]byte, error) { return []byte(s.content), s.err }

// capableSource implements all three optional capabilities, standing in for the
// container log source.
type capableSource struct {
	fakeSource
	timestamps bool
}

func (s capableSource) WithTimestamps(on bool) viewerpkg.Source {
	s.timestamps = on
	return s
}
func (s capableSource) FollowCmd() *exec.Cmd { return exec.Command("true") }
func (s capableSource) PagerCmd() *exec.Cmd  { return exec.Command("true") }

// ── Helpers ──────────────────────────────────────────────────────────────────

// open builds a viewer on a source and drives it to the state the router would:
// sized, and with the document loaded.
func open(t *testing.T, source viewerpkg.Source) Model {
	t.Helper()
	m := NewWithSource(config.Default(), source)
	m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})

	init := m.Init()
	if init == nil {
		t.Fatal("Init returned no command, so the source is never loaded")
	}
	return feed(t, m, resolve(t, init)...)
}

// resolve runs a Cmd and returns the messages it produced, unwrapping a
// tea.Batch into its members. Init batches the load with the spinner's first
// tick, so calling the Cmd alone yields a BatchMsg and the document never
// arrives — which is exactly the silence this unwraps.
func resolve(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, resolve(t, c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

const sampleJSON = `{"name":"web","ports":[80,443],"env":{"MODE":"prod","DEBUG":false}}`

func jsonModel(t *testing.T) Model {
	t.Helper()
	return open(t, fakeSource{name: "config.json", content: sampleJSON})
}

func logModel(t *testing.T, content string) Model {
	t.Helper()
	return open(t, capableSource{fakeSource: fakeSource{
		name: "logs · api", kind: viewerpkg.KindLog, content: content,
	}})
}
