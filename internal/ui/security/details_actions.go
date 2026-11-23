package security

import (
	"errors"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/clipboard"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// The finding detail acts (DEVDESK-751). Its keys read the finding on screen
// (subjectFinding), never the table row behind it, and each one is decided by an
// availability the header and the handler share (Rule 130). DevDesk never runs
// the command under Run: Y copies it, and the user runs it.

// reasonNoCommand is why Y is greyed: the finding has nothing to copy.
const reasonNoCommand = "This finding has no command to run"

// writeClipboard is the clipboard write, a variable so a test can take it: the
// real one writes to the machine's clipboard. TestMain replaces it.
var writeClipboard = clipboard.WriteAll

// FixCommandCopiedMsg reports the clipboard write of the command under Run
// (Rule 109).
type FixCommandCopiedMsg struct {
	Command string
	Err     error
}

// subjectFinding is what a results or details action acts on: the finding on
// screen in the details state, the selected row in the results state. A re-scan
// puts the table back at the top, so the detail must never read the cursor.
func (m Model) subjectFinding() (scan.Finding, bool) {
	switch m.state {
	case StateDetails:
		if m.selectedFinding == nil {
			return scan.Finding{}, false
		}
		return *m.selectedFinding, true
	case StateResults:
		return m.findingsTable.Selected()
	}
	return scan.Finding{}, false
}

// canOpenFindingFile says whether enter can open the subject's file. It looks at
// the path only: whether the file exists is for the viewer to say.
func (m Model) canOpenFindingFile() shortcut.Availability {
	f, ok := m.subjectFinding()
	if !ok {
		return shortcut.Unavailable(reasonNoFinding)
	}
	_, a := findingFilePath(f, m.targetPath, m.targetOnDisk())
	return a
}

// canCopyFixCommand says whether Y has a command to copy.
func (m Model) canCopyFixCommand() shortcut.Availability {
	f, ok := m.subjectFinding()
	if !ok {
		return shortcut.Unavailable(reasonNoFinding)
	}
	if _, ok := runCommand(f); !ok {
		return shortcut.Unavailable(reasonNoCommand)
	}
	return shortcut.Availability{}
}

// FindingFileRefusedMsg reports that enter did not open the finding's file, and
// why (Rule 109).
type FindingFileRefusedMsg struct{ Reason string }

// openFindingFile asks the router to open the subject's file in the viewer. The
// Cmd follows the links of the path first (findingFilePath only reads it): a
// link that leaves the scanned directory is refused with a reason.
func (m Model) openFindingFile() (tea.Model, tea.Cmd) {
	if a := m.canOpenFindingFile(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	f, _ := m.subjectFinding()
	path, _ := findingFilePath(f, m.targetPath, m.targetOnDisk())
	root := m.targetPath
	return m, func() tea.Msg {
		if !staysInside(root, path) {
			return FindingFileRefusedMsg{Reason: reasonFileLinkOutsideTarget}
		}
		return uiviewer.OpenRequestMsg{Source: viewer.NewFileSource(path)}
	}
}

// staysInside says whether path, with every link on it followed, is still under
// root, with every link on it followed too. A part of the path that does not
// exist is not a link: the existing part is resolved and the rest kept, so a
// missing file is still for the viewer to report. A path or a root that cannot be
// resolved for another reason (a permission) is not refused: the viewer cannot
// read it either.
func staysInside(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return true
	}
	realPath, err := evalExisting(path)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(realRoot, realPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// evalExisting is filepath.EvalSymlinks on the longest existing prefix of path,
// followed by what does not exist.
func evalExisting(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return resolved, err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	resolved, err = evalExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

// copyFixCommand puts the command under Run on the clipboard, whole. It never
// runs it. The command is copied into the Cmd (Rule 110).
func (m Model) copyFixCommand() (tea.Model, tea.Cmd) {
	if a := m.canCopyFixCommand(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	f, _ := m.subjectFinding()
	command, _ := runCommand(f)
	return m, func() tea.Msg {
		return FixCommandCopiedMsg{Command: command, Err: writeClipboard(command)}
	}
}

// handleFixCommandCopied reports the clipboard write: a notice on success, an
// error without its cause on failure (Rule 128).
func (m Model) handleFixCommandCopied(msg FixCommandCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [security] copy command: %v", msg.Err)
		return m, m.footer.Error("Failed to copy the command — check logs")
	}
	return m, m.footer.Info("Command copied to the clipboard")
}

// refreshDetails rebuilds the detail on the result just put on screen, or closes
// it when the finding is gone. It reports whether the detail is still open. The
// reason a detail closes is already in the footer: the verdict that brought the
// result says the rule is cleared, or still reported.
func (m *Model) refreshDetails() bool {
	if m.state != StateDetails {
		return false
	}
	if m.selectedFinding != nil && m.result != nil {
		for _, f := range m.result.Findings {
			if sameFinding(f, *m.selectedFinding) {
				m.selectedFinding = &f
				m.detailsViewport.SetContent(m.buildDetailsContent())
				return true
			}
		}
	}
	m.state = StateResults
	m.selectedFinding = nil
	return false
}

// verifyingStatus is the footer status while a fix waits on its re-scan: a state
// of the screen with the shared activity dot, derived on every frame and gone
// with the verdict. Only the target being verified says it.
func (m Model) verifyingStatus() (sharedcomponents.Status, bool) {
	if v := m.misconfigVerifying; v == nil || v.Target != m.targetPath {
		return sharedcomponents.Status{}, false
	}
	return sharedcomponents.Status{Text: "Re-scanning to confirm the fix...", Activity: true}, true
}
