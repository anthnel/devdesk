package security

import (
	"context"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/imagepull"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/trust"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/sigcol"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

var blockedByRule = trust.Result{
	Verdict: trust.IdentityMismatch, Decision: trust.Block,
	Rule: trust.Rule{Mode: trust.ModeKeyless, Source: trust.SourceUser, Origin: "trust.yaml:2"},
}

var warnedByContinuity = trust.Result{
	Verdict: trust.Unsigned, Decision: trust.Warn,
	Rule: trust.Rule{Mode: trust.ModeKeyless, Source: trust.SourceContinuity, Origin: "continuity with golang@sha256:old"},
}

func signed(dir string, results map[string]trust.Result) RemediationSignaturesMsg {
	return RemediationSignaturesMsg{Target: dir, Results: results}
}

func TestACandidateItsSignatureBlocksCannotBeChosen(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = cursorOn(t, feed(t, m, signed(dir, map[string]trust.Result{"golang:1.23": blockedByRule})), "golang:1.23")

	a := m.canSelectCandidate()
	if a.Enabled() || !strings.Contains(a.Reason, "trust.yaml:2") {
		t.Fatalf("availability = %+v, want a refusal naming the rule", a)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "space") {
		t.Error("space is lit on a candidate it cannot choose")
	}
	m = feed(t, m, testutil.Key(" "))
	if len(m.remediation.selected) != 0 || m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("selected %v, footer %q", m.remediation.selected, m.footer.Text())
	}
	// Still shown: that the tag was republished by someone else is the point.
	if row, _ := m.remediation.table.Selected(); sigcol.Cell(row.Signature) == "-" {
		t.Error("the blocked candidate's Sig cell says nothing")
	}
}

// Not knowing is not a no (Rule 130): a candidate may be chosen before its
// verdict lands, and is let go if the verdict blocks it.
func TestAChoiceItsVerdictThenBlocksIsReleased(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = choose(t, m, "golang:1.23")
	if len(m.remediation.selected) != 1 {
		t.Fatal("the candidate could not be chosen while its verdict was pending")
	}
	m = feed(t, m, signed(dir, map[string]trust.Result{"golang:1.23": blockedByRule}))
	if len(m.remediation.selected) != 0 {
		t.Error("a blocked candidate stayed chosen")
	}
	if m.footer.Level() != sharedcomponents.LevelWarning || !strings.Contains(m.footer.Text(), "Released golang:1.23") {
		t.Errorf("footer = %q", m.footer.Text())
	}
}

func TestAWarningIsRepeatedInTheConfirmation(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = choose(t, feed(t, m, signed(dir, map[string]trust.Result{"golang:1.23": warnedByContinuity})), "golang:1.23")
	if len(m.remediation.selected) != 1 {
		t.Fatal("a warned candidate must stay choosable")
	}
	m = prepared(t, m)
	text := confirmationText(m.remediation.pending)
	if !strings.Contains(text, "(signature: Not signed — continuity with golang@sha256:old)") {
		t.Errorf("confirmation:\n%s", text)
	}
}

func TestAVerdictLandingUnderTheConfirmationStopsTheWrite(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = prepared(t, choose(t, m, "golang:1.23"))
	// The verdict arrives while the modal is open: the selection is released,
	// but pending is what the modal shows.
	m.remediation.signatures["golang:1.23"] = blockedByRule

	m, cmd := step(t, m, testutil.Key("y"))
	m, _ = step(t, m, run(t, cmd))
	if fileContent(t, dir) != twoStageFile {
		t.Error("a blocked image was written")
	}
	if m.footer.Level() != sharedcomponents.LevelError || !strings.Contains(m.footer.Text(), "Not written") {
		t.Errorf("footer = %q", m.footer.Text())
	}
}

func TestOffChecksNothingAndSaysSo(t *testing.T) {
	cfg := testConfig()
	cfg.Scan.ImageVerification = "off"
	m := Model{config: cfg, activeTab: TabRemediation, remediation: newRemediationState()}
	m.remediation.entries = []remediation.Entry{{Image: "golang:1.21", Candidates: []string{"golang:1.23"}}}
	if cmds := m.startSignatureChecks(); len(cmds) != 0 || len(m.remediation.verifying) != 0 {
		t.Errorf("off started %d checks", len(cmds))
	}
	if h, ok := m.signatureHeader(); !ok || h.Value != "off" {
		t.Errorf("header = %+v, %v", h, ok)
	}
}

// fixedVerifier signs everything but one reference.
type fixedVerifier struct{ unsigned string }

func (f fixedVerifier) Verify(_ context.Context, ref string, _ trust.Rule) (trust.Verdict, error) {
	if strings.HasPrefix(ref, f.unsigned) {
		return trust.Unsigned, nil
	}
	return trust.Verified, nil
}

func (fixedVerifier) Identities(context.Context, string) ([]trust.Identity, error) { return nil, nil }

func TestEveryImageOfAnEntryIsChecked(t *testing.T) {
	rule := trust.Rule{Match: "docker.io/library/golang", Mode: trust.ModeKey, Source: trust.SourceUser, Origin: "trust.yaml:1"}
	d := imagepull.Deps{
		Enabled:  true,
		Policy:   func() (trust.Policy, error) { return trust.Policy{Rules: []trust.Rule{rule}}, nil },
		Verifier: fixedVerifier{unsigned: "docker.io/library/golang@sha256:1.22"},
		Digest: func(ref string) (string, error) {
			return "sha256:" + remediation.ParseRef(ref).Tag, nil
		},
	}
	entries := []remediation.Entry{
		{Image: "golang:1.21", Candidates: []string{"golang:1.23", "golang:1.22"}},
		{Reason: "could not be resolved"}, // nothing to verify
	}
	cmds := checkSignaturesCmds("/repo", entries, d)
	if len(cmds) != 1 {
		t.Fatalf("%d commands for one resolvable entry", len(cmds))
	}
	msg := cmds[0]().(RemediationSignaturesMsg)
	if len(msg.Results) != 3 {
		t.Fatalf("results = %v", msg.Results)
	}
	if msg.Results["golang:1.23"].Verdict != trust.Verified || msg.Results["golang:1.22"].Decision != trust.Block {
		t.Errorf("results = %+v", msg.Results)
	}
}
