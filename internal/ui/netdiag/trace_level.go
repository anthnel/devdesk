package netdiag

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	// traceTitleMargin is what the frame takes from the title line: corners,
	// rules and the spaces around the title.
	traceTitleMargin = 8

	// traceTitleTrail is what follows "Network" in the sub-level's title.
	traceTitleTrail = " › Path › "
)

// startFromForm starts a trace on tg from the Diagnostics form. It reuses this
// model rather than a second instance: the previous run is replaced, then the
// run is launched. It returns nil, and starts nothing, when the platform cannot
// trace. The Cmd does the I/O only (Rule 110).
func (pm *PathModel) startFromForm(tg pathTarget) tea.Cmd {
	if !pm.launchable().Enabled() {
		return nil
	}
	pm.ready(tg)
	_, cmd := pm.launch(tg)
	return cmd
}

// ready puts the model in its ready state on tg: nothing is sent, the source is
// this machine and the previous run is forgotten.
func (pm *PathModel) ready(tg pathTarget) {
	pm.state = pathReady
	pm.target = tg
	pm.source = hostSource()
	pm.picker = nil
	pm.clearRun()
	pm.footer.Clear()
}

// openTrace enters the sub-level on tg, remembering where esc leads back to.
func (m *Model) openTrace(origin ViewState, tg pathTarget) {
	m.traceOrigin = origin
	m.state = StateTrace
	m.activeTab = tabDiagnostics
	m.pathModel.ready(tg)
}

// inTrace reports whether the trace sub-level is on screen.
func (m *Model) inTrace() bool {
	return m.activeTab == tabDiagnostics && m.state == StateTrace
}

// handleKeyTrace is the keyboard of the sub-level: esc goes back (or cancels a
// run), everything else is the path model's, which ignores letters. While a
// trace runs only the table's navigation passes: the running mode lists esc
// alone.
func (m *Model) handleKeyTrace(msg tea.KeyPressMsg) (*Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		return m.escFromTrace()
	}
	if m.pathModel.running() && !isPathNavKey(key) {
		return m, nil
	}
	var cmd tea.Cmd
	m.pathModel, cmd = m.pathModel.handleKey(msg)
	return m, cmd
}

// escFromTrace cancels a running trace; otherwise it goes back to where the
// sub-level was opened from, with its fields, focus, filter and results kept.
func (m *Model) escFromTrace() (*Model, tea.Cmd) {
	if m.pathModel.running() {
		_, cmd := m.pathModel.cancelRun()
		return m, cmd
	}
	m.state = m.traceOrigin
	if m.traceOrigin == StateInput {
		m.restoreFormFocus()
	}
	return m, nil
}

// restoreFormFocus puts the cursor back where focusedField says it is, so only
// that field blinks.
func (m *Model) restoreFormFocus() {
	for _, in := range []*textinput.Model{&m.targetInput, &m.portInput, &m.dnsServerInput} {
		in.Blur()
	}
	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Focus()
	case fieldPort:
		m.portInput.Focus()
	case fieldDNSServer:
		m.dnsServerInput.Focus()
	}
}

// traceTitle is "Network › Path › host", the host cut with an ellipsis so the
// line never outgrows the frame.
func (m *Model) traceTitle(base string) string {
	host := m.pathModel.target.Host
	if m.width > 0 {
		avail := m.width - traceTitleMargin - ansi.StringWidth(base+traceTitleTrail)
		host = ansi.Truncate(host, max(avail, 1), "…")
	}
	return base + titleTrailStyle().Render(traceTitleTrail+host)
}

// traceShortcuts is the only constructor of the sub-level's shortcuts. The
// selector and a running trace are modes and replace the list; ready and done
// share one key set, with greyed entries (Rule 130).
func (m *Model) traceShortcuts() shortcut.Shortcuts {
	pm := m.pathModel
	if pm.pickerOpen() {
		return shortcut.Shortcuts{
			{Key: "enter", Description: "Trace from it"},
			{Key: "esc", Description: "Close"},
		}
	}
	if pm.running() {
		return shortcut.Shortcuts{{Key: "esc", Description: "Cancel the trace"}}
	}
	enter := "Trace the path"
	if pm.state == pathDone {
		enter = "Trace again"
	}
	esc := "Back to the results"
	if m.traceOrigin == StateInput {
		esc = "Back to the form"
	}
	return shortcut.Shortcuts{
		{Key: "tab", Description: "Switch tab"},
		{Key: "enter", Description: enter, Disabled: !pm.launchable().Enabled()},
		{Key: pathSourceKey, Description: "Trace from…", Disabled: !pm.sourceChoosable().Enabled()},
		{Key: "esc", Description: esc},
		{Key: "?", Description: "Help"},
	}
}

// titleTrailStyle colors what trails "Network" in the title.
func titleTrailStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.ColorPrimary).Background(theme.ColorBackground)
}
