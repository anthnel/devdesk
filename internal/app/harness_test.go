package app

import (
	"errors"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The router is the one place that sees every view through an interface rather
// than by name, so its defects are the ones no view-level test can reach: a key
// swallowed before it is forwarded, a message routed to the wrong view, a
// viewport sized against a stale footer height.
//
// These tests build an App directly. New() cannot be called: it reads the
// terminal size from os.Stdout and panics when there is none, which is always
// the case under go test — see §1.3 D16 in the backlog.
//
// HOME is redirected for the whole package. Context switching, theme listing
// and the scan caches all resolve paths under it, so redirecting it makes those
// commands safe to execute instead of merely assertable.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)

	home, err := os.MkdirTemp("", "devdesk-app-test")
	if err != nil {
		log.SetOutput(os.Stderr)
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	// :agent-usage reads Claude Code transcripts from here; never the real ones.
	_ = os.Unsetenv("CLAUDE_CONFIG_DIR")

	code := m.Run()

	_ = os.RemoveAll(home)
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// errNotOnDisk stands in for the cache miss the router has to survive: a
// metadata entry whose full result file is gone.
var errNotOnDisk = errors.New("open result: no such file or directory")

// testConfig leaves the GitLab URL empty: a configured URL makes the router
// attempt an auto-login, which reaches the network.
func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Forge.URL = ""
	return cfg
}

// fakeView stands in for a real view. It implements every optional interface
// the router probes for — FormView, FooterView, HeaderView, FilterBarView and
// help.Provider — with each answer settable, so a test can build the exact
// shape it needs. It records every message forwarded to it, which is how the
// tests tell "the view received the key" from "the router consumed it".
type fakeView struct {
	title        string
	body         string
	editing      bool
	filterBar    bool
	footerHeight int
	footer       string
	shortcuts    shortcut.Shortcuts
	helpContent  help.Content

	// onKey runs before a key is recorded, so a test can make the view change
	// shape in response to one — the filter bar opening is the case the router
	// has to notice.
	onKey func(*fakeView)

	inits    int
	received []tea.Msg
}

func (v *fakeView) Init() tea.Cmd { v.inits++; return nil }

func (v *fakeView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && v.onKey != nil {
		_ = key
		v.onKey(v)
	}
	v.received = append(v.received, msg)
	return v, nil
}

func (v *fakeView) View() string                     { return v.body }
func (v *fakeView) InEditMode() bool                 { return v.editing }
func (v *fakeView) FilterBarVisible() bool           { return v.filterBar }
func (v *fakeView) GetFooterHeight() int             { return v.footerHeight }
func (v *fakeView) RenderFooter(int) string          { return v.footer }
func (v *fakeView) GetShortcuts() shortcut.Shortcuts { return v.shortcuts }
func (v *fakeView) GetTitle() string                 { return v.title }
func (v *fakeView) GetIcon() string                  { return "" }
func (v *fakeView) GetHelpContent() help.Content     { return v.helpContent }
func (v *fakeView) GetHeaderInfo(ctx string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{{Key: "Context", Value: ctx}}
}

// keysSeen returns the String() of every key the router forwarded.
func (v *fakeView) keysSeen() []string {
	var out []string
	for _, msg := range v.received {
		if key, ok := msg.(tea.KeyMsg); ok {
			out = append(out, key.String())
		}
	}
	return out
}

func (v *fakeView) sawKey(name string) bool {
	for _, k := range v.keysSeen() {
		if k == name {
			return true
		}
	}
	return false
}

// receivedOf reports whether the view was forwarded a message of type T.
func receivedOf[T tea.Msg](v *fakeView) (T, bool) {
	for _, msg := range v.received {
		if typed, ok := msg.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

// bareView implements tea.Model and nothing else, which is how the router
// behaves when a view offers no footer, no header and no help.
type bareView struct{ received []tea.Msg }

func (v *bareView) Init() tea.Cmd { return nil }
func (v *bareView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	v.received = append(v.received, msg)
	return v, nil
}
func (v *bareView) View() string { return "bare" }

// isolateHome gives the test a HOME of its own. The one TestMain sets lives as
// long as the process, so under -count=N a test met the files its own previous
// run had written: a context already created, a forge index still fresh enough
// to skip the walk (DEVDESK-36). TestMain's HOME stays as the fallback for code
// that runs outside a router.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
}

// router builds an App hosting view as the dashboard, laid out at 180x40.
func router(t *testing.T, view tea.Model) *App {
	t.Helper()
	return routerAt(t, view, 180, 40)
}

func routerAt(t *testing.T, view tea.Model, width, height int) *App {
	t.Helper()
	isolateHome(t)
	a := &App{
		config:         testConfig(),
		currentContext: "default",
		viewport:       newViewport(width, height),
		currentView:    command.ViewDashboard,
		views:          map[command.ViewType]tea.Model{command.ViewDashboard: view},
		// A session-only store, as newWithSize builds: it keeps the tests off
		// the developer's real keychain and makes seeding a token one Save call.
		sharedState: &shared.State{
			Secrets: credentials.SessionOnly("under test"),
		},
		commandInput: newCommandInput(),
		suggester:    command.NewSuggester(),
		sugCursor:    -1,
		jobs:         jobs.New(),
		width:        width,
		height:       height,
	}
	// Lay it out once, as New() does, so the viewport height a test measures
	// against is the one the router actually computed.
	a.resize(width, height)
	return a
}

// feedKey drives one key through the router and returns the command it emitted.
func feedKey(t *testing.T, a *App, key tea.KeyMsg) tea.Cmd {
	t.Helper()
	_, cmd := a.handleKeyMsg(key)
	return cmd
}

// ── The command palette on screen (DEVDESK-109) ──────────────────────────────
//
// The palette is an overlay: it is not a field of the model but cells of the
// rendered screen. These helpers read it back from View(), without asking the
// code under test where it believes the frame is, so a wrong geometry shows up
// as a wrong screen rather than agreeing with itself.

// paletteHeaderLines is the header block above the viewport: 7 rows and the
// blank line under them. The command line and its blank line are gone.
const paletteHeaderLines = 8

// paletteSizes are the four terminal sizes the story is checked at.
var paletteSizes = [][2]int{{80, 24}, {120, 40}, {60, 30}, {40, 16}}

// footedView is a fakeView with the usual two-line footer (blank line + info
// line), padded to the terminal's width like the real footers, which
// lipgloss.Place does not do for a block already as wide as the screen.
type footedView struct{ fakeView }

func (v *footedView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.fakeView.Update(msg)
	return v, cmd
}

func (v *footedView) GetFooterHeight() int { return 2 }

func (v *footedView) RenderFooter(width int) string {
	return theme.EmptyLineBg(width) + "\n" + theme.PadWithBg("FOOTER", width)
}

// footedFake is a footedView with a body.
func footedFake(body string) *footedView { return &footedView{fakeView{body: body}} }

// fullWidthBody is a body whose every viewport row is full of letters, so both
// sides of and the space under the palette have something to compare.
func fullWidthBody() string { return strings.Repeat("abcdefghij", 40) }

// screen is the rendered view, ANSI stripped, one entry per terminal row.
func screen(a *App) []string { return strings.Split(ansi.Strip(a.View()), "\n") }

// paletteFrame is the palette as found on the screen.
type paletteFrame struct {
	X, Y, Width int
	// Lines are the frame's rows, cut out of the stripped screen at [X, X+Width).
	Lines []string
}

// findPaletteFrame looks for the frame's top border (its title is in it) and
// follows it down to the bottom rule, or to the last screen row when clipped.
func findPaletteFrame(lines []string) (paletteFrame, bool) {
	for y, l := range lines {
		for _, title := range []string{"┌─ Go to ", "┌─ Contexts "} {
			idx := strings.Index(l, title)
			if idx < 0 {
				continue
			}
			rest := l[idx:]
			end := strings.Index(rest, "┐")
			if end < 0 {
				continue
			}
			f := paletteFrame{X: ansi.StringWidth(l[:idx]), Y: y, Width: ansi.StringWidth(rest[:end+len("┐")])}
			for i := y; i < len(lines); i++ {
				seg := ansi.Cut(lines[i], f.X, f.X+f.Width)
				f.Lines = append(f.Lines, seg)
				if i > y && strings.HasPrefix(seg, "└") {
					break
				}
			}
			return f, true
		}
	}
	return paletteFrame{}, false
}

// mustFrame returns the open palette's frame or fails with the screen.
func mustFrame(t *testing.T, a *App) paletteFrame {
	t.Helper()
	lines := screen(a)
	f, ok := findPaletteFrame(lines)
	if !ok {
		t.Fatalf("no palette frame on screen:\n%s", strings.Join(lines, "\n"))
	}
	return f
}

// rule is the bottom rule (status line) or "" when the frame is clipped.
func (f paletteFrame) rule() string {
	if last := f.Lines[len(f.Lines)-1]; strings.HasPrefix(last, "└") {
		return last
	}
	return ""
}

// rows are the suggestion rows: between the ├…┤ separator and the bottom rule.
func (f paletteFrame) rows() []string {
	sep := -1
	for i := 1; i < len(f.Lines); i++ {
		if strings.HasPrefix(f.Lines[i], "├") {
			sep = i
			break
		}
	}
	if sep < 0 || f.rule() == "" {
		return nil
	}
	return f.Lines[sep+1 : len(f.Lines)-1]
}

// input is the row holding the prompt and the typed text.
func (f paletteFrame) input() string {
	if len(f.Lines) < 2 {
		return ""
	}
	return f.Lines[1]
}

// listRows returns the open palette's suggestion rows that carry content: the
// frame keeps a constant height, so its unused rows are blank padding.
func listRows(t *testing.T, a *App) []string {
	t.Helper()
	rows := mustFrame(t, a).rows()
	for len(rows) > 0 && strings.Trim(rows[len(rows)-1], "│ ") == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// filet returns the text of the open palette's bottom rule.
func filet(t *testing.T, a *App) string {
	t.Helper()
	f := mustFrame(t, a)
	if f.rule() == "" {
		t.Fatalf("the palette frame has no bottom rule (clipped?):\n%s", strings.Join(f.Lines, "\n"))
	}
	return f.rule()
}

// sgrRe matches one SGR escape sequence.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// activeParams returns the SGR parameters in force just before byte idx of raw:
// everything since the last reset, joined with ';'.
func activeParams(raw string, idx int) string {
	acc := ""
	for _, loc := range sgrRe.FindAllStringIndex(raw[:idx], -1) {
		params := raw[loc[0]+2 : loc[1]-1]
		if params == "" || params == "0" {
			acc = ""
			continue
		}
		acc += ";" + params
	}
	return acc
}

var fgRe = regexp.MustCompile(`38;2;\d+;\d+;\d+`)

// fgParams is the truecolor foreground parameter lipgloss emits for c.
func fgParams(t *testing.T, c lipgloss.Color) string {
	t.Helper()
	got := fgRe.FindString(lipgloss.NewStyle().Foreground(c).Render("x"))
	if got == "" {
		t.Fatalf("no truecolor foreground for %q (call testutil.TrueColor first)", string(c))
	}
	return got
}

// lastWindowSize returns the last size the view was told, if any.
func lastWindowSize(v *fakeView) (tea.WindowSizeMsg, bool) {
	var last tea.WindowSizeMsg
	found := false
	for _, m := range v.received {
		if size, ok := m.(tea.WindowSizeMsg); ok {
			last, found = size, true
		}
	}
	return last, found
}

// requestsResize reports whether cmd carries a WindowSizeMsg. Opening or
// closing the palette changes no height, so none may be requested.
func requestsResize(cmd tea.Cmd) bool {
	_, ok := testutil.MsgOf[tea.WindowSizeMsg](cmd)
	return ok
}
