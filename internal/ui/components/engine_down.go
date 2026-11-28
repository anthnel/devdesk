package components

import "github.com/anthnel/devdesk/internal/docker"

// EngineDown is the derived state of a view whose engine could not be asked at all
// (OC-24, DEVDESK-822): the binary is not on PATH, the daemon does not answer, or
// the socket refuses this user.
//
// It is not a footer message. A message expires after three seconds while the
// engine stays down, and a list refresh runs every two to ten seconds, so the line
// would blink or vanish. The view holds this state and returns Status() from its
// own status() on every frame (Rule 128: a state, derived, never timed); it ends
// with Clear, which the first successful list calls.
//
// The zero value is "the engine is fine" and is usable as a struct field.
type EngineDown struct {
	text string
}

// Observe records err when an *docker.UnavailableError with a sentence is found in
// its chain and reports whether it did. Any other error, nil included, leaves the
// state as it was: it ends on a successful list, never because an unrelated
// failure went by.
func (e *EngineDown) Observe(err error) bool {
	text, ok := docker.UnavailableMessage(err)
	if !ok {
		return false
	}
	e.text = text
	return true
}

// Clear ends the state.
func (e *EngineDown) Clear() { e.text = "" }

// Active reports whether the engine is currently held as unavailable.
func (e EngineDown) Active() bool { return e.text != "" }

// Text is the sentence held, "" when none is.
func (e EngineDown) Text() string { return e.text }

// Status is the footer line of the state: the sentence at level Error while it is
// held, the zero Status otherwise. It has no Activity: a down engine is not work
// in progress.
func (e EngineDown) Status() Status {
	if e.text == "" {
		return Status{}
	}
	return Status{Text: e.text, Level: LevelError}
}
