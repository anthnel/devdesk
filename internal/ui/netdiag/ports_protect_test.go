package netdiag

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-718 (step 4 of DEVDESK-708, S-01a criteria 1 to 3): K is greyed on a
// process DevDesk refuses to signal, the footer says why, no confirmation
// opens, and the row stays. The verdict comes from processOps.protection, which
// production wires to ports.ProtectionOf; every test but the wiring ones
// substitutes a fake, so no real PID is read and no signal can leave.

// The wording agreed in the story, written out here so that a drift in the
// constants of internal/ports fails a test instead of following them silently.
const (
	reasonSelfText    = "This is DevDesk itself"
	reasonParentText  = "This process started DevDesk"
	reasonInitText    = "PID 1 is the system's init process"
	reasonWindowsText = "Windows system process"
)

// protectedModel is a wide Ports model whose cursor is on the row (pid,
// process), which the fake says is p. A second, ordinary row follows it.
func protectedModel(t *testing.T, windows bool, pid, process string, p ports.Protection) (*Model, *fakeProcs) {
	t.Helper()
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{sock(pid, process), sock("9001", "ordinary")}})
	m.portsModel.table.Table().SetCursor(0)
	procs := installProcs(m, windows)
	procs.protectAs(pid, p)
	if row, ok := m.portsModel.table.Selected(); !ok || row.PID != pid {
		t.Fatalf("setup: selected row = %+v, want PID %s", row, pid)
	}
	return m, procs
}

func visiblePIDs(m *Model) []string {
	var out []string
	for _, s := range m.portsModel.table.Visible() {
		out = append(out, s.PID)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ── S-01a criteria 1, 2 and 3: greyed, with the reason, nothing opens ────────

func TestKillIsGreyedOnAProtectedProcess(t *testing.T) {
	for _, tt := range []struct {
		name    string
		windows bool
		pid     string
		process string
		prot    ports.Protection
		reason  string
	}{
		{"DevDesk itself (criterion 1)", false, "4242", "dk", ports.ProtectedSelf, reasonSelfText},
		{"the process that started it (criterion 2)", false, "3100", "bash", ports.ProtectedParent, reasonParentText},
		{"PID 1 (criterion 2)", false, "1", "systemd", ports.ProtectedInit, reasonInitText},
		{"Windows PID 4 (criterion 3)", true, "4", "System", ports.ProtectedWindowsSystem, reasonWindowsText},
		{"Windows PID 0 (criterion 3)", true, "0", "Idle", ports.ProtectedWindowsSystem, reasonWindowsText},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.prot.Reason(); got != tt.reason {
				t.Fatalf("setup: %v.Reason() = %q, want the agreed %q", tt.prot, got, tt.reason)
			}
			m, procs := protectedModel(t, tt.windows, tt.pid, tt.process, tt.prot)

			// Rule 130: advertised, greyed, same label.
			if !testutil.HasShortcut(m.GetShortcuts(), keymap.Kill) {
				t.Fatal("K disappeared from the header instead of being greyed")
			}
			if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
				t.Error("K is offered on a process DevDesk refuses to signal")
			}
			for _, s := range m.GetShortcuts() {
				if s.Key == keymap.Kill && s.Description != killLabel(m) {
					t.Errorf("greyed K reads %q, want its own label %q", s.Description, killLabel(m))
				}
			}

			next, cmd := step(t, m, testutil.Key(keymap.Kill))

			if next.portsModel.confirmModal != nil {
				t.Error("K opened the confirmation on a protected process")
			}
			if next.portsModel.pendingKill != nil {
				t.Errorf("K captured a target on a protected process: %+v", next.portsModel.pendingKill)
			}
			if got := next.portsModel.footer.Text(); got != tt.reason {
				t.Errorf("footer = %q, want exactly the reason %q", got, tt.reason)
			}
			if got := next.portsModel.footer.Level(); got != components.LevelWarning {
				t.Errorf("footer level = %v, want Warning (nothing failed: Rules 128 and 130)", got)
			}
			if cmd == nil {
				t.Error("the refusal returned no command, so its footer line would never clear")
			}
			if !contains(visiblePIDs(next), tt.pid) {
				t.Errorf("the row of PID %s left the table (visible: %v)", tt.pid, visiblePIDs(next))
			}
			if row, ok := next.portsModel.table.Selected(); !ok || row.PID != tt.pid {
				t.Errorf("the selection moved off the protected row: %+v", row)
			}
			if next.portsModel.InEditMode() {
				t.Error("the refused K left the view in edit mode")
			}
			if len(procs.signals()) != 0 {
				t.Errorf("signalled %v", procs.signals())
			}
		})
	}
}

