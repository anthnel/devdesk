package netdiag

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The two-step stop of Linux and macOS (DEVDESK-711, S-02b): K asks the process
// to stop (SIGTERM), the tab watches it for up to stopGrace without blocking,
// and only a second confirmation sends SIGKILL. Windows has no clean stop and
// never reaches any of this: its Yes is the kill itself (S-02a).
//
// The machine lives in PortsModel.stop. Every step is a message carrying the
// generation of its stop, so a tick that outlives its stop (esc, a rebuilt view)
// touches nothing, and only Update changes the model (Rule 110).

// stopGrace is how long the process gets to leave after the SIGTERM; the texts
// say "5 s". stopProbeEvery is a var so one test can shorten it.
const stopGrace = 5 * time.Second

var stopProbeEvery = 250 * time.Millisecond

// stopNameWidth is how many cells of a process name a confirmation carries, so
// its lines fit an 80-column terminal (ConfirmModal does not wrap).
const stopNameWidth = 32

// stopPhase is where a stop is.
type stopPhase int

const (
	stopSignalling  stopPhase = iota + 1 // SIGTERM in flight
	stopWaiting                          // SIGTERM delivered, probing until the deadline
	stopAskingForce                      // still running at the deadline; the second confirmation is open
	stopForcing                          // SIGKILL in flight
)

// pendingStop is the one stop in progress on Linux and macOS (S-02b criterion
// 7). The target is the one captured when the first confirmation opened, with
// the identity both signals are verified against.
type pendingStop struct {
	target   killTarget
	phase    stopPhase
	gen      uint64
	deadline time.Time
	// probeLogged is set once an unreadable probe was logged: a probe that stays
	// unreadable would otherwise log every tick (up to 20 lines per stop).
	probeLogged bool
}

// stopGenerations hands out the generation of each stop. It is a package
// counter, so two netdiag views built one after the other never share one.
var stopGenerations atomic.Uint64

func nextStopGen() uint64 { return stopGenerations.Add(1) }

// BackgroundMsg is a message of work netdiag started, which the router hands to
// netdiag whether or not it is on screen: a stop is a chain of messages that
// arrive wherever the user is. Only netdiag's own types implement it.
type BackgroundMsg interface{ netdiagBackground() }

// portsTermResultMsg carries the result of the SIGTERM of the stop of
// generation gen. at is when the signal call returned: the grace runs from it.
type portsTermResultMsg struct {
	gen          uint64
	pid, process string
	at           time.Time
	err          error
}

// portsStopProbeMsg carries one probe of the process being stopped. at is the
// tick instant, compared with the deadline; an err is an unreadable probe, which
// counts as still running.
type portsStopProbeMsg struct {
	gen      uint64
	at       time.Time
	liveness ports.Liveness
	err      error
}

func (portsTermResultMsg) netdiagBackground() {}
func (portsStopProbeMsg) netdiagBackground()  {}
func (portsKillResultMsg) netdiagBackground() {}

// NewPortsKillResultMsg builds the result of a kill that belongs to no two-step
// stop, for the router's tests (like NewPortsDataMsg). It panics outside a test
// binary: production never builds a result that no kill produced.
func NewPortsKillResultMsg(pid string) tea.Msg {
	requireTestBinary("NewPortsKillResultMsg", testing.Testing())
	return portsKillResultMsg{pid: pid}
}

// stopPending reports whether a stop is in progress, whatever its phase.
func (pm *PortsModel) stopPending() bool { return pm.stop != nil }

// waiting reports whether the SIGTERM was delivered and the process is watched.
func (pm *PortsModel) waiting() bool { return pm.stop != nil && pm.stop.phase == stopWaiting }

// endStop closes the stop in progress and lifts its busy mark.
func (pm *PortsModel) endStop() {
	if pm.stop != nil {
		pm.table.ClearBusy(pm.stop.target.PID)
	}
	pm.stop = nil
}

