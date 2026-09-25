package ociresources

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/trust"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
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
