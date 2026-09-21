package security

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/dockerfile"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/remediation"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// Writing the chosen bases into the Dockerfiles (§3.2, phase C).
//
// DevDesk proposes; it does not decide. Nothing is written unless the user asks
// (the write key), sees exactly what will change, and confirms — with the
// default answer No. The write is the Dockerfile's bytes at the located ranges
// and nothing else, and it adds no commit, no branch and no push: the
// repository stays the user's own, and what was written is one `git diff` away.

// writeRemediationKey writes the chosen bases. It is a **declared exception** to
// the uppercase vocabulary (keymap.DeclaredExceptions): the action exists on one
// sub-screen, and a global capital would cost more than it returns. ^O is "Write
// Out" in nano, needs no terminal mode that bubbletea leaves off, and — unlike
// ctrl+w, ctrl+s and ctrl+d — is claimed by neither a browser tab, flow control
// nor end-of-file.
const writeRemediationKey = "ctrl+o"

// ── Messages (Rule 109) ──────────────────────────────────────────────────────

// RemediationWritePreparedMsg is the answer to computing the write: every file
// as it would become, and what git says about each. It carries the bytes the
// confirmation is about, so the write that follows is that one.
type RemediationWritePreparedMsg struct {
	Target string
	Files  []preparedWrite
	Err    error
}

// RemediationWrittenMsg reports the write. Written names the files replaced
// before it stopped; Failed the one it stopped on, with Err.
type RemediationWrittenMsg struct {
	Target  string
	Written []string
	Failed  string
	Err     error
}

// ── The plan ─────────────────────────────────────────────────────────────────

// remediationChange is one image moved to another, with where.
type remediationChange struct {
	File  string // relative to the repository, forward slashes
	Line  int
	Stage string
	Edit  dockerfile.Edit
	// DropsDigest: the reference was pinned by digest, which the new tag does
	// not carry.
	DropsDigest bool
}

// preparedWrite is one file, as it is and as it would become.
type preparedWrite struct {
	File     string
	Path     string
	Original []byte
	Updated  []byte
	Changes  []remediationChange
	State    git.FileState
	// StateErr is set when git could not answer, which is not the same as
	// "outside a repository" and is said differently.
	StateErr bool
}

// remediationChanges is what the selection would change, in file order.
func (m Model) remediationChanges() []remediationChange {
	var changes []remediationChange
	for i, e := range m.remediation.entries {
		ref, ok := m.remediation.selected[i]
		if !ok || !e.Stage.Editable {
			continue
		}
		changes = append(changes, remediationChange{
			File: e.File, Line: e.Stage.Line, Stage: e.StageLabel,
			Edit:        dockerfile.Edit{Span: e.Stage.Span, Old: e.Stage.Image, New: ref},
			DropsDigest: remediation.ParseRef(e.Stage.Image).Digest != "",
		})
	}
	return changes
}

// groupByFile keeps the order files first appear in.
func groupByFile(changes []remediationChange) (order []string, byFile map[string][]remediationChange) {
	byFile = map[string][]remediationChange{}
	for _, c := range changes {
		if _, seen := byFile[c.File]; !seen {
			order = append(order, c.File)
		}
		byFile[c.File] = append(byFile[c.File], c)
	}
	return order, byFile
}

func edits(changes []remediationChange) []dockerfile.Edit {
	out := make([]dockerfile.Edit, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Edit)
	}
	return out
}

// ── Choosing ─────────────────────────────────────────────────────────────────

// canSelectCandidate is the one computation behind the shortcut column and the
// handler (Rule 130). Only a candidate that has been scanned can be chosen: a
// bump is proposed with its evidence, and a candidate nobody measured has none.
func (m Model) canSelectCandidate() shortcut.Availability {
	if m.activeTab != TabRemediation {
		return shortcut.Unavailable(reasonNotRemediationTab)
	}
	row, ok := m.remediation.table.Selected()
	switch {
	case !ok:
		return shortcut.Unavailable(reasonNoImageRow)
	case row.Current:
		return shortcut.Unavailable(reasonPickACandidate)
	case !row.Scanned:
		return shortcut.Unavailable(reasonScanFirst)
	case !m.remediation.entries[row.Entry].Stage.Editable:
		if reason := m.remediation.entries[row.Entry].Stage.NoEditReason; reason != "" {
			return shortcut.Unavailable(reason)
		}
		return shortcut.Unavailable(reasonNotEditable)
	}
	return shortcut.Availability{}
}

