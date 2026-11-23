package netdiag

import (
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-717 (step 3 of DEVDESK-708): the confirmation of the Ports tab acts on
// the target it captured when it opened (S-01a criteria 5 and 6, bug
// DEVDESK-714), and says the truth on Windows (S-02a criteria 1 to 4).
//
// No test here reaches ports.Kill: every model installs a fake processOps whose
// kill only records the PID it was asked to signal.

// fakeProcs records the PIDs the Ports tab asked to signal.
type fakeProcs struct {
	killed []string
	// killedWith records the identity each signal was asked to verify against
	// (DEVDESK-709), in the order of killed.
	killedWith []ports.Identity
	err        error
	// confirmed is the identity the confirmation reads for a PID, confirmErr the
	// error it returns instead, confirmAsked every PID it was asked about. A PID
	// in neither gets the identity it was listed with (DEVDESK-709).
	confirmed    map[string]ports.Identity
	confirmErr   map[string]error
	confirmAsked []string
	// protect is the verdict per PID; a PID not in it is Unprotected. asked
	// records every PID the model asked about, in order (DEVDESK-718).
	protect map[string]ports.Protection
	asked   []string

	// S-02b (DEVDESK-711): the first signal of a stop is SIGTERM, then the table
	// probes the process, and SIGKILL only follows a second confirmation.
	// terminated records the PIDs asked to stop (ports.Terminate), terminatedWith
	// the identity each was verified against; termErr is what terminate returns.
	terminated     []string
	terminatedWith []ports.Identity
	termErr        error
	// probed records every PID probed and the identity it was compared with.
	// probeAnswer is the verdict per PID (a PID not in it is ProcessRunning, the
	// zero value) and probeErr the error every probe returns with it.
	probed      []string
	probedWith  []ports.Identity
	probeAnswer map[string]ports.Liveness
	probeErr    error
}

// signals is every PID a signal was asked for, SIGTERM and SIGKILL alike: what a
// test that expects nothing to leave asserts is empty.
func (f *fakeProcs) signals() []string {
	return append(append([]string(nil), f.terminated...), f.killed...)
}

func (f *fakeProcs) ops(windows bool) processOps {
	return processOps{
		protection: func(pid string) ports.Protection {
			f.asked = append(f.asked, pid)
			return f.protect[pid]
		},
		confirm: func(pid string, listed ports.Identity) (ports.Identity, error) {
			f.confirmAsked = append(f.confirmAsked, pid)
			if err := f.confirmErr[pid]; err != nil {
				return ports.Identity{}, err
			}
			if id, ok := f.confirmed[pid]; ok {
				return id, nil
			}
			return listed, nil
		},
		terminate: func(pid string, listed ports.Identity) error {
			f.terminated = append(f.terminated, pid)
			f.terminatedWith = append(f.terminatedWith, listed)
			return f.termErr
		},
		probe: func(pid string, listed ports.Identity) (ports.Liveness, error) {
			f.probed = append(f.probed, pid)
			f.probedWith = append(f.probedWith, listed)
			return f.probeAnswer[pid], f.probeErr
		},
		kill: func(pid string, listed ports.Identity) error {
			f.killed = append(f.killed, pid)
			f.killedWith = append(f.killedWith, listed)
			return f.err
		},
		windows: windows,
	}
}

// protectAs makes the fake say p about pid from now on.
func (f *fakeProcs) protectAs(pid string, p ports.Protection) {
	if f.protect == nil {
		f.protect = map[string]ports.Protection{}
	}
	f.protect[pid] = p
}

// installProcs puts a fresh recording fake on the model and returns it.
func installProcs(m *Model, windows bool) *fakeProcs {
	f := &fakeProcs{}
	m.portsModel.ops = f.ops(windows)
	return f
}

// idOf is the identity the fixtures give the process with this PID: the PID
// itself, so two rows of one process agree and two processes differ. A row with
// no PID has none (DEVDESK-709).
func idOf(pid string) ports.Identity {
	n, err := strconv.ParseUint(pid, 10, 64)
	if err != nil {
		return ports.Identity{}
	}
	return ports.IdentityForTest(n)
}

// sock is a LISTEN socket held by pid; the port follows the PID so rows differ.
// Its identity is idOf(pid).
func sock(pid, process string) ports.Socket {
	return sockWith(pid, process, idOf(pid))
}

// sockWith is sock with an explicit identity: the zero value for a row DevDesk
// could not read, a deferred one for a Windows listing over its budget, or the
// stamp of another process holding the same PID.
func sockWith(pid, process string, id ports.Identity) ports.Socket {
	return ports.Socket{
		Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:" + pid, PeerAddr: "0.0.0.0:*",
		PID: pid, Process: process, Identity: id,
	}
}

// nodeFirst and nodeLast are the same sockets in two orders: the refresh of the
// table reorders them while a confirmation is open (DEVDESK-714).
func nodeFirst() []ports.Socket {
	return []ports.Socket{sock("4242", "node"), sock("812", "sshd"), sock("1204", "nginx")}
}

func nodeLast() []ports.Socket {
	return []ports.Socket{sock("812", "sshd"), sock("1204", "nginx"), sock("4242", "node")}
}

// confirmOnNode returns a wide model whose cursor is on node (PID 4242) with
// the confirmation open, and the fake that records signals.
func confirmOnNode(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, portsDataMsg{ports: nodeFirst()})
	m.portsModel.table.Table().SetCursor(0)
	procs := installProcs(m, false)
	if row, ok := m.portsModel.table.Selected(); !ok || row.PID != "4242" {
		t.Fatalf("setup: selected row = %+v, want node (PID 4242)", row)
	}
	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.portsModel.confirmModal == nil {
		t.Fatal("setup: K on node did not open the confirmation")
	}
	return m, procs
}

