package netdiag

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-709 (S-01b, criteria 2, 4, 6 and 7 on the interface side): K verifies
// which process a row is. It is greyed when DevDesk cannot tell, the signal
// goes with the identity the row was listed with, a changed identity sends
// nothing and re-reads the table, and a deferred identity (Windows, over the
// listing budget) is read when the confirmation opens.
//
// Nothing here reaches ports.Kill, ports.ConfirmIdentity or ports.List: every
// model installs a fakeProcs. A test that drains a Cmd shortens the footer timer
// first (testutil.FastTimers), and no test runs the command a portsRefreshMsg
// leads to, since it would read the real socket table.

// The sentences of the story, written out so that a drift in the constants of
// this package or of internal/ports fails a test instead of following them.
const (
	unverifiableText       = "Cannot verify which process this is"
	changedWarning         = "node (PID 4242) is no longer the process that was listed — table refreshed"
	deferredChangedWarning = "node.exe (PID 4412) is no longer the process that was listed — table refreshed"
	unverifiableAtSignal   = "Cannot verify which process PID 4242 is — not stopped"
	refusedByTheSystem4412 = "Refused by the system — DevDesk cannot signal PID 4412"
)

// listedAt is the instant a Windows listing stamped on the rows it deferred.
var listedAt = time.Unix(1_700_000_000, 0)

// identityModel is a wide Ports model whose cursor is on rows[0], with the
// recording fake installed.
func identityModel(t *testing.T, windows bool, rows ...ports.Socket) (*Model, *fakeProcs) {
	t.Helper()
	m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 160, Height: 40})
	m = feed(t, m, portsDataMsg{ports: rows})
	m.portsModel.table.Table().SetCursor(0)
	procs := installProcs(m, windows)
	if row, ok := m.portsModel.table.Selected(); !ok || row.PID != rows[0].PID {
		t.Fatalf("setup: selected row = %+v, want PID %s", row, rows[0].PID)
	}
	return m, procs
}

// unknownNode is a model on node (PID 4242), listed with no identity, and an
// ordinary row after it.
func unknownNode(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	return identityModel(t, false, sockWith("4242", "node", ports.Identity{}), sock("9001", "ordinary"))
}

// deferredNode is a Windows model on node.exe (PID 4412), listed with a deferred
// identity, and an ordinary row after it.
func deferredNode(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	return identityModel(t, true,
		sockWith("4412", "node.exe", ports.DeferredIdentityForTest(listedAt)), sock("812", "sshd"))
}

func countRefreshes(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(portsRefreshMsg); ok {
			n++
		}
	}
	return n
}

func countTicks(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(portsTickMsg); ok {
			n++
		}
	}
	return n
}

// outcome is what a refusal leaves behind: the footer line, its level, and how
// many reads of the table it asked for.
type outcome struct {
	text      string
	level     components.Level
	refreshes int
}

// outcomeOf drains cmd (the footer timer is shortened by the caller) and reads
// the footer.
func outcomeOf(m *Model, cmd tea.Cmd) outcome {
	msgs := testutil.Msgs(cmd)
	return outcome{
		text:      m.portsModel.footer.Text(),
		level:     m.portsModel.footer.Level(),
		refreshes: countRefreshes(msgs),
	}
}

// ── Criterion 4: K is greyed when the identity is unknown ────────────────────

func TestTheUnverifiableReasonIsTheAgreedSentence(t *testing.T) {
	if reasonUnverifiable != unverifiableText {
		t.Errorf("reasonUnverifiable = %q, want the agreed %q", reasonUnverifiable, unverifiableText)
	}
}