// startStop sends the SIGTERM to the captured target: the first Yes.
func (pm *PortsModel) startStop(t killTarget) (*PortsModel, tea.Cmd) {
	gen := nextStopGen()
	pm.stop = &pendingStop{target: t, phase: stopSignalling, gen: gen}
	pm.table.MarkBusy(t.PID, "Stopping "+shortWho(t))
	return pm, terminateCmd(pm.ops.terminate, t, gen)
}

// handleTermResult starts the wait once the SIGTERM is delivered. A refusal ends
// the stop on the spot: there is nothing to wait for (criterion 6).
func (pm *PortsModel) handleTermResult(msg portsTermResultMsg) (*PortsModel, tea.Cmd) {
	s := pm.stop
	if s == nil || s.gen != msg.gen || s.phase != stopSignalling {
		return pm, nil
	}
	if msg.err != nil {
		pm.endStop()
		return pm, pm.refusal(msg.pid, msg.process, msg.err)
	}
	s.phase = stopWaiting
	s.deadline = msg.at.Add(stopGrace)
	pm.table.MarkBusy(s.target.PID, "Waiting for "+shortWho(s.target)+" to exit")
	return pm, stopProbeCmd(pm.ops.probe, s.target, s.gen)
}

// handleStopProbe reads one probe: gone or a zombie ends the stop, a process
// still running at the deadline opens the second confirmation, and anything
// else waits for the next tick. An unreadable probe counts as running, so
// nothing is announced that was not seen.
func (pm *PortsModel) handleStopProbe(msg portsStopProbeMsg) (*PortsModel, tea.Cmd) {
	s := pm.stop
	if s == nil || s.gen != msg.gen || s.phase != stopWaiting {
		return pm, nil
	}
	switch {
	case msg.err != nil:
		if !s.probeLogged {
			s.probeLogged = true
			log.Printf("ERROR [netdiag/ports] probe pid=%s: %v (further probe errors of this stop are not logged)", s.target.PID, msg.err)
		}
	case msg.liveness.Stopped():
		if msg.liveness == ports.ProcessZombie {
			log.Printf("INFO [netdiag/ports] pid=%s is a zombie: counted as stopped", s.target.PID)
		}
		target := s.target
		pm.endStop()
		return pm, tea.Batch(pm.footer.Info(stoppedText(target)), requestPortsRefresh())
	}
	if !msg.at.Before(s.deadline) {
		return pm.askForce()
	}
	return pm, stopProbeCmd(pm.ops.probe, s.target, s.gen)
}

// askForce opens the second confirmation. The row stops being busy: the modal
// is what is going on now, and nothing is forced without its answer.
//
// It opens on a timer, not on a key, so the user may be typing in the filter.
// The filter is left (its text stays applied, as after enter) so the modal is
// the only thing holding the keyboard, and the modal drops its y shortcut: a
// letter of that text cannot answer Yes, and the SIGKILL needs a deliberate
// move to Yes and enter. Leaving the filter is the simplest choice that loses
// nothing: the typed text and the narrowed table stay as they were.
func (pm *PortsModel) askForce() (*PortsModel, tea.Cmd) {
	s := pm.stop
	s.phase = stopAskingForce
	pm.table.ClearBusy(s.target.PID)
	pm.table.FilterBar().EndSearch()
	pm.confirmModal = components.NewConfirmModal(forceTitle, forcePrompt(s.target)).WithoutYesShortcut()
	return pm, pm.footer.Warn(shortWho(s.target) + " did not exit within 5 s")
}

// handleForceYes sends the SIGKILL to the captured target, after the same
// checks as the first Yes except that an absent row does not stop it: a process
// that is stopping often closes its sockets before it exits, which is the case
// this step exists for. The identity is verified at the signal itself.
func (pm *PortsModel) handleForceYes() (*PortsModel, tea.Cmd) {
	s := pm.stop
	t := s.target
	pm.confirmModal = nil
	if pm.targetPresence(t) == replaced {
		pm.endStop()
		pid, _ := strconv.Atoi(t.PID)
		return pm, pm.refusal(t.PID, t.Process, &ports.IdentityChangedError{PID: pid})
	}
	if p := pm.ops.protection(t.PID); p != ports.Unprotected {
		pm.endStop()
		return pm, pm.footer.Warn(fmt.Sprintf("PID %s not stopped: %s", t.PID, p.Reason()))
	}
	s.phase = stopForcing
	pm.table.MarkBusy(t.PID, "Killing "+shortWho(t))
	return pm, killProcessCmd(pm.ops.kill, t, s.gen)
}