// runKill executes the command a confirmed Yes returned. It must be the kill
// command itself, with no footer timer batched in (that would sleep 3 s).
func runKill(t *testing.T, cmd tea.Cmd) portsKillResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Yes returned no command")
	}
	res, ok := cmd().(portsKillResultMsg)
	if !ok {
		t.Fatalf("the command did not return a portsKillResultMsg")
	}
	return res
}

// runTerm executes the command a confirmed Yes returned on Linux and macOS: the
// SIGTERM itself, with no footer timer and no probe tick batched in.
func runTerm(t *testing.T, cmd tea.Cmd) portsTermResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Yes returned no command")
	}
	res, ok := cmd().(portsTermResultMsg)
	if !ok {
		t.Fatalf("the command did not return a portsTermResultMsg")
	}
	return res
}

// killLabel is the label of K on the platform the model is set to: the gesture
// asks for a stop on Linux and macOS and forces on Windows (S-02b, Q6).
func killLabel(m *Model) string {
	if m.portsModel.ops.windows {
		return "Kill process"
	}
	return "Stop process"
}

// modalLines is the confirmation as rendered, border and padding removed, one
// entry per content line, stopping before the buttons.
func modalLines(m *Model) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(m.portsModel.confirmModal.View()), "\n") {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "│┌┐└┘─"))
		if strings.Contains(l, "Yes") && strings.Contains(l, "No") {
			break
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// modalTitle and modalBody split modalLines at the title's blank line.
func modalTitle(m *Model) string { return modalLines(m)[0] }

func modalBody(m *Model) string {
	return strings.TrimLeft(strings.Join(modalLines(m)[1:], "\n"), "\n")
}

// squash folds every run of whitespace into one space.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// ── S-01a criterion 5 and the DEVDESK-714 bug ────────────────────────────────

func TestYesStopsTheCapturedTargetAfterTheTableReorders(t *testing.T) {
	m, procs := confirmOnNode(t)

	// The refresh lands before the user answers. The cursor stays on index 0,
	// which is now sshd: the pre-fix code killed that row.
	m = feed(t, m, portsDataMsg{ports: nodeLast()})
	if row, _ := m.portsModel.table.Selected(); row.PID != "812" {
		t.Fatalf("setup: after the reorder the selected row is %+v, want sshd (PID 812)", row)
	}

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	res := runTerm(t, cmd)

	if res.pid != "4242" {
		t.Errorf("stop result pid = %q, want 4242", res.pid)
	}
	if len(procs.terminated) != 1 || procs.terminated[0] != "4242" {
		t.Errorf("SIGTERM went to %v, want exactly [4242] (never the selected row, 812)", procs.terminated)
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v before any second confirmation", procs.killed)
	}
	if !m.portsModel.table.IsBusy("4242") {
		t.Error("the captured target is not marked busy")
	}
	if m.portsModel.table.IsBusy("812") {
		t.Error("the row under the cursor was marked busy: Yes read the selection")
	}
	if m.portsModel.confirmModal != nil || m.portsModel.pendingKill != nil {
		t.Error("the confirmation or its captured target survived Yes")
	}
}

// Rule 110: the command receives values, never the model. Whatever happens to
// the model between Yes and the moment the command runs, the SIGTERM goes to the
// PID captured in Update and through the function captured in Update.
func TestTheTerminateCommandCarriesValuesNotTheModel(t *testing.T) {
	m, procs := confirmOnNode(t)
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})

	m.portsModel.table.SetItems(nodeLast())
	m.portsModel.table.Table().SetCursor(2)
	m.portsModel.pendingKill = nil
	m.portsModel.stop = nil
	m.portsModel.ops = processOps{terminate: func(string, ports.Identity) error {
		panic("the command read the model's processOps after Update returned")
	}}

	res := runTerm(t, cmd)

	if res.pid != "4242" || len(procs.terminated) != 1 || procs.terminated[0] != "4242" {
		t.Errorf("result pid = %q, signalled = %v, want 4242 through the function captured in Update", res.pid, procs.terminated)
	}
}