func TestKillIsGreyedWhenTheIdentityIsUnknown(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, procs := unknownNode(t)

	// Rule 130: advertised, greyed, same label.
	if !testutil.HasShortcut(m.GetShortcuts(), keymap.Kill) {
		t.Fatal("K disappeared from the header instead of being greyed")
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
		t.Error("K is offered on a row DevDesk cannot verify")
	}
	for _, s := range m.GetShortcuts() {
		if s.Key == keymap.Kill && s.Description != killLabel(m) {
			t.Errorf("greyed K reads %q, want its own label %q", s.Description, killLabel(m))
		}
	}

	next, cmd := step(t, m, testutil.Key(keymap.Kill))

	if next.portsModel.confirmModal != nil {
		t.Error("K opened the confirmation on a row DevDesk cannot verify")
	}
	if next.portsModel.pendingKill != nil {
		t.Errorf("K captured a target on an unverifiable row: %+v", next.portsModel.pendingKill)
	}
	if cmd == nil {
		t.Error("the refusal returned no command, so its footer line would never clear")
	}
	got := outcomeOf(next, cmd)
	if got.text != unverifiableText {
		t.Errorf("footer = %q, want exactly %q", got.text, unverifiableText)
	}
	if got.level != components.LevelWarning {
		t.Errorf("footer level = %v, want Warning (nothing failed: Rules 128 and 130)", got.level)
	}
	if got.refreshes != 0 {
		t.Errorf("the greyed K asked for %d reads of the table, want none", got.refreshes)
	}
	if !contains(visiblePIDs(next), "4242") {
		t.Errorf("the row of PID 4242 left the table (visible: %v)", visiblePIDs(next))
	}
	if row, ok := next.portsModel.table.Selected(); !ok || row.PID != "4242" {
		t.Errorf("the selection moved off the unverifiable row: %+v", row)
	}
	if next.portsModel.InEditMode() {
		t.Error("the refused K left the view in edit mode")
	}
	if len(procs.confirmAsked) != 0 {
		t.Errorf("the refused K asked the system to confirm %v", procs.confirmAsked)
	}
	if len(procs.signals()) != 0 {
		t.Errorf("signalled %v", procs.signals())
	}
}

// The protection comes first, as in ports.Kill: DevDesk's own row says what it
// is, not that DevDesk cannot verify it.
func TestProtectionOutranksTheUnknownIdentity(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name   string
		prot   ports.Protection
		reason string
	}{
		{"DevDesk itself", ports.ProtectedSelf, reasonSelfText},
		{"the process that started it", ports.ProtectedParent, reasonParentText},
		{"PID 1", ports.ProtectedInit, reasonInitText},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := unknownNode(t)
			procs.protectAs("4242", tt.prot)

			if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
				t.Fatal("K is offered on a protected process")
			}
			next, cmd := step(t, m, testutil.Key(keymap.Kill))

			if got := outcomeOf(next, cmd); got.text != tt.reason || got.level != components.LevelWarning {
				t.Errorf("footer = %q (%v), want %q at Warning, not %q", got.text, got.level, tt.reason, unverifiableText)
			}
			if next.portsModel.confirmModal != nil {
				t.Error("K opened the confirmation on a protected process")
			}
		})
	}
}

// A socket with no PID has no identity either; the refusal names the missing
// PID, which is the first fact.
func TestNoPIDOutranksTheUnknownIdentity(t *testing.T) {
	m, _ := identityModel(t, false, sockWith("", "kernel", ports.Identity{}))

	next, _ := step(t, m, testutil.Key(keymap.Kill))

	if got := next.portsModel.footer.Text(); got != reasonNoPID {
		t.Errorf("footer = %q, want %q", got, reasonNoPID)
	}
}

// Rule 130: the set of keys is the same on an unverifiable row and a verifiable
// one.
func TestTheKeySetDoesNotChangeOnAnUnverifiableRow(t *testing.T) {
	m, _ := unknownNode(t)
	unverifiable := testutil.ShortcutKeys(m.GetShortcuts())
	verifiable := testutil.ShortcutKeys(feed(t, m, testutil.Key("down")).GetShortcuts())

	if !reflect.DeepEqual(unverifiable, verifiable) {
		t.Errorf("an unverifiable row advertises %v, a verifiable one %v", unverifiable, verifiable)
	}
}

// The grey is read from the row, on every render, and never asks the system
// anything: a deferred row is offered (the read comes with the confirmation),
// and drawing the header costs no read.
func TestTheHeaderNeverReadsAnIdentity(t *testing.T) {
	m, procs := deferredNode(t)

	for range 3 {
		if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill) {
			t.Fatal("K is greyed on a deferred row: its identity is read when the confirmation opens")
		}
		m.GetHelpContent()
		m.RenderFooter(120)
	}

	if len(procs.confirmAsked) != 0 {
		t.Errorf("rendering asked the system to confirm %v", procs.confirmAsked)
	}
}

