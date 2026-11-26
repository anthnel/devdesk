// Package about is the `:about` screen — what this build of DevDesk is, and
// where it keeps its files.
//
// What the view shows is deliberately **the binary, not the machine**. The
// versions of Trivy, gitleaks or Docker are environment state: they change
// without DevDesk being rebuilt, they require going to fetch them, and the
// dashboard already shows them. Nothing here is measured — everything is
// known at startup — which is also why the screen offers no refresh: there
// is nothing to reload.
package about

import (
	"log"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthnel/devdesk/internal/browser"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/version"
)

// SourceURL is where this project is developed.
const SourceURL = "https://github.com/anthnel/devdesk"

// HelpURL is the online documentation, opened by `W`.
const HelpURL = "https://anthnel.github.io/devdesk/"

// openBrowser is the one place this package reaches the real browser. Tests
// replace it; a source guard fails on any other call to browser.Open.
var openBrowser = browser.Open

// BrowserOpenedMsg reports the outcome of opening the online help. Update
// turns it into a footer message; the command that opens the browser never
// touches the model (Rule 110).
type BrowserOpenedMsg struct {
	Error error
}

// field is one label/value line. An empty Value is rendered as "unknown"
// rather than as a blank, so a missing answer reads as a missing answer.
type field struct {
	Label string
	Value string
}

// section is a titled group of fields.
type section struct {
	Title  string
	Fields []field
}

// Model is the about screen. It holds no live state: the sections are computed
// once in New and never change, so Update only ever moves the scroll offset.
type Model struct {
	width, height int
	offset        int
	sections      []section
	footer        sharedcomponents.FooterMessage
}

// New builds the screen from the running build and the current configuration.
func New(cfg *config.Config) Model {
	return Model{sections: sections(version.Get(), cfg)}
}

// sections assembles what the screen shows, in reading order.
func sections(info version.Info, cfg *config.Config) []section {
	build := []field{
		{Label: "Version", Value: info.Version},
		{Label: "Commit", Value: commitField(info)},
		{Label: "Built", Value: buildDate(info.Date)},
		{Label: "Go", Value: info.Go},
		{Label: "Platform", Value: info.Platform},
	}

	return []section{
		{Title: "Build", Fields: build},
		{Title: "Paths", Fields: paths(cfg)},
		{Title: "Project", Fields: []field{
			{Label: "Source", Value: SourceURL},
			{Label: "Documentation", Value: HelpURL},
		}},
	}
}

// commitField names the commit, and says when the tree it was built from had
// uncommitted changes — a dirty build is not reproducible from its SHA, which
// is exactly what someone reading this screen to report a bug needs to know.
func commitField(info version.Info) string {
	if info.Dirty && info.Commit != version.Unknown {
		return info.Commit + " (modified)"
	}
	return info.Commit
}

// buildDate renders an RFC 3339 stamp as a readable UTC minute.
//
// The raw string is rendered as-is if it fails to parse: it comes from a
// `-ldflags` that nothing validates, so refusing it would display "unknown"
// for information that is actually there.
func buildDate(raw string) string {
	if raw == "" || raw == version.Unknown {
		return version.Unknown
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return parsed.UTC().Format("2006-01-02 15:04 MST")
}

// paths lists the directories DevDesk reads and writes.
//
// A path the application cannot resolve is rendered "unknown" rather than
// omitted: the missing line would suggest the file does not exist, when
// what actually failed is resolving the home directory.
func paths(cfg *config.Config) []field {
	dir, err := config.ConfigDir()
	if err != nil {
		return []field{{Label: "Config", Value: version.Unknown}}
	}

	log := version.Unknown
	if cfg != nil && cfg.App.LogFile != "" {
		log = cfg.App.LogFile
	}

	return []field{
		{Label: "Config", Value: dir},
		{Label: "Themes", Value: filepath.Join(dir, "themes")},
		{Label: "Cache", Value: filepath.Join(dir, "cache")},
		{Label: "Log", Value: log},
	}
}

// Init implements tea.Model. Nothing is fetched, so there is no command.
func (m Model) Init() tea.Cmd { return nil }

// Update handles resizing, scrolling and the outcome of opening the online help.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.offset = m.clampOffset(m.offset)
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case BrowserOpenedMsg:
		return m.handleBrowserOpened(msg)
	}
	// The component consumes the expiration addressed to it (Rule 128). This
	// screen never posts a message itself; it receives one through the
	// router's broadcast (components.PostFooterMsg), which is the only way
	// the router has into a footer.
	m.footer.Handle(msg)
	return m, nil
}

// handleBrowserOpened reports in the footer how opening the online help went.
// A failure repeats the address, so it can be typed by hand where no browser
// can be started (SSH, no desktop).
func (m Model) handleBrowserOpened(msg BrowserOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [about] open browser: %v", msg.Error)
		return m, m.footer.Error("Could not open the browser, go to " + HelpURL)
	}
	return m, m.footer.Info("Opened the online help in your browser")
}

// openHelp is the command behind `W`. It only calls the opener and reports
// the result as a message.
func openHelp() tea.Msg {
	return BrowserOpenedMsg{Error: openBrowser(HelpURL)}
}

// handleKey moves the scroll offset, and opens the online help on `W`. The viewport is driven by hand rather
// than by bubbles/viewport, whose default KeyMap carries the vim aliases this
// project removed (Rule 111).
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		m.offset = m.clampOffset(m.offset - 1)
	case "down":
		m.offset = m.clampOffset(m.offset + 1)
	case "pgup":
		m.offset = m.clampOffset(m.offset - m.height)
	case "pgdown":
		m.offset = m.clampOffset(m.offset + m.height)
	case "home":
		m.offset = 0
	case "end":
		m.offset = m.clampOffset(len(m.lines()))
	case keymap.Web:
		return m, openHelp
	}
	return m, nil
}

// clampOffset keeps the offset inside what there is to scroll. A body shorter
// than the viewport has nothing to scroll, and reports an offset of zero
// rather than a negative one.
func (m Model) clampOffset(offset int) int {
	maxOffset := len(m.lines()) - m.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// InEditMode implements the router's contract. Nothing on this screen takes
// text, so `:` and every other global key always reaches the router.
func (m Model) InEditMode() bool { return false }