// The same on Windows, where Yes is the kill itself (S-02a): the command carries
// values and the result is part of no two-step stop (generation 0).
func TestTheKillCommandCarriesValuesNotTheModel(t *testing.T) {
	m, procs := windowsNode(t)
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})

	m.portsModel.table.SetItems([]ports.Socket{sock("812", "sshd"), sock("4412", "node.exe")})
	m.portsModel.table.Table().SetCursor(1)
	m.portsModel.pendingKill = nil
	m.portsModel.ops = processOps{kill: func(string, ports.Identity) error {
		panic("the command read the model's processOps after Update returned")
	}}

	res := runKill(t, cmd)

	if res.pid != "4412" || len(procs.killed) != 1 || procs.killed[0] != "4412" {
		t.Errorf("result pid = %q, signalled = %v, want 4412 through the function captured in Update", res.pid, procs.killed)
	}
	if res.gen != 0 {
		t.Errorf("a Windows kill result carries generation %d, want 0: it is part of no two-step stop", res.gen)
	}
}

func TestTheConfirmationShowsTheCapturedNameAndPID(t *testing.T) {
	m, _ := confirmOnNode(t)

	want := killTarget{PID: "4242", Process: "node", Identity: idOf("4242")}
	if m.portsModel.pendingKill == nil || *m.portsModel.pendingKill != want {
		t.Fatalf("pendingKill = %+v, want %+v", m.portsModel.pendingKill, want)
	}
	if got := modalBody(m); !strings.Contains(got, "node (PID 4242)") {
		t.Errorf("confirmation text = %q, want it to name node (PID 4242)", got)
	}
}

// A confirmation is only ever answered by a message from the modal: a Yes that
// arrives with nothing open used to kill whatever the cursor stood on.
func TestAStrayYesKillsNothing(t *testing.T) {
	m := portsModel(t)
	procs := installProcs(m, false)
	m.portsModel.table.Table().SetCursor(0) // sshd, PID 812

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})

	if cmd != nil {
		t.Error("a Yes with no confirmation open returned a command")
	}
	if len(procs.signals()) != 0 {
		t.Errorf("a stray Yes signalled %v", procs.signals())
	}
	if m.portsModel.table.IsBusy("812") {
		t.Error("a stray Yes marked the selected row busy")
	}
}

// ── S-01a criterion 6 ────────────────────────────────────────────────────────

func TestTheConfirmationDoesNotFollowTheSelection(t *testing.T) {
	for _, tt := range []struct {
		name string
		then []tea.Msg
	}{
		{"the selection moves", append(testutil.Keys("down", "down", "up"), portsDataMsg{ports: nodeLast()})},
		{"the process disappears", []tea.Msg{portsDataMsg{ports: []ports.Socket{sock("812", "sshd"), sock("1204", "nginx")}}}},
		{"the table empties", []tea.Msg{portsDataMsg{ports: nil}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := confirmOnNode(t)
			before := ansi.Strip(m.portsModel.confirmModal.View())

			m = feed(t, m, tt.then...)

			if m.portsModel.confirmModal == nil {
				t.Fatal("the confirmation closed by itself")
			}
			if got := ansi.Strip(m.portsModel.confirmModal.View()); got != before {
				t.Errorf("the confirmation text changed:\nbefore:\n%s\nafter:\n%s", before, got)
			}
			if want := (killTarget{PID: "4242", Process: "node", Identity: idOf("4242")}); m.portsModel.pendingKill == nil || *m.portsModel.pendingKill != want {
				t.Errorf("pendingKill = %+v, want %+v", m.portsModel.pendingKill, want)
			}
			if len(procs.signals()) != 0 {
				t.Errorf("signalled %v without an answer", procs.signals())
			}
		})
	}
}

