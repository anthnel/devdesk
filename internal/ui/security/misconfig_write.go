package security

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Fixing a misconfiguration in place (§3.78, phase B).
//
// This is the same act as writing a base image (phase C of §3.2) applied to a
// second class of finding, so it is the same key, the same confirmation that
// defaults to No, and the same write: the bytes at one located range and
// nothing else.
//
// What differs is that only **some** rules can be fixed. The catalog in
// internal/remediation is deliberately finite, and a rule outside it is not a
// failure — it is the case the MCP server covers by handing a calling agent the
// span and the wording. So ctrl+o greys out here far more often than it does on
// the Remediation tab, and that greying is what makes the boundary between the
// two paths visible instead of guessed at.

// ── Reasons (Rule 130) ───────────────────────────────────────────────────────

const (
	reasonNotMisconfigTab = "Built-in fixes belong to the Misconfigurations tab"
	reasonFileNotOnDisk   = "This target is an image — its files are inside it, not on disk"
)

// ── Messages (Rule 109) ──────────────────────────────────────────────────────

// MisconfigFixPreparedMsg is the answer to computing the fix: the file as it
// would become, and what git says about it. It carries the bytes the
// confirmation is about, so the write that follows is that one.
type MisconfigFixPreparedMsg struct {
	Target string
	File   preparedWrite
	Rule   string
	Err    error
}

// MisconfigFixWrittenMsg reports the write.
type MisconfigFixWrittenMsg struct {
	Target string
	File   string
	Rule   string
	Err    error
}

// ── Availability ─────────────────────────────────────────────────────────────

// canFixMisconfig is the one computation behind the shortcut column and the
// handler (Rule 130). It answers in the order the user would ask: am I in the
// right place, is there a row, can its file be written at all, and is this rule
// one the catalog knows.
//
// The last question runs the fix itself. That is deliberate: a rule can be in
// the catalog and still decline this instance — a base image whose distribution
// cannot be identified, a stage that already sets a user — and greying out on
// "the catalog has an entry" while the handler then refuses would be exactly
// the two computations Rule 130 forbids.
func (m Model) canFixMisconfig() shortcut.Availability {
	if m.activeTab != TabMisconfig {
		return shortcut.Unavailable(reasonNotMisconfigTab)
	}
	f, ok := m.findingsTable.Selected()
	if !ok {
		return shortcut.Unavailable(reasonNoFinding)
	}
	if m.result == nil || m.result.TargetType != scan.TargetDirectory {
		return shortcut.Unavailable(reasonFileNotOnDisk)
	}
	if _, ok := remediation.FixFor(f); !ok {
		return shortcut.Unavailable(remediation.ReasonNoFixForRule)
	}
	// Reading the file here would be I/O on a path the header calls every
	// frame, so the declines that need the content are left to the Cmd and
	// reach the footer. What is settled here is everything the finding alone
	// can answer.
	return shortcut.Availability{}
}

// ── Preparing ────────────────────────────────────────────────────────────────

