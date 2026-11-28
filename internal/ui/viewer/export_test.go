package viewer

// SwapClipboard replaces the clipboard write for the external test package of
// this directory (canary_inspect_test.go, OC-23) and returns what puts it back.
// Like every _test.go file it is compiled into the test binary only.
func SwapClipboard(write func(string) error) (restore func()) {
	previous := writeClipboard
	writeClipboard = write
	return func() { writeClipboard = previous }
}
