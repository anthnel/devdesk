// Package colorprofile forces a lipgloss colour profile in tests.
//
// Under `go test` lipgloss detects no TTY, falls back to the Ascii profile and
// strips every escape sequence, so an assertion about styling passes whatever
// the code does. Tests that check something is or is not styled (Rule 122) call
// TrueColor first, or Force from TestMain.
//
// This is a leaf package on purpose: it imports only lipgloss and termenv.
// Hosting it in internal/ui/testutil would create an import cycle for the
// packages that testutil itself depends on (testutil -> shortcut -> theme), so
// those packages could not use it from their own tests.
package colorprofile

import (
	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"
)

// TrueColor forces the TrueColor profile for the length of a test and restores
// the previous profile on Cleanup.
func TrueColor(t interface {
	Helper()
	Cleanup(func())
}) {
	t.Helper()
	t.Cleanup(Force())
}

// Force sets the TrueColor profile and returns a func restoring the previous
// one. It is meant for TestMain, where no *testing.T exists.
func Force() (restore func()) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	return func() { lipgloss.SetColorProfile(previous) }
}
