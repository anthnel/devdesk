package app

import (
	"errors"
	"io"
	"log"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
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

// router builds an App hosting view as the dashboard, laid out at 180x40.
func router(t *testing.T, view tea.Model) *App {
	t.Helper()
	return routerAt(t, view, 180, 40)
}

func routerAt(t *testing.T, view tea.Model, width, height int) *App {
	t.Helper()
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
		commandInput:     newCommandInput(),
		completionEngine: command.NewCompletionEngine(),
		jobs:             jobs.New(),
		width:            width,
		height:           height,
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
