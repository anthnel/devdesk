package security

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/imagepull"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/trust"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Signatures in the Remediation tab (§3.82). Every image of the tab is asked
// whether its signature holds — the base in use against the rules alone, a
// candidate also against whoever signed that base — in the background, when the
// candidates are known. A candidate is shown whatever its verdict: that a tag
// was republished by someone else is the point. One the verdict blocks cannot
// be chosen, and the same verdict would refuse its pull.

// RemediationSignaturesMsg carries the verdicts of one base image and its
// candidates, keyed by reference as written.
type RemediationSignaturesMsg struct {
	Target  string
	Results map[string]trust.Result
}

// signatureTimeout bounds one entry's checks: a few cosign runs each.
const signatureTimeout = 3 * time.Minute

// signatureSlots caps how many entries are checked at once. Each cosign run
// takes seconds and a registry round trip; a repository with ten Dockerfiles
// should not start forty.
var signatureSlots = make(chan struct{}, 4)

// newPullDeps wires the verification; tests swap it so no registry or cosign
// is reached.
var newPullDeps = imagepull.Default

func (m Model) verifiesImages() bool { return m.config == nil || m.config.Scan.VerifiesImages() }

// checkSignaturesCmds asks for every image of every entry, one Cmd per entry:
// the base first, since a candidate is compared with it.
func checkSignaturesCmds(target string, entries []remediation.Entry, d imagepull.Deps) []tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range entries {
		if e.Image == "" {
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			signatureSlots <- struct{}{}
			defer func() { <-signatureSlots }()
			ctx, cancel := context.WithTimeout(context.Background(), signatureTimeout)
			defer cancel()

			results := map[string]trust.Result{e.Image: imagepull.Check(ctx, e.Image, "", d)}
			for _, c := range e.Candidates {
				results[c] = imagepull.Check(ctx, c, e.Image, d)
			}
			return RemediationSignaturesMsg{Target: target, Results: results}
		})
	}
	return cmds
}

// startSignatureChecks marks the tab's images as being checked and returns the
// checks, or nothing when this context does not verify.
func (m *Model) startSignatureChecks() []tea.Cmd {
	if !m.verifiesImages() {
		return nil
	}
	for _, e := range m.remediation.entries {
		if e.Image == "" {
			continue
		}
		m.remediation.verifying[e.Image] = true
		for _, c := range e.Candidates {
			m.remediation.verifying[c] = true
		}
	}
	return checkSignaturesCmds(m.remediation.target, m.remediation.entries, newPullDeps(m.config, m.deps))
}

// handleRemediationSignatures stores the verdicts, and releases a chosen
// candidate its verdict now blocks — it was chosen before the answer came in,
// which Rule 130 allows, and must not be written now that it is in.
func (m Model) handleRemediationSignatures(msg RemediationSignaturesMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.remediation.target {
		return m, nil
	}
	for ref, res := range msg.Results {
		m.remediation.signatures[ref] = res
		delete(m.remediation.verifying, ref)
	}
	var released []string
	for entry, ref := range m.remediation.selected {
		if res, ok := msg.Results[ref]; ok && res.Decision == trust.Block {
			delete(m.remediation.selected, entry)
			released = append(released, ref+": "+res.Reason())
		}
	}
	m.refreshRemediation()
	if len(released) == 0 {
		return m, nil
	}
	sort.Strings(released)
	return m, m.footer.Warn("Released " + strings.Join(released, "; "))
}

// signatureBlock is the refusal a chosen candidate's verdict makes, if any.
func (m Model) signatureBlock(ref string) shortcut.Availability {
	if res, ok := m.remediation.signatures[ref]; ok && res.Decision == trust.Block {
		return shortcut.Unavailable(res.Reason())
	}
	return shortcut.Availability{}
}

// signatureWarning is what the write's confirmation repeats about a candidate
// that may be written but was warned about.
func (m Model) signatureWarning(ref string) string {
	if res, ok := m.remediation.signatures[ref]; ok && res.Decision == trust.Warn {
		return res.Reason()
	}
	return ""
}

// signatureHeader is the results header's field when the check is off.
func (m Model) signatureHeader() (shortcut.HeaderInfo, bool) {
	if m.activeTab != TabRemediation || m.verifiesImages() {
		return shortcut.HeaderInfo{}, false
	}
	return shortcut.HeaderInfo{Key: "Signatures", Value: "off", Style: theme.DimStyle}, true
}