func TestKillStaysOfferedOnAnOrdinaryProcess(t *testing.T) {
	m, procs := protectedModel(t, false, "4242", "dk", ports.ProtectedSelf)
	m = feed(t, m, testutil.Key("down")) // the ordinary row

	if row, _ := m.portsModel.table.Selected(); row.PID != "9001" {
		t.Fatalf("setup: selected row = %+v, want the ordinary row", row)
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill) {
		t.Error("K is greyed on an unprotected process")
	}

	m = feed(t, m, testutil.Key(keymap.Kill))

	if m.portsModel.confirmModal == nil {
		t.Fatal("K on an unprotected process did not open the confirmation")
	}
	if want := (killTarget{PID: "9001", Process: "ordinary", Identity: idOf("9001")}); m.portsModel.pendingKill == nil || *m.portsModel.pendingKill != want {
		t.Errorf("pendingKill = %+v, want %+v", m.portsModel.pendingKill, want)
	}
	if len(procs.signals()) != 0 {
		t.Errorf("signalled %v before an answer", procs.signals())
	}
}

// The verdict follows the cursor on every render: nothing is cached from the
// row K was last greyed on.
func TestTheGreyFollowsTheCursor(t *testing.T) {
	m, _ := protectedModel(t, false, "4242", "dk", ports.ProtectedSelf)

	for i, tc := range []struct {
		key  string
		want bool // K enabled
	}{
		{"", false}, {"down", true}, {"up", false}, {"down", true},
	} {
		if tc.key != "" {
			m = feed(t, m, testutil.Key(tc.key))
		}
		if got := testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill); got != tc.want {
			row, _ := m.portsModel.table.Selected()
			t.Errorf("step %d (%q, row %s): K enabled = %v, want %v", i, tc.key, row.PID, got, tc.want)
		}
	}
}

// Rule 130: the set of keys is the same on a protected row and an ordinary one.
func TestTheKeySetDoesNotChangeOnAProtectedRow(t *testing.T) {
	m, _ := protectedModel(t, false, "4242", "dk", ports.ProtectedSelf)
	protected := testutil.ShortcutKeys(m.GetShortcuts())
	ordinary := testutil.ShortcutKeys(feed(t, m, testutil.Key("down")).GetShortcuts())

	if !reflect.DeepEqual(protected, ordinary) {
		t.Errorf("a protected row advertises %v, an ordinary one %v", protected, ordinary)
	}
}

// ── The target is rechecked when Yes arrives ─────────────────────────────────

