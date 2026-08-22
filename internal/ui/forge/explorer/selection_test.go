package explorer

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The selection is roots plus exclusions rather than a list of repositories,
// so these tests are all about what it can answer *without* having discovered
// anything. Every path below names a node that was never fetched.

func TestTickingAGroupTakesEverythingUnderIt(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")

	for _, path := range []string{
		"acme/platform",
		"acme/platform/backend",
		"acme/platform/tools/ci",
	} {
		if !s.includes(path) {
			t.Errorf("%q is not included, but its group was ticked", path)
		}
	}
	if s.includes("acme/other") {
		t.Error("a sibling of the ticked group was included")
	}
	if s.includes("acme") {
		t.Error("the parent of the ticked group was included")
	}
}

// The trailing separator is the whole of it: a prefix match alone makes
// "acme/platform-tools" a child of "acme/platform".
func TestASiblingSharingANamePrefixIsNotADescendant(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")

	if s.includes("acme/platform-tools") {
		t.Error("acme/platform-tools was included by acme/platform")
	}
	if got := s.state("acme/platform"); got != theme.CheckAll {
		t.Errorf("state = %v, want CheckAll", got)
	}
}

func TestDrillingInAndUntickingCutsOneSubtree(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")
	s.toggle("acme/platform/legacy")

	if s.includes("acme/platform/legacy") {
		t.Error("the unticked subgroup is still included")
	}
	if s.includes("acme/platform/legacy/old-api") {
		t.Error("a child of the unticked subgroup is still included")
	}
	if !s.includes("acme/platform/backend") {
		t.Error("unticking one subgroup took its siblings with it")
	}
}

// Re-ticking below an exclusion turns that branch back on, which is what makes
// the control reversible at any depth rather than only at the level it was used.
func TestReTickingUnderAnExclusionTurnsItBackOn(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")
	s.toggle("acme/platform/legacy")
	s.toggle("acme/platform/legacy/keep-me")

	if !s.includes("acme/platform/legacy/keep-me") {
		t.Error("the re-ticked node is not included")
	}
	if !s.includes("acme/platform/legacy/keep-me/deeper") {
		t.Error("the re-ticked node does not carry its own children")
	}
	if s.includes("acme/platform/legacy/drop-me") {
		t.Error("re-ticking one child re-included its siblings")
	}
}

func TestTickingTwiceIsUnticking(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")
	s.toggle("acme/platform")

	if s.includes("acme/platform") {
		t.Error("ticking twice left the node selected")
	}
	if !s.isEmpty() {
		t.Error("the selection is not empty after the only root was unticked")
	}
}

// A single project is a root like any other (decision 8), and carries nothing
// below it.
func TestAProjectCanBeSelectedOnItsOwn(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform/backend")

	if !s.includes("acme/platform/backend") {
		t.Error("the project was not selected")
	}
	if s.includes("acme/platform/frontend") {
		t.Error("selecting one project took a sibling with it")
	}
	if got := s.state("acme/platform"); got != theme.CheckSome {
		t.Errorf("the parent group shows %v, want CheckSome — one child is on", got)
	}
}

func TestTheCheckboxStatesCoverTheThreeCases(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")
	s.toggle("acme/platform/legacy")

	tests := map[string]theme.CheckState{
		// Everything under it is on.
		"acme/platform/backend": theme.CheckAll,
		// On, but something below is off.
		"acme/platform": theme.CheckSome,
		// Off itself, but something below is on.
		"acme": theme.CheckSome,
		// Off, with nothing on below.
		"acme/platform/legacy":     theme.CheckNone,
		"acme/platform/legacy/old": theme.CheckNone,
		"other":                    theme.CheckNone,
	}

	for path, want := range tests {
		if got := s.state(path); got != want {
			t.Errorf("state(%q) = %v, want %v", path, got, want)
		}
	}
}

// The confirmation screen states this, because the repository count cannot be
// known until discovery has run (decision 7).
func TestTheCountsAreRootsAndExclusions(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")
	s.toggle("other/tools")
	s.toggle("acme/platform/legacy")

	roots, exclusions := s.counts()
	if roots != 2 || exclusions != 1 {
		t.Errorf("counts() = (%d, %d), want (2, 1)", roots, exclusions)
	}
	if len(s.rootPaths()) != 2 {
		t.Errorf("rootPaths() = %v, want the two roots", s.rootPaths())
	}
}

// Unticking a root removes it rather than leaving it behind an exclusion that
// says the same thing — two ways to express one state is how a display starts
// disagreeing with itself.
func TestUntickingARootDropsItRatherThanMaskingIt(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme/platform")
	s.toggle("acme/platform")

	if s.roots["acme/platform"] {
		t.Error("the root is still there, masked by an exclusion")
	}
	if got := s.state("acme/platform"); got != theme.CheckNone {
		t.Errorf("state = %v, want CheckNone", got)
	}
}

// A top-level group has no parent, so the walk up has to stop rather than
// looping on an empty string.
func TestATopLevelPathTerminates(t *testing.T) {
	s := newCloneSelection()
	s.toggle("acme")

	if !s.includes("acme/anything") {
		t.Error("a top-level root does not include its children")
	}
	if s.includes("elsewhere") {
		t.Error("a top-level root included an unrelated top-level path")
	}
}
