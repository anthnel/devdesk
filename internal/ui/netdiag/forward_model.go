package netdiag

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/forward"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The Forward tab: the open port redirections, and the form that opens one
// (§3.1).
//
// It holds no listener. The registry is the router's — a view is dropped whole
// by reinitializeViews on a config save and on a context switch, and a bound
// port going with it would be leaked in silence. This model asks
// (forward.Open / forward.Close / forward.Refresh) and is told
// (forward.ChangedMsg).

// forwardRefreshInterval is how often the tab asks for a fresh snapshot.
//
// It is a constant rather than a setting because the two things it shows move
// on their own — the connection counters — and neither is worth a context key
// (§3.1 keeps the forwards out of the configuration entirely). Two seconds is
// what the Ports tab defaults to, and the same reading is what makes them feel
// like one screen.
const forwardRefreshInterval = 2 * time.Second

// forwardTickMsg drives the snapshot chain.
type forwardTickMsg struct{}

func forwardTickCmd() tea.Cmd {
	return tea.Tick(forwardRefreshInterval, func(time.Time) tea.Msg { return forwardTickMsg{} })
}

// ForwardModel is the tab's state.
type ForwardModel struct {
	width  int
	height int

	table datatable.Model[forward.Forward]

	// form is non-nil while N's form has the screen (Rule 112).
	form *ForwardForm
	// confirmModal guards K. Stopping a forward drops the connections it is
	// carrying, which is not something to do on a mistyped key.
	confirmModal *components.ConfirmModal

	// ticking says a snapshot chain is alive. The chain runs only while there
	// is something to refresh and is restarted when a forward appears — the
	// shape of the router's job spinner (D5). Without it the tab would ask the
	// router for a broadcast every two seconds for the whole session, on a
	// table that is empty and cannot change on its own.
	ticking bool

	// footer is this tab's own message line, like every other tab's: a shared
	// one would let a message set here outlive the switch away.
	footer components.FooterMessage
}

// forwardColumns describes the table.
//
// Local and Target are what a forward *is* and never drop. Everything else is
// Optional, in the order it is worth losing: Total before Age before the
// error, since a count of everything ever carried is the least urgent of them
// (Rule 116 — a column gives way whole, never one cell at a time).
func forwardColumns() []datatable.Column[forward.Forward] {
	target := func(f forward.Forward) string { return f.Target }

	return []datatable.Column[forward.Forward]{
		{
			Title: "Local", Sizing: datatable.SizingFixed, MinWidth: 7,
			Cell:   func(f forward.Forward) string { return strconv.Itoa(f.LocalPort) },
			Search: func(f forward.Forward) string { return strconv.Itoa(f.LocalPort) },
		},
		{
			// Optional, but the last of them to go: it is what a route is
			// called, and losing it would leave a row that says only a port
			// every route shares. Empty for a raw TCP forward, which has none.
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: 12, Optional: true,
			Cell: func(f forward.Forward) string {
				if f.Name == "" {
					return "-"
				}
				return f.Name
			},
			Search: func(f forward.Forward) string { return f.Name },
			Style: func(f forward.Forward) lipgloss.Style {
				if f.Name == "" {
					return theme.DimStyle
				}
				return lipgloss.NewStyle()
			},
		},
		{
			Title: "Target", Sizing: datatable.SizingContent, MinWidth: 20, Flex: 1,
			Cell: target, Search: target,
		},
		{
			// Six cells is exactly "paused", the widest thing this column says.
			Title: "Live", Sizing: datatable.SizingFixed, MinWidth: 6,
			Cell: func(f forward.Forward) string {
				switch f.State {
				case forward.StatePaused:
					return f.State.String()
				case forward.StateUnbound:
					return "-"
				}
				return strconv.Itoa(f.Active)
			},
			Style: func(f forward.Forward) lipgloss.Style {
				if f.State != forward.StateLive || f.Active == 0 {
					return theme.DimStyle
				}
				return lipgloss.NewStyle()
			},
		},
		{
			Title: "Total", Sizing: datatable.SizingFixed, MinWidth: 7, Optional: true, DropFirst: true,
			Cell:  func(f forward.Forward) string { return strconv.FormatInt(f.Total, 10) },
			Style: dimWhenZero(func(f forward.Forward) int { return int(f.Total) }),
		},
		{
			// Age counts from the last bind, so a listener that is not bound has
			// none to report.
			Title: "Age", Sizing: datatable.SizingFixed, MinWidth: 9, Optional: true,
			Cell: func(f forward.Forward) string {
				if f.State != forward.StateLive {
					return "-"
				}
				return theme.TimeAgo(f.Opened)
			},
			Style: func(f forward.Forward) lipgloss.Style {
				if f.State != forward.StateLive {
					return theme.DimStyle
				}
				return lipgloss.NewStyle()
			},
		},
		{
			Title: "Last error", Sizing: datatable.SizingContent, MinWidth: 12, Optional: true,
			Cell: func(f forward.Forward) string {
				if f.LastErr == "" {
					return "-"
				}
				return f.LastErr
			},
			// The healthy case is the majority one and gets no colour; the
			// absence of an error reads as an absence (Rule 122).
			Style: func(f forward.Forward) lipgloss.Style {
				if f.LastErr == "" {
					return theme.DimStyle
				}
				return theme.StatusErrorStyle
			},
		},
	}
}

