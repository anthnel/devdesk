package netdiag

import (
	"context"
	"net/netip"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/wslenv"
)

const (
	// defaultPathResolveTimeout bounds the name resolution of a target.
	defaultPathResolveTimeout = 5 * time.Second
)

// pathState is where the path trace is in its life cycle.
type pathState int

const (
	// pathReady is the zero value: the target is known (or empty) and nothing
	// was sent; enter launches.
	pathReady pathState = iota
	// pathRunning: a trace is in flight.
	pathRunning
	// pathDone: a trace ended (or was canceled); the hops stay on screen.
	pathDone
)

// PathModel is the path trace model: a table of hops that fills in while the
// trace runs. The target comes from the Diagnostics form, never from here.
type PathModel struct {
	width  int
	height int

	engine         pathEngine
	resolveTimeout time.Duration

	table datatable.Model[pathStep]

	state  pathState
	target pathTarget
	addr   netip.Addr
	// route is the way out of this machine; nil until the resolved message.
	route *pathRoute
	// hops is what the engine reported so far; the table shows
	// visiblePathHops(hops). At done it is replaced by Result.Hops.
	hops   []nettrace.Hop
	result *nettrace.Result
	// errText is the persistent footer error of a run that never traced. It is
	// cleared by the next launch or by ready, not by a timer.
	errText string

	// gen identifies the run whose messages are accepted; cancel and msgs
	// belong to that run.
	gen    int
	cancel context.CancelFunc
	msgs   <-chan tea.Msg

	footer components.FooterMessage

	// source is where the trace starts: this machine, or a running container
	// (DEVDESK-330). picker is the open selector, nil when closed; sourcesGen
	// identifies the listing whose answer it accepts; container is what the
	// column says about the source of the current run.
	source     pathSource
	picker     *pathSourcePicker
	sourcesGen int
	container  *pathContainer

	// platform is the WSL2 detection of the current run, set by handleResolved
	// and cleared with the run.
	platform wslenv.Info

	// known is the route stage's reading handed over by Model.openTrace, the
	// only writer (DEVDESK-473). The zero value knows nothing.
	known pathKnownRoutes
}

// pathSourceKey opens the "Trace from" selector (declared exception, surface
// netdiag/trace).
const pathSourceKey = "ctrl+o"

// pickerOpen reports whether the selector holds the keyboard.
func (pm *PathModel) pickerOpen() bool { return pm.picker != nil }

func newPathModel(e pathEngine) *PathModel {
	return &PathModel{
		engine:         e,
		resolveTimeout: defaultPathResolveTimeout,
		table: datatable.New(datatable.Config[pathStep]{
			Columns:    pathColumns(),
			SortColumn: -1, // the path order is the content
		}),
	}
}

func (pm *PathModel) running() bool { return pm.state == pathRunning }

func (pm *PathModel) resize(width, height int) {
	pm.width = width
	pm.height = max(height, 3)
	pm.table.Resize(width, pm.height)
}

// launchable is the one answer to "can a trace start here", read by the header
// and by the handler (Rule 130).
func (pm *PathModel) launchable() shortcut.Availability {
	if err := pm.engine.supported(); err != nil {
		return shortcut.Unavailable(pathUnsupportedReason(err))
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
	case pathSourcesMsg:
		return pm.handleSources(msg)
	case tea.KeyPressMsg:
		return pm.handleKey(msg)
	}
	// Anything else (a paste included, Rule 111) is a footer expiry or dropped.
	pm.footer.Handle(msg)
	return pm, nil
}

func (pm *PathModel) handleResolved(msg pathResolvedMsg) (*PathModel, tea.Cmd) {
	if msg.gen != pm.gen {
		return pm, nil
	}
	pm.addr = msg.addr
	pm.platform = msg.platform
	route := newPathRoute(msg.route, msg.routeErr)
	pm.route = &route
	if !msg.source.isHost() {
		c := newPathContainer(msg.source, msg.ctr, msg.ctrErr).withBridgeRead(msg.bridge)
		pm.container = &c
	}
	pm.refreshSteps()
	return pm, waitForPathMsg(pm.msgs)
}