// toggleCandidate chooses the candidate under the cursor for its stage, or
// releases it. Choosing another one of the same stage replaces the first.
func (m Model) toggleCandidate() (tea.Model, tea.Cmd) {
	if a := m.canSelectCandidate(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	row, _ := m.remediation.table.Selected()
	if m.remediation.selected[row.Entry] == row.Ref {
		delete(m.remediation.selected, row.Entry)
		m.refreshRemediation()
		return m, nil
	}
	if other, clash := m.sharesADefault(row.Entry, row.Ref); clash {
		return m, m.footer.Warn(fmt.Sprintf(
			"Stage %s reads the same ARG and has %s chosen — one base image for both",
			m.remediation.entries[other].StageLabel, m.remediation.selected[other]))
	}
	m.remediation.selected[row.Entry] = row.Ref
	m.refreshRemediation()
	return m, nil
}

// sharesADefault finds another stage of the same file that reads the ARG
// default this one does and has a different base chosen. One ARG is one place in
// the file: two answers for it cannot both be written.
func (m Model) sharesADefault(entry int, ref string) (int, bool) {
	this := m.remediation.entries[entry]
	for i, other := range m.remediation.entries {
		if i == entry || other.File != this.File || other.Stage.Span != this.Stage.Span {
			continue
		}
		if chosen, ok := m.remediation.selected[i]; ok && chosen != ref {
			return i, true
		}
	}
	return 0, false
}

// ── Showing the diff ─────────────────────────────────────────────────────────

func (m Model) canPreviewRemediation() shortcut.Availability {
	switch {
	case m.activeTab != TabRemediation:
		return shortcut.Unavailable(reasonNotRemediationTab)
	case len(m.remediation.selected) == 0:
		return shortcut.Unavailable(reasonNothingChosen)
	}
	return shortcut.Availability{}
}

// showRemediationDiff opens what the chosen bases would change, in the viewer.
// The diff is computed by the source's Load, on a Cmd (Rule 110), from the same
// Rewrite the write uses.
func (m Model) showRemediationDiff() (tea.Model, tea.Cmd) {
	if a := m.canPreviewRemediation(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	source := remediationDiffSource{Root: m.remediation.target, Changes: m.remediationChanges()}
	return m, func() tea.Msg { return uiviewer.OpenRequestMsg{Source: source} }
}

// remediationDiffSource is the change, as the viewer reads it.
type remediationDiffSource struct {
	Root    string
	Changes []remediationChange
}

func (s remediationDiffSource) Name() string { return "diff · base image bumps" }

func (s remediationDiffSource) Kind() viewer.Kind { return viewer.KindPlain }

func (s remediationDiffSource) Load() ([]byte, error) {
	order, byFile := groupByFile(s.Changes)
	var b strings.Builder
	for _, file := range order {
		content, err := readDockerfile(s.Root, file)
		if err != nil {
			return nil, err
		}
		diff, err := dockerfile.Diff(file, content, edits(byFile[file]))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		b.WriteString(diff + "\n")
	}
	return []byte(b.String()), nil
}

// ── Writing ──────────────────────────────────────────────────────────────────

func (m Model) canWriteRemediation() shortcut.Availability {
	switch {
	case m.activeTab != TabRemediation:
		return shortcut.Unavailable(reasonNotRemediationTab)
	case len(m.remediation.selected) == 0:
		return shortcut.Unavailable(reasonNothingChosen)
	}
	return shortcut.Availability{}
}

// prepareRemediationWrite starts the write by computing it: nothing is asked of
// the user until the files have been read and what git says about each is
// known, so the confirmation can state it.
func (m Model) prepareRemediationWrite() (tea.Model, tea.Cmd) {
	if a := m.canWriteRemediation(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	return m, prepareRemediationWriteCmd(m.remediation.target, m.remediationChanges())
}

func readDockerfile(root, rel string) ([]byte, error) {
	return readFile(filepath.Join(root, filepath.FromSlash(rel)))
}

func (m Model) handleRemediationWritePrepared(msg RemediationWritePreparedMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.remediation.target {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [security/remediation] prepare write: %v", msg.Err)
		return m, m.footer.Error("Could not prepare the change — the Dockerfile may have changed, check logs")
	}
	m.remediation.pending = msg.Files
	// Rule 104: the safe answer is the default — ConfirmModal opens on No.
	m.confirmModal = sharedcomponents.NewConfirmModal("Write Dockerfile", confirmationText(msg.Files))
	return m, nil
}

// confirmationText says what will change, and what git will and will not be able
// to undo. "Nothing is lost" is true of the ordinary case and this is where the
// other cases are told apart.
func confirmationText(files []preparedWrite) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Replace the base image in %d file(s)?\n\n", len(files))
	for _, f := range files {
		for _, c := range f.Changes {
			fmt.Fprintf(&b, "%s:%d  %s -> %s\n", f.File, c.Line, c.Edit.Old, c.Edit.New)
			if c.DropsDigest {
				b.WriteString("  (drops the digest pin)\n")
			}
		}
	}
	b.WriteString("\n")
	for _, f := range files {
		fmt.Fprintf(&b, "%s: %s\n", f.File, gitAdvice(f))
	}
	return b.String()
}

func gitAdvice(f preparedWrite) string {
	if f.StateErr {
		return "git could not be read — this write may not be undoable"
	}
	switch f.State {
	case git.FileClean:
		return "tracked and clean — review with git diff, undo with git checkout"
	case git.FileModified:
		return "has uncommitted changes — git checkout would discard them along with this edit"
	case git.FileUntracked:
		return "not tracked by git — this write cannot be undone with git"
	}
	return "not in a git repository — this write cannot be undone with git"
}

// handleRemediationConfirmed runs the write the modal was about.
func (m Model) handleRemediationConfirmed() (tea.Model, tea.Cmd) {
	files := m.remediation.pending
	m.remediation.pending = nil
	m.confirmModal = nil
	return m, writeRemediationCmd(m.remediation.target, files)
}

func (m Model) handleRemediationWritten(msg RemediationWrittenMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.remediation.target {
		return m, nil
	}
	if msg.Err != nil {
		cmd := m.reportFailedWrite(msg)
		return m, cmd
	}
	// What was chosen is written. The Dockerfiles are read again so the table
	// shows the image now in them, whose result is already in the cache.
	m.remediation.selected = map[int]string{}
	m.remediation.phase = phaseIdle
	reread := m.enterRemediation()
	footer := m.footer.Info(fmt.Sprintf("Updated %d Dockerfile(s) — review with git diff", len(msg.Written)))
	return m, tea.Batch(reread, footer)
}

// reportFailedWrite sets the footer on the model it is called on, which is why
// it takes a pointer: on a copy the message would be set and then thrown away.
func (m *Model) reportFailedWrite(msg RemediationWrittenMsg) tea.Cmd {
	if errors.Is(msg.Err, dockerfile.ErrChanged) {
		// Nothing of this file was written: the user confirmed a diff, and the
		// file is no longer the one it was computed from.
		return m.footer.Error(fmt.Sprintf("%s changed since the preview — review it again (%d written)", msg.Failed, len(msg.Written)))
	}
	log.Printf("ERROR [security/remediation] write %s: %v", msg.Failed, msg.Err)
	return m.footer.Error(fmt.Sprintf("Failed to write %s — check logs (%d written)", msg.Failed, len(msg.Written)))
}