// ── Criterion 1 and 3: the signal goes with the identity the row was listed with

func TestYesSignalsWithTheListedIdentity(t *testing.T) {
	m, procs := confirmOnNode(t)

	if !reflect.DeepEqual(procs.confirmAsked, []string{"4242"}) {
		t.Errorf("the confirmation was opened after asking about %v, want [4242]", procs.confirmAsked)
	}
	if m.portsModel.pendingKill == nil || m.portsModel.pendingKill.Identity != idOf("4242") {
		t.Fatalf("pendingKill = %+v, want it to carry the identity of node (PID 4242)", m.portsModel.pendingKill)
	}

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	res := runTerm(t, cmd)

	// The SIGTERM is verified against the identity the row was listed with, like
	// the SIGKILL that may follow it (S-02b criterion 5).
	if want := []ports.Identity{idOf("4242")}; !reflect.DeepEqual(procs.terminatedWith, want) {
		t.Errorf("SIGTERM verified against identities %v, want %v", procs.terminatedWith, want)
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v on the first Yes", procs.killed)
	}
	if res.pid != "4242" || res.process != "node" {
		t.Errorf("stop result = (pid %q, process %q), want (4242, node)", res.pid, res.process)
	}
}

// ── Criterion 2: a changed identity sends nothing and re-reads the table ─────

func TestIdentityChangedText(t *testing.T) {
	for _, tt := range []struct{ process, pid, want string }{
		{"node", "4242", changedWarning},
		{"node.exe", "4412", deferredChangedWarning},
		{"", "4242", "PID 4242 is no longer the process that was listed — table refreshed"},
	} {
		if got := identityChangedText(tt.process, tt.pid); got != tt.want {
			t.Errorf("identityChangedText(%q, %q) = %q, want %q", tt.process, tt.pid, got, tt.want)
		}
	}
}

// The kill itself found another process under the PID.
func TestAChangedIdentityAtTheSignalWarnsAndRefreshes(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	changed := &ports.IdentityChangedError{PID: 4242}

	for _, tt := range []struct {
		name    string
		process string
		err     error
		want    string
	}{
		{"as returned", "node", changed, changedWarning},
		{"wrapped as Kill wraps it", "node", fmt.Errorf("killing process 4242: %w", changed), changedWarning},
		{"no process name", "", changed, "PID 4242 is no longer the process that was listed — table refreshed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := portsModel(t)
			m.portsModel.table.MarkBusy("4242", "Killing node (4242)")

			m, cmd := step(t, m, portsKillResultMsg{pid: "4242", process: tt.process, err: tt.err})
			msgs := testutil.Msgs(cmd)

			if got := m.portsModel.footer.Text(); got != tt.want {
				t.Errorf("footer = %q, want %q", got, tt.want)
			}
			if got := m.portsModel.footer.Level(); got != components.LevelWarning {
				t.Errorf("footer level = %v, want Warning: nothing failed, DevDesk declined", got)
			}
			if m.portsModel.table.IsBusy("4242") {
				t.Error("the busy mark survived a refusal: nothing is being killed")
			}
			if n := countRefreshes(msgs); n != 1 {
				t.Errorf("the refusal asked for %d reads of the table, want exactly 1", n)
			}
			if n := countTicks(msgs); n != 0 {
				t.Errorf("the refusal started %d tick chains, want none", n)
			}
			for _, bad := range []string{"terminated", "Failed", "check logs", "Refused by the system"} {
				if strings.Contains(m.portsModel.footer.Text(), bad) {
					t.Errorf("footer = %q must not say %q", m.portsModel.footer.Text(), bad)
				}
			}
		})
	}
}