// dimWhenZero greys a counter that has nothing to say (Rule 122).
func dimWhenZero(count func(forward.Forward) int) func(forward.Forward) lipgloss.Style {
	return func(f forward.Forward) lipgloss.Style {
		if count(f) == 0 {
			return theme.DimStyle
		}
		return lipgloss.NewStyle()
	}
}

// newForwardModel creates the tab.
func newForwardModel() *ForwardModel {
	return &ForwardModel{
		table: datatable.New(datatable.Config[forward.Forward]{
			Columns: forwardColumns(),
			// The registry already hands them over oldest first, which is the
			// order they were opened in — the one order a row does not move in
			// when another forward is added.
			SortColumn: -1,
		}),
	}
}

// InEditMode returns true when the form, the search box or the confirmation
// holds the keyboard.
func (fm *ForwardModel) InEditMode() bool {
	return fm.form != nil || fm.table.InEditMode() || fm.confirmModal != nil
}

// resize lays the table out. fm.height is the viewport content height the app
// sent (Rule 124); the filter bar lives in the footer, outside it (Rule 116).
func (fm *ForwardModel) resize(width, height int) {
	fm.width = width
	fm.height = height
	if width == 0 {
		return
	}
	if fm.form != nil {
		fm.form.SetWidth(width)
	}
	fm.table.Resize(width, max(height, 3))
}

func (fm *ForwardModel) update(msg tea.Msg) (*ForwardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case forward.ChangedMsg:
		return fm.handleChanged(msg)
	case forwardTickMsg:
		return fm.handleTick()
	case ForwardFormSubmitMsg:
		return fm.handleFormSubmit(msg)
	case ForwardFormCancelMsg:
		fm.form = nil
		return fm, nil
	case components.ConfirmModalYesMsg:
		fm.confirmModal = nil
		return fm.closeSelected()
	case components.ConfirmModalNoMsg:
		fm.confirmModal = nil
		return fm, nil
	case tea.KeyMsg:
		return fm.handleKey(msg)
	}
	return fm, nil
}

// handleChanged takes the router's snapshot and restarts the chain if one is
// needed. The cursor and the scroll survive SetItems, which is what lets this
// run every two seconds.
func (fm *ForwardModel) handleChanged(msg forward.ChangedMsg) (*ForwardModel, tea.Cmd) {
	fm.table.SetItems(msg.Forwards)
	return fm, fm.ensureTick()
}

// ensureTick starts the chain when there is something to watch and none is
// alive. It is the one place a chain starts, so no path can leave two running.
func (fm *ForwardModel) ensureTick() tea.Cmd {
	if fm.ticking || len(fm.table.Items()) == 0 {
		return nil
	}
	fm.ticking = true
	return forwardTickCmd()
}

// handleTick asks the router for a snapshot and renews itself, or lets the
// chain die once the last forward is gone.
func (fm *ForwardModel) handleTick() (*ForwardModel, tea.Cmd) {
	if len(fm.table.Items()) == 0 {
		fm.ticking = false
		return fm, nil
	}
	return fm, tea.Batch(forward.Refresh, forwardTickCmd())
}

// handleFormSubmit closes the form and asks the router to open the forward, or
// the route when the form carried a name. Every refusal is the registry's to
// give, and arrives at the footer.
func (fm *ForwardModel) handleFormSubmit(msg ForwardFormSubmitMsg) (*ForwardModel, tea.Cmd) {
	fm.form = nil
	if msg.Name != "" {
		return fm, forward.OpenRoute(msg.Name, msg.Target)
	}
	return fm, forward.Open(msg.LocalPort, msg.Target)
}

func (fm *ForwardModel) handleKey(msg tea.KeyMsg) (*ForwardModel, tea.Cmd) {
	if fm.confirmModal != nil {
		var cmd tea.Cmd
		fm.confirmModal, cmd = fm.confirmModal.Update(msg)
		return fm, cmd
	}
	if fm.form != nil {
		return fm.handleFormKey(msg)
	}
	if fm.table.InEditMode() {
		cmd := fm.table.Update(msg)
		fm.table.GotoTop() // a narrowing query starts from the first match
		return fm, cmd
	}
	return fm.handleKeyNormal(msg)
}

