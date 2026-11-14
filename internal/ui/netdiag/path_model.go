package netdiag

import (
	"context"
	"net/netip"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	// reasonPathUnsupported is shown when the platform cannot trace a path. The
	// header greys enter out and the handler refuses with the same text.
	reasonPathUnsupported = "The path trace is not supported on this platform"

	// defaultPathResolveTimeout bounds the name resolution of a target.
	defaultPathResolveTimeout = 5 * time.Second

	// pathInputCharLimit holds a 253-character host name, a colon and a port.
	pathInputCharLimit = 262
)

// pathState is where the Path tab is in its life cycle.
type pathState int

const (
	// pathEditing: the target field has the keyboard.
	pathEditing pathState = iota
	// pathRunning: a trace is in flight; the field is blurred.
	pathRunning
	// pathDone: a trace ended (or was canceled); the hops stay on screen.
	pathDone
)

// PathModel is the Path tab: a target field above a table of hops that fills
// in while the trace runs.
type PathModel struct {
	width int

	engine         pathEngine
	resolveTimeout time.Duration

	input textinput.Model
	table datatable.Model[nettrace.Hop]

	state  pathState
	target pathTarget
	addr   netip.Addr
	// hops is what the engine reported so far; the table shows
	// visiblePathHops(hops). At done it is replaced by Result.Hops.
	hops   []nettrace.Hop
	result *nettrace.Result
	// errText is the persistent footer error of a run that never traced. It is
	// cleared by a change of the field or by the next launch, not by a timer.
	errText string

	// gen identifies the run whose messages are accepted; cancel and msgs
	// belong to that run.
	gen    int
	cancel context.CancelFunc
	msgs   <-chan tea.Msg

	footer components.FooterMessage
}

func newPathModel(e pathEngine) *PathModel {
	in := textinput.New()
	theme.StyleTextInput(&in)
	in.Placeholder = "hostname or IPv4 address[:port]"
	in.CharLimit = pathInputCharLimit
	in.Focus()

	return &PathModel{
		engine:         e,
		resolveTimeout: defaultPathResolveTimeout,
		input:          in,
		table: datatable.New(datatable.Config[nettrace.Hop]{
			Columns:    pathColumns(),
			SortColumn: -1, // the path order is the content
		}),
	}
}

// InEditMode reports whether the target field has the keyboard.
func (pm *PathModel) InEditMode() bool { return pm.state == pathEditing }

func (pm *PathModel) running() bool { return pm.state == pathRunning }

func (pm *PathModel) resize(width, height int) {
	pm.width = width
	pm.input.SetWidth(max(width-ansi.StringWidth(pathBarLabel)-4, 10))
	pm.table.Resize(width, max(height, 3))
}

// launchable is the one answer to "can a trace start here", read by the header
// and by the handler (Rule 130).
func (pm *PathModel) launchable() shortcut.Availability {
	if pm.engine.supported() != nil {
		return shortcut.Unavailable(reasonPathUnsupported)
	}
	return shortcut.Availability{}
}

func (pm *PathModel) update(msg tea.Msg) (*PathModel, tea.Cmd) {
	switch msg := msg.(type) {
	case pathResolvedMsg:
		return pm.handleResolved(msg)
	case pathHopMsg:
		return pm.handleHop(msg)
	case pathDoneMsg:
		return pm.handleDone(msg)
	case tea.KeyPressMsg:
		return pm.handleKey(msg)
	case tea.PasteMsg:
		return pm.handlePaste(msg)
	}
	// Anything else is a footer expiry or the cursor blink of the field: the
	// field needs the blink itself, or its cursor freezes.
	pm.footer.Handle(msg)
	var cmd tea.Cmd
	pm.input, cmd = pm.input.Update(msg)
	return pm, cmd
}

func (pm *PathModel) handleResolved(msg pathResolvedMsg) (*PathModel, tea.Cmd) {
	if msg.gen != pm.gen {
		return pm, nil
	}
	pm.addr = msg.addr
	return pm, waitForPathMsg(pm.msgs)
}

func (pm *PathModel) handleHop(msg pathHopMsg) (*PathModel, tea.Cmd) {
	if msg.gen != pm.gen {
		return pm, nil
	}
	pm.hops = append(pm.hops, msg.hop)
	pm.table.SetItems(visiblePathHops(pm.hops))
	return pm, waitForPathMsg(pm.msgs)
}

