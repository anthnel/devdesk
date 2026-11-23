package security

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// DEVDESK-751 (steps 4 and 7): the finding detail acts. Six keys, each greyed
// with its reason when it has nothing to act on (Rule 130), and each one
// acting on the finding on screen rather than on the table row behind it.

// The six keys of the detail, in the order of the header column, and the words
// beside them. The descriptions are at most ten cells: the shortcut column is
// 19 cells wide at 80 columns and `<ctrl+o>` takes nine of them.
var detailKeys = []string{"esc", keymap.Web, "enter", "ctrl+o", keymap.Copy, keymap.Exclude}

var detailDescriptions = map[string]string{
	"esc":          "Back",
	keymap.Web:     "Open ref",
	"enter":        "Open file",
	"ctrl+o":       "Apply fix",
	keymap.Copy:    "Copy cmd",
	keymap.Exclude: "Exclude",
}

// gitleaksSecret is a Gitleaks finding with a fingerprint, the only kind X can
// put in .gitleaksignore.
func gitleaksSecret(file string, line int) scan.Finding {
	return scan.Finding{
		ID: "aws-access-token", Title: "AWS key committed", Severity: scan.SeverityHigh,
		Source: scan.SourceGitleaks, File: file, Line: line, Match: "AKIA0000",
		Fingerprint: file + ":aws-access-token:" + string(rune('0'+line%10)),
		References:  []string{"https://example.test/aws"},
	}
}

// actionCase is one finding of the matrix of the detail's keys, with the reason
// each key is greyed for ("" = the key acts).
type actionCase struct {
	name    string
	f       scan.Finding
	image   bool
	enter   string
	fix     string
	copy    string
	exclude string
}

func declineReasonOf(t *testing.T, f scan.Finding) string {
	t.Helper()
	rule, ok := remediation.FixFor(f)
	if !ok || rule.Declines == nil || rule.Declines(f) == "" {
		t.Fatalf("%s must decline from the finding alone", f.ID)
	}
	return rule.Declines(f)
}

func actionCases(t *testing.T) []actionCase {
	t.Helper()
	trivySecret := scan.Finding{ID: "aws-access-key-id", Title: "AWS key", Severity: scan.SeverityHigh,
		Source: scan.SourceTrivySecret, File: "app/.env", Line: 3}
	license := scan.Finding{ID: "GPL-3.0", Title: "Copyleft licence", Severity: scan.SeverityLow,
		Source: scan.SourceTrivyLicense, File: "go.mod"}
	ci := scan.Finding{ID: "pipeline-score", Title: "Pipeline", Severity: scan.SeverityMedium,
		Source: scan.SourcePlumber, File: ".gitlab-ci.yml", Job: "build"}
	instruction := cveWith("", "Update golang.org/x/net to 0.7.0")
	if !scan.IsFixInstruction(instruction) {
		t.Fatal("the fixture must be an instruction, not a command")
	}
	return []actionCase{
		{name: "a CVE with a command", f: cveWith("gomod", "go get golang.org/x/net@v0.7.0"),
			enter: reasonNoFile, fix: reasonNotMisconfigTab, copy: "", exclude: reasonNotASecret},
		{name: "a CVE with an instruction", f: instruction,
			enter: reasonNoFile, fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a Gitleaks secret", f: gitleaksSecret("config/prod.env", 12),
			enter: "", fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: ""},
		{name: "a Trivy secret", f: trivySecret,
			enter: "", fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: reasonNotGitleaks},
		{name: "a secret whose file leaves the target", f: gitleaksSecret("../etc/passwd", 1),
			enter: reasonFileOutsideTarget, fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: ""},
		{name: "a license", f: license,
			enter: "", fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a CI finding", f: ci,
			enter: "", fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a built-in fix on a directory", f: misconfigOf("DS002"),
			enter: "", fix: "", copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a built-in fix on an image", f: misconfigOf("DS002"), image: true,
			enter: reasonFileNotOnDisk, fix: reasonFileNotOnDisk, copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a fix the finding declines", f: unconfinedSeccomp(),
			enter: "", fix: declineReasonOf(t, unconfinedSeccomp()), copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a rule the catalog has no fix for", f: misconfigOf("KSV-0012"),
			enter: "", fix: noFixReason(misconfigOf("KSV-0012")), copy: reasonNoCommand, exclude: reasonNotASecret},
		{name: "a minimal finding", f: scan.Finding{ID: "X-1", Title: "Only a title"},
			enter: reasonNoFile, fix: reasonNotMisconfigTab, copy: reasonNoCommand, exclude: reasonNotASecret},
	}
}

