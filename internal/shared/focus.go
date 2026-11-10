package shared

// TerminalFocusMsg tells a view whether the terminal window has the focus
// (DEVDESK-210). The router sends it only to the view on screen: on a blur or
// focus edge, and when a view enters the screen.
//
// Resume is true only on the focus edge that follows a blur: the view may
// catch up on what it paused (a refresh tick, for example). A view entering
// the screen gets Resume false, and the message never asks for a scan, an
// Init or a footer message.
type TerminalFocusMsg struct {
	Focused bool
	Resume  bool
}