func TestNoAndEscCloseWithoutASignal(t *testing.T) {
	for _, tt := range []struct {
		name   string
		answer func(t *testing.T, m *Model) (*Model, tea.Cmd)
	}{
		{"No message", func(t *testing.T, m *Model) (*Model, tea.Cmd) {
			return step(t, m, components.ConfirmModalNoMsg{})
		}},
		{"esc", func(t *testing.T, m *Model) (*Model, tea.Cmd) {
			m, cmd := step(t, m, testutil.Key("esc"))
			if cmd == nil {
				t.Fatal("esc on the confirmation returned no command")
			}
			if _, ok := cmd().(components.ConfirmModalNoMsg); !ok {
				t.Fatalf("esc did not answer No")
			}
			return step(t, m, components.ConfirmModalNoMsg{})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := confirmOnNode(t)

			m, cmd := tt.answer(t, m)

			if m.portsModel.confirmModal != nil || m.portsModel.pendingKill != nil {
				t.Error("the confirmation or its captured target stayed open")
			}
			if cmd != nil {
				t.Error("closing the confirmation returned a command")
			}
			if len(procs.signals()) != 0 {
				t.Errorf("signalled %v", procs.signals())
			}
			if m.portsModel.table.IsBusy("4242") {
				t.Error("a row was marked busy though nothing was signalled")
			}
		})
	}
}

// ── Decision: the target must still be in the last table received ────────────

const goneWarning = "node (PID 4242) is no longer listed — not stopped"

func TestYesOnAVanishedTargetSendsNothing(t *testing.T) {
	for _, tt := range []struct {
		name  string
		table []ports.Socket
	}{
		{"the PID is gone", []ports.Socket{sock("812", "sshd"), sock("1204", "nginx")}},
		{"the PID is back under another name", []ports.Socket{sock("4242", "python"), sock("812", "sshd")}},
		{"the table is empty", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := confirmOnNode(t)
			m = feed(t, m, portsDataMsg{ports: tt.table})

			m, cmd := step(t, m, components.ConfirmModalYesMsg{})

			if len(procs.signals()) != 0 {
				t.Errorf("signalled %v for a target that is no longer listed", procs.signals())
			}
			if cmd == nil {
				t.Error("no command returned, so the footer warning would never clear")
			}
			if got := m.portsModel.footer.Text(); got != goneWarning {
				t.Errorf("footer = %q, want %q", got, goneWarning)
			}
			if got := m.portsModel.footer.Level(); got != components.LevelWarning {
				t.Errorf("footer level = %v, want Warning (nothing failed, a precondition is unmet)", got)
			}
			if m.portsModel.confirmModal != nil || m.portsModel.pendingKill != nil {
				t.Error("the confirmation stayed open after Yes")
			}
			if m.portsModel.table.IsBusy("4242") {
				t.Error("the vanished target was marked busy")
			}
		})
	}
}

// The check reads the last table received, not the rows the filter shows: a
// target listed under another position, or among duplicates, still counts.
func TestYesOnATargetStillListedSignalsIt(t *testing.T) {
	m, procs := confirmOnNode(t)
	m = feed(t, m, portsDataMsg{ports: append(nodeLast(), sock("4242", "node"))}) // two sockets, same process

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	runTerm(t, cmd)

	if len(procs.terminated) != 1 || procs.terminated[0] != "4242" {
		t.Errorf("SIGTERM went to %v, want [4242]", procs.terminated)
	}
}

// classify names the outcome of targetPresence for a failure message without
// this file naming its type: got is compared with the three constants.
func classify[T comparable](got, isPresent, isReplaced, isAbsent T) string {
	switch got {
	case isPresent:
		return "present"
	case isReplaced:
		return "replaced"
	case isAbsent:
		return "absent"
	}
	return "unknown"
}

