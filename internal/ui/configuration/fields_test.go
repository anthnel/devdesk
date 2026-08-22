package configuration

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
)

func allFields(t *testing.T) []field {
	t.Helper()
	var out []field
	for _, s := range sections([]string{"default", "mocha"}, command.ViewNames(), "/home/u/.devdesk/config.yaml", config.ForgeGitLab, forge.VocabularyFor(config.ForgeGitLab)) {
		out = append(out, s.Fields...)
	}
	return out
}

// Every field carries the accessor its kind needs. Twenty-nine settings written
// by hand is exactly where a missing or mismatched accessor hides — the field
// renders, and reads or writes nothing.
func TestEveryFieldCarriesTheAccessorItsKindNeeds(t *testing.T) {
	for _, f := range allFields(t) {
		switch f.Kind {
		case kindText:
			if f.str == nil {
				t.Errorf("%q is a text field with no string accessor", f.Label)
			}
		case kindInteger:
			if f.num == nil {
				t.Errorf("%q is an integer field with no int accessor", f.Label)
			}
			if f.min >= f.max {
				t.Errorf("%q has bounds [%d, %d], which admits nothing", f.Label, f.min, f.max)
			}
		case kindToggle:
			if f.flag == nil {
				t.Errorf("%q is a toggle with no bool accessor", f.Label)
			}
		case kindCycle:
			if f.str == nil {
				t.Errorf("%q is a cycle field with no string accessor", f.Label)
			}
			if len(f.Options) < 2 {
				t.Errorf("%q cycles through %d options; a closed set needs at least two", f.Label, len(f.Options))
			}
		case kindStatic:
			if f.fact == "" {
				t.Errorf("%q is a static row with nothing to show", f.Label)
			}
			if f.str != nil || f.num != nil || f.flag != nil {
				t.Errorf("%q is a static row carrying an accessor; nothing may write it", f.Label)
			}
		}
	}
}

// Two fields addressing the same setting would make one of them silently
// ineffective — whichever the user did not edit last wins on save.
func TestNoTwoFieldsAddressTheSameSetting(t *testing.T) {
	cfg := config.Default()
	fields := allFields(t)

	for i, a := range fields {
		for _, b := range fields[i+1:] {
			if a.Kind != b.Kind || a.Kind == kindStatic {
				continue // a static row addresses no setting
			}
			if samePointer(cfg, a, b) {
				t.Errorf("%q and %q edit the same setting", a.Label, b.Label)
			}
		}
	}
}

func samePointer(c *config.Config, a, b field) bool {
	switch a.Kind {
	case kindToggle:
		return a.flag(c) == b.flag(c)
	case kindInteger:
		return a.num(c) == b.num(c)
	default:
		return a.str(c) == b.str(c)
	}
}

// A cycle field whose current value is outside its options must still move.
// config.Default() is the realistic case: every cycle field's default has to be
// one of the values it offers, or the first ←/→ jumps somewhere arbitrary.
func TestEveryCycleFieldDefaultsToOneOfItsOptions(t *testing.T) {
	cfg := config.Default()

	for _, f := range allFields(t) {
		if f.Kind != kindCycle {
			continue
		}
		v := *f.str(cfg)
		if v == "" {
			continue // applyDefaults fills these at load; Default() leaves some empty
		}
		found := false
		for _, opt := range f.Options {
			if opt == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q defaults to %q, which is not among %v", f.Label, v, f.Options)
		}
	}
}

// The three settings the router has to special-case are matched by label, so a
// rename that misses one silently drops the behaviour.
func TestTheSpeciallyHandledLabelsExist(t *testing.T) {
	labels := map[string]bool{}
	for _, f := range allFields(t) {
		labels[f.Label] = true
	}

	for _, want := range []string{themeLabel, secretBackendLabel} {
		if !labels[want] {
			t.Errorf("no field is labelled %q, so its special handling is dead", want)
		}
	}
	for want := range serverModeFields {
		if !labels[want] {
			t.Errorf("serverModeFields names %q, which is not a field", want)
		}
	}
}

// ── validation ──────────────────────────────────────────────────────────────

