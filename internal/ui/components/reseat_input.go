package components

import "charm.land/bubbles/v2/textinput"

// ReseatInput rebuilds the scroll window of a textinput for its current width,
// keeping its value, cursor position, Err and suggestions.
//
// bubbles scrolls a textinput through two internal offsets (offset and
// offsetRight) that only SetCursor, SetValue and Update refresh. After a
// SetWidth that shrinks the field, a cursor still inside the old window leaves
// that too wide window in place: the view overflows its box and the cursor cell
// can land beyond it (bubbles v2.2.1 and v2.2.2). Call it after every SetWidth
// that can shrink a field, from Update only (Rule 110), never from View.
//
// It never calls SetValue, which would clear Err, run the validator and, since
// bubbles v2.2.2, refresh the suggestions and reset the current one. CursorEnd
// moves the window to the end of the line for the current width, then SetCursor
// puts the cursor back, scrolling left only if it is left of that window.
// Suggestions, CharLimit and focus are not touched.
//
// It rebuilds the window from the end of the line, so call it only when the
// width changed: on a field that fits, with the cursor in the middle of a
// window, it scrolls the line for no reason.
//
// Remove it the day bubbles refreshes its window in SetWidth:
// TestShrinkWithoutReseatLeavesTheCursorOutside goes red then.
func ReseatInput(in *textinput.Model) {
	pos := in.Position()
	in.CursorEnd()
	in.SetCursor(pos)
}