// targetPresence replaces targetStillListed (DEVDESK-709): the last table holds
// the target (present), holds that PID under another identity (replaced), or
// does not hold it (absent).
func TestTargetPresence(t *testing.T) {
	m := feed(t, portsModel(t), portsDataMsg{ports: append(nodeFirst(),
		sockWith("5000", "deferred", ports.DeferredIdentityForTest(time.Unix(1_700_000_000, 0))),
	)})
	pm := m.portsModel

	for _, tt := range []struct {
		name   string
		target killTarget
		want   string
	}{
		{"same PID, same name, same identity", killTarget{PID: "4242", Process: "node", Identity: idOf("4242")}, "present"},
		{"another listed process", killTarget{PID: "812", Process: "sshd", Identity: idOf("812")}, "present"},
		{"same PID, same name, another identity", killTarget{PID: "4242", Process: "node", Identity: ports.IdentityForTest(99)}, "replaced"},
		{"the row is deferred: it never conflicts", killTarget{PID: "5000", Process: "deferred", Identity: ports.IdentityForTest(1)}, "present"},
		{"the target is deferred: it never conflicts", killTarget{PID: "4242", Process: "node", Identity: ports.DeferredIdentityForTest(time.Unix(1_700_000_000, 0))}, "present"},
		{"same PID, another name, same identity", killTarget{PID: "4242", Process: "python", Identity: idOf("4242")}, "absent"},
		{"unknown PID", killTarget{PID: "9999", Process: "node", Identity: idOf("9999")}, "absent"},
		{"name of another row", killTarget{PID: "9999", Process: "sshd", Identity: idOf("9999")}, "absent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(pm.targetPresence(tt.target), present, replaced, absent); got != tt.want {
				t.Errorf("targetPresence(%+v) = %s, want %s", tt.target, got, tt.want)
			}
		})
	}
}

// Two sockets of one process are one process: the last table lists the PID
// twice and both rows carry the same identity.
func TestTargetPresenceReadsEveryRowOfThePID(t *testing.T) {
	m := feed(t, portsModel(t), portsDataMsg{ports: append(nodeLast(), sock("4242", "node"))})

	got := m.portsModel.targetPresence(killTarget{PID: "4242", Process: "node", Identity: idOf("4242")})

	if classify(got, present, replaced, absent) != "present" {
		t.Errorf("a PID listed twice with one identity is %s, want present", classify(got, present, replaced, absent))
	}
}

// Rule 128 at 80 columns: the warning is one centred line, whole, and the
// footer keeps the height the router budgeted.
func TestTheVanishedTargetWarningFitsAt80Columns(t *testing.T) {
	m, _ := confirmOnNode(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{sock("812", "sshd")}})
	m, _ = step(t, m, components.ConfirmModalYesMsg{})

	if got := infoLine(m, 80); got != goneWarning {
		t.Errorf("footer line at 80 columns = %q, want %q", got, goneWarning)
	}
	lines := strings.Split(portsFooter(m, 80), "\n")
	if len(lines) != m.GetFooterHeight() {
		t.Errorf("RenderFooter emits %d lines, GetFooterHeight says %d", len(lines), m.GetFooterHeight())
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("footer line %d is %d columns wide, want at most 80", i, w)
		}
	}
}

// ── S-02a: the wording per platform ──────────────────────────────────────────

const (
	windowsPrompt = "Terminate node.exe (PID 4412)? Windows has no clean stop for this process: it is ended at once and cannot save anything."
	windowsHelp   = "End the selected process at once, after confirmation (Ports tab)"
	// S-02b (DEVDESK-711): what Linux and macOS say, written out in full so that a
	// drift in the production strings fails a test instead of following them.
	unixStopBody = "Ask sshd (PID 812) to stop?\n\nSends SIGTERM and waits up to 5 s. You can force it afterwards."
	unixHelp     = "Ask the selected process to stop (SIGTERM), after confirmation; if it still runs 5 s later, offer SIGKILL (Ports tab)"
)

// windowsNode is a Windows model with the confirmation open on node.exe.
func windowsNode(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{sock("4412", "node.exe"), sock("812", "sshd")}})
	m.portsModel.table.Table().SetCursor(0)
	procs := installProcs(m, true)
	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.portsModel.confirmModal == nil {
		t.Fatal("setup: K on node.exe did not open the confirmation")
	}
	return m, procs
}

// helpFor is the help line the Ports tab documents for K.
func helpFor(m *Model) string {
	for _, kb := range m.GetHelpContent().KeyBindings {
		if kb.Key == keymap.Kill && strings.Contains(kb.Description, "(Ports tab)") {
			return kb.Description
		}
	}
	return ""
}

