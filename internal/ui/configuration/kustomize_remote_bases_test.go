package configuration

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-767: kustomize builds an overlay with the network cut. The one row
// that opens it again sits in Kustomize's group of the tools tab, as a checkbox
// like Gitleaks' "Scan git history", and is off in a new context.

const allowRemoteBasesLabel = "Allow remote bases"

func TestTheToolsTabOffersAllowRemoteBasesUnderKustomizeOnly(t *testing.T) {
	m := newModel(t)

	found := 0
	for _, s := range m.sections {
		for _, f := range s.Fields {
			if f.Label != allowRemoteBasesLabel {
				continue
			}
			found++
			if f.Group != "Kustomize" || f.Kind != kindToggle || s.Title != toolsTab {
				t.Errorf("row %q: tab %q, group %q, kind %v, want a checkbox in Kustomize's group of %q",
					f.Label, s.Title, f.Group, f.Kind, toolsTab)
			}
			if f.hintOn == "" || f.hintOff == "" || f.hintOn == f.hintOff {
				t.Errorf("hints on %q off %q: each state needs its own sentence", f.hintOn, f.hintOff)
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d rows labelled %q, want 1", found, allowRemoteBasesLabel)
	}
}

// Space is the one key that flips a checkbox, and the value is written as it
// changes (a checkbox settles at once, so esc has nothing to offer).
func TestSpaceFlipsAllowRemoteBasesAndSavesIt(t *testing.T) {
	m := focusOn(t, newModel(t), "Kustomize › "+allowRemoteBasesLabel)
	if m.config.Scan.Tools.Kustomize.AllowRemoteBases {
		t.Fatal("a new context allows remote bases")
	}
	if testutil.ShortcutEnabled(m.GetShortcuts(), "esc") {
		t.Error("esc is offered on a checkbox, which settles at once")
	}
	before := m.hint(m.current())

	m = feed(t, m, testutil.Key("enter"))
	if m.config.Scan.Tools.Kustomize.AllowRemoteBases {
		t.Error("enter flipped a checkbox")
	}

	m = feed(t, m, testutil.Key("space"))
	if !m.config.Scan.Tools.Kustomize.AllowRemoteBases {
		t.Fatal("space did not turn Allow remote bases on")
	}
	if after := m.hint(m.current()); after == before {
		t.Errorf("the footer says %q in both states", after)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reloading the saved config: %v", err)
	}
	if !reloaded.Scan.Tools.Kustomize.AllowRemoteBases {
		t.Error("the file does not hold the switch that was just turned on")
	}

	m = feed(t, m, testutil.Key("space"))
	if m.config.Scan.Tools.Kustomize.AllowRemoteBases {
		t.Error("a second space did not turn it off")
	}
}