// handleForceNo leaves the process running: nothing is sent.
func (pm *PortsModel) handleForceNo() (*PortsModel, tea.Cmd) {
	t := pm.stop.target
	pm.confirmModal = nil
	pm.endStop()
	return pm, pm.footer.Info(notForcedText(t))
}

// cancelWait is esc: it stops waiting. The SIGTERM already sent is not taken
// back and nothing is sent. Outside the wait there is nothing to cancel.
func (pm *PortsModel) cancelWait() (*PortsModel, tea.Cmd) {
	if !pm.waiting() {
		return pm, nil
	}
	t := pm.stop.target
	pm.endStop()
	return pm, pm.footer.Info(stoppedWaitingText(t))
}

// handleKillResult closes a kill. A result of a two-step stop (gen != 0) lifts
// the busy mark and ends the stop only when it is the current one: a SIGKILL
// that was sent is always announced, but must not clear the mark of another
// stop. A result of generation 0 is a Windows kill, which has no stop.
func (pm *PortsModel) handleKillResult(msg portsKillResultMsg) (*PortsModel, tea.Cmd) {
	current := pm.stop != nil && msg.gen != 0 && pm.stop.gen == msg.gen
	if msg.gen == 0 || current {
		// On every outcome: a kill that failed has to let the socket state show
		// again rather than go on turning.
		pm.table.ClearBusy(msg.pid)
	}
	if current {
		pm.stop = nil
	}
	if msg.gen != 0 && errors.Is(msg.err, os.ErrProcessDone) {
		// It left between the deadline and the second Yes: the stop did its job.
		t := killTarget{PID: msg.pid, Process: msg.process}
		return pm, tea.Batch(pm.footer.Info(stoppedText(t)), requestPortsRefresh())
	}
	if msg.err != nil {
		return pm, pm.refusal(msg.pid, msg.process, msg.err)
	}
	return pm, pm.footer.Info(fmt.Sprintf("Process %s terminated", msg.pid))
}

// shortWho names a target for a row label or a footer line: "nginx (812)", or
// "PID 812" when the system gave no name.
func shortWho(t killTarget) string {
	if t.Process == "" {
		return "PID " + t.PID
	}
	return t.Process + " (" + t.PID + ")"
}

// modalWho names a target in a confirmation, with the name cut so the line fits
// 80 columns: "nginx (PID 812)", or "PID 812" without a name.
func modalWho(t killTarget) string {
	if t.Process == "" {
		return "PID " + t.PID
	}
	return theme.TruncateWidth(t.Process, stopNameWidth) + " (PID " + t.PID + ")"
}

// stopPrompt is the first confirmation on Linux and macOS: it announces both
// steps and offers only the first.
func stopPrompt(t killTarget) string {
	return fmt.Sprintf("Ask %s to stop?\n\nSends SIGTERM and waits up to 5 s. You can force it afterwards.", modalWho(t))
}

// forcePrompt is the second confirmation: the process did not leave in time.
func forcePrompt(t killTarget) string {
	return fmt.Sprintf("%s did not exit within 5 s.\n\nForce kill (SIGKILL)? It gets no chance to shut down cleanly.", modalWho(t))
}

func stoppedText(t killTarget) string { return shortWho(t) + " stopped" }

func stoppedWaitingText(t killTarget) string {
	return "Stopped waiting for " + shortWho(t) + " — the stop request was already sent"
}

func notForcedText(t killTarget) string {
	return shortWho(t) + " left running — no SIGKILL sent"
}
