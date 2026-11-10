package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-266: Space reaches the launch form's text fields, so an entrypoint
// like `sh -c "..."` or a volume path with a blank can be typed.
func TestLaunchFormSpaceIsInsertedInEachTextField(t *testing.T) {
	tests := []struct {
		name  string
		field func(f *LaunchForm) int
		value func(f *LaunchForm) string
	}{
		{"entrypoint", (*LaunchForm).fieldEntrypoint, func(f *LaunchForm) string { return f.entrypointInput.Value() }},
		{"env", (*LaunchForm).fieldEnv, func(f *LaunchForm) string { return f.envInput.Value() }},
		{"volumes", (*LaunchForm).fieldVolumes, func(f *LaunchForm) string { return f.volInput.Value() }},
		{"user", (*LaunchForm).fieldUser, func(f *LaunchForm) string { return f.userInput.Value() }},
		{"extra ports", (*LaunchForm).fieldExtraPorts, func(f *LaunchForm) string { return f.extraPortsInput.Value() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newKeysForm()
			focusLaunchField(f, tc.field(f))

			f, _ = f.Update(testutil.Type("a")[0])
			f, _ = f.Update(testutil.Key("space"))
			f, _ = f.Update(testutil.Type("b")[0])

			if got := tc.value(f); got != "a b" {
				t.Errorf("%s = %q after typing a, space, b, want %q", tc.name, got, "a b")
			}
		})
	}
}

func TestLaunchFormSpaceIsInsertedInTheNameField(t *testing.T) {
	f := newKeysForm()
	focusLaunchField(f, 0)

	f, _ = f.Update(testutil.Type("a")[0])
	f, _ = f.Update(testutil.Key("space"))
	f, _ = f.Update(testutil.Type("b")[0])

	if got := f.nameInput.Value(); got != "a b" {
		t.Errorf("name = %q after typing a, space, b, want %q", got, "a b")
	}
}

func TestLaunchFormSpaceStillTogglesAPort(t *testing.T) {
	f := newKeysForm()
	focusLaunchField(f, f.firstPortField())
	before := f.ports[0].enabled

	f, _ = f.Update(testutil.Key("space"))

	if f.ports[0].enabled == before {
		t.Errorf("space left the port enabled=%v, want it flipped", before)
	}
}