// prepareMisconfigFix starts the fix by computing it: nothing is asked of the
// user until the file has been read, rewritten and put to git, so the
// confirmation can state what it is about.
func (m Model) prepareMisconfigFix() (tea.Model, tea.Cmd) {
	if a := m.canFixMisconfig(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	f, _ := m.findingsTable.Selected()
	return m, prepareMisconfigFixCmd(m.targetPath, f)
}

// prepareMisconfigFixCmd reads the file, asks the catalog for the edits and
// applies them. The finding is copied in (Rule 110).
//
// A rule that declines here is not an error: it is an answer, and it reaches the
// footer as a warning rather than as a failure.
func prepareMisconfigFixCmd(target string, f scan.Finding) tea.Cmd {
	return func() tea.Msg {
		rule, ok := remediation.FixFor(f)
		if !ok {
			return MisconfigFixPreparedMsg{Target: target, Rule: f.ID, Err: errDeclined{remediation.ReasonNoFixForRule}}
		}
		path := filepath.Join(target, filepath.FromSlash(f.File))
		original, err := readFile(path)
		if err != nil {
			return MisconfigFixPreparedMsg{Target: target, Rule: f.ID, Err: err}
		}
		edits, reason := rule.Fix(original, f)
		if reason != "" {
			return MisconfigFixPreparedMsg{Target: target, Rule: f.ID, Err: errDeclined{reason}}
		}
		updated, err := patch.Rewrite(original, edits)
		if err != nil {
			return MisconfigFixPreparedMsg{Target: target, Rule: f.ID, Err: fmt.Errorf("%s: %w", f.File, err)}
		}
		diff, err := patch.Diff(f.File, original, edits)
		if err != nil {
			return MisconfigFixPreparedMsg{Target: target, Rule: f.ID, Err: fmt.Errorf("%s: %w", f.File, err)}
		}
		file := preparedWrite{File: f.File, Path: path, Original: original, Updated: updated}
		if file.State, err = git.StateOf(path); err != nil {
			log.Printf("ERROR [security/misconfig] git state of %s: %v", f.File, err)
			file.StateErr = true
		}
		return MisconfigFixPreparedMsg{Target: target, File: file, Rule: rule.Title + "\n\n" + diff}
	}
}

// errDeclined carries a catalog refusal through the same channel as a failure,
// so the handler has one message to read, and tells the two apart by type rather
// than by inspecting a string.
type errDeclined struct{ reason string }

func (e errDeclined) Error() string { return e.reason }

// ── Confirming and writing ───────────────────────────────────────────────────

func (m Model) handleMisconfigFixPrepared(msg MisconfigFixPreparedMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.targetPath {
		return m, nil // an answer about a result no longer on screen
	}
	if d, ok := msg.Err.(errDeclined); ok {
		// Nothing failed: the catalog will not fix this instance, and says why.
		return m, m.footer.Warn(d.reason)
	}
	if msg.Err != nil {
		log.Printf("ERROR [security/misconfig] prepare fix: %v", msg.Err)
		return m, m.footer.Error("Could not prepare the fix — the file may have changed, check logs")
	}
	m.misconfigPending = &msg.File
	// Rule 104: the safe answer is the default — ConfirmModal opens on No.
	m.confirmModal = sharedcomponents.NewConfirmModal("Fix misconfiguration", misconfigConfirmationText(msg.File, msg.Rule))
	return m, nil
}

// misconfigConfirmationText shows the diff itself, not a summary of it.
//
// A base image bump is one token replaced by another and reads as a sentence; a
// misconfiguration fix inserts a block, and no sentence conveys what is about to
// land in the file. What the user agrees to is the diff, so the diff is what the
// modal shows — followed by what git will and will not be able to undo.
func misconfigConfirmationText(f preparedWrite, body string) string {
	return body + "\n" + fmt.Sprintf("%s: %s\n", f.File, gitAdvice(f))
}

// handleMisconfigFixConfirmed runs the write the modal was about.
func (m Model) handleMisconfigFixConfirmed() (tea.Model, tea.Cmd) {
	file := m.misconfigPending
	m.misconfigPending = nil
	m.confirmModal = nil
	if file == nil {
		return m, nil
	}
	return m, writeMisconfigFixCmd(m.targetPath, *file)
}

func writeMisconfigFixCmd(target string, f preparedWrite) tea.Cmd {
	return func() tea.Msg {
		err := patch.WriteIfUnchanged(f.Path, f.Original, f.Updated)
		return MisconfigFixWrittenMsg{Target: target, File: f.File, Err: err}
	}
}

func (m Model) handleMisconfigFixWritten(msg MisconfigFixWrittenMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.targetPath {
		return m, nil
	}
	if msg.Err != nil {
		cmd := m.reportFailedFix(msg)
		return m, cmd
	}
	// The finding on screen is still the one the scan reported: the file changed,
	// the result did not. Saying so is the honest message, and it is what sends
	// the user to the re-scan that decides (phase B3).
	return m, m.footer.Info(fmt.Sprintf("Fixed %s — re-scan to confirm the rule is gone", msg.File))
}

// reportFailedFix sets the footer on the model it is called on, which is why it
// takes a pointer: on a copy the message would be set and then thrown away.
func (m *Model) reportFailedFix(msg MisconfigFixWrittenMsg) tea.Cmd {
	if errors.Is(msg.Err, patch.ErrChanged) {
		return m.footer.Error(fmt.Sprintf("%s changed since the preview — review it again", msg.File))
	}
	log.Printf("ERROR [security/misconfig] write %s: %v", msg.File, msg.Err)
	return m.footer.Error(fmt.Sprintf("Failed to write %s — check logs", msg.File))
}
