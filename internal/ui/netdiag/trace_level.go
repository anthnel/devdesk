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
	// reasonTraceBusy is the footer refusal of Trace Path while the Path tab is
	// already tracing. The button stays lit: a running trace is not a greyed-out
	// cause (Rule 130).
	reasonTraceBusy = "A path trace is already running — esc cancels it on the Path tab"

	// traceTitleMargin is what the frame takes from the title line: corners,
	// rules and the spaces around the title.
	traceTitleMargin = 8

	// traceTitleTrail is what follows "Network" in the sub-level's title.
	traceTitleTrail = " › Path › "
)

// startFromForm starts a trace on tg from the Diagnostics form. It reuses this
// model rather than a second instance: the field and the previous run are
// replaced, then the run is launched. It returns nil, and starts nothing, when
// the platform cannot trace. The Cmd does the I/O only (Rule 110).
func (pm *PathModel) startFromForm(tg pathTarget) tea.Cmd {
	if !pm.launchable().Enabled() {
		return nil
	}
	_ = pm.prefill(tg.Host) // the run blurs the field again, so its focus Cmd is moot
	_, cmd := pm.launch(tg)
	return cmd
}

// handleKeyTrace is the keyboard of the sub-level. Only esc and the table's
// navigation act: every other key is dropped here and never reaches the Path
// model, whose key handler would turn its hidden field live again after a failed
// run.
func (m *Model) handleKeyTrace(msg tea.KeyPressMsg) (*Model, tea.Cmd) {
	key := msg.String()
	switch {
	case key == "esc":
		return m.escFromTrace()
	case isPathNavKey(key):
		return m, m.pathModel.table.Update(msg)
	}
	return m, nil
}

// escFromTrace cancels a running trace; once it has ended, it goes back to the
// form with its fields, its focus and the Diagnostics results untouched.
func (m *Model) escFromTrace() (*Model, tea.Cmd) {
	if m.pathModel.running() {
		_, cmd := m.pathModel.cancelRun()
		return m, cmd
	}
	m.state = StateInput
	m.restoreFormFocus()
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

// traceShortcuts: the running trace replaces the list, an ended one keeps it
// short. Nothing here relaunches or picks a source (Rule 138).
func (m *Model) traceShortcuts() shortcut.Shortcuts {
	if m.pathModel.running() {
		return shortcut.Shortcuts{{Key: "esc", Description: "Cancel the trace"}}
	}
	return shortcut.Shortcuts{
		{Key: "tab", Description: "Switch tab"},
		{Key: "esc", Description: "Back to the form"},
		{Key: "?", Description: "Help"},
	}
}

// titleTrailStyle colors what trails "Network" in the title.
func titleTrailStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.ColorPrimary).Background(theme.ColorBackground)
}
