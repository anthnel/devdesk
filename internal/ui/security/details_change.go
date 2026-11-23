package security

import (
	"errors"
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// The Change section of the finding detail (DEVDESK-753): what the built-in fix
// would write, computed off Update by the Cmd the detail starts when it opens,
// stored by Update, and read from the model by the header, ctrl+o and the Fix
// line, which therefore read no disk (Rule 130).

// maxChangeRows is how many rows of the diff the section shows; the
// confirmation of ctrl+o has all of them.
const maxChangeRows = 8

// labelChange is the label of the section, and noChangeText what it says when
// DevDesk has no change to show (the closed vocabulary of the line).
const (
	labelChange         = "Change: "
	noChangeText        = "none: DevDesk cannot write this fix"
	preparingChange     = "preparing the preview..."
	preparingFooter     = "Preparing the change preview..."
	footerPreviewFailed = "Could not preview the fix — check logs"
)

// previewState is what the stored preview knows. The "expired" state is the
// stale one: previewStale is already the name of the time comparison.
type previewState int

const (
	previewPending  previewState = iota // asked, not answered yet
	previewReady                        // the change was computed
	previewDeclined                     // the rule declines this instance (or the path is refused)
	previewExpired                      // the file changed after the scan
	previewFailed                       // the computation failed (unreadable, gone)
)

// ChangePreviewMsg is the answer to the preview Cmd (Rule 109): the shared
// computation's message, numbered so a late one is ignored.
type ChangePreviewMsg struct {
	Seq      int
	Target   string
	Prepared MisconfigFixPreparedMsg
}

// changePreview is the preview of the finding on screen, kept by Update. The
// zero value is "no request": Seq starts at 1.
type changePreview struct {
	Seq      int
	Target   string
	Finding  scan.Finding
	State    previewState
	Prepared MisconfigFixPreparedMsg
	Failure  string
}

// scannedAt is when the result on screen was measured, the zero time when it
// does not say.
func (m Model) scannedAt() time.Time {
	if m.result == nil {
		return time.Time{}
	}
	return m.result.EndTime
}

// startChangePreview stores a pending preview of the finding on screen and
// returns the Cmd that computes it, or nil when there is nothing to compute: not
// a detail, not a catalog rule, not a directory, or a refusal the finding alone
// settles. It mutates the model, so it is called from Update only; the finding is
// copied into the Cmd (Rule 110).
func (m *Model) startChangePreview() tea.Cmd {
	if m.state != StateDetails || m.selectedFinding == nil {
		return nil
	}
	f := *m.selectedFinding
	if _, a := misconfigFixAvailability(f, m.targetOnDisk()); !a.Enabled() {
		return nil
	}
	m.previewSeq++
	seq, target, scannedAt := m.previewSeq, m.targetPath, m.scannedAt()
	m.changePreview = changePreview{Seq: seq, Target: target, Finding: f, State: previewPending}
	return func() tea.Msg {
		return ChangePreviewMsg{Seq: seq, Target: target, Prepared: computeMisconfigFix(target, f, scannedAt)}
	}
}

// restartChangePreview asks again, for a file the fix just rewrote, and shows
// the section as preparing at once.
func (m *Model) restartChangePreview() tea.Cmd {
	cmd := m.startChangePreview()
	if cmd != nil {
		m.detailsViewport.SetContent(m.buildDetailsContent())
	}
	return cmd
}

// handleChangePreview stores the answer when it is the current one: the number,
// the target and the finding on screen must all be the request's.
func (m Model) handleChangePreview(msg ChangePreviewMsg) (tea.Model, tea.Cmd) {
	p := m.changePreview
	if m.state != StateDetails || m.selectedFinding == nil || p.Seq == 0 || msg.Seq != p.Seq ||
		msg.Target != p.Target || !sameFinding(*m.selectedFinding, p.Finding) {
		return m, nil
	}
	p.Prepared = msg.Prepared
	var cmd tea.Cmd
	switch err := msg.Prepared.Err; {
	case err == nil:
		p.State = previewReady
	case isStale(err):
		p.State = previewExpired
	case declinedReason(err) != "":
		p.State = previewDeclined
	default:
		log.Printf("ERROR [security/misconfig] preview %s: %s", p.Finding.ID, withoutPath(err))
		p.State, p.Failure = previewFailed, previewFailure(p.Finding.File, err)
		cmd = m.footer.Error(footerPreviewFailed)
	}
	m.changePreview = p
	m.detailsViewport.SetContent(m.buildDetailsContent())
	return m, cmd
}

// declinedReason is the reason of a catalog refusal, "" for any other error.
func declinedReason(err error) string {
	var d errDeclined
	if errors.As(err, &d) {
		return d.reason
	}
	return ""
}

// isStale says whether err is the refusal of a file written after the scan.
func isStale(err error) bool { return declinedReason(err) == reasonPreviewStale }

// storedPreview is the stored preview when it is about f.
func (m Model) storedPreview(f scan.Finding) (changePreview, bool) {
	p := m.changePreview
	if m.state != StateDetails || p.Seq == 0 || p.Target != m.targetPath || !sameFinding(p.Finding, f) {
		return changePreview{}, false
	}
	return p, true
}

// previewVerdict is what the stored preview says about ctrl+o, read with no I/O:
// a stale or declined preview greys it with its reason. A pending or failed one
// leaves it lit, since not knowing is not a no (Rule 130): the press computes the
// fix again and reports its own error.
func (m Model) previewVerdict(f scan.Finding) shortcut.Availability {
	p, ok := m.storedPreview(f)
	if !ok {
		return shortcut.Availability{}
	}
	switch p.State {
	case previewExpired, previewDeclined:
		return shortcut.Unavailable(declinedReason(p.Prepared.Err))
	}
	return shortcut.Availability{}
}

// fixStatus is the Fix line of f: the status of the finding, unless the stored
// preview says the built-in fix cannot run.
func (m Model) fixStatus(f scan.Finding) fixStatus {
	s := fixStatusOf(f, m.targetOnDisk())
	if s.State != fixBuiltIn {
		return s
	}
	if a := m.previewVerdict(f); !a.Enabled() {
		return fixStatus{State: fixBuiltInDeclined, Reason: a.Reason}
	}
	return s
}

// previewingStatus says whether the footer announces the preview: while the
// answer for the finding on screen is awaited.
func (m Model) previewingStatus() bool {
	if m.selectedFinding == nil {
		return false
	}
	p, ok := m.storedPreview(*m.selectedFinding)
	return ok && p.State == previewPending
}

// detailsChange is the Change section: the diff the built-in fix would write, a
// version move for a vulnerability, or why there is nothing to show. It is
// written in ColorText alone: the markers + and - carry the meaning.
func (m Model) detailsChange(f scan.Finding, t detailsText) []string {
	if p, ok := m.storedPreview(f); ok {
		return m.detailsPreview(p, t)
	}
	if delta, ok := versionDelta(f); ok {
		return t.field(labelChange, delta, t.text)
	}
	return t.field(labelChange, noChangeText, t.text)
}

// detailsPreview is the section of a stored preview.
func (m Model) detailsPreview(p changePreview, t detailsText) []string {
	switch p.State {
	case previewPending:
		return t.field(labelChange, preparingChange, t.text)
	case previewExpired:
		return t.field(labelChange, "stale preview: "+p.Finding.File+" changed after the scan", t.text)
	case previewFailed:
		return t.field(labelChange, "preview failed: "+p.Failure, t.text)
	case previewDeclined:
		return t.field(labelChange, noChangeText, t.text)
	}
	lines := t.field(labelChange, p.Prepared.File.File, t.text)
	rows, hidden := p.Prepared.Lines, 0
	if len(rows) > maxChangeRows {
		rows, hidden = rows[:maxChangeRows], len(rows)-maxChangeRows
	}
	for _, row := range rows {
		lines = append(lines, t.text.Render(oneLine(row, t.width)))
	}
	if more := moreLines(hidden); more != "" {
		lines = append(lines, t.text.Render(more))
	}
	return append(lines, t.field("Git: ", gitAdvice(p.Prepared.File), t.text)...)
}

// versionDelta is "<pkg> <version> -> <fixed>" for a vulnerability with a fixed
// version, the one scan.PickFixed chooses (the whole FixedIn when it cannot
// choose, as the command DevDesk offers does).
func versionDelta(f scan.Finding) (string, bool) {
	if f.PkgName == "" || f.Version == "" || f.FixedIn == "" {
		return "", false
	}
	fixed := scan.PickFixed(f.Version, f.FixedIn)
	if fixed == "" {
		fixed = f.FixedIn
	}
	return f.PkgName + " " + f.Version + " -> " + fixed, true
}