func TestYesRechecksTheCapturedTarget(t *testing.T) {
	t.Run("a target that became protected is not signalled", func(t *testing.T) {
		m, procs := confirmOnNode(t)
		procs.protectAs("4242", ports.ProtectedSelf) // after the confirmation opened

		m, cmd := step(t, m, components.ConfirmModalYesMsg{})

		if len(procs.signals()) != 0 {
			t.Errorf("signalled %v: a PID that became protected between K and Yes", procs.signals())
		}
		if cmd == nil {
			t.Error("no command returned, so the footer warning would never clear")
		}
		if got := m.portsModel.footer.Text(); !strings.Contains(got, reasonSelfText) {
			t.Errorf("footer = %q, want it to carry %q", got, reasonSelfText)
		}
		if got := m.portsModel.footer.Level(); got != components.LevelWarning {
			t.Errorf("footer level = %v, want Warning", got)
		}
		if m.portsModel.table.IsBusy("4242") {
			t.Error("the refused target was marked busy")
		}
		if m.portsModel.confirmModal != nil || m.portsModel.pendingKill != nil {
			t.Error("the confirmation stayed open after Yes")
		}
	})

	t.Run("the captured target is rechecked, not the row under the cursor", func(t *testing.T) {
		m, procs := confirmOnNode(t)
		m = feed(t, m, portsDataMsg{ports: nodeLast()}) // the cursor is now on sshd, PID 812
		procs.protectAs("812", ports.ProtectedParent)   // the selection is protected; the target is not

		_, cmd := step(t, m, components.ConfirmModalYesMsg{})
		runTerm(t, cmd)

		if len(procs.terminated) != 1 || procs.terminated[0] != "4242" {
			t.Errorf("SIGTERM went to %v, want [4242]: the verdict on the selected row must not decide", procs.terminated)
		}
	})

	t.Run("a protected target is refused though another row is the selection", func(t *testing.T) {
		m, procs := confirmOnNode(t)
		m = feed(t, m, portsDataMsg{ports: nodeLast()})
		procs.protectAs("4242", ports.ProtectedSelf)

		m, _ = step(t, m, components.ConfirmModalYesMsg{})

		if len(procs.signals()) != 0 {
			t.Errorf("signalled %v", procs.signals())
		}
		if got := m.portsModel.footer.Text(); !strings.Contains(got, reasonSelfText) {
			t.Errorf("footer = %q, want it to carry %q", got, reasonSelfText)
		}
	})

	t.Run("an unprotected target is signalled as before", func(t *testing.T) {
		m, procs := confirmOnNode(t)

		_, cmd := step(t, m, components.ConfirmModalYesMsg{})
		runTerm(t, cmd)

		if len(procs.terminated) != 1 || procs.terminated[0] != "4242" {
			t.Errorf("SIGTERM went to %v, want [4242]", procs.terminated)
		}
	})
}

// ── A ProtectedError from Kill reads as a warning (defence in depth) ─────────

const protectedSelfWarning = "PID 4242 not stopped: This is DevDesk itself"

func TestAProtectedErrorFromKillReadsAsAWarning(t *testing.T) {
	pe := &ports.ProtectedError{PID: 4242, Protection: ports.ProtectedSelf}

	for _, tt := range []struct {
		name string
		err  error
	}{
		{"as returned", pe},
		{"wrapped", fmt.Errorf("killing process 4242: %w", pe)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := portsModel(t)
			m.portsModel.table.MarkBusy("4242", "Killing dk (4242)")

			m, cmd := step(t, m, portsKillResultMsg{pid: "4242", err: tt.err})

			if got := m.portsModel.footer.Text(); got != protectedSelfWarning {
				t.Errorf("footer = %q, want %q", got, protectedSelfWarning)
			}
			if got := m.portsModel.footer.Level(); got != components.LevelWarning {
				t.Errorf("footer level = %v, want Warning: nothing failed, DevDesk declined", got)
			}
			if cmd == nil {
				t.Error("no clear timer after the refusal")
			}
			if m.portsModel.table.IsBusy("4242") {
				t.Error("the busy mark survived a refusal: nothing is being killed")
			}
			for _, bad := range []string{"terminated", "Failed", "check logs", "Refused by the system"} {
				if strings.Contains(m.portsModel.footer.Text(), bad) {
					t.Errorf("footer = %q must not say %q", m.portsModel.footer.Text(), bad)
				}
			}
		})
	}
}

