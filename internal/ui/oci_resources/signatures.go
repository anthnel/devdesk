package ociresources

import (
	"context"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imagepull"
	"github.com/anthnel/devdesk/internal/trust"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/sigcol"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// What the Images tab says about signature checks (§3.82): nothing when they
// run as configured, and a header field when they do not — off for this
// context, or a trust.yaml that cannot be read, which refuses every pull until
// it is fixed. Each pull reads the file again; this is read when the view
// starts, so the header can say so before anyone presses G.

// TrustPolicyCheckedMsg carries what ~/.devdesk/trust.yaml holds.
type TrustPolicyCheckedMsg struct {
	Rules    int
	Warnings []string
	Err      error
}

// loadTrustPolicy is a variable so tests read no real home directory.
var loadTrustPolicy = func() (trust.Policy, []string, error) {
	path, err := trust.PolicyPath()
	if err != nil {
		return trust.Policy{}, nil, err
	}
	return trust.Load(path)
}

func checkTrustPolicyCmd() tea.Cmd {
	return func() tea.Msg {
		p, warnings, err := loadTrustPolicy()
		return TrustPolicyCheckedMsg{Rules: len(p.Rules), Warnings: warnings, Err: err}
	}
}

func (m Model) handleTrustPolicyChecked(msg TrustPolicyCheckedMsg) (tea.Model, tea.Cmd) {
	m.trustPolicyErr = msg.Err
	for _, w := range msg.Warnings {
		log.Printf("WARN [oci_resources] trust policy: %s", w)
	}
	if !m.verifiesImages() && msg.Rules > 0 {
		log.Printf("WARN [oci_resources] image_verification is off: the %d rule(s) of trust.yaml are not applied", msg.Rules)
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] trust policy: %v", msg.Err)
		if m.verifiesImages() {
			return m, m.footer.Error("trust.yaml is invalid — pulls are refused until it is fixed (check logs)")
		}
	}
	return m, nil
}

func (m Model) verifiesImages() bool { return m.config == nil || m.config.Scan.VerifiesImages() }

// signatureHeader is the Images tab's field, when there is something to say.
func (m Model) signatureHeader() (shortcut.HeaderInfo, bool) {
	switch {
	case !m.verifiesImages():
		return shortcut.HeaderInfo{Key: "Signatures", Value: "off", Style: theme.DimStyle}, true
	case m.trustPolicyErr != nil:
		return shortcut.HeaderInfo{Key: "Signatures", Value: "trust.yaml invalid", Style: theme.SeverityTextStyle("CRITICAL")}, true
	}
	return shortcut.HeaderInfo{}, false
}

// ── The Sig column (§3.82) ───────────────────────────────────────────────────
//
// Each local image's verdict on the content on disk: the digest it was pulled
// under, checked against the rules — never continuity, which would compare it
// with itself. An image no rule covers is decided without cosign, so the checks
// that cost anything are the few images a rule names; one DevDesk pulled was
// verified then, and comes from the verdict cache. Nothing acts on the column:
// G is verified when it runs, whatever the cell says.

// ImageSignatureCheckedMsg carries one image's verdict, for the digest it was
// asked about — a pull in between makes it stale.
type ImageSignatureCheckedMsg struct {
	Name   string
	Pinned string
	Result trust.Result
}

// sigAsk is what was asked about an image, and when.
type sigAsk struct {
	Pinned string
	At     time.Time
}

// signatureRetry is how long a check that could not run waits before it is
// asked again; a verdict is kept for as long as the image's digest holds.
const signatureRetry = 30 * time.Minute

// localSignatureSlots caps the checks running at once.
var localSignatureSlots = make(chan struct{}, 4)

// signatureDue lists the images to ask about: pulled, not asked for this
// digest yet, or asked long enough ago and answered by a failure — or by a
// verdict inferred from one, which the verdict cache does not keep either.
func (m Model) signatureDue(now time.Time) map[string]string {
	due := map[string]string{}
	for _, img := range m.images {
		pinned := imagepull.LocalDigest(img.Name(), img.RepoDigests)
		if img.Tag == "" || img.Tag == "<none>" || pinned == "" {
			continue
		}
		asked, ok := m.sigAsked[img.Name()]
		res := m.signatures[img.Name()]
		failed := res.Verdict == trust.Failed || !res.Proven()
		if !ok || asked.Pinned != pinned || (failed && now.Sub(asked.At) >= signatureRetry) {
			due[img.Name()] = pinned
		}
	}
	return due
}

// startLocalSignatureChecks marks the due images as being checked and returns
// the checks — nothing when this context does not verify.
func (m *Model) startLocalSignatureChecks(now time.Time) tea.Cmd {
	if !m.verifiesImages() {
		return nil
	}
	due := m.signatureDue(now)
	if len(due) == 0 {
		return nil
	}
	deps := m.pullDeps()
	cmds := make([]tea.Cmd, 0, len(due))
	for name, pinned := range due {
		m.sigAsked[name] = sigAsk{Pinned: pinned, At: now}
		m.sigVerifying[name] = true
		cmds = append(cmds, func() tea.Msg {
			localSignatureSlots <- struct{}{}
			defer func() { <-localSignatureSlots }()
			ctx, cancel := context.WithTimeout(context.Background(), signatureCheckTimeout)
			defer cancel()
			return ImageSignatureCheckedMsg{Name: name, Pinned: pinned, Result: imagepull.Check(ctx, pinned, "", deps)}
		})
	}
	return tea.Batch(cmds...)
}

// signatureCheckTimeout bounds one image's check: a few cosign runs.
const signatureCheckTimeout = 2 * time.Minute

func (m Model) handleImageSignatureChecked(msg ImageSignatureCheckedMsg) (tea.Model, tea.Cmd) {
	if m.sigAsked[msg.Name].Pinned != msg.Pinned {
		return m, nil // about a digest the image no longer has
	}
	m.signatures[msg.Name] = msg.Result
	delete(m.sigVerifying, msg.Name)
	m.updateImageTable()
	return m, nil
}

// imageSignature is the Sig cell of an image.
func (m Model) imageSignature(img docker.Image) sigcol.State {
	if img.Tag == "" || img.Tag == "<none>" || !m.verifiesImages() {
		return sigcol.State{}
	}
	pinned := imagepull.LocalDigest(img.Name(), img.RepoDigests)
	if pinned == "" {
		return sigcol.State{LocalBuild: true}
	}
	if m.sigAsked[img.Name()].Pinned != pinned {
		return sigcol.State{}
	}
	res, known := m.signatures[img.Name()]
	return sigcol.State{Result: res, Known: known, Verifying: m.sigVerifying[img.Name()]}
}
