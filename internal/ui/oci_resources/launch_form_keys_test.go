package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-200 characterization of the launch form's checkboxes: Space flips
// them, Enter never does (Rule 135).

type launchCheckbox struct {
	name  string
	field func(f *LaunchForm) int
	get   func(f *LaunchForm) bool
}

func launchCheckboxes() []launchCheckbox {
	return []launchCheckbox{
		{"remove", (*LaunchForm).fieldOptionRemove, func(f *LaunchForm) bool { return f.optionRemove }},
		{"detach", (*LaunchForm).fieldOptionDetach, func(f *LaunchForm) bool { return f.optionDetach }},
		{"interactive", (*LaunchForm).fieldOptionInteractive, func(f *LaunchForm) bool { return f.optionInteractiveTTY }},
		{"first port", (*LaunchForm).firstPortField, func(f *LaunchForm) bool { return f.ports[0].enabled }},
	}
}

func newKeysForm() *LaunchForm {
	return NewLaunchForm("api:v1", []string{"80/tcp"}, nil, 120)
}

func focusLaunchField(f *LaunchForm, field int) {
	f.focusedField = field
	f.updateFocus()
}

func TestLaunchFormSpaceTogglesEachCheckbox(t *testing.T) {
	for _, cb := range launchCheckboxes() {
		t.Run(cb.name, func(t *testing.T) {
			f := newKeysForm()
			focusLaunchField(f, cb.field(f))
			start := cb.get(f)

			f, _ = f.Update(testutil.Key(" "))
			if cb.get(f) == start {
				t.Errorf("space on %s left it at %v, want it flipped", cb.name, start)
			}
			f, _ = f.Update(testutil.Key(" "))
			if cb.get(f) != start {
				t.Errorf("a second space on %s left it at %v, want %v", cb.name, cb.get(f), start)
			}
		})
	}
}

func TestLaunchFormDetachAndInteractiveExcludeEachOther(t *testing.T) {
	f := newKeysForm()

	focusLaunchField(f, f.fieldOptionDetach())
	f, _ = f.Update(testutil.Key(" "))
	focusLaunchField(f, f.fieldOptionInteractive())
	f, _ = f.Update(testutil.Key(" "))
	if !f.optionInteractiveTTY || f.optionDetach {
		t.Errorf("interactive after detach: interactive=%v detach=%v, want true/false", f.optionInteractiveTTY, f.optionDetach)
	}

	focusLaunchField(f, f.fieldOptionDetach())
	f, _ = f.Update(testutil.Key(" "))
	if !f.optionDetach || f.optionInteractiveTTY {
		t.Errorf("detach after interactive: detach=%v interactive=%v, want true/false", f.optionDetach, f.optionInteractiveTTY)
	}
}

func TestLaunchFormEnterNeverTogglesACheckbox(t *testing.T) {
	for _, cb := range launchCheckboxes() {
		t.Run(cb.name, func(t *testing.T) {
			f := newKeysForm()
			field := cb.field(f)
			focusLaunchField(f, field)
			start := cb.get(f)

			f, _ = f.Update(testutil.Key("enter"))
			if cb.get(f) != start {
				t.Errorf("enter on the %s checkbox changed it to %v; Enter must not toggle a checkbox (Rule 135)", cb.name, cb.get(f))
			}
			if f.focusedField != field+1 {
				t.Errorf("enter on the %s checkbox moved focus to %d, want %d (advance)", cb.name, f.focusedField, field+1)
			}
		})
	}
}