func (c actionCase) open(t *testing.T) Model {
	t.Helper()
	if c.image {
		return openDetailOnImage(t, c.f, 160, 30)
	}
	return openDetailOn(t, c.f, t.TempDir(), scan.TargetDirectory, 160, 30)
}

func (c actionCase) reasons() map[string]string {
	return map[string]string{
		"enter": c.enter, "ctrl+o": c.fix, keymap.Copy: c.copy, keymap.Exclude: c.exclude,
	}
}

func shortcutOf(t *testing.T, list shortcut.Shortcuts, key string) shortcut.Shortcut {
	t.Helper()
	for _, s := range list {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no shortcut %q in %v", key, testutil.ShortcutKeys(list))
	return shortcut.Shortcut{}
}

// ── The subject of an action ─────────────────────────────────────────────────

func TestTheSubjectInTheResultsStateIsTheSelectedRow(t *testing.T) {
	m := scannedModel(t)
	want, ok := m.findingsTable.Selected()
	if !ok {
		t.Fatal("the fixture has no selected row")
	}
	got, ok := m.subjectFinding()
	if !ok || got.ID != want.ID {
		t.Errorf("subjectFinding() = (%q, %v), want the selected row %q", got.ID, ok, want.ID)
	}
}

func TestTheSubjectInTheDetailsStateIsTheFindingOnScreen(t *testing.T) {
	m := detailsModel(t)
	got, ok := m.subjectFinding()
	if !ok || m.selectedFinding == nil || got.ID != m.selectedFinding.ID {
		t.Errorf("subjectFinding() = (%q, %v), want %v", got.ID, ok, m.selectedFinding)
	}
}

// A re-scan puts the table back at the top (setResult runs GotoTop): the detail
// must not go back through the table's cursor, which now names another finding.
func TestTheSubjectIsNotTheTableRowBehindTheDetail(t *testing.T) {
	f := cveWith("gomod", "go get golang.org/x/net@v0.7.0")
	other := cveWith("gomod", "go get golang.org/x/text@v0.3.8")
	other.ID, other.PkgName, other.Severity = "CVE-2026-2222", "golang.org/x/text", scan.SeverityLow
	f.Severity = scan.SeverityCritical
	result := &scan.Result{Target: t.TempDir(), TargetType: scan.TargetDirectory, Findings: []scan.Finding{f, other}}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(160, 30))
	m = selectFinding(t, m, func(x scan.Finding) bool { return x.ID == other.ID })
	m = feed(t, m, testutil.Key("enter"))
	if m.selectedFinding == nil || m.selectedFinding.ID != other.ID {
		t.Fatalf("the detail shows %v, want %s", m.selectedFinding, other.ID)
	}

	m.findingsTable.GotoTop()
	if top, _ := m.findingsTable.Selected(); top.ID == other.ID {
		t.Fatal("the fixture must put another finding at the top of the table")
	}
	got, ok := m.subjectFinding()
	if !ok || got.ID != other.ID {
		t.Errorf("subjectFinding() = %q, want the detail's %q", got.ID, other.ID)
	}
	if a := m.canCopyFixCommand(); !a.Enabled() {
		t.Errorf("Y is greyed (%q) on a finding that has a command", a.Reason)
	}
}

func TestThereIsNoSubjectInTheInventoryOrOnAnEmptyResult(t *testing.T) {
	if _, ok := inventoryModel(t, inventoryFixtures()...).subjectFinding(); ok {
		t.Error("the inventory has a subject finding")
	}
	empty := feed(t, NewWithPreloadedResult(testConfig(), nil, &scan.Result{Target: "/work/repo"}), testutil.Resize(160, 30))
	if _, ok := empty.subjectFinding(); ok {
		t.Error("an empty result has a subject finding")
	}
}

// ── The six keys (Rule 130) ──────────────────────────────────────────────────

func TestTheDetailAdvertisesSixKeysWithTheirWords(t *testing.T) {
	m := detailsModel(t)
	list := m.GetShortcuts()
	if got := testutil.ShortcutKeys(list); !slices.Equal(got, detailKeys) {
		t.Fatalf("keys = %v, want %v", got, detailKeys)
	}
	for _, s := range list {
		if want := detailDescriptions[s.Key]; s.Description != want {
			t.Errorf("%s reads %q, want %q", s.Key, s.Description, want)
		}
	}
}

