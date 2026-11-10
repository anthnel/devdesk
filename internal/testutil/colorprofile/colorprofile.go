// Package colorprofile used to force a Lip Gloss colour profile in tests.
//
// Lip Gloss v2 has no global colour profile: Style.Render always emits the
// full-colour SGR sequence and the downsampling happens at the output writer.
// A test that checks something is or is not styled (Rule 122) therefore needs
// no setup, and TrueColor and Force do nothing. They keep their signatures so
// the callers stay unchanged; the package goes away in DEVDESK-201 step F.
//
// This is a leaf package on purpose: hosting it in internal/ui/testutil would
// create an import cycle for the packages that testutil itself depends on
// (testutil -> shortcut -> theme).
package colorprofile

// TrueColor is a no-op kept for its callers.
func TrueColor(t interface {
	Helper()
	Cleanup(func())
}) {
	t.Helper()
}

// Force is a no-op kept for TestMain callers; the returned func does nothing.
func Force() (restore func()) {
	return func() {}
}