// A refresh request is one read now, outside the tick chain: a second
// portsTickMsg would start a second chain. It reads while paused too, as n does,
// and leaves the pause on screen.
func TestARefreshRequestReadsTheTableOnce(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprintf("paused=%v", paused), func(t *testing.T) {
			m := portsModel(t)
			if paused {
				m = feed(t, m, testutil.Key(keymap.Space))
			}
			if m.portsModel.paused != paused {
				t.Fatalf("setup: paused = %v, want %v", m.portsModel.paused, paused)
			}

			m, cmd := step(t, m, portsRefreshMsg{})

			if cmd == nil {
				t.Fatal("a refresh request returned no command")
			}
			// Not run: it would read the real socket table. Its code is the closure
			// fetchPortsCmd returns (the name survives inlining), not a batch that
			// also re-arms the tick, nor a tea.Tick.
			if name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name(); !strings.Contains(name, "fetchPortsCmd") {
				t.Errorf("the command is %s, want the closure of fetchPortsCmd: a second chain or no read at all", name)
			}
			if m.portsModel.paused != paused {
				t.Errorf("the refresh changed the pause: paused = %v, want %v", m.portsModel.paused, paused)
			}
		})
	}
}

func TestRequestPortsRefreshAsksOnceAndReadsNothing(t *testing.T) {
	msgs := testutil.Msgs(requestPortsRefresh())

	if len(msgs) != 1 {
		t.Fatalf("requestPortsRefresh produced %d messages, want 1", len(msgs))
	}
	if _, ok := msgs[0].(portsRefreshMsg); !ok {
		t.Errorf("requestPortsRefresh produced %T, want portsRefreshMsg", msgs[0])
	}
}

// The table already shows another process under the PID when Yes arrives: no
// system call is needed to know, and nothing is sent.
func TestYesOnAPIDNowHeldByAnotherProcessSendsNothing(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, procs := confirmOnNode(t)
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{
		sockWith("4242", "node", ports.IdentityForTest(99)), sock("812", "sshd"),
	}})

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	got := outcomeOf(m, cmd)

	if len(procs.signals()) != 0 {
		t.Errorf("signalled %v for a PID another process holds", procs.signals())
	}
	if got.text != changedWarning || got.level != components.LevelWarning {
		t.Errorf("footer = %q (%v), want %q at Warning", got.text, got.level, changedWarning)
	}
	if got.refreshes != 1 {
		t.Errorf("Yes asked for %d reads of the table, want exactly 1", got.refreshes)
	}
	if m.portsModel.confirmModal != nil || m.portsModel.pendingKill != nil {
		t.Error("the confirmation stayed open after Yes")
	}
	if m.portsModel.table.IsBusy("4242") {
		t.Error("the refused target was marked busy")
	}
}

// A target the table no longer lists keeps the sentence of D174, and asks for
// no read: the table is already what says it is gone.
func TestAnAbsentTargetKeepsTheD174Message(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name  string
		table []ports.Socket
	}{
		{"the PID is gone", []ports.Socket{sock("812", "sshd")}},
		{"the PID is back under another name", []ports.Socket{sock("4242", "python"), sock("812", "sshd")}},
		{"the table is empty", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := confirmOnNode(t)
			m = feed(t, m, portsDataMsg{ports: tt.table})

			m, cmd := step(t, m, components.ConfirmModalYesMsg{})
			got := outcomeOf(m, cmd)

			if got.text != goneWarning || got.level != components.LevelWarning {
				t.Errorf("footer = %q (%v), want %q at Warning", got.text, got.level, goneWarning)
			}
			if got.refreshes != 0 {
				t.Errorf("an absent target asked for %d reads of the table, want none", got.refreshes)
			}
			if len(procs.signals()) != 0 {
				t.Errorf("signalled %v", procs.signals())
			}
		})
	}
}

// ── Criterion 5, on the interface side ───────────────────────────────────────

func TestUnverifiableAtTheSignalIsAWarning(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"as returned", &ports.UnverifiableError{PID: 4242}},
		{"wrapped as Kill wraps it", fmt.Errorf("killing process 4242: %w", &ports.UnverifiableError{PID: 4242})},
		// The cause is for the log: it does not turn DevDesk's refusal into the
		// system's.
		{"with an OS cause", &ports.UnverifiableError{PID: 4242, Cause: os.ErrPermission}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := portsModel(t)
			m.portsModel.table.MarkBusy("4242", "Killing node (4242)")

			m, cmd := step(t, m, portsKillResultMsg{pid: "4242", process: "node", err: tt.err})
			got := outcomeOf(m, cmd)

			if got.text != unverifiableAtSignal {
				t.Errorf("footer = %q, want exactly %q", got.text, unverifiableAtSignal)
			}
			if got.level != components.LevelWarning {
				t.Errorf("footer level = %v, want Warning: a precondition is unmet, nothing failed", got.level)
			}
			if got.refreshes != 0 {
				t.Errorf("asked for %d reads of the table, want none: nothing changed", got.refreshes)
			}
			if m.portsModel.table.IsBusy("4242") {
				t.Error("the busy mark survived a refusal")
			}
		})
	}
}