// S-02a criterion 1.
func TestWindowsConfirmationIsHonest(t *testing.T) {
	m, _ := windowsNode(t)

	if got := squash(modalBody(m)); got != windowsPrompt {
		t.Errorf("Windows confirmation = %q, want %q", got, windowsPrompt)
	}
	if got := squash(killPrompt(killTarget{PID: "4412", Process: "node.exe"}, true)); got != windowsPrompt {
		t.Errorf("killPrompt(windows) = %q, want %q", got, windowsPrompt)
	}
}

// S-02a criterion 2: one gesture, no wait, no second confirmation.
func TestWindowsStopIsOneGesture(t *testing.T) {
	m, procs := windowsNode(t)

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	if m.portsModel.confirmModal != nil {
		t.Error("a second confirmation is open after Yes")
	}
	res := runKill(t, cmd) // the command is the kill itself: nothing to wait for
	if len(procs.killed) != 1 || procs.killed[0] != "4412" {
		t.Fatalf("signalled %v, want exactly [4412]", procs.killed)
	}

	m, _ = step(t, m, res)
	if m.portsModel.confirmModal != nil {
		t.Error("a confirmation opened after the result")
	}
	if got := m.portsModel.footer.Text(); got != "Process 4412 terminated" {
		t.Errorf("footer = %q, want %q", got, "Process 4412 terminated")
	}
	if m.portsModel.table.IsBusy("4412") {
		t.Error("the row is still busy after the result")
	}
}

// S-02a criterion 3: nothing on Windows suggests a clean stop, and K stays
// "Kill".
func TestWindowsLabelsPromiseNoCleanStop(t *testing.T) {
	m, _ := windowsNode(t)
	body := modalBody(m) // read before Yes closes the confirmation

	labels := map[string]string{
		"modal title": modalTitle(m),
		"help":        helpFor(m),
	}
	for _, s := range m.GetShortcuts() {
		if s.Key == keymap.Kill {
			labels["shortcut"] = s.Description
		}
	}
	busy := feed(t, m, components.ConfirmModalYesMsg{})
	labels["busy label"], _ = busy.portsModel.table.BusyLabel("4412")
	done := feed(t, busy, portsKillResultMsg{pid: "4412"})
	labels["success"] = done.portsModel.footer.Text()

	want := map[string]string{
		"modal title": "Kill Process",
		"help":        windowsHelp,
		"shortcut":    "Kill process",
		"busy label":  "Killing node.exe (4412)",
		"success":     "Process 4412 terminated",
	}
	for name, exp := range want {
		if labels[name] != exp {
			t.Errorf("%s = %q, want %q", name, labels[name], exp)
		}
	}

	forbidden := []string{"SIGTERM", "SIGKILL", "graceful", "Stop", "Ask", "clean"}
	for name, got := range labels {
		for _, bad := range forbidden {
			if strings.Contains(got, bad) {
				t.Errorf("%s = %q, which suggests a clean stop (%q)", name, got, bad)
			}
		}
	}
	for _, bad := range []string{"SIGTERM", "SIGKILL", "graceful", "Ask "} {
		if strings.Contains(body, bad) {
			t.Errorf("the Windows confirmation body mentions %q", bad)
		}
	}
}

// The key itself does not move with the platform, and the two lists differ by
// one entry: esc, which stops the wait of the two-step stop and so exists on
// Linux and macOS only (S-02b criterion 4).
func TestTheKillKeyIsTheSameOnEveryPlatform(t *testing.T) {
	win, _ := identityModel(t, true, sock("4412", "node.exe"), sock("812", "sshd"))
	unix, _ := identityModel(t, false, sock("4242", "node"), sock("812", "sshd"))

	if keymap.Kill != "K" {
		t.Fatalf("keymap.Kill = %q, want K", keymap.Kill)
	}
	if !testutil.HasShortcut(win.GetShortcuts(), keymap.Kill) || !testutil.HasShortcut(unix.GetShortcuts(), keymap.Kill) {
		t.Fatal("K is missing on one platform")
	}
	var unixKeys []string
	for _, k := range testutil.ShortcutKeys(unix.GetShortcuts()) {
		if k != "esc" {
			unixKeys = append(unixKeys, k)
		}
	}
	if got, want := strings.Join(testutil.ShortcutKeys(win.GetShortcuts()), ","), strings.Join(unixKeys, ","); got != want {
		t.Errorf("shortcut keys differ beyond esc: windows %v, unix without esc %v", got, want)
	}
	if !testutil.HasShortcut(unix.GetShortcuts(), "esc") {
		t.Error("Linux and macOS lost the esc entry of the two-step stop")
	}
	if testutil.HasShortcut(win.GetShortcuts(), "esc") {
		t.Error("Windows has an esc entry: there is no wait to stop there")
	}
}

