package viewer

import (
	"log"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/viewer"
)

// Init loads the source, if there is one.
func (m Model) Init() tea.Cmd {
	if m.source == nil {
		return nil
	}
	return tea.Batch(loadCmd(m.source), m.spinner.Tick)
}

// loadCmd fetches a source's content. Every read the viewer does goes through
// here, on a command's goroutine — Update does no I/O (Rule 110).
func loadCmd(source viewer.Source) tea.Cmd {
	return func() tea.Msg {
		data, err := source.Load()
		return DocumentLoadedMsg{
			Name:    source.Name(),
			Kind:    source.Kind(),
			Content: data,
			Err:     err,
		}
	}
}

// followTickMsg asks for the next read of a followed source. It carries the
// generation it was started for, so a loop the user has stopped cannot
// resurrect itself — see stopFollowing.
type followTickMsg struct{ gen int }

func followTickCmd(every time.Duration, gen int) tea.Cmd {
	// A non-positive interval would make tea.Tick fire without pausing and spin
	// one docker exec per frame. Refused here rather than trusted, the way the
	// ports tab refuses a hand-edited 0.
	if every <= 0 {
		every = defaultFollowInterval
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return followTickMsg{gen: gen} })
}

// defaultFollowInterval is the floor a source with no sensible answer falls to.
const defaultFollowInterval = 2 * time.Second

// InEditMode keeps command mode out while the search field has the keyboard.
func (m Model) InEditMode() bool { return m.bar.InEditMode() }

// FilterBarVisible tells the router to close the viewport border into the bar
// (Rule 136).
func (m Model) FilterBarVisible() bool {
	// Every display but the tree is text, and the search belongs to all of
	// them: a rendered Markdown is searched on what it shows, which is the only
	// answer that agrees with the screen.
	return m.bar.IsVisible() && m.display != displayTree
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case DocumentLoadedMsg:
		return m.handleDocumentLoaded(msg)

	case PagerExitMsg:
		return m.handlePagerExit(msg)

	case followTickMsg:
		return m.handleFollowTick(msg)

	case spinner.TickMsg:
		// The chain stops when nothing is loading, and Init/reload restart it.
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.footer.SetSpinnerFrame(m.spinner.View())
		return m, cmd
	}

	m.footer.Handle(msg)
	return m, nil
}

// resize lays out whichever half is on screen. Both are sized, not just the
// active one: a `f` between two window sizes would otherwise show a pane laid
// out for the previous width.
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height

	m.tree.Resize(width, height)

	m.textViewport.Width = width
	m.textViewport.Height = max(height, 1)
	m.bar.Resize(width)
	m.rebuildText() // the wrap and the padding both depend on the width
}

func (m Model) handleDocumentLoaded(msg DocumentLoadedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		log.Printf("ERROR [viewer] load %s: %v", msg.Name, msg.Err)
		return m, m.footer.Error(loadErrorMessage(msg.Err))
	}
	m.footer.Clear()
	return m, m.applyDocument(viewer.Open(msg.Name, msg.Kind, msg.Content))
}

// loadErrorMessage names the two refusals and hides the rest behind the log
// (Rule 128): "file is too large" is something the user can act on, an OS error
// string in a footer is not.
func loadErrorMessage(err error) string {
	switch err {
	case viewer.ErrTooLarge:
		return "File is too large to view"
	case viewer.ErrBinary:
		return "Not a text file"
	default:
		return "Failed to open — check logs"
	}
}

func (m Model) handlePagerExit(msg PagerExitMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [viewer] pager: %v", msg.Err)
	}
	// Whatever the pager or the follow showed, the document may have moved on
	// while the TUI was suspended. Reloading is the only way back to something
	// true.
	if m.source == nil {
		return m, nil
	}
	m.loading = true
	return m, loadCmd(m.source)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.bar.InEditMode() {
		var cmd tea.Cmd
		m.bar, cmd = m.bar.Update(msg)
		m.rebuildText()
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		return m.goBack()
	case "f":
		m.toggleDisplay()
		return m, nil
	case "c":
		m.toggleHighlight()
		return m, nil
	case "ctrl+r":
		return m.reload()
	}

	if m.display == displayTree {
		return m.handleTreeKey(msg)
	}
	return m.handleTextKey(msg)
}

// goBack returns to whoever opened the document. With no origin — the empty
// viewer the router builds for its own contract test — there is nowhere to go,
// and swallowing esc silently is better than switching to a view nobody asked
// for.
func (m Model) goBack() (tea.Model, tea.Cmd) {
	if m.OriginView == "" {
		return m, nil
	}
	// Leaving stops the clock. The router keeps this view, so a loop left
	// running would go on shelling out to docker every two seconds for a
	// document nobody is looking at — and the user would come back to a pane
	// that had scrolled itself to the bottom while they were gone.
	m = m.stopFollowing()
	origin := m.OriginView
	return m, func() tea.Msg { return BackToOriginMsg{Origin: origin} }
}

func (m Model) reload() (tea.Model, tea.Cmd) {
	if m.source == nil {
		return m, nil
	}
	m.loading = true
	return m, tea.Batch(loadCmd(m.source), m.spinner.Tick)
}

func (m Model) handleTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "right":
		m.toggleNode(true)
		return m, nil
	case "left":
		m.toggleNode(false)
		return m, nil
	}
	return m, m.tree.Update(msg)
}

