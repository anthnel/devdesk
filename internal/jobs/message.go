package jobs

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// StartMsg asks the router to register a run and then dispatch the work that
// fills it.
//
// The view builds the run — it is the only place that knows the targets — and
// the router admits it, stamps it with the current context and starts the work
// (D10). Registration happens in Update, never inside a Cmd (Rule 110).
//
// Work rides on the message rather than being batched beside it because the
// order matters: a transition naming a run the registry has not admitted yet is
// refused, and a tea.Batch gives no order. The router is the sequencer, which
// is a smaller thing to get right than a Sequence at every launch site — and it
// leaves the work runnable only by the router, so a test can read what was
// launched without launching it.
type StartMsg struct {
	Run Run

	// Work is a builder rather than a command because the context is stamped
	// by the router, one step after the view has described the run (D8). Work
	// that writes into a context-scoped cache needs that name, and reading it
	// back from the configuration inside the command is the defect the stamp
	// exists to close: a batch outliving a context switch wrote its last
	// results into the cache of the context it had switched to.
	//
	// It is never nil for a run with work: Start passes a builder that ignores
	// the name, so a launch site that has no use for it reads as it did.
	Work func(contextName string) tea.Cmd

	// Cancel stops the run's queue, and is nil for the kinds that have nothing
	// to stop. It travels here rather than arriving later like an item's cancel
	// because a progressive run owns its context from the launch: the pipeline
	// is started in Update, so the function exists before the run is admitted.
	Cancel context.CancelFunc

	// Invocation names the MCP call that asked for this run, and is empty when
	// a person pressed a key (§3.61).
	//
	// It travels on the message rather than being matched by time: a router
	// that remembered "an invocation is in flight" and gave its identifier to
	// the next StartMsg would have a window of one Update cycle, in which a
	// keypress can land — and the agent would be handed the identifier of the
	// scan the user just started by hand. It is the shape of Run.Context: a
	// fact carried by what it is about, decided once.
	Invocation string
}

// WithInvocation stamps the run a jobs.Start* command is about to register with
// the call that asked for it.
//
// A decorator rather than a fourth constructor: Start, StartInContext and
// StartCancellable already differ along two axes, and an invocation-carrying
// variant of each would be six. A view's MCP handler wraps the command it was
// already returning, at the one place that knows who asked.
//
// An empty invocation returns the command untouched, so a keyboard launch pays
// nothing and reads as it did.
func WithInvocation(invocation string, cmd tea.Cmd) tea.Cmd {
	if invocation == "" || cmd == nil {
		return cmd
	}
	return func() tea.Msg {
		msg := cmd()
		start, ok := msg.(StartMsg)
		if !ok {
			return msg
		}
		start.Invocation = invocation
		return start
	}
}

// RefusedMsg says an invocation will not produce a run, and why.
//
// It is the sibling of StartMsg and lives beside it for that reason: between
// them they are everything an invocation can come to. A keypress needs no such
// message — the view puts the same sentence in its footer (Rule 130) — which is
// why Refuse returns nil for an empty invocation rather than a message nobody
// would route.
//
// Reason is the view's own Availability.Reason, so the sentence an agent reads
// is the one the header greys with and the footer prints. One calculation,
// three readers.
type RefusedMsg struct {
	Invocation string
	Reason     string
}

// Refuse builds the message, or nothing at all for a keyboard launch.
func Refuse(invocation, reason string) tea.Cmd {
	if invocation == "" {
		return nil
	}
	return func() tea.Msg {
		return RefusedMsg{Invocation: invocation, Reason: reason}
	}
}

// Start builds the message for work that does not depend on the context.
func Start(run Run, work tea.Cmd) tea.Cmd {
	return StartInContext(run, func(string) tea.Cmd { return work })
}

// StartCancellable builds the message for a run whose queue can be stopped —
// today the clone, whose pipeline is launched from Update and hands back its
// context there and then.
func StartCancellable(run Run, work tea.Cmd, cancel context.CancelFunc) tea.Cmd {
	return func() tea.Msg {
		return StartMsg{
			Run:    run,
			Work:   func(string) tea.Cmd { return work },
			Cancel: cancel,
		}
	}
}

// CancelOpenMsg asks the router to stop the open run of a kind (D3, D7).
//
// A kind rather than an identifier: a progressive run belongs to a screen that
// owns the display while it goes, so there is one at a time and the view can
// name it without holding a JobID — which is the registry state D1 keeps out of
// views.
type CancelOpenMsg struct {
	Kind Kind
}

// CancelOpen builds the message.
func CancelOpen(kind Kind) tea.Cmd {
	return func() tea.Msg { return CancelOpenMsg{Kind: kind} }
}

// CancelMsg asks the router to stop a run by identifier (D7).
//
// An identifier here, where CancelOpenMsg names a kind, and the difference is
// which view is asking. A launch site cannot hold a JobID — it would have to
// before the registry allocated one — but `:jobs` reads the identifier out of
// the snapshot it is already rendering, which is the run the cursor is on.
type CancelMsg struct {
	ID JobID
}

// Cancel builds the message.
func Cancel(id JobID) tea.Cmd {
	return func() tea.Msg { return CancelMsg{ID: id} }
}

// CancelItemMsg asks the router to stop one target of a run.
type CancelItemMsg struct {
	ID     JobID
	Target string
}

// CancelItem builds the message.
func CancelItem(id JobID, target string) tea.Cmd {
	return func() tea.Msg { return CancelItemMsg{ID: id, Target: target} }
}

// StartInContext builds the message for work that writes into a context-scoped
// cache. The builder is called once, in Update, with the name the router
// stamped on the run — so every command it returns carries that name however
// long the batch runs.
func StartInContext(run Run, work func(contextName string) tea.Cmd) tea.Cmd {
	return func() tea.Msg { return StartMsg{Run: run, Work: work} }
}

// ChangedMsg carries what the registry holds to every view that might show it.
// It is the whole of the broadcast (D1): a view keeps its own copy of a
// snapshot rather than reading a pointer the router writes, so nothing is read
// from View() while something else is writing it.
//
// It is sent on two occasions, and they are different in nature: a transition —
// something actually happened — and a spinner tick, where only the frame moved.
// The receiver does not have to tell them apart, which is why there is one
// message and not two.
//
// It lives in this package rather than in the router's so that every view can
// name it without importing the router, which imports them.
type ChangedMsg struct {
	// Runs is a copy, oldest first, every context included. Filtering on the
	// current one is the view's business (D8, FilterContext).
	Runs []Run

	// Frame is the current spinner frame, bare — no escape sequence. Rule 122:
	// a table cell is measured before it is styled, so a styled frame of seven
	// visible cells measures twenty-eight and is truncated inside its own
	// escape, which then bleeds down the rest of the table.
	Frame string

	// RenderedFrame is the same frame styled, which is what a footer takes
	// (Rule 128 — measurement there goes through lipgloss.Width, which ignores
	// escapes). The router fills both so the two readings of one frame cannot
	// drift, and so this package needs to know nothing about styling.
	RenderedFrame string
}

// Running counts the runs in the snapshot that have not settled. It saves every
// receiver the same three lines.
func (m ChangedMsg) Running() int {
	return len(Unfinished(m.Runs))
}
