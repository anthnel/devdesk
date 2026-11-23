package netdiag

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

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
	err    error
	// protect is the verdict per PID; a PID not in it is Unprotected. asked
	// records every PID the model asked about, in order (DEVDESK-718).
	protect map[string]ports.Protection
	asked   []string
}

func (f *fakeProcs) ops(windows bool) processOps {
	return processOps{
		protection: func(pid string) ports.Protection {
			f.asked = append(f.asked, pid)
			return f.protect[pid]
		},
		kill: func(pid string) error {
			f.killed = append(f.killed, pid)
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

// sock is a LISTEN socket held by pid; the port follows the PID so rows differ.
func sock(pid, process string) ports.Socket {
	return ports.Socket{
		Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:" + pid, PeerAddr: "0.0.0.0:*",
		PID: pid, Process: process,
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

func TestYesKillsTheCapturedTargetAfterTheTableReorders(t *testing.T) {
	m, procs := confirmOnNode(t)

	// The refresh lands before the user answers. The cursor stays on index 0,
	// which is now sshd: the pre-fix code killed that row.
	m = feed(t, m, portsDataMsg{ports: nodeLast()})
	if row, _ := m.portsModel.table.Selected(); row.PID != "812" {
		t.Fatalf("setup: after the reorder the selected row is %+v, want sshd (PID 812)", row)
	}

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	res := runKill(t, cmd)

	if res.pid != "4242" {
		t.Errorf("kill result pid = %q, want 4242", res.pid)
	}
	if len(procs.killed) != 1 || procs.killed[0] != "4242" {
		t.Errorf("signalled PIDs = %v, want exactly [4242] (never the selected row, 812)", procs.killed)
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
// the model between Yes and the moment the command runs, the signal goes to the
// PID captured in Update and through the function captured in Update.
func TestTheKillCommandCarriesValuesNotTheModel(t *testing.T) {
	m, procs := confirmOnNode(t)
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})

	m.portsModel.table.SetItems(nodeLast())
	m.portsModel.table.Table().SetCursor(2)
	m.portsModel.pendingKill = nil
	m.portsModel.ops = processOps{kill: func(string) error {
		panic("the command read the model's processOps after Update returned")
	}}

	res := runKill(t, cmd)

	if res.pid != "4242" || len(procs.killed) != 1 || procs.killed[0] != "4242" {
		t.Errorf("result pid = %q, signalled = %v, want 4242 through the function captured in Update", res.pid, procs.killed)
	}
}

func TestTheConfirmationShowsTheCapturedNameAndPID(t *testing.T) {
	m, _ := confirmOnNode(t)

	want := killTarget{PID: "4242", Process: "node"}
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
	if len(procs.killed) != 0 {
		t.Errorf("a stray Yes signalled %v", procs.killed)
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
			if want := (killTarget{PID: "4242", Process: "node"}); m.portsModel.pendingKill == nil || *m.portsModel.pendingKill != want {
				t.Errorf("pendingKill = %+v, want %+v", m.portsModel.pendingKill, want)
			}
			if len(procs.killed) != 0 {
				t.Errorf("signalled %v without an answer", procs.killed)
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
			if len(procs.killed) != 0 {
				t.Errorf("signalled %v", procs.killed)
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

			if len(procs.killed) != 0 {
				t.Errorf("signalled %v for a target that is no longer listed", procs.killed)
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
	runKill(t, cmd)

	if len(procs.killed) != 1 || procs.killed[0] != "4242" {
		t.Errorf("signalled %v, want [4242]", procs.killed)
	}
}

func TestTargetStillListed(t *testing.T) {
	m := feed(t, portsModel(t), portsDataMsg{ports: nodeFirst()})
	pm := m.portsModel

	for _, tt := range []struct {
		name   string
		target killTarget
		want   bool
	}{
		{"same PID, same name", killTarget{PID: "4242", Process: "node"}, true},
		{"another listed process", killTarget{PID: "812", Process: "sshd"}, true},
		{"same PID, another name", killTarget{PID: "4242", Process: "python"}, false},
		{"unknown PID", killTarget{PID: "9999", Process: "node"}, false},
		{"name of another row", killTarget{PID: "9999", Process: "sshd"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := pm.targetStillListed(tt.target); got != tt.want {
				t.Errorf("targetStillListed(%+v) = %v, want %v", tt.target, got, tt.want)
			}
		})
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
	unixBody      = "Send SIGKILL to sshd (PID 812)?\n\nThe process gets no chance to shut down cleanly."
	unixHelp      = "Send SIGKILL to the selected process, after confirmation (Ports tab)"
	windowsHelp   = "End the selected process at once, after confirmation (Ports tab)"
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

// The key itself does not move with the platform.
func TestTheKillKeyIsTheSameOnEveryPlatform(t *testing.T) {
	win, _ := windowsNode(t)
	unix := portsModel(t)
	installProcs(unix, false)

	if keymap.Kill != "K" {
		t.Fatalf("keymap.Kill = %q, want K", keymap.Kill)
	}
	got, want := testutil.ShortcutKeys(win.GetShortcuts()), testutil.ShortcutKeys(unix.GetShortcuts())
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("shortcut keys differ between platforms: windows %v, unix %v", got, want)
	}
}

// S-02a criterion 4: Linux and macOS read exactly what they read before, to the
// character.
func TestUnixConfirmationIsUnchanged(t *testing.T) {
	m := portsModel(t)
	m.portsModel.table.Table().SetCursor(0) // sshd, PID 812
	installProcs(m, false)
	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.portsModel.confirmModal == nil {
		t.Fatal("K on sshd did not open the confirmation")
	}

	if got := modalTitle(m); got != "Kill Process" {
		t.Errorf("title = %q, want %q", got, "Kill Process")
	}
	if got := modalBody(m); got != unixBody {
		t.Errorf("body = %q, want %q", got, unixBody)
	}
	if got := killPrompt(killTarget{PID: "812", Process: "sshd"}, false); got != unixBody {
		t.Errorf("killPrompt(unix) = %q, want %q", got, unixBody)
	}
	if got := helpFor(m); got != unixHelp {
		t.Errorf("help = %q, want %q", got, unixHelp)
	}
	for _, s := range m.GetShortcuts() {
		if s.Key == keymap.Kill && s.Description != "Kill process" {
			t.Errorf("shortcut description = %q, want %q", s.Description, "Kill process")
		}
	}
	if label, _ := feed(t, m, components.ConfirmModalYesMsg{}).portsModel.table.BusyLabel("812"); label != "Killing sshd (812)" {
		t.Errorf("busy label = %q, want %q", label, "Killing sshd (812)")
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
}
