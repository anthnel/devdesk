package netdiag

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-210 step 4: the ports tick keeps running while the terminal is
// blurred but skips the read; the user's own pause is a separate state.

func portsMsgTypes(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		out = append(out, fmt.Sprintf("%T", msg))
	}
	return out
}

func portsCount[T tea.Msg](msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			n++
		}
	}
	return n
}

// fastPorts returns the parent model on the ports tab with a short tick, so
// running a command to completion does not sleep for two seconds.
func fastPorts(t *testing.T) *Model {
	t.Helper()
	m := portsModel(t)
	m.portsModel.refresh = time.Millisecond
	return m
}

func TestABlurredPortsTickKeepsTheChainButSkipsTheRead(t *testing.T) {
	m := feed(t, fastPorts(t), shared.TerminalFocusMsg{Focused: false})

	_, cmd := step(t, m, portsTickMsg{})
	msgs := testutil.Msgs(cmd)

	if portsCount[portsTickMsg](msgs) != 1 {
		t.Errorf("a blurred tick must re-arm the chain once, got %v", portsMsgTypes(msgs))
	}
	if portsCount[portsDataMsg](msgs) != 0 {
		t.Errorf("a blurred tick read the socket table: %v", portsMsgTypes(msgs))
	}
}

func TestPortsFocusResumeReadsOnceWithoutATick(t *testing.T) {
	m := feed(t, fastPorts(t), shared.TerminalFocusMsg{Focused: false})

	m, cmd := step(t, m, shared.TerminalFocusMsg{Focused: true, Resume: true})
	msgs := testutil.Msgs(cmd)

	if portsCount[portsDataMsg](msgs) != 1 {
		t.Errorf("resume must read once, got %v", portsMsgTypes(msgs))
	}
	if portsCount[portsTickMsg](msgs) != 0 {
		t.Errorf("resume started a second tick chain: %v", portsMsgTypes(msgs))
	}

	_, cmd = step(t, m, portsTickMsg{})
	msgs = testutil.Msgs(cmd)
	if portsCount[portsDataMsg](msgs) != 1 || portsCount[portsTickMsg](msgs) != 1 {
		t.Errorf("a focused tick after resume must read and re-arm, got %v", portsMsgTypes(msgs))
	}
}

func TestPortsFocusResumeLeavesTheUserPauseAlone(t *testing.T) {
	m := fastPorts(t)
	m = feed(t, m, testutil.Key("space"))
	if !m.portsModel.paused {
		t.Fatal("space did not pause the poll")
	}
	m = feed(t, m, shared.TerminalFocusMsg{Focused: false})

	m, cmd := step(t, m, shared.TerminalFocusMsg{Focused: true, Resume: true})

	if cmd != nil {
		t.Errorf("resume read while the user's pause is on: %v", portsMsgTypes(testutil.Msgs(cmd)))
	}
	if !m.portsModel.paused {
		t.Error("the focus edge lifted the user's pause")
	}
	if !m.portsModel.table.FilterBar().IsTokenActive(filterTokenPaused) {
		t.Error("the paused token went out after the focus edge")
	}
	if m.portsModel.statusLine().Text == "" {
		t.Error("the status line no longer says paused")
	}
}

func TestPortsFocusEdgesWithoutResumeIssueNoCommand(t *testing.T) {
	for _, msg := range []shared.TerminalFocusMsg{{Focused: true}, {Focused: false}} {
		_, cmd := step(t, fastPorts(t), msg)
		if cmd != nil {
			t.Errorf("%+v returned a command, want nil", msg)
		}
	}
}

// A blur must not touch the user's pause either way.
func TestPortsBlurDoesNotSetTheUserPause(t *testing.T) {
	m := feed(t, fastPorts(t), shared.TerminalFocusMsg{Focused: false})
	if m.portsModel.paused || m.portsModel.statusLine().Text != "" {
		t.Error("a blur showed up as the user's pause")
	}
}

// The focus message reaches the ports model whatever tab is showing.
func TestNetdiagRoutesTheFocusToPortsOnAnyTab(t *testing.T) {
	for tab := 0; tab < 4; tab++ {
		t.Run(fmt.Sprintf("after %d tab presses", tab), func(t *testing.T) {
			m := newTestModel(t)
			for i := 0; i < tab; i++ {
				m = feed(t, m, testutil.Key("tab"))
			}
			m.portsModel.refresh = time.Millisecond
			m = feed(t, m, shared.TerminalFocusMsg{Focused: false})

			_, cmd := step(t, m, portsTickMsg{})
			msgs := testutil.Msgs(cmd)
			if portsCount[portsDataMsg](msgs) != 0 {
				t.Errorf("the ports model never heard the blur: %v", portsMsgTypes(msgs))
			}
		})
	}
}