// Every protection reads as "PID N not stopped: <its reason>".
func TestAProtectedErrorNamesItsReason(t *testing.T) {
	for _, p := range []ports.Protection{
		ports.ProtectedSelf, ports.ProtectedParent, ports.ProtectedInit, ports.ProtectedWindowsSystem,
	} {
		pe := &ports.ProtectedError{PID: 77, Protection: p}
		m, _ := step(t, portsModel(t), portsKillResultMsg{pid: "77", err: pe})

		if want := "PID 77 not stopped: " + p.Reason(); m.portsModel.footer.Text() != want {
			t.Errorf("%v: footer = %q, want %q", p, m.portsModel.footer.Text(), want)
		}
		if m.portsModel.footer.Level() != components.LevelWarning {
			t.Errorf("%v: footer level = %v, want Warning", p, m.portsModel.footer.Level())
		}
	}
}

// End to end: the view's own check said Unprotected, the SIGTERM was refused
// anyway, and that refusal reaches the footer as a warning with nothing marked as
// stopping and no wait started (S-02b criterion 6).
func TestAStopRefusedByThePackageEndsAsAWarning(t *testing.T) {
	m, procs := confirmOnNode(t)
	procs.termErr = &ports.ProtectedError{PID: 4242, Protection: ports.ProtectedSelf}

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	res := runTerm(t, cmd)
	m, _ = step(t, m, res)

	if got := m.portsModel.footer.Text(); got != protectedSelfWarning {
		t.Errorf("footer = %q, want %q", got, protectedSelfWarning)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelWarning {
		t.Errorf("footer level = %v, want Warning", got)
	}
	if m.portsModel.table.IsBusy("4242") {
		t.Error("the row is still marked busy after the refusal")
	}
	if m.portsModel.stop != nil {
		t.Error("a refused SIGTERM left a stop pending")
	}
}

// The same refusal on Windows, where Yes is the kill: nothing changes (S-02a).
func TestAKillRefusedByThePackageEndsAsAWarningOnWindows(t *testing.T) {
	m, procs := windowsNode(t)
	procs.err = &ports.ProtectedError{PID: 4412, Protection: ports.ProtectedWindowsSystem}

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	m, _ = step(t, m, runKill(t, cmd))

	if got, want := m.portsModel.footer.Text(), "PID 4412 not stopped: "+reasonWindowsText; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
	if m.portsModel.table.IsBusy("4412") {
		t.Error("the row is still marked busy after the refusal")
	}
}

// Non-regression: anything that is not a ProtectedError keeps its treatment,
// a red error with the same sentences as before.
func TestOtherKillErrorsStayErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"permission", fmt.Errorf("killing process 812: %w", os.ErrPermission), "Refused by the system — DevDesk cannot signal PID 812"},
		{"already gone", fmt.Errorf("killing process 812: %w", os.ErrProcessDone), "PID 812 is no longer running"},
		{"anything else", errors.New("operation not permitted"), "Failed to kill PID 812 — check logs"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := step(t, portsModel(t), portsKillResultMsg{pid: "812", err: tt.err})

			if got := m.portsModel.footer.Text(); got != tt.want {
				t.Errorf("footer = %q, want %q", got, tt.want)
			}
			if got := m.portsModel.footer.Level(); got != components.LevelError {
				t.Errorf("footer level = %v, want Error", got)
			}
		})
	}
}

// ── Rule 110 ─────────────────────────────────────────────────────────────────

// The verdict is read in Update and in the render, never in the command: the
// command is the kill and nothing else.
func TestTheKillCommandNeverEvaluatesTheProtection(t *testing.T) {
	m, procs := confirmOnNode(t)

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	if !contains(procs.asked, "4242") {
		t.Errorf("Yes did not recheck the captured target (asked %v)", procs.asked)
	}

	procs.asked = nil
	runTerm(t, cmd)
	if len(procs.asked) != 0 {
		t.Errorf("the kill command asked about %v: the verdict must be read in Update (Rule 110)", procs.asked)
	}
}

