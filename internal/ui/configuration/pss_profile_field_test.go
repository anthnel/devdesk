package configuration

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-686 (S-01), criteria 1 and 6: scan.pss_profile in the configuration
// view. A cycle field over a closed set (Rule 132), an absent value reads as
// the default and stays absent, a value outside the set is refused with the
// valid values listed, and one the file already holds gets a standing warning.

const pssProfileLabel = "Pod Security profile"

func pssModel(t *testing.T, v string) Model {
	t.Helper()
	isolateMotionHome(t)
	m := newModel(t)
	m.config.Scan.PSSProfile = v
	return focusOn(t, m, pssProfileLabel)
}

func TestThePSSProfileIsACycleFieldInTheScanRemediationGroup(t *testing.T) {
	f := fieldNamed(t, pssProfileLabel)
	if f.Kind != kindCycle {
		t.Errorf("kind = %v, want kindCycle (Rule 132)", f.Kind)
	}
	if f.Group != fieldNamed(t, "Base image bumps").Group {
		t.Errorf("group = %q, want the group of Base image bumps", f.Group)
	}
	if strings.Join(f.Options, ",") != strings.Join(config.PSSProfiles(), ",") {
		t.Errorf("options = %v, want %v", f.Options, config.PSSProfiles())
	}
	m := newModel(t)
	found := false
	for _, s := range m.sections {
		if s.Title != "scan" {
			continue
		}
		for _, fl := range s.Fields {
			found = found || fl.Label == pssProfileLabel
		}
	}
	if !found {
		t.Error("the field is not on the scan tab")
	}
}

// Criterion 1: showing the default writes nothing; the first arrow picks the
// next value explicitly and the cycle persists through a ConfigSavedMsg.
func TestAnAbsentPSSProfileShowsTheDefaultAndStaysAbsent(t *testing.T) {
	m := pssModel(t, "")
	if got := fieldNamed(t, pssProfileLabel).Value(m.config); got != "all (default)" {
		t.Errorf("Value = %q, want %q", got, "all (default)")
	}
	if !strings.Contains(plainRows(m), "all (default)") {
		t.Error("the row does not read 'all (default)'")
	}
	if m.config.Scan.PSSProfile != "" {
		t.Error("displaying the default wrote it")
	}

	updated, cmd := m.Update(testutil.Key("right"))
	next := updated.(Model)
	if next.config.Scan.PSSProfile != config.PSSProfileBaseline {
		t.Fatalf("first → gave %q, want baseline", next.config.Scan.PSSProfile)
	}
	msg, ok := testutil.MsgOf[ConfigSavedMsg](cmd)
	if !ok {
		t.Fatal("no ConfigSavedMsg; the value is never persisted")
	}
	if msg.Config.Scan.PSSProfile != config.PSSProfileBaseline {
		t.Errorf("ConfigSavedMsg carries %q, want baseline", msg.Config.Scan.PSSProfile)
	}
	next = feed(t, next, testutil.Key("right"))
	if next.config.Scan.PSSProfile != config.PSSProfileRestricted {
		t.Errorf("second → gave %q, want restricted", next.config.Scan.PSSProfile)
	}
}

func TestSpaceAndEnterDoNothingOnThePSSProfile(t *testing.T) {
	for _, key := range []string{"space", "enter"} {
		t.Run(key, func(t *testing.T) {
			m := pssModel(t, config.PSSProfileBaseline)
			updated, cmd := m.Update(testutil.Key(key))
			if got := updated.(Model).config.Scan.PSSProfile; got != config.PSSProfileBaseline {
				t.Errorf("%s changed the profile to %q (Rule 135)", key, got)
			}
			if _, ok := testutil.MsgOf[ConfigSavedMsg](cmd); ok {
				t.Errorf("%s saved the configuration", key)
			}
		})
	}
}

// Criterion 6: an entry outside all | baseline | restricted is refused, the
// message lists the valid values, and the configuration keeps its value.
func TestAPSSProfileOutsideTheSetIsRefusedWithTheValidValues(t *testing.T) {
	f := fieldNamed(t, pssProfileLabel)
	for _, bad := range []string{"strict", "Baseline", "privileged"} {
		cfg := config.Default()
		cfg.Scan.PSSProfile = config.PSSProfileBaseline
		err := f.Apply(cfg, bad)
		if err == nil {
			t.Errorf("Apply(%q) accepted a value outside the set", bad)
			continue
		}
		for _, valid := range config.PSSProfiles() {
			if !strings.Contains(err.Error(), valid) {
				t.Errorf("Apply(%q) error %q does not list %q", bad, err, valid)
			}
		}
		if cfg.Scan.PSSProfile != config.PSSProfileBaseline {
			t.Errorf("Apply(%q) changed the value to %q", bad, cfg.Scan.PSSProfile)
		}
	}
	for _, good := range config.PSSProfiles() {
		cfg := config.Default()
		if err := f.Apply(cfg, good); err != nil || cfg.Scan.PSSProfile != good {
			t.Errorf("Apply(%q) = %v, value %q", good, err, cfg.Scan.PSSProfile)
		}
	}
}

// Criterion 6: a value the file holds outside the set is a standing warning on
// the scan tab (level Warning, Rule 128), naming the valid values; a valid one
// is not.
func TestAnInvalidPSSProfileWarnsOnTheScanTab(t *testing.T) {
	m := pssModel(t, "strict")
	st := m.status()
	if st.Level != components.LevelWarning {
		t.Fatalf("Level = %v, want LevelWarning", st.Level)
	}
	for _, want := range []string{`"strict"`, "all", "baseline", "restricted"} {
		if !strings.Contains(st.Text, want) {
			t.Errorf("warning %q lacks %q", st.Text, want)
		}
	}
	if !strings.Contains(plainRows(m), "strict") {
		t.Error("the row hides the value the file holds")
	}
	if !strings.Contains(plainFooter(m), "baseline") {
		t.Errorf("footer lacks the warning:\n%s", plainFooter(m))
	}

	// Another tab shows its own hint.
	other := focusOn(t, m, "Theme")
	if other.status().Level == components.LevelWarning && strings.Contains(other.status().Text, "strict") {
		t.Error("the warning shows on a tab other than scan")
	}
	for _, v := range []string{"", config.PSSProfileAll, config.PSSProfileBaseline, config.PSSProfileRestricted} {
		if pssModel(t, v).status().Level == components.LevelWarning {
			t.Errorf("%q still warns", v)
		}
	}
}

// An error message keeps priority over the standing warning (Rule 128).
func TestAnErrorMessageBeatsThePSSProfileWarning(t *testing.T) {
	m := pssModel(t, "strict")
	_ = m.footer.Error("Failed to save — check logs")
	got := plainFooter(m)
	if !strings.Contains(got, "Failed to save") {
		t.Errorf("the error is not shown:\n%s", got)
	}
}

// Rule 114: the help says what the setting does.
func TestHelpExplainsThePSSProfile(t *testing.T) {
	m := newModel(t)
	for _, s := range m.GetHelpContent().Sections {
		if s.Title != pssProfileLabel {
			continue
		}
		for _, want := range []string{"baseline", "restricted", "all"} {
			if !strings.Contains(s.Body, want) {
				t.Errorf("help lacks %q:\n%s", want, s.Body)
			}
		}
		return
	}
	t.Fatalf("no help section titled %q (Rule 114)", pssProfileLabel)
}
