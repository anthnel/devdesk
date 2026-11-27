// Package enginefixture holds the recorded exchanges with a container engine
// (OC-08, DEVDESK-805): one text file per exchange (".cli"), what the engine
// answered to one argv, byte for byte, and the replay that serves them.
//
// A fixture is the tool (docker or podman), its version, its mode (rootful or
// rootless), the argv without the binary name, and the exit code, stdout and
// stderr. Parse reads a file and Format writes it back: for any file Parse
// accepts, Format(Parse(b)) == b, so there is exactly one way to write a
// fixture. Replay answers an argv from a selection of fixtures and remembers
// what it answered.
//
// The package never starts a process and never imports "testing": the helpers
// that serve a fixture live in _test.go files of internal/docker and in
// internal/testutil/fakecli. Only those may import this package (guard E1,
// TestOnlyTestsImportEngineFixture), so the fixtures never reach the dk binary.
// The format and the rules are described in docs/architecture/network.md,
// "Recorded fixtures".
package enginefixture
