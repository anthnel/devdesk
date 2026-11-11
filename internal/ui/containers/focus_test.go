package containers

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-210 step 4: the refresh chain keeps ticking while the terminal is
// blurred but skips the read; one read happens when the focus comes back.

func msgTypes(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		out = append(out, fmt.Sprintf("%T", msg))
	}
	return out
}

func countOf[T tea.Msg](msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			n++
		}
	}
	return n
}

func TestABlurredTickKeepsTheChainButSkipsTheRead(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	testutil.FastTimers(t, &refreshInterval)
	m := loadedModel(t)
	m = feed(t, m, shared.TerminalFocusMsg{Focused: false})

	_, cmd := step(t, m, RefreshTickMsg(time.Now()))
	msgs := testutil.Msgs(cmd)

	if countOf[RefreshTickMsg](msgs) != 1 {
		t.Errorf("a blurred tick must re-arm the chain once, got %v", msgTypes(msgs))
	}
	if countOf[ContainersListMsg](msgs) != 0 || countOf[ContainerMetricsMsg](msgs) != 0 {
		t.Errorf("a blurred tick read the daemon: %v", msgTypes(msgs))
	}
}

func TestFocusResumeReadsOnceWithoutANewChain(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	testutil.FastTimers(t, &refreshInterval)
	m := feed(t, loadedModel(t), shared.TerminalFocusMsg{Focused: false})

	m, cmd := step(t, m, shared.TerminalFocusMsg{Focused: true, Resume: true})
	msgs := testutil.Msgs(cmd)

	if countOf[ContainersListMsg](msgs) != 1 || countOf[ContainerMetricsMsg](msgs) != 1 {
		t.Errorf("resume must read the list and the metrics once, got %v", msgTypes(msgs))
	}
	if countOf[RefreshTickMsg](msgs) != 0 {
		t.Errorf("resume started a second tick chain: %v", msgTypes(msgs))
	}

	// Focused again: the next tick reads as usual.
	_, cmd = step(t, m, RefreshTickMsg(time.Now()))
	msgs = testutil.Msgs(cmd)
	if countOf[ContainersListMsg](msgs) != 1 || countOf[RefreshTickMsg](msgs) != 1 {
		t.Errorf("a focused tick after resume must read and re-arm, got %v", msgTypes(msgs))
	}
}

func TestFocusEdgesWithoutResumeIssueNoCommand(t *testing.T) {
	tests := []struct {
		name string
		msg  shared.TerminalFocusMsg
	}{
		{"entering the screen focused", shared.TerminalFocusMsg{Focused: true}},
		{"blur", shared.TerminalFocusMsg{Focused: false}},
		{"blur with a stray resume", shared.TerminalFocusMsg{Focused: false, Resume: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := step(t, loadedModel(t), tt.msg)
			if cmd != nil {
				t.Errorf("%+v returned a command, want nil", tt.msg)
			}
		})
	}
}