// ── Criterion 6: the system's refusal stays the system's ─────────────────────

func TestARefusedHandleSaysRefusedByTheSystem(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	err := fmt.Errorf("killing process 4412: opening the process: %w", os.ErrPermission)
	m := portsModel(t)
	m.portsModel.table.MarkBusy("4412", "Killing node.exe (4412)")

	m, cmd := step(t, m, portsKillResultMsg{pid: "4412", process: "node.exe", err: err})
	got := outcomeOf(m, cmd)

	if got.text != refusedByTheSystem4412 {
		t.Errorf("footer = %q, want %q", got.text, refusedByTheSystem4412)
	}
	if got.level != components.LevelError {
		t.Errorf("footer level = %v, want Error: the system refused", got.level)
	}
	if strings.Contains(got.text, "Cannot verify") {
		t.Errorf("footer = %q: an access refusal is not an unverifiable identity", got.text)
	}
	if got.refreshes != 0 {
		t.Errorf("asked for %d reads of the table, want none", got.refreshes)
	}
	if m.portsModel.table.IsBusy("4412") {
		t.Error("the busy mark survived the refusal")
	}
}

// ── Criterion 7: a deferred identity is read when the confirmation opens ─────

func TestADeferredIdentityIsReadWhenTheConfirmationOpens(t *testing.T) {
	m, procs := deferredNode(t)
	read := ports.IdentityForTest(777)
	procs.confirmed = map[string]ports.Identity{"4412": read}

	m = feed(t, m, testutil.Key(keymap.Kill))

	if !reflect.DeepEqual(procs.confirmAsked, []string{"4412"}) {
		t.Errorf("confirmation asked about %v, want [4412]", procs.confirmAsked)
	}
	if m.portsModel.confirmModal == nil {
		t.Fatal("K on a deferred row did not open the confirmation")
	}
	target := m.portsModel.pendingKill
	if target == nil || target.PID != "4412" || target.Process != "node.exe" {
		t.Fatalf("pendingKill = %+v, want node.exe (PID 4412)", target)
	}
	if target.Identity != read {
		t.Errorf("pendingKill.Identity = %+v, want the one read at the confirmation, %+v", target.Identity, read)
	}
	if got := squash(modalBody(m)); got != windowsPrompt {
		t.Errorf("confirmation = %q, want the Windows prompt %q", got, windowsPrompt)
	}
	if len(procs.killed) != 0 {
		t.Errorf("signalled %v before an answer", procs.killed)
	}

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	res := runKill(t, cmd)

	if want := []ports.Identity{read}; !reflect.DeepEqual(procs.killedWith, want) {
		t.Errorf("signalled with identities %v, want %v (the one read, not the deferred one)", procs.killedWith, want)
	}
	if res.pid != "4412" || res.process != "node.exe" {
		t.Errorf("kill result = (pid %q, process %q), want (4412, node.exe)", res.pid, res.process)
	}
}

// What was read at the confirmation is compared with the table at Yes like any
// other identity: a refresh that lists a different stamp under the PID refuses.
func TestADeferredTargetReplacedBeforeYesIsRefused(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, procs := deferredNode(t)
	procs.confirmed = map[string]ports.Identity{"4412": ports.IdentityForTest(777)}
	m = feed(t, m, testutil.Key(keymap.Kill))
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{
		sockWith("4412", "node.exe", ports.IdentityForTest(5)), sock("812", "sshd"),
	}})

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	got := outcomeOf(m, cmd)

	if len(procs.killed) != 0 {
		t.Errorf("signalled %v for a PID another process holds", procs.killed)
	}
	if got.text != deferredChangedWarning || got.level != components.LevelWarning || got.refreshes != 1 {
		t.Errorf("outcome = %+v, want %q at Warning and one read", got, deferredChangedWarning)
	}
}