// handleFormKey lets the form have the keyboard, and answers the refusals the
// form itself decides rather than letting the keypress vanish (Rule 130).
func (fm *ForwardModel) handleFormKey(msg tea.KeyMsg) (*ForwardModel, tea.Cmd) {
	if msg.String() == "enter" {
		if reason := fm.form.Problem(); reason != "" {
			return fm, fm.footer.Warn(reason)
		}
	}
	var cmd tea.Cmd
	fm.form, cmd = fm.form.Update(msg)
	return fm, cmd
}

func (fm *ForwardModel) handleKeyNormal(msg tea.KeyMsg) (*ForwardModel, tea.Cmd) {
	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "home", "end", "/":
		return fm, fm.table.Update(msg)
	case keymap.New:
		fm.form = NewForwardForm(fm.width)
		return fm, nil
	case keymap.Kill:
		return fm.confirmClose()
	case " ":
		return fm.toggleSelected()
	}
	return fm, nil
}

// Why an action does not apply, written once so the header, the footer and the
// tests cannot drift apart on the wording (Rule 129 — English US).
const (
	reasonNoForwardRow = "No forward selected"
)

// closable reports whether K has a forward to stop (Rule 130).
func (fm *ForwardModel) closable() shortcut.Availability {
	return fm.rowSelected()
}

// switchable reports whether space has a forward to pause or resume. It is the
// same question as closable's, asked once so the two cannot disagree.
func (fm *ForwardModel) switchable() shortcut.Availability {
	return fm.rowSelected()
}

func (fm *ForwardModel) rowSelected() shortcut.Availability {
	if _, ok := fm.table.Selected(); !ok {
		return shortcut.Unavailable(reasonNoForwardRow)
	}
	return shortcut.Availability{}
}

// toggleLabel is what space does to the selected row, for the header: pausing a
// live forward, resuming any other. With nothing selected it keeps the pair's
// name, as a greyed entry keeps the label of its action (Rule 130).
func (fm *ForwardModel) toggleLabel() string {
	if entry, ok := fm.table.Selected(); ok && entry.State != forward.StateLive {
		return "Resume forward"
	}
	return "Pause forward"
}

// toggleSelected asks the router to pause or resume the selected forward.
func (fm *ForwardModel) toggleSelected() (*ForwardModel, tea.Cmd) {
	if a := fm.switchable(); !a.Enabled() {
		return fm, fm.footer.Warn(a.Reason)
	}
	entry, _ := fm.table.Selected()
	return fm, forward.Toggle(entry.ID)
}

// confirmClose asks before dropping the connections the forward is carrying.
func (fm *ForwardModel) confirmClose() (*ForwardModel, tea.Cmd) {
	if c := fm.closable(); !c.Enabled() {
		return fm, fm.footer.Warn(c.Reason)
	}
	entry, _ := fm.table.Selected()
	from := entry.Addr()
	if entry.Name != "" {
		from = entry.URL()
	}
	body := fmt.Sprintf("Stop forwarding %s to %s?", from, entry.Target)
	body += "\n\nIt is removed from the saved forwards and will not come back at the next launch."
	if entry.Active > 0 {
		body += fmt.Sprintf("\n\n%d connection(s) in progress will be dropped.", entry.Active)
	}
	fm.confirmModal = components.NewConfirmModal("Delete Forward", body)
	return fm, nil
}

// closeSelected asks the router to stop the forward, once confirmed.
func (fm *ForwardModel) closeSelected() (*ForwardModel, tea.Cmd) {
	entry, ok := fm.table.Selected()
	if !ok {
		return fm, nil
	}
	return fm, forward.Close(entry.ID)
}

// view renders the form when it is open (Rule 112), and otherwise the table —
// always the table, empty or not (Rule 139).
func (fm *ForwardModel) view() string {
	if fm.form != nil {
		return fm.form.View()
	}
	return fm.table.View()
}

// status is what the footer says when no message is pending: a state, derived
// every frame, never set with a timer (Rule 128).
func (fm *ForwardModel) status() components.Status {
	if fm.form != nil {
		if fm.form.Named() {
			return components.Status{Text: "A named route is served on network.proxy_port, on 127.0.0.1 only"}
		}
		return components.Status{Text: "Forwards listen on 127.0.0.1 only, on a port at or above 1024"}
	}
	if len(fm.table.Items()) == 0 {
		return components.Status{Text: "No forward open — N opens one"}
	}
	return components.Status{}
}

// summaryLine counts what is open, for the header (Rule 139). When some are not
// bound it says how many are, because the total alone would read as all of them
// listening.
func (fm *ForwardModel) summaryLine() string {
	items := fm.table.Items()
	live := 0
	for _, f := range items {
		if f.State == forward.StateLive {
			live++
		}
	}
	if live == len(items) {
		return fmt.Sprintf("%d", len(items))
	}
	return fmt.Sprintf("%d live of %d", live, len(items))
}