// Reading the header costs one verdict for the selected row, not one per row.
func TestTheHeaderAsksAboutTheSelectedRowOnly(t *testing.T) {
	m, procs := protectedModel(t, false, "4242", "dk", ports.ProtectedSelf)
	procs.asked = nil

	m.GetShortcuts()

	if len(procs.asked) == 0 {
		t.Fatal("the header did not ask whether the selected process is protected")
	}
	for _, pid := range procs.asked {
		if pid != "4242" {
			t.Errorf("the header asked about PID %s, which is not the selected row", pid)
		}
	}
}

// ── Footer width ─────────────────────────────────────────────────────────────

// Rule 128 at 80 columns: the reason is one centred line, whole, and the footer
// keeps the height the router budgeted.
func TestTheProtectionFooterFitsAt80Columns(t *testing.T) {
	check := func(t *testing.T, m *Model, want string) {
		t.Helper()
		if got := infoLine(m, 80); got != want {
			t.Errorf("footer line at 80 columns = %q, want %q", got, want)
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

	for _, p := range []ports.Protection{
		ports.ProtectedSelf, ports.ProtectedParent, ports.ProtectedInit, ports.ProtectedWindowsSystem,
	} {
		t.Run("K on "+p.Reason(), func(t *testing.T) {
			m, _ := protectedModel(t, p == ports.ProtectedWindowsSystem, "4242", "dk", p)
			m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
			m = feed(t, m, testutil.Key(keymap.Kill))
			check(t, m, p.Reason())
		})
		t.Run("refusal from Kill, "+p.Reason(), func(t *testing.T) {
			m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 80, Height: 30})
			m = feed(t, m, portsKillResultMsg{pid: "4194304", err: &ports.ProtectedError{PID: 4194304, Protection: p}})
			check(t, m, "PID 4194304 not stopped: "+p.Reason())
		})
	}
}

// ── Production wiring ────────────────────────────────────────────────────────

func TestRealProcessOpsProtectThroughThePortsPackage(t *testing.T) {
	ops := realProcessOps()

	if ops.protection == nil {
		t.Fatal("realProcessOps().protection is nil")
	}
	if got, want := reflect.ValueOf(ops.protection).Pointer(), reflect.ValueOf(ports.ProtectionOf).Pointer(); got != want {
		t.Error("realProcessOps().protection is not ports.ProtectionOf")
	}
}

// The real verdict, through the real model, with the signal itself replaced by
// the fake: DevDesk's own PID greys K and no Kill is reachable.
func TestTheRealProtectionGreysDevDeskItself(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{sock(self, "dk"), sock("9001", "ordinary")}})
	m.portsModel.table.Table().SetCursor(0)
	procs := &fakeProcs{}
	ops := realProcessOps()
	fake := procs.ops(false)
	ops.terminate, ops.probe, ops.kill = fake.terminate, fake.probe, fake.kill
	m.portsModel.ops = ops

	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
		t.Fatalf("K is offered on DevDesk's own PID %s", self)
	}
	next, _ := step(t, m, testutil.Key(keymap.Kill))
	if next.portsModel.confirmModal != nil {
		t.Error("K opened the confirmation on DevDesk's own PID")
	}
	if got := next.portsModel.footer.Text(); got != reasonSelfText {
		t.Errorf("footer = %q, want %q", got, reasonSelfText)
	}
	if len(procs.signals()) != 0 {
		t.Errorf("signalled %v", procs.signals())
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func TestTheHelpSaysWhereKIsGreyed(t *testing.T) {
	const want = "K is greyed on DevDesk itself, on the process that started it, on PID 1, and on PIDs 0 and 4 under Windows; the footer says why."

	for _, s := range portsModel(t).GetHelpContent().Sections {
		if strings.HasPrefix(s.Title, "Ports Tab") && strings.Contains(squash(s.Body), want) {
			return
		}
	}
	t.Errorf("no Ports Tab help section contains %q", want)
}