// And a row the next refresh lists as deferred again is not a conflict: the
// stamp read at the confirmation stands, and the kill verifies it.
func TestADeferredRowAtYesDoesNotBlockTheKill(t *testing.T) {
	m, procs := deferredNode(t)
	procs.confirmed = map[string]ports.Identity{"4412": ports.IdentityForTest(777)}
	m = feed(t, m, testutil.Key(keymap.Kill))
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{
		sockWith("4412", "node.exe", ports.DeferredIdentityForTest(listedAt.Add(2*time.Second))), sock("812", "sshd"),
	}})

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	runKill(t, cmd)

	if !reflect.DeepEqual(procs.killed, []string{"4412"}) {
		t.Errorf("signalled %v, want [4412]", procs.killed)
	}
}

// The read at the confirmation can be refused, or find nothing to verify: the
// refusal shows at the press of K, with no confirmation, and says the same as
// the same error coming back from the kill.
func TestADeferredReadThatFailsRefusesAtThePressOfK(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name string
		err  error
		want outcome
	}{
		{
			"the system refused the read (criterion 6)",
			fmt.Errorf("reading the identity of process 4412: %w", os.ErrPermission),
			outcome{refusedByTheSystem4412, components.LevelError, 0},
		},
		{
			"the process is gone",
			fmt.Errorf("reading the identity of process 4412: %w", os.ErrProcessDone),
			outcome{"PID 4412 is no longer running", components.LevelError, 0},
		},
		{
			"DevDesk cannot tell",
			&ports.UnverifiableError{PID: 4412},
			outcome{"Cannot verify which process PID 4412 is — not stopped", components.LevelWarning, 0},
		},
		{
			"born after the listing",
			&ports.IdentityChangedError{PID: 4412},
			outcome{deferredChangedWarning, components.LevelWarning, 1},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := deferredNode(t)
			procs.confirmErr = map[string]error{"4412": tt.err}

			next, cmd := step(t, m, testutil.Key(keymap.Kill))

			if next.portsModel.confirmModal != nil || next.portsModel.pendingKill != nil {
				t.Error("a refused read opened the confirmation")
			}
			if cmd == nil {
				t.Error("the refusal returned no command, so its footer line would never clear")
			}
			if got := outcomeOf(next, cmd); got != tt.want {
				t.Errorf("outcome = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(procs.confirmAsked, []string{"4412"}) {
				t.Errorf("confirmation asked about %v, want [4412]", procs.confirmAsked)
			}
			if len(procs.killed) != 0 {
				t.Errorf("signalled %v", procs.killed)
			}
			if next.portsModel.table.IsBusy("4412") {
				t.Error("the refused row was marked busy")
			}
			if !contains(visiblePIDs(next), "4412") {
				t.Errorf("the row of PID 4412 left the table (visible: %v)", visiblePIDs(next))
			}
		})
	}
}

// ── One classifier, three sites ──────────────────────────────────────────────

