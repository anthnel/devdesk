package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The header's enter entry follows the focus of the browser form (DEVDESK-300,
// IS-11, step 3, Rule 130): one entry, keys never change, a lit enter always
// acts, a greyed enter never does.

type formFocus struct {
	name  string
	field func(*RegistryBrowser) int
	// wantLabel is "" where the label is not pinned (a greyed checkbox).
	wantLabel   string
	wantEnabled bool
}

// formFocuses lists every focus position of the form for one mode.
func formFocuses(searcher bool) []formFocus {
	repoLabel, firstLabel := "Browse tags", "Browse tags"
	if searcher {
		repoLabel, firstLabel = "Search", "Search"
	}
	out := []formFocus{
		{"Repository", func(*RegistryBrowser) int { return brFieldRepo }, repoLabel, true},
		{"first checkbox", func(b *RegistryBrowser) int { return b.brFieldReg(0) }, "", false},
		{"last checkbox", func(b *RegistryBrowser) int { return b.brFieldReg(len(b.rows) - 1) }, "", false},
		{"first button", func(b *RegistryBrowser) int { return b.brFieldButton(0) }, firstLabel, true},
	}
	if searcher {
		out = append(out, formFocus{"Open Tags button",
			func(b *RegistryBrowser) int { return b.brFieldButton(1) }, "Open tags", true})
	}
	return out
}

type formMode struct {
	name     string
	model    func(*testing.T) Model
	searcher bool
}

var formModes = []formMode{
	{"flat, no Searcher", browsingModel, false},
	{"flat, Searcher", browsingModel, true},
	{"grouped, no Searcher", groupedModel, false},
	{"grouped, Searcher", groupedModel, true},
}

func setSearcher(t *testing.T, on bool) {
	t.Helper()
	if on {
		withImageSearcher(t, &fakeSearcher{})
	} else {
		withImageSearcher(t, nil)
	}
}

// formAt returns a model with a name typed and the focus on the given position.
func formAt(t *testing.T, mode formMode, f formFocus) Model {
	t.Helper()
	m := typeInto(t, mode.model(t), "nginx")
	return downTo(t, m, f.field(m.registryBrowser))
}

func enterEntry(t *testing.T, sc shortcut.Shortcuts) shortcut.Shortcut {
	t.Helper()
	n := 0
	var got shortcut.Shortcut
	for _, s := range sc {
		if s.Key == "enter" {
			got = s
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d enter entries in the header %v, want exactly one", n, testutil.ShortcutKeys(sc))
	}
	return got
}

func TestTheFormEnterLabelFollowsTheFocus(t *testing.T) {
	for _, mode := range formModes {
		for _, f := range formFocuses(mode.searcher) {
			t.Run(mode.name+"/"+f.name, func(t *testing.T) {
				setSearcher(t, mode.searcher)
				e := enterEntry(t, formAt(t, mode, f).GetShortcuts())
				if e.Disabled == f.wantEnabled {
					t.Errorf("enter Disabled = %v, want %v", e.Disabled, !f.wantEnabled)
				}
				if f.wantLabel != "" && e.Description != f.wantLabel {
					t.Errorf("enter label = %q, want %q", e.Description, f.wantLabel)
				}
			})
		}
	}
}

func TestTheFormKeysNeverChangeWithTheFocus(t *testing.T) {
	want := []string{"enter", "esc"}
	for _, mode := range formModes {
		for _, f := range formFocuses(mode.searcher) {
			t.Run(mode.name+"/"+f.name, func(t *testing.T) {
				setSearcher(t, mode.searcher)
				got := testutil.ShortcutKeys(formAt(t, mode, f).GetShortcuts())
				if !sameStrings(got, want) {
					t.Errorf("keys = %v, want %v", got, want)
				}
			})
		}
	}
}

// A lit enter always acts (a Cmd or a state change); a greyed one only advances.
func TestEnterIsLitWhereItActs(t *testing.T) {
	for _, mode := range formModes {
		for _, f := range formFocuses(mode.searcher) {
			t.Run(mode.name+"/"+f.name, func(t *testing.T) {
				setSearcher(t, mode.searcher)
				withFullTagRecorder(t)
				m := formAt(t, mode, f)
				lit := testutil.ShortcutEnabled(m.GetShortcuts(), "enter")
				b := m.registryBrowser
				field := b.focusedField

				m, cmd := step(t, m, testutil.Key("enter"))
				acted := cmd != nil || m.registryBrowser.state != browserStateInput
				if lit && !acted {
					t.Error("enter is lit but did nothing")
				}
				if !lit && acted {
					t.Error("enter is greyed but acted")
				}
				if !lit && m.registryBrowser.focusedField != field+1 {
					t.Errorf("greyed enter: focus = %d, want %d", m.registryBrowser.focusedField, field+1)
				}
			})
		}
	}
}
