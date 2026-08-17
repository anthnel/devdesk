package components

import (
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// FooterMessage is the one line of state every view shows below its viewport
// (Rules 124, 128). It owns both the text and the three-second timer, which is
// what makes it a component rather than a rendering helper: eight views each
// carried their own pair of fields, their own clear message and their own
// lipgloss block, and that is how the errors ended up left-aligned everywhere
// while the notices were centred — nobody ever centred the error branch.
//
// Usage, from Update() only (Rule 110):
//
//	case SomethingFailedMsg:
//	    return m, m.footer.Error("Failed to load — check logs")
//
//	default:
//	    if m.footer.Handle(msg) {
//	        return m, nil
//	    }
//
// And from View(), which stays read-only:
//
//	m.footer.View(width, components.Status{Text: m.actionLine()})

// FooterMsgDuration is how long a timed message stays on screen (Rule 128).
//
// It is an exported var rather than a const because tea.Tick really sleeps, and
// a test that inspects a Cmd carrying this timer — testutil.Msgs runs the whole
// batch — pays the full three seconds per assertion. internal/ui/testutil
// shortens it from its init, so every view test package gets that for free; the
// production value is what this file states and what
// TestAMessageGetsThreeSeconds pins.
var FooterMsgDuration = 3 * time.Second

// Level is a footer message's severity. It decides the colour and nothing else.
//
// The three are defined by what happened, not by how it feels:
//
//	LevelError   — an operation failed, or the system refused it.
//	LevelWarning — the action cannot be honoured as asked, but nothing failed:
//	               a precondition is unmet, or it is already running.
//	LevelInfo    — a neutral fact, or an operation that succeeded.
type Level int

const (
	LevelInfo Level = iota
	LevelWarning
	LevelError
)

// Style returns the style a level renders in.
func (l Level) Style() lipgloss.Style {
	switch l {
	case LevelError:
		return theme.FooterErrorStyle
	case LevelWarning:
		return theme.FooterWarnStyle
	default:
		return theme.FooterInfoStyle
	}
}

// Status is what a view derives on every frame: a progress line, a hint, or a
// table loading its rows. It carries no timer — a batch outlives the three
// seconds a message gets, which is why the sync and prune lines were rendered
// from state rather than assigned to footerInfo in the first place.
//
// It is a parameter of View rather than a field of FooterMessage because it is
// derived, not stored: containers' action line comes from BusyLabels(), which
// changes with no event to push it on.
type Status struct {
	Text string
	// Spinner prepends the current spinner frame — a table loading its rows.
	Spinner bool
}

// footerMsgSeq numbers messages so a timer can name the one it was started for.
var footerMsgSeq atomic.Uint64

// ClearFooterMsg expires the message identified by ID.
//
// The ID is what makes the type safe to share across packages. Two things would
// otherwise go wrong: a timer started by one view could clear the message of
// whichever view is active when it lands, and — the bug that was already
// present in all eight local implementations — a message set at t+2.9s was
// wiped at t+3s by its predecessor's timer.
type ClearFooterMsg struct{ ID uint64 }

// FooterMessage holds the current message, its level and its identity.
type FooterMessage struct {
	text         string
	level        Level
	id           uint64
	spinnerFrame string
}

// Info, Warn and Error set the message and return its expiry timer. Call them
// from Update() and return the Cmd — a message set without its timer never
// clears (Rule 128).
func (f *FooterMessage) Info(text string) tea.Cmd  { return f.set(text, LevelInfo) }
func (f *FooterMessage) Warn(text string) tea.Cmd  { return f.set(text, LevelWarning) }
func (f *FooterMessage) Error(text string) tea.Cmd { return f.set(text, LevelError) }

func (f *FooterMessage) set(text string, level Level) tea.Cmd {
	if text == "" {
		f.Clear()
		return nil
	}
	f.text = text
	f.level = level
	f.id = footerMsgSeq.Add(1)

	id := f.id
	return tea.Tick(FooterMsgDuration, func(time.Time) tea.Msg {
		return ClearFooterMsg{ID: id}
	})
}

// Clear removes the message now, without waiting for its timer.
func (f *FooterMessage) Clear() {
	f.text = ""
	f.level = LevelInfo
	f.id = 0
}

// Handle consumes a ClearFooterMsg addressed to the current message and reports
// whether it did. A stale ID is dropped, and so is one belonging to another
// view: both leave the message on screen for the time it was given.
func (f *FooterMessage) Handle(msg tea.Msg) bool {
	expiry, ok := msg.(ClearFooterMsg)
	if !ok {
		return false
	}
	if f.id == 0 || expiry.ID != f.id {
		return false
	}
	f.Clear()
	return true
}

// SetSpinnerFrame stores the frame a loading Status renders with. Call it from
// the view's spinner.TickMsg handler, passing spinner.View().
//
// It takes the **rendered** spinner rather than a bare frame, which is what
// theme.SpinnerMessage took before it: every view already sets its spinner's
// Style to theme.SpinnerStyle(), so the colour is decided there and restyling
// here would nest one sequence inside another.
//
// That is the opposite of the rule for a table cell (Rule 122), and the
// difference is what is doing the measuring. A cell is measured by runewidth,
// which counts an escape's bytes as columns; this line is measured by
// lipgloss.Width, which does not.
func (f *FooterMessage) SetSpinnerFrame(frame string) { f.spinnerFrame = frame }

// IsSet reports whether a message is currently displayed.
func (f *FooterMessage) IsSet() bool { return f.text != "" }

// Text and Level report what is displayed. They exist for tests and for the
// netdiag header, which picks the active tab's message.
func (f *FooterMessage) Text() string { return f.text }
func (f *FooterMessage) Level() Level { return f.level }

// ID identifies the message currently displayed, 0 when there is none. It is
// what the timer names, and it lets a test in another package build the expiry
// this message will receive — running the real Cmd sleeps for three seconds,
// which is a long time to pay per assertion.
func (f *FooterMessage) ID() uint64 { return f.id }

// View renders the footer's line of text, always at the full width and always
// centred — including when it is empty, because Rule 124 budgets a line for it
// whatever it holds.
//
// Precedence: error, warning, info, then the derived status. A failure the user
// has not read yet matters more than the progress of what is still running.
func (f *FooterMessage) View(width int, status Status) string {
	if width <= 0 {
		return ""
	}

	// A timed message wins over the derived status: a failure the user has not
	// read yet matters more than the progress of what is still running.
	//
	// Truncation is not cosmetic — a line wider than the viewport wraps onto a
	// second one, and the router budgeted exactly one (Rule 124). Everything
	// below it would shift by a line.
	if f.text != "" {
		return centerLine(f.level.Style().Render(theme.TruncateWidth(f.text, width)), width)
	}
	if status.Text == "" {
		return theme.EmptyLineBg(width)
	}

	// lipgloss.Width, not theme.StringWidth: the frame arrives already styled,
	// and runewidth would count its escape sequence as columns.
	spinner, budget := "", width
	if status.Spinner && f.spinnerFrame != "" {
		spinner = f.spinnerFrame + theme.Bg(" ")
		budget -= lipgloss.Width(f.spinnerFrame) + 1
	}
	if budget <= 0 {
		return theme.EmptyLineBg(width)
	}
	return centerLine(spinner+theme.FooterInfoStyle.Render(theme.TruncateWidth(status.Text, budget)), width)
}

// centerLine centres already-styled content on a line filled with the app
// background. lipgloss does not inherit a background (Rule 115), so the padding
// either side has to carry one of its own.
func centerLine(content string, width int) string {
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Width(width).
		Align(lipgloss.Center).
		Render(content)
}
