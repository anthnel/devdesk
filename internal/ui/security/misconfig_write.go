package security

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/jobs"
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
	// Rule is the AVD id, which the verification re-scan is checked against.
	// Body is what the confirmation shows — the fix's title and the diff — and
	// the two are separate fields because one is read by a human and the other
	// compared against a later scan.
	Rule string
	Body string
	// Instance says which occurrence of the rule, for the same reason — see
	// instanceOf.
	Instance string
	Err      error
}

// MisconfigFixWrittenMsg reports the write.
type MisconfigFixWrittenMsg struct {
	Target   string
	File     string
	Rule     string
	Instance string
	Err      error
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
		return MisconfigFixPreparedMsg{Target: target, File: file, Rule: f.ID, Instance: instanceOf(f), Body: rule.Title + "\n\n" + diff}
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
	m.misconfigRule = msg.Rule
	m.misconfigInstance = msg.Instance
	// Rule 104: the safe answer is the default — ConfirmModal opens on No.
	m.confirmModal = sharedcomponents.NewConfirmModal("Fix misconfiguration", misconfigConfirmationText(msg.File, msg.Body))
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
	rule, instance := m.misconfigRule, m.misconfigInstance
	m.misconfigPending = nil
	m.misconfigRule = ""
	m.misconfigInstance = ""
	m.confirmModal = nil
	if file == nil {
		return m, nil
	}
	return m, writeMisconfigFixCmd(m.targetPath, rule, instance, *file)
}

func writeMisconfigFixCmd(target, rule, instance string, f preparedWrite) tea.Cmd {
	return func() tea.Msg {
		err := patch.WriteIfUnchanged(f.Path, f.Original, f.Updated)
		return MisconfigFixWrittenMsg{Target: target, File: f.File, Rule: rule, Instance: instance, Err: err}
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
	// The file changed; the result did not. A re-scan is what turns "written"
	// into "fixed", so it starts here rather than being left to the user — as a
	// job, visible in `:jobs` and stoppable with K.
	v := misconfigVerify{Target: msg.Target, Rule: msg.Rule, File: msg.File, Instance: msg.Instance}
	m.misconfigVerifying = &v
	scan := m.startMisconfigVerification(v)
	footer := m.footer.Info(fmt.Sprintf("Fixed %s — re-scanning to confirm %s is gone", msg.File, msg.Rule))
	return m, tea.Batch(scan, footer)
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

// ── Verifying (§3.78, phase B3) ──────────────────────────────────────────────
//
// A written file is not a fixed one. The rule that was faulted is still in the
// result on screen, because that result was measured before the edit — and the
// whole discipline §3.2 set for base images is that a fix is judged by a
// re-scan, never by the confidence of whatever produced it. So the write starts
// one, and the verdict it reports is binary: the rule is gone, or it is not.
//
// The scan is an ordinary job (§3.58), which is what makes starting it on the
// user's behalf acceptable: it shows up in `:jobs` with a label saying why, and
// `K` stops it. Nothing is hidden and nothing is unstoppable.

// misconfigVerify is the fix waiting on a re-scan to say whether it worked.
type misconfigVerify struct {
	Target   string
	Rule     string
	File     string
	Instance string
}

// MisconfigVerifiedMsg is the verdict: whether the rule the fix was about is
// still reported for that file.
type MisconfigVerifiedMsg struct {
	Target  string
	Rule    string
	File    string
	Cleared bool
	// Result is the scan the verdict was read from, so the tab can stop showing
	// findings the edit has already dealt with.
	Result *scan.Result
	Err    error
}

// startMisconfigVerification re-scans the target the fix was written into.
//
// It runs even when the catalog is sure of itself, because being sure is not
// the same as having measured — and the cases where a rule survives its own fix
// are exactly the ones nobody predicted.
func (m Model) startMisconfigVerification(v misconfigVerify) tea.Cmd {
	if m.scanningTarget(v.Target) {
		// Something is already measuring this target; its result will answer.
		return nil
	}
	job := inventoryScanJob{Kind: kindRepo, Name: v.Target}
	opts := m.scanOptions()
	return jobs.StartInContext(
		jobs.NewRun(jobs.KindScan, command.ViewSecurity, "", "verify fix", v.Target),
		func(contextName string) tea.Cmd {
			return rescanCmd([]inventoryScanJob{job}, opts, contextName)
		},
	)
}

// verifyMisconfigCmd reads the result the re-scan stored and answers whether the
// rule is still there. It runs off Update (Rule 110).
func verifyMisconfigCmd(v misconfigVerify) tea.Cmd {
	return func() tea.Msg {
		result, err := cache.ReadWorkspaceScanResult(v.Target)
		if err != nil {
			return MisconfigVerifiedMsg{Target: v.Target, Rule: v.Rule, File: v.File, Err: err}
		}
		return MisconfigVerifiedMsg{
			Target: v.Target, Rule: v.Rule, File: v.File,
			Cleared: !holdsRule(result, v.Rule, v.File, v.Instance), Result: result,
		}
	}
}

// holdsRule says whether a result still reports this rule for this file —
// and, when instance is set, this occurrence of it.
//
// It matches through remediation.RuleKey rather than on the string, for the
// reason the catalog does: Trivy spells one rule two ways, and a comparison
// that missed the other spelling would report every fix as successful.
//
// The occurrence matters for a manifest (§3.80): a Deployment with two
// containers is flagged for KSV-0001 twice in one file, and the fix edits one
// of them. Judged on the rule and the file alone, the untouched container
// would read as the fix having failed.
func holdsRule(result *scan.Result, rule, file, instance string) bool {
	if result == nil {
		return false
	}
	want := remediation.RuleKey(rule)
	for _, f := range result.Findings {
		if scan.Categorize(f) != scan.CategoryMisconfiguration {
			continue
		}
		if f.File == file && remediation.RuleKey(f.ID) == want && (instance == "" || instanceOf(f) == instance) {
			return true
		}
	}
	return false
}

// instanceOf identifies one occurrence of a rule in a file, without its line,
// which the fix itself moves. Trivy words the occurrence in Message ("Container
// 'api' of Deployment 'web' should set…"), kubeconform in Title
// ("Deployment/web: …"); the pair covers both.
func instanceOf(f scan.Finding) string {
	return f.Title + "\n" + f.Message
}

// handleMisconfigVerified reports the verdict, and swaps in the result it was
// read from.
//
// The swap is what keeps the screen from contradicting the footer: without it a
// line saying the rule is gone would sit above a table still listing it.
func (m Model) handleMisconfigVerified(msg MisconfigVerifiedMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.targetPath {
		return m, nil
	}
	m.misconfigVerifying = nil
	if msg.Err != nil {
		log.Printf("ERROR [security/misconfig] verify %s: %v", msg.Rule, msg.Err)
		return m, m.footer.Error("Could not read the verification scan — check logs")
	}
	if msg.Result != nil {
		m.setResult(msg.Result)
	}
	if !msg.Cleared {
		// Not a failure: the file was written, and the rule still fires. Saying
		// so is the only honest answer, and it is why the scan exists.
		return m, m.footer.Warn(fmt.Sprintf("%s still reported in %s after the fix", msg.Rule, msg.File))
	}
	return m, m.footer.Info(fmt.Sprintf("%s cleared in %s — confirmed by a re-scan", msg.Rule, msg.File))
}