// An out-of-range or unparseable number is refused, not coerced. Coercing to
// zero is how `trivy_server: ":"` reached a config file.
func TestAnIntegerFieldRefusesRatherThanCoerces(t *testing.T) {
	cfg := config.Default()
	f := integer("Jobs", func(c *config.Config) *int { return &c.Forge.Pull.ParallelJobs }, 1, 32, "")
	cfg.Forge.Pull.ParallelJobs = 4

	for _, bad := range []string{"", "abc", "0", "33", "-1"} {
		if err := f.Apply(cfg, bad); err == nil {
			t.Errorf("Apply(%q) was accepted", bad)
		}
		if got := cfg.Forge.Pull.ParallelJobs; got != 4 {
			t.Fatalf("Apply(%q) changed the setting to %d despite failing", bad, got)
		}
	}

	if err := f.Apply(cfg, " 8 "); err != nil {
		t.Errorf("Apply(\" 8 \") = %v, want the surrounding space trimmed and accepted", err)
	}
	if cfg.Forge.Pull.ParallelJobs != 8 {
		t.Errorf("ParallelJobs = %d, want 8", cfg.Forge.Pull.ParallelJobs)
	}
}

// The Trivy server address is the one text field with a validator, and it is
// what stops a stray ":" reaching Trivy and failing the whole scan.
func TestTheTrivyServerFieldRefusesAnAddressTrivyCannotParse(t *testing.T) {
	cfg := config.Default()
	f := fieldNamed(t, "Trivy server")

	if err := f.Apply(cfg, ":"); err == nil {
		t.Error("Apply(\":\") was accepted; Trivy fails the whole scan on that")
	}
	if cfg.Scan.TrivyServer != "" {
		t.Errorf("TrivyServer = %q after a refused value", cfg.Scan.TrivyServer)
	}
	if err := f.Apply(cfg, "https://trivy:4954"); err != nil {
		t.Errorf("a valid address was refused: %v", err)
	}
}

// Clearing a field has to be possible: an empty Trivy server is how the user
// leaves client-server mode.
func TestClearingTheTrivyServerIsAllowed(t *testing.T) {
	cfg := config.Default()
	cfg.Scan.TrivyServer = "https://trivy:4954"
	f := fieldNamed(t, "Trivy server")

	if err := f.Apply(cfg, "   "); err != nil {
		t.Fatalf("clearing was refused: %v", err)
	}
	if cfg.Scan.TrivyServer != "" {
		t.Errorf("TrivyServer = %q, want it cleared", cfg.Scan.TrivyServer)
	}
}

func TestCycleWraps(t *testing.T) {
	cfg := config.Default()
	f := fieldNamed(t, "Clone method")
	cfg.Forge.CloneMethod = "https"

	f.Cycle(cfg, 1)
	if cfg.Forge.CloneMethod != "ssh" {
		t.Fatalf("after one step: %q, want ssh", cfg.Forge.CloneMethod)
	}
	f.Cycle(cfg, 1)
	if cfg.Forge.CloneMethod != "https" {
		t.Errorf("after wrapping: %q, want https", cfg.Forge.CloneMethod)
	}
	f.Cycle(cfg, -1)
	if cfg.Forge.CloneMethod != "ssh" {
		t.Errorf("stepping back: %q, want ssh", cfg.Forge.CloneMethod)
	}
}

// A value outside the closed set lands on the first option rather than being
// carried forward — a hand-edited config file is where this comes from.
func TestCycleFromAnUnknownValueLandsOnTheFirstOption(t *testing.T) {
	cfg := config.Default()
	f := fieldNamed(t, "Clone method")
	cfg.Forge.CloneMethod = "carrier-pigeon"

	f.Cycle(cfg, 1)

	if cfg.Forge.CloneMethod != f.Options[1] {
		t.Errorf("got %q, want %q — an unknown value is treated as index 0", cfg.Forge.CloneMethod, f.Options[1])
	}
}

func fieldNamed(t *testing.T, label string) field {
	t.Helper()
	for _, f := range allFields(t) {
		if f.Label == label {
			return f
		}
	}
	t.Fatalf("no field labelled %q; the labels are %s", label, strings.Join(labelsOf(allFields(t)), ", "))
	return field{}
}

func labelsOf(fields []field) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.Label)
	}
	return out
}

