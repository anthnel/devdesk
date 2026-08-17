package viewer

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/viewer"
)

// display is how the document is shown. Two values, not three: "plain text" is
// this pane with the colour off, and giving it a display of its own would make
// one screen reachable two ways — the shape §3.9 removed from the secret
// backend and the deleted `:theme` command.
type display int

const (
	displayTree display = iota
	displayText
)

// Model is the document viewer.
type Model struct {
	config *config.Config
	width  int
	height int

	// source is where the document came from, and it is kept rather than
	// discarded after the first read: reload, follow and the timestamps toggle
	// are all questions for the origin, not for the bytes.
	source  viewer.Source
	doc     viewer.Document
	loading bool
	// spinner turns while the document is being read. The load is reported in
	// the footer, so the pane keeps whatever it was already showing.
	spinner spinner.Model

	display   display
	highlight bool
	wrap      bool
	minLevel  viewer.Level
	// showTimestamps is tracked here rather than asked of the source:
	// WithTimestamps returns a new source instead of mutating one, so nothing is
	// shared with a command already in flight (Rule 110), and there is therefore
	// nothing to read the flag back off.
	showTimestamps bool

	// The tree half.
	tree      datatable.Model[treeRow]
	collapsed map[int]bool

	// The text half. lines is the document cut into token spans; it is rebuilt
	// when the document loads and when `c` toggles, never per frame.
	lines        []docLine
	textViewport viewport.Model
	bar          components.FilterBar
	matchedLines int

	// OriginView is where esc returns to. The router sets it from whatever view
	// asked for the document.
	OriginView command.ViewType

	footer components.FooterMessage
}

// New builds an empty viewer.
//
// It exists so the router can create the view like any other — the contract test
// builds every view and asks it for a title and its shortcuts. On screen it says
// there is nothing open, which is the truth and is only ever reached by opening
// the view without a document.
func New(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		spinner:      s,
		config:       cfg,
		highlight:    true,
		collapsed:    make(map[int]bool),
		tree:         datatable.New(datatable.Config[treeRow]{Columns: treeColumns(), SortColumn: -1}),
		textViewport: viewport.New(0, 0),
		bar:          components.NewFilterBar(),
		display:      displayText,
	}
}

// NewWithSource builds a viewer that will load this source when it starts.
func NewWithSource(cfg *config.Config, source viewer.Source) Model {
	m := New(cfg)
	m.source = source
	m.loading = true
	return m
}

// Name is what the header calls the document.
func (m Model) Name() string {
	if m.doc.Name != "" {
		return m.doc.Name
	}
	if m.source != nil {
		return m.source.Name()
	}
	return ""
}

// hasDocument reports whether anything has been loaded. Most of the keys and
// every shortcut but esc are conditional on it (Rule 130).
func (m Model) hasDocument() bool {
	return m.doc.Name != "" || m.doc.Text != ""
}

// structured reports whether the tree display is available at all.
func (m Model) structured() bool {
	return m.doc.Kind.Structured() && m.doc.Root != nil
}

// isLog reports whether the verbosity filter applies.
func (m Model) isLog() bool { return m.doc.Kind == viewer.KindLog }

// applyDocument installs a freshly parsed document and settles every piece of
// state that depends on it.
//
// One function rather than a handful of assignments at the call sites: a
// document arrives from a first load, from a reload and from a timestamps
// toggle, and a display left pointing at a tree that the new document does not
// have would render an empty pane with nothing to say why.
//
// It returns the footer message's expiry timer, which the caller must pass on:
// a message set without one never clears (Rule 128).
func (m *Model) applyDocument(doc viewer.Document) tea.Cmd {
	m.doc = doc
	m.loading = false
	m.collapsed = make(map[int]bool)
	m.lines = buildLines(doc, m.highlight)

	// A structured document opens on its tree, which is what the request asked
	// for: a .json opens as json, a .xml as xml. Everything else has no tree to
	// open on.
	if m.structured() {
		m.display = displayTree
	} else {
		m.display = displayText
	}

	// A verbosity filter left over from a previous document would hide lines of
	// this one for a reason the user set on something else.
	if !m.isLog() {
		m.minLevel = viewer.LevelUnknown
	}
	m.syncVerbosityToken()

	m.rebuildTree()
	m.tree.GotoTop()
	m.rebuildText()
	m.textViewport.GotoTop()

	// A warning, not an error: nothing failed, the document simply cannot be
	// shown the way its name asked for.
	if doc.ParseErr != nil {
		return m.footer.Warn("Malformed " + doc.Name + " — showing text")
	}
	return nil
}

// toggleHighlight is `c`. It rebuilds the spans, which is the one thing that
// costs anything here — and the reason it is a key rather than something
// recomputed on every frame.
func (m *Model) toggleHighlight() {
	m.highlight = !m.highlight
	m.lines = buildLines(m.doc, m.highlight)
	m.rebuildTree()
	m.rebuildText()
}

// toggleDisplay is `f`. It does nothing for a document with no tree, and the
// shortcut is hidden in that case rather than offered and ignored.
func (m *Model) toggleDisplay() {
	if !m.structured() {
		return
	}
	if m.display == displayTree {
		m.display = displayText
		return
	}
	m.display = displayTree
}

// formatLabel is what the header prints: the kind and the display, and the
// filter when there is one.
func (m Model) formatLabel() string {
	if !m.hasDocument() {
		return "-"
	}
	label := m.doc.Kind.String()
	if m.structured() {
		if m.display == displayTree {
			return label + " · tree"
		}
		return label + " · text"
	}
	if m.isLog() && m.minLevel != viewer.LevelUnknown {
		return label + " · " + minLevelLabel(m.minLevel)
	}
	return label
}
