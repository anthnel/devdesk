// Package indicator defines the activity states a table row's first column can
// show, their glyphs, and the precedence between concurrent sources of state.
// It is pure: no dependency on theme, jobs or datatable.
package indicator

// State is what the first column says about a row. The zero value is Idle (rest).
type State int

const (
	Idle       State = iota // the row's own icon, or an empty cell
	Running                 // blinks; busy counts as Running
	Queued                  // waiting
	Failed                  // op failed, container dead or exited non-zero
	Suspended               // agent idle, container paused
	Restarting              // container restarting
	Done                    // job done, agent done
	Cancelled               // job cancelled
	Skipped                 // job skipped, container exited 0
	Unknown                 // agent unknown, container created
)

// states lists every state but Idle, in declaration order.
var states = []State{Running, Queued, Failed, Suspended, Restarting, Done, Cancelled, Skipped, Unknown}

var names = map[State]string{
	Idle: "idle", Running: "running", Queued: "queued", Failed: "failed",
	Suspended: "suspended", Restarting: "restarting", Done: "done",
	Cancelled: "cancelled", Skipped: "skipped", Unknown: "unknown",
}

var glyphs = map[State]string{
	Queued: "◴", Failed: "⊗", Suspended: "◐", Restarting: "◕",
	Done: "◉", Cancelled: "⊘", Skipped: "⊖", Unknown: "◌",
}

const (
	runningOn  = "●"
	runningOff = "○"
)

// States returns every state but Idle, in declaration order.
func States() []State { return append([]State(nil), states...) }

// Glyph returns the one-rune glyph of s; on is the blink phase (Running only).
// Idle has no glyph and yields an empty string.
func Glyph(s State, on bool) string {
	if s == Running {
		if on {
			return runningOn
		}
		return runningOff
	}
	return glyphs[s]
}

// Animated reports whether s blinks.
func Animated(s State) bool { return s == Running }

// String returns the lowercase name of s.
func (s State) String() string {
	if n, ok := names[s]; ok {
		return n
	}
	return "invalid"
}

// Activity is everything that can be true of one row at once.
type Activity struct {
	Running, Queued, Failed bool  // Failed = a remembered failure
	Observed                State // computed by the view; Idle if none
}

// Resolve applies running > queued > remembered failure > observed > idle.
func Resolve(a Activity) State {
	switch {
	case a.Running:
		return Running
	case a.Queued:
		return Queued
	case a.Failed:
		return Failed
	}
	return a.Observed
}