// refusal is the only place a refused or failed stop becomes a footer line. The
// press of K (the confirmation's read), Yes (the table compared with the target)
// and the kill's result reach it, and one error leaves the same footer line, at
// the same level, with the same reads of the table, wherever it surfaces.
func TestOneClassifierServesTheOpeningTheYesAndTheResult(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name string
		err  error
		want outcome
	}{
		{"identity changed", &ports.IdentityChangedError{PID: 4412}, outcome{deferredChangedWarning, components.LevelWarning, 1}},
		{"cannot verify", &ports.UnverifiableError{PID: 4412}, outcome{"Cannot verify which process PID 4412 is — not stopped", components.LevelWarning, 0}},
		{"cannot verify, cause is an OS refusal", &ports.UnverifiableError{PID: 4412, Cause: os.ErrPermission}, outcome{"Cannot verify which process PID 4412 is — not stopped", components.LevelWarning, 0}},
		{"refused by the system", os.ErrPermission, outcome{refusedByTheSystem4412, components.LevelError, 0}},
		{"gone", os.ErrProcessDone, outcome{"PID 4412 is no longer running", components.LevelError, 0}},
		{"anything else", errors.New("operation not permitted"), outcome{"Failed to kill PID 4412 — check logs", components.LevelError, 0}},
		{"protected", &ports.ProtectedError{PID: 4412, Protection: ports.ProtectedSelf}, outcome{"PID 4412 not stopped: " + reasonSelfText, components.LevelWarning, 0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Site 1: the press of K, through the confirmation's read.
			m, procs := deferredNode(t)
			procs.confirmErr = map[string]error{"4412": tt.err}
			opened, cmd := step(t, m, testutil.Key(keymap.Kill))
			if got := outcomeOf(opened, cmd); got != tt.want {
				t.Errorf("at the opening: %+v, want %+v", got, tt.want)
			}

			// Site 3: the result of the kill, wrapped as ports.Kill wraps it.
			m, _ = deferredNode(t)
			m.portsModel.table.MarkBusy("4412", "Killing node.exe (4412)")
			done, cmd := step(t, m, portsKillResultMsg{
				pid: "4412", process: "node.exe", err: fmt.Errorf("killing process 4412: %w", tt.err),
			})
			if got := outcomeOf(done, cmd); got != tt.want {
				t.Errorf("in the result: %+v, want %+v", got, tt.want)
			}
			if done.portsModel.table.IsBusy("4412") {
				t.Error("the busy mark survived the result")
			}
		})
	}

	// Site 2: Yes, when the table holds another process under the PID. Its only
	// verdict is the changed identity, and it reads like the other two.
	t.Run("at Yes, the identity changed", func(t *testing.T) {
		m, procs := deferredNode(t)
		procs.confirmed = map[string]ports.Identity{"4412": ports.IdentityForTest(777)}
		m = feed(t, m, testutil.Key(keymap.Kill))
		m = feed(t, m, portsDataMsg{ports: []ports.Socket{
			sockWith("4412", "node.exe", ports.IdentityForTest(5)), sock("812", "sshd"),
		}})

		m, cmd := step(t, m, components.ConfirmModalYesMsg{})

		want := outcome{deferredChangedWarning, components.LevelWarning, 1}
		if got := outcomeOf(m, cmd); got != want {
			t.Errorf("at Yes: %+v, want %+v", got, want)
		}
	})
}

// ── Rule 128 at 80 columns ───────────────────────────────────────────────────

func TestTheIdentityFootersFitAt80Columns(t *testing.T) {
	fit := func(t *testing.T, m *Model, want string) {
		t.Helper()
		if got := infoLine(m, 80); got != want {
			t.Errorf("footer line at 80 columns = %q, want %q", got, want)
		}
		lines := strings.Split(portsFooter(m, 80), "\n")
		if len(lines) != m.GetFooterHeight() {
			t.Errorf("RenderFooter emits %d lines, GetFooterHeight says %d", len(lines), m.GetFooterHeight())
		}
	}

	t.Run("K on an unverifiable row", func(t *testing.T) {
		m, _ := unknownNode(t)
		m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
		m = feed(t, m, testutil.Key(keymap.Kill))
		fit(t, m, unverifiableText)
	})
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"identity changed", &ports.IdentityChangedError{PID: 4242}, changedWarning},
		{"cannot verify", &ports.UnverifiableError{PID: 4242}, unverifiableAtSignal},
	} {
		t.Run("result: "+tt.name, func(t *testing.T) {
			m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 80, Height: 30})
			m = feed(t, m, portsKillResultMsg{pid: "4242", process: "node", err: tt.err})
			fit(t, m, tt.want)
		})
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func TestPortsHelpNamesTheIdentityCheck(t *testing.T) {
	wants := []string{
		"still belongs to the process it listed",
		"cannot tell which process holds the socket",
	}
	for _, s := range portsModel(t).GetHelpContent().Sections {
		if !strings.HasPrefix(s.Title, "Ports Tab") {
			continue
		}
		body := squash(s.Body)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("the Ports Tab help does not contain %q", want)
			}
		}
		return
	}
	t.Error("no Ports Tab help section")
}
