// Package termtitle saves and restores the terminal window title around a
// program run.
//
// Bubble Tea clears the title when it exits, so whatever the shell or the
// terminal had set before DevDesk started is lost. XTWINOPS can push the
// current title on the terminal's stack and pop it back afterwards. A terminal
// that does not implement the stack ignores both sequences.
package termtitle

import "io"

const (
	// pushTitle saves the window title on the terminal's title stack.
	pushTitle = "\x1b[22;2t"
	// popTitle restores the title saved by pushTitle.
	popTitle = "\x1b[23;2t"
)

// Preserve pushes the current window title, calls run, then pops it back. The
// pop is deferred so it is written on a normal return, on an error and while a
// panic unwinds; a panic keeps propagating with its original value. Only the
// two constant sequences are written, never variable data.
func Preserve(w io.Writer, run func() error) error {
	_, _ = io.WriteString(w, pushTitle)
	defer func() { _, _ = io.WriteString(w, popTitle) }()
	return run()
}
