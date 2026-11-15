package netdiag

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// traceKeyTarget names the target field in the Trace Path validation.
const traceKeyTarget = "target"

// traceable is the one answer to "can a trace start from the form", read by
// the button, the header and the handler (Rule 130).
func (m *Model) traceable() shortcut.Availability {
	return m.pathModel.launchable()
}

// traceTargetErrors is the validation of the target, with the path trace's own
// wording.
func (m *Model) traceTargetErrors() []components.FieldError {
	if _, err := parsePathTarget(m.targetInput.Value()); err != nil {
		return []components.FieldError{{Key: traceKeyTarget, Text: capitalize(err.Error())}}
	}
	return nil
}

// syncTraceMarks underlines the target only while it fails validation. Call it
// from Update() only (Rule 110).
func (m *Model) syncTraceMarks() {
	m.traceMarks.Sync(traceKeyTarget, &m.targetInput, m.traceValidation.Invalid(traceKeyTarget))
}

// revalidateTrace refreshes the target's error after an edit; it says nothing
// until a trace has been refused.
func (m *Model) revalidateTrace() {
	m.traceValidation.Revalidate(m.traceTargetErrors())
	m.syncTraceMarks()
}

// traceFromForm handles enter on the Trace Path button. An unsupported
// platform is refused with its reason, a trace already running with a warning,
// an invalid target with an underline and a footer error; otherwise the trace
// sub-level opens on the Path model and the returned Cmd starts the run.
func (m *Model) traceFromForm() (*Model, tea.Cmd) {
	if a := m.traceable(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	if m.pathModel.running() {
		return m, m.footer.Warn(reasonPathBusy)
	}
	errs := m.traceTargetErrors()
	m.traceValidation.Submit(errs)
	m.syncTraceMarks()
	if len(errs) > 0 {
		return m, nil
	}
	tg, _ := parsePathTarget(m.targetInput.Value()) // accepted by traceTargetErrors
	m.openTrace(StateInput, tg)
	cmd := m.pathModel.startFromForm(tg)
	// Posted after startFromForm: launching clears the sub-level's footer.
	if n := traceUnusedNotice(m.portInput.Value(), m.dnsServerInput.Value()); n != "" {
		cmd = tea.Batch(cmd, m.pathModel.footer.Info(n))
	}
	return m, cmd
}

// traceUnusedNotice says which Diagnostics fields the path trace ignores: it
// probes the path to a host, so Port and DNS Server do not apply. Blank means
// not filled; it returns "" when neither is.
func traceUnusedNotice(port, dns string) string {
	hasPort := strings.TrimSpace(port) != ""
	hasDNS := strings.TrimSpace(dns) != ""
	switch {
	case hasPort && hasDNS:
		return "Port and DNS Server are not used by the path trace"
	case hasPort:
		return "Port is not used by the path trace"
	case hasDNS:
		return "DNS Server is not used by the path trace"
	}
	return ""
}