// The set of keys does not change from one finding to the next: what changes is
// whether an entry is greyed (Rule 130).
func TestTheDetailKeySetIsTheSameForEveryFinding(t *testing.T) {
	for _, c := range actionCases(t) {
		t.Run(c.name, func(t *testing.T) {
			list := c.open(t).GetShortcuts()
			if got := testutil.ShortcutKeys(list); !slices.Equal(got, detailKeys) {
				t.Errorf("keys = %v, want %v", got, detailKeys)
			}
			for _, key := range detailKeys {
				if !testutil.HasShortcut(list, key) {
					t.Errorf("%s is missing instead of greyed", key)
				}
			}
		})
	}
}

// A key is greyed exactly when its availability says so, and the availability
// carries the reason the press of the key gives.
func TestEachDetailKeyIsGreyedForItsReason(t *testing.T) {
	for _, c := range actionCases(t) {
		t.Run(c.name, func(t *testing.T) {
			m := c.open(t)
			list := m.GetShortcuts()
			avail := map[string]shortcut.Availability{
				"enter":        m.canOpenFindingFile(),
				"ctrl+o":       m.canFixMisconfig(),
				keymap.Copy:    m.canCopyFixCommand(),
				keymap.Exclude: m.canExclude(),
			}
			for key, want := range c.reasons() {
				if got := avail[key].Reason; got != want {
					t.Errorf("%s: reason = %q, want %q", key, got, want)
				}
				if got := shortcutOf(t, list, key).Disabled; got != (want != "") {
					t.Errorf("%s: Disabled = %v, want %v", key, got, want != "")
				}
			}
			noRefs := len(c.f.References) == 0
			if got := shortcutOf(t, list, keymap.Web).Disabled; got != noRefs {
				t.Errorf("W: Disabled = %v with %d references", got, len(c.f.References))
			}
			if shortcutOf(t, list, "esc").Disabled {
				t.Error("esc is greyed")
			}
		})
	}
}

// Pressing a greyed key says why, at the Warn level, and does nothing else: no
// modal, no viewer, no clipboard, and the detail stays where it is.
func TestAGreyedDetailKeyGivesItsReasonAndDoesNothing(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	written := stubClipboard(t, nil)
	for _, c := range actionCases(t) {
		for key, reason := range c.reasons() {
			if reason == "" {
				continue
			}
			t.Run(c.name+"/"+key, func(t *testing.T) {
				m := c.open(t)
				before := *m.selectedFinding
				m, cmd := step(t, m, testutil.Key(key))

				if m.footer.Text() != reason || m.footer.Level() != components.LevelWarning {
					t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reason)
				}
				if m.confirmModal != nil || m.misconfigPending != nil || m.findingToIgnore != nil {
					t.Error("a refused key opened a confirmation")
				}
				if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.ID != before.ID {
					t.Error("a refused key left the detail")
				}
				for _, msg := range testutil.Msgs(cmd) {
					switch msg.(type) {
					case uiviewer.OpenRequestMsg, FixCommandCopiedMsg:
						t.Errorf("a refused key emitted %T", msg)
					}
				}
			})
		}
	}
	if len(*written) != 0 {
		t.Errorf("a refused key wrote %q to the clipboard", *written)
	}
}

// The header calls the availabilities on every frame: none of them reads the
// disk. A file that may not exist is the viewer's to report.
func TestTheDetailShortcutsReadNothingFromTheDisk(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "no", "such", "directory")
	m := openDetailOn(t, misconfigOf("DS002"), gone, scan.TargetDirectory, 160, 30)
	list := m.GetShortcuts()
	for _, key := range []string{"enter", "ctrl+o"} {
		if !testutil.ShortcutEnabled(list, key) {
			t.Errorf("%s is greyed on a target whose directory is not there: the header must not look", key)
		}
	}
	if _, err := os.Stat(gone); err == nil {
		t.Error("the header created the directory")
	}
}

// ── X in the detail is X in the results state ────────────────────────────────

// secretsDetail opens the detail of a Gitleaks secret on a real directory, so
// .gitleaksignore can be written there.
func secretsDetail(t *testing.T, secrets ...scan.Finding) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	result := &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: secrets}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(160, 30))
	m.switchTab(tabOf(t, secrets[0]))
	return m, dir
}