func (pm *PathModel) handleDone(msg pathDoneMsg) (*PathModel, tea.Cmd) {
	if msg.gen != pm.gen {
		return pm, nil
	}
	pm.releaseRun()
	if msg.err != nil {
		pm.hops = nil
		pm.table.SetItems(nil)
		pm.errText = pathErrorLabel(pm.target.Host, msg.err)
		pm.state = pathEditing
		return pm, pm.input.Focus()
	}
	res := msg.result
	pm.result = &res
	pm.hops = res.Hops
	pm.table.SetItems(visiblePathHops(res.Hops))
	pm.state = pathDone
	return pm, nil
}

// releaseRun cancels the run's context (releasing it) and forgets its channel.
func (pm *PathModel) releaseRun() {
	if pm.cancel != nil {
		pm.cancel()
		pm.cancel = nil
	}
	pm.msgs = nil
}

func isPathNavKey(key string) bool {
	switch key {
	case "up", "down", "pgup", "pgdown", "home", "end":
		return true
	}
	return false
}

func (pm *PathModel) handleKey(msg tea.KeyPressMsg) (*PathModel, tea.Cmd) {
	key := msg.String()
	if isPathNavKey(key) {
		return pm, pm.table.Update(msg)
	}
	switch pm.state {
	case pathEditing:
		return pm.handleEditKey(msg)
	case pathRunning:
		if key == "esc" {
			return pm.cancelRun()
		}
	case pathDone:
		switch key {
		case "enter":
			return pm.launch(pm.target)
		case "esc":
			pm.state = pathEditing
			return pm, pm.input.Focus()
		}
	}
	return pm, nil
}

func (pm *PathModel) handleEditKey(msg tea.KeyPressMsg) (*PathModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return pm.launchFromInput()
	case "esc", "tab", "shift+tab":
		return pm, nil
	}
	return pm.updateInput(msg)
}

func (pm *PathModel) handlePaste(msg tea.PasteMsg) (*PathModel, tea.Cmd) {
	if pm.state != pathEditing {
		return pm, nil // a paste reaches the focused field only (Rule 111)
	}
	return pm.updateInput(msg)
}

// updateInput hands msg to the field; a changed value drops the stale error.
func (pm *PathModel) updateInput(msg tea.Msg) (*PathModel, tea.Cmd) {
	before := pm.input.Value()
	var cmd tea.Cmd
	pm.input, cmd = pm.input.Update(msg)
	if pm.input.Value() != before {
		pm.errText = ""
	}
	return pm, cmd
}

func (pm *PathModel) launchFromInput() (*PathModel, tea.Cmd) {
	tg, err := parsePathTarget(pm.input.Value())
	if err != nil {
		return pm, pm.footer.Error(capitalize(err.Error()))
	}
	return pm.launch(tg)
}

// launch starts a run: the model changes here, in Update; the Cmd only does I/O.
func (pm *PathModel) launch(tg pathTarget) (*PathModel, tea.Cmd) {
	if a := pm.launchable(); !a.Enabled() {
		return pm, pm.footer.Warn(a.Reason)
	}
	pm.releaseRun()
	pm.gen++
	pm.target = tg
	pm.addr = netip.Addr{}
	pm.hops = nil
	pm.result = nil
	pm.errText = ""
	pm.footer.Clear()
	pm.table.SetItems(nil)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg, pathChanCap)
	pm.cancel, pm.msgs = cancel, ch
	pm.input.Blur()
	pm.state = pathRunning
	return pm, startPathRun(ctx, pm.gen, tg, pm.resolveTimeout, pm.engine, ch)
}

// cancelRun answers esc at once: the run's late messages are dropped by the
// generation bump, the hops already seen stay on screen.
func (pm *PathModel) cancelRun() (*PathModel, tea.Cmd) {
	pm.releaseRun()
	pm.gen++
	pm.result = &nettrace.Result{Outcome: nettrace.OutcomeCancelled}
	pm.state = pathDone
	return pm, nil
}

// startPathRun builds the launch Cmd from values only, so that no closure
// reads the model (Rule 110).
func startPathRun(ctx context.Context, gen int, tg pathTarget, timeout time.Duration, e pathEngine, ch chan tea.Msg) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			producePath(ctx, gen, tg, timeout, e, ch)
			return nil
		},
		waitForPathMsg(ch),
	)
}