func (pm *PathModel) handleHop(msg pathHopMsg) (*PathModel, tea.Cmd) {
	if msg.gen != pm.gen {
		return pm, nil
	}
	pm.hops = append(pm.hops, msg.hop)
	pm.refreshSteps()
	return pm, waitForPathMsg(pm.msgs)
}

func (pm *PathModel) handleDone(msg pathDoneMsg) (*PathModel, tea.Cmd) {
	if msg.gen != pm.gen {
		return pm, nil
	}
	pm.releaseRun()
	if msg.err != nil {
		return pm.handleDoneError(msg.err)
	}
	res := msg.result
	pm.result = &res
	pm.hops = res.Hops
	pm.refreshSteps()
	pm.state = pathDone
	return pm, nil
}

// handleDoneError ends a failed run. A kernel "no route" keeps the local rows
// (source, container, interface, gateway) with the route marked NoRoute; any
// other error clears every row.
func (pm *PathModel) handleDoneError(err error) (*PathModel, tea.Cmd) {
	pm.hops = nil
	if nettrace.IsNoRoute(err) {
		pm.route = &pathRoute{NoRoute: true}
		pm.refreshSteps()
	} else {
		pm.route = nil
		pm.container = nil
		pm.platform = wslenv.Info{}
		pm.table.SetItems(nil)
	}
	pm.errText = pathErrorLabel(pm.target.Host, err)
	pm.state = pathDone
	return pm, nil
}

// refreshSteps rebuilds the table rows from the route and the hops.
func (pm *PathModel) refreshSteps() {
	steps := withNoResponse(pathSteps(pm.container, pm.route, pm.hops), pm.result)
	pm.table.SetItems(withWSL(steps, pm.platform))
}

// clearRun forgets what the last run put on screen: the resolved address, the
// route, the container, the hops, the outcome and the error.
func (pm *PathModel) clearRun() {
	pm.addr = netip.Addr{}
	pm.hops = nil
	pm.route = nil
	pm.container = nil
	pm.platform = wslenv.Info{}
	pm.result = nil
	pm.errText = ""
	pm.table.SetItems(nil)
}

// hopCount is the number of hop rows (the local rows are not counted).
func (pm *PathModel) hopCount() int {
	n := 0
	for _, s := range pm.table.Items() {
		if s.Kind == stepHop {
			n++
		}
	}
	return n
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
	if pm.pickerOpen() {
		return pm.handlePickerKey(key)
	}
	if key == pathSourceKey {
		return pm.openPicker()
	}
	if isPathNavKey(key) {
		return pm, pm.table.Update(msg)
	}
	if pm.state == pathRunning {
		if key == "esc" {
			return pm.cancelRun()
		}
		return pm, nil
	}
	if key == "enter" {
		return pm.launch(pm.target)
	}
	return pm, nil
}

// runEngine returns the engine one run uses: pm.engine, with the route read
// answered from pm.known first when the trace starts on this machine. A
// container source reads its own route. It only reads pm, so a Cmd can capture
// the result.
func (pm *PathModel) runEngine(tg pathTarget) pathEngine {
	e := pm.engine
	if pm.source.isHost() {
		e.route = pm.known.routeFirst(tg.Host, pm.engine.route)
	}
	return e
}

// launch starts a run: the model changes here, in Update; the Cmd only does I/O.
func (pm *PathModel) launch(tg pathTarget) (*PathModel, tea.Cmd) {
	if tg.Host == "" {
		return pm, nil // nothing to trace
	}
	if a := pm.launchable(); !a.Enabled() {
		return pm, pm.footer.Warn(a.Reason)
	}
	pm.releaseRun()
	pm.gen++
	pm.target = tg
	pm.clearRun()
	pm.footer.Clear()

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg, pathChanCap)
	pm.cancel, pm.msgs = cancel, ch
	pm.state = pathRunning
	return pm, startPathRun(ctx, pm.gen, tg, pm.source, pm.resolveTimeout, pm.runEngine(tg), ch)
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
func startPathRun(ctx context.Context, gen int, tg pathTarget, src pathSource, timeout time.Duration, e pathEngine, ch chan tea.Msg) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			producePath(ctx, gen, tg, src, timeout, e, ch)
			return nil
		},
		waitForPathMsg(ch),
	)
}