// Every value the Default view field offers has to name a view the router can
// actually open. FullNames() also carries the action commands -- context, theme,
// quit -- so a field built from it offered "quit" as a landing view.
func TestTheDefaultViewFieldOffersOnlyViews(t *testing.T) {
	f := fieldNamed(t, "Default view")

	if len(f.Options) == 0 {
		t.Fatal("the field offers nothing")
	}
	for _, name := range f.Options {
		view, err := command.Parse(name)
		if err != nil || view == "" {
			t.Errorf("%q is offered as a default view but does not name one (%v)", name, err)
		}
	}
	for _, action := range []string{"quit", "theme", "context"} {
		if slices.Contains(f.Options, action) {
			t.Errorf("%q is an action, not a view, and must not be offered", action)
		}
	}
}

// The Paths group reads as three paths and then the option that qualifies them.
// A checkbox wedged between two value rows breaks the column they share, which
// is what put Show hidden files between the workspaces root and the log file.
func TestThePathsGroupEndsWithItsCheckbox(t *testing.T) {
	var paths []field
	for _, f := range allFields(t) {
		if f.Group == "Paths" {
			paths = append(paths, f)
		}
	}

	want := []string{"Workspaces dir", "Config file", "Log file", "Show hidden files"}
	if len(paths) != len(want) {
		t.Fatalf("the Paths group holds %v, want %v", labelsOf(paths), want)
	}
	for i, label := range want {
		if paths[i].Label != label {
			t.Fatalf("the Paths group reads %v, want %v", labelsOf(paths), want)
		}
	}
	if paths[len(paths)-1].Kind != kindToggle {
		t.Error("the group does not end on its checkbox")
	}
	for _, f := range paths[:len(paths)-1] {
		if f.Kind == kindToggle {
			t.Errorf("%q is a checkbox among the value rows", f.Label)
		}
	}
}

// The config file is shown, not edited: it is where the keystrokes land, and
// nothing in this view can move it.
func TestTheConfigFileRowIsReadOnly(t *testing.T) {
	f := fieldNamed(t, "Config file")

	if f.Kind != kindStatic {
		t.Fatalf("Config file is a %v; it must be static", f.Kind)
	}
	if f.focusable() {
		t.Error("the cursor can stop on a row no key acts upon")
	}
	if f.Value(config.Default()) == "" {
		t.Error("the row shows nothing")
	}
}

// The tab is `network`, not `docker`, and it is named after the config section
// it writes — as all five are.
//
// The one setting the old tab held was never a Docker setting: it names the
// container the route traces and the ports table run in. Renaming the tab
// without renaming the key would have left the one tab about to grow as the
// only one whose name says nothing about where its values land.
func TestEveryTabIsNamedAfterTheSectionItWrites(t *testing.T) {
	all := sections([]string{"default"}, command.ViewNames(), "/tmp/config.yaml", config.ForgeGitLab, forge.VocabularyFor(config.ForgeGitLab))

	want := []string{"app", "gitlab", "scan", "network", "status"}
	got := make([]string, 0, len(all))
	for _, s := range all {
		got = append(got, s.Title)
	}
	if len(got) != len(want) {
		t.Fatalf("the tabs are %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the tabs are %v, want %v — network keeps docker's slot", got, want)
		}
	}
}

// Every dial the netdiag view reads is editable, and each is bounded.
//
// internal/netcheck said its constants would become settings when somebody
// asked; this is the list they became, and an unbounded one would let a user
// write a zero that netcheck then has to defend itself against.
func TestTheNetworkTabEditsEveryNetdiagDial(t *testing.T) {
	cfg := config.Default()

	want := map[string]*int{
		"Check timeout (s)":     &cfg.Network.CheckTimeout,
		"Ping count":            &cfg.Network.PingCount,
		"Traceroute max hops":   &cfg.Network.TracerouteMaxHops,
		"Ports refresh (s)":     &cfg.Network.PortsRefreshInterval,
		"Expiry warning (days)": &cfg.Network.CertExpiryWarnDays,
	}

	for label, ref := range want {
		f := fieldNamed(t, label)
		if f.Kind != kindInteger {
			t.Errorf("%q is a %v, want an integer field", label, f.Kind)
			continue
		}
		if f.num(cfg) != ref {
			t.Errorf("%q does not address the setting it names", label)
		}
		if f.min < 1 {
			t.Errorf("%q admits %d; a zero dial is what netcheck has to normalize away", label, f.min)
		}
	}

	if f := fieldNamed(t, "Network tool image"); f.str(cfg) != &cfg.Network.ToolImage {
		t.Error("the tool image no longer addresses network.tool_image")
	}
}