func TestXInTheDetailOpensTheSameConfirmationAsInTheResults(t *testing.T) {
	m, _ := secretsDetail(t, gitleaksSecret("config/prod.env", 12))
	results, _ := step(t, m, testutil.Key(keymap.Exclude))
	detail := feed(t, m, testutil.Key("enter"))
	detail, _ = step(t, detail, testutil.Key(keymap.Exclude))

	if results.confirmModal == nil || detail.confirmModal == nil {
		t.Fatalf("X opened no confirmation (results %v, detail %v)", results.confirmModal != nil, detail.confirmModal != nil)
	}
	if got, want := detail.confirmModal.View(), results.confirmModal.View(); got != want {
		t.Errorf("the confirmations differ:\nresults:\n%s\ndetail:\n%s", want, got)
	}
	if detail.findingToIgnore == nil || results.findingToIgnore == nil || detail.findingToIgnore.Fingerprint != results.findingToIgnore.Fingerprint {
		t.Error("the two confirmations are about different findings")
	}
	if detail.state != StateDetails {
		t.Error("X left the detail")
	}
}

func TestConfirmingXInTheDetailAddsTheSecretToTheIgnoreFile(t *testing.T) {
	f := gitleaksSecret("config/prod.env", 12)
	m, dir := secretsDetail(t, f)
	m = feed(t, m, testutil.Key("enter"))
	m, _ = step(t, m, testutil.Key(keymap.Exclude))

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	msg, ok := testutil.MsgOf[SecretIgnoredMsg](cmd)
	if !ok {
		t.Fatalf("answering Yes produced no SecretIgnoredMsg")
	}
	if msg.Error != nil || msg.Finding.Fingerprint != f.Fingerprint {
		t.Errorf("SecretIgnoredMsg = %+v, want the finding on screen and no error", msg)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".gitleaksignore"))
	if err != nil || !strings.Contains(string(got), f.Fingerprint) {
		t.Errorf(".gitleaksignore = %q (%v), want it to hold %q", got, err, f.Fingerprint)
	}
	m = feed(t, m, msg)
	if m.footer.Level() != components.LevelInfo || !strings.Contains(m.footer.Text(), "config/prod.env") {
		t.Errorf("footer = %q (level %v), want an Info naming the file", m.footer.Text(), m.footer.Level())
	}
}

func TestAnsweringNoToXInTheDetailWritesNothing(t *testing.T) {
	m, dir := secretsDetail(t, gitleaksSecret("config/prod.env", 12))
	m = feed(t, m, testutil.Key("enter"), testutil.Key(keymap.Exclude))
	if m.confirmModal == nil {
		t.Fatal("X opened no confirmation to answer")
	}
	m = feed(t, m, components.ConfirmModalNoMsg{})

	if m.confirmModal != nil || m.findingToIgnore != nil {
		t.Error("the confirmation outlived a No")
	}
	if m.state != StateDetails {
		t.Error("No left the detail")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitleaksignore")); err == nil {
		t.Error("No wrote .gitleaksignore")
	}
}

// X acts on the finding on screen, not on the table's cursor, which a re-scan
// moves back to the top.
func TestXInTheDetailIsAboutTheFindingOnScreen(t *testing.T) {
	first := gitleaksSecret("a/first.env", 1)
	second := gitleaksSecret("b/second.env", 2)
	second.Severity = scan.SeverityLow
	m, _ := secretsDetail(t, first, second)
	m = selectFinding(t, m, func(f scan.Finding) bool { return f.File == second.File })
	m = feed(t, m, testutil.Key("enter"))
	m.findingsTable.GotoTop()
	if top, _ := m.findingsTable.Selected(); top.File == second.File {
		t.Fatal("the fixture must put the other secret at the top of the table")
	}

	m, _ = step(t, m, testutil.Key(keymap.Exclude))
	if m.findingToIgnore == nil || m.findingToIgnore.File != second.File {
		t.Fatalf("X is about %v, want %s", m.findingToIgnore, second.File)
	}
	if got := m.confirmModal.View(); !strings.Contains(got, second.File) || strings.Contains(got, first.File) {
		t.Errorf("the confirmation names the wrong file:\n%s", got)
	}
}

// ── esc and W are what they were ─────────────────────────────────────────────

func TestEscAndWKeepTheirMeaningInTheDetail(t *testing.T) {
	m := detailsModel(t)
	if _, cmd := step(t, m, testutil.Key(keymap.Web)); cmd == nil {
		t.Error("W on a finding with a reference issues no command")
	}
	back := feed(t, m, testutil.Key("esc"))
	if back.state != StateResults {
		t.Errorf("esc left the detail in state %v, want the results", back.state)
	}
}