// S-02b criterion 1, replacing S-02a criterion 4 ("Linux and macOS read exactly
// what they read before"), which is void by construction: the first confirmation
// no longer offers a SIGKILL. Every string a user reads on the first step is
// pinned here.
func TestUnixConfirmationAsksToStop(t *testing.T) {
	m := portsModel(t)
	m.portsModel.table.Table().SetCursor(0) // sshd, PID 812
	installProcs(m, false)
	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.portsModel.confirmModal == nil {
		t.Fatal("K on sshd did not open the confirmation")
	}

	if got := modalTitle(m); got != "Stop Process" {
		t.Errorf("title = %q, want %q", got, "Stop Process")
	}
	if got := modalBody(m); got != unixStopBody {
		t.Errorf("body = %q, want %q", got, unixStopBody)
	}
	if got := stopPrompt(killTarget{PID: "812", Process: "sshd"}); got != unixStopBody {
		t.Errorf("stopPrompt = %q, want %q", got, unixStopBody)
	}
	if got := killPrompt(killTarget{PID: "812", Process: "sshd"}, false); got != unixStopBody {
		t.Errorf("killPrompt(unix) = %q, want the stop prompt %q", got, unixStopBody)
	}
	if got := helpFor(m); got != unixHelp {
		t.Errorf("help = %q, want %q", got, unixHelp)
	}
	for _, s := range m.GetShortcuts() {
		if s.Key == keymap.Kill && s.Description != "Stop process" {
			t.Errorf("shortcut description = %q, want %q", s.Description, "Stop process")
		}
	}
	if label, _ := feed(t, m, components.ConfirmModalYesMsg{}).portsModel.table.BusyLabel("812"); label != "Stopping sshd (812)" {
		t.Errorf("busy label = %q, want %q", label, "Stopping sshd (812)")
	}
}

func TestKillHelpDescriptionPerPlatform(t *testing.T) {
	if got := killHelpDescription(false); got != unixHelp {
		t.Errorf("killHelpDescription(false) = %q, want %q", got, unixHelp)
	}
	if got := killHelpDescription(true); got != windowsHelp {
		t.Errorf("killHelpDescription(true) = %q, want %q", got, windowsHelp)
	}
}

// Production wires the real thing: ports.Kill and the platform the binary runs
// on. Nothing here calls it.
func TestRealProcessOpsMatchThePlatform(t *testing.T) {
	ops := realProcessOps()

	if ops.windows != (runtime.GOOS == "windows") {
		t.Errorf("realProcessOps().windows = %v on %s", ops.windows, runtime.GOOS)
	}
	if ops.kill == nil {
		t.Fatal("realProcessOps().kill is nil")
	}
	if got, want := reflect.ValueOf(ops.kill).Pointer(), reflect.ValueOf(ports.Kill).Pointer(); got != want {
		t.Error("realProcessOps().kill is not ports.Kill")
	}
	if ops.terminate == nil {
		t.Fatal("realProcessOps().terminate is nil")
	}
	if got, want := reflect.ValueOf(ops.terminate).Pointer(), reflect.ValueOf(ports.Terminate).Pointer(); got != want {
		t.Error("realProcessOps().terminate is not ports.Terminate")
	}
	if ops.probe == nil {
		t.Fatal("realProcessOps().probe is nil")
	}
	if got, want := reflect.ValueOf(ops.probe).Pointer(), reflect.ValueOf(ports.Probe).Pointer(); got != want {
		t.Error("realProcessOps().probe is not ports.Probe")
	}
	if ops.confirm == nil {
		t.Fatal("realProcessOps().confirm is nil")
	}
	if got, want := reflect.ValueOf(ops.confirm).Pointer(), reflect.ValueOf(ports.ConfirmIdentity).Pointer(); got != want {
		t.Error("realProcessOps().confirm is not ports.ConfirmIdentity")
	}
}
