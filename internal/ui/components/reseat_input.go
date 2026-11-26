package components

import "charm.land/bubbles/v2/textinput"

// ReseatInput rebuilds the scroll window of a textinput for its current width,
// keeping its value, cursor position and Err.
//
// bubbles scrolls a textinput through two internal offsets (offset and
// offsetRight) that only SetCursor, SetValue and Update refresh. After a
// SetWidth that shrinks the field, a cursor still inside the old window leaves
// that too wide window in place: the view overflows its box and the cursor cell
// can land beyond it (bubbles v2.2.1 and v2.2.2). Call it after every SetWidth
// that can shrink a field, from Update only (Rule 110), never from View.
//
// SetValue clears Err when the field has no validator, so Err is restored. With
// a validator, SetValue runs it two more times (with "" and with the value),
// which must therefore be cheap and free of side effects. Suggestions, CharLimit
// and focus are not touched.
//
// It rebuilds the window from the end of the line, so call it only when the
// width changed: on a field that fits, with the cursor in the middle of a
// window, it scrolls the line for no reason.
//
// Remove it the day bubbles refreshes its window in SetWidth:
// TestShrinkWithoutReseatLeavesTheCursorOutside goes red then.
func ReseatInput(in *textinput.Model) {
	value, pos, err := in.Value(), in.Position(), in.Err
	in.SetValue("")
	in.SetValue(value)
	in.SetCursor(pos)
	in.Err = err
}