func (m Model) handleTextKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "/":
		return m, m.bar.ActivateSearch()
	case "w":
		m.wrap = !m.wrap
		m.rebuildText()
		return m, nil
	case "v":
		if !m.isLog() {
			return m, nil
		}
		m.cycleVerbosity()
		return m, nil
	case "t":
		return m.toggleTimestamps()
	case keymap.Fetch:
		return m.toggleFollow()
	case keymap.Pager:
		return m.openPager()

	// The scroll keys. `q` is deliberately absent: it is the application's quit
	// key (Rule 111), and the logs pane this replaced was the one screen that
	// swallowed it.
	case "up":
		m.textViewport.ScrollUp(1)
	case "down":
		m.textViewport.ScrollDown(1)
	case "pgup":
		m.textViewport.HalfPageUp()
	case "pgdown":
		m.textViewport.HalfPageDown()
	case "home":
		m.textViewport.GotoTop()
	case "end":
		m.textViewport.GotoBottom()
	}
	return m, nil
}

// ── The source's optional capabilities ───────────────────────────────────────
//
// Each is probed with a type assertion, the way the router probes FooterView.
// They are three single-method interfaces rather than one with three methods
// precisely so there is no half-satisfied state: CLAUDE.md's HeaderView warning
// is about a view supplying two of four methods and silently satisfying none.

// timestamps returns the source's timestamp capability, if it has one.
func (m Model) timestamps() (viewer.Timestamped, bool) {
	src, ok := m.source.(viewer.Timestamped)
	return src, ok
}

func (m Model) followable() (viewer.Followable, bool) {
	src, ok := m.source.(viewer.Followable)
	return src, ok
}

func (m Model) pageable() (viewer.Pageable, bool) {
	src, ok := m.source.(viewer.Pageable)
	return src, ok
}

// timestampsOn tracks the toggle rather than asking the source, which holds it
// but cannot be interrogated through the interface — WithTimestamps returns a
// new source rather than mutating one, so nothing is shared with a command
// already in flight (Rule 110).
func (m Model) toggleTimestamps() (tea.Model, tea.Cmd) {
	src, ok := m.timestamps()
	if !ok {
		return m, nil
	}
	m.showTimestamps = !m.showTimestamps
	m.source = src.WithTimestamps(m.showTimestamps)
	m.loading = true
	return m, loadCmd(m.source)
}

// toggleFollow is `F`. It re-reads the source on its own clock, into this pane.
//
// It used to be tea.ExecProcess over `docker logs -f`, and the defect that
// replaced it is worth keeping written down: the only way out of that process
// is ctrl+c, a suspended TUI does not intercept it, so it killed DevDesk and
// gave the terminal back in whatever mode the child had left it — after which
// keys stopped answering. A capability whose only exit kills the application is
// not one.
//
// The read is the one `ctrl+r` already does: same command, same message, same
// handler. Following is that key on a timer and nothing more, which is why it
// costs no new loading path.
func (m Model) toggleFollow() (tea.Model, tea.Cmd) {
	src, ok := m.followable()
	if !ok {
		return m, nil
	}
	if m.following {
		return m.stopFollowing(), nil
	}
	m.following = true
	m.followGen++
	// The first read is immediate. Waiting a whole interval before anything
	// moves would leave the user unsure the key did anything, and what is on
	// screen is already up to one interval old.
	return m, tea.Batch(loadCmd(m.source), followTickCmd(src.FollowInterval(), m.followGen))
}

// stopFollowing is the one place following is turned off, so no path can leave
// the flag set with no tick behind it, or a tick running with the flag clear.
func (m Model) stopFollowing() Model {
	m.following = false
	// Bumping the generation is what actually ends the loop. The tick already
	// in flight blocks for its whole interval and arrives whatever happens
	// here; the mismatch is what makes it a no-op instead of it finding the
	// flag turned back on and carrying a second loop into the new run.
	m.followGen++
	return m
}

// handleFollowTick re-reads, then schedules the next one.
//
// A stale tick — from a loop the user stopped, or restarted — returns no
// command, which is how the old loop dies rather than running beside the new.
func (m Model) handleFollowTick(msg followTickMsg) (tea.Model, tea.Cmd) {
	if !m.following || msg.gen != m.followGen || m.source == nil {
		return m, nil
	}
	src, ok := m.followable()
	if !ok {
		return m.stopFollowing(), nil
	}
	// m.loading stays false deliberately: a load is reported in the footer with
	// a spinner (Rule 139), and one blinking every two seconds would read as
	// something going wrong with the thing that is working. The follow status
	// line is what says the pane is live.
	return m, tea.Batch(loadCmd(m.source), followTickCmd(src.FollowInterval(), msg.gen))
}

// openPager hands the document to the system pager.
//
// It survives for one source and deliberately: `less` handles a gigabyte and
// follows it, which a viewport holding the whole document in memory never will.
// Every other pager path in the application is gone.
func (m Model) openPager() (tea.Model, tea.Cmd) {
	src, ok := m.pageable()
	if !ok {
		return m, nil
	}
	return m, tea.ExecProcess(src.PagerCmd(), func(err error) tea.Msg {
		return PagerExitMsg{Err: err}
	})
}
