package netdiag

import (
	"fmt"
	"os"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-711: a SIGKILL that was really sent is always announced, but its
// result only lifts the busy mark and ends the stop when its generation is the
// current stop's. A result of another generation must not clear the mark of a
// stop it does not belong to.

func TestAKillResultOfAnotherGenerationIsAnnouncedButClearsNothing(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name  string
		err   error
		want  string
		level components.Level
	}{
		{"success", nil, "Process 812 terminated", components.LevelInfo},
		{"refused", fmt.Errorf("killing process 812: %w", os.ErrPermission),
			"Refused by the system — DevDesk cannot signal PID 812", components.LevelError},
		{"already gone", fmt.Errorf("killing process 812: %w", os.ErrProcessDone),
			stoppedNginx, components.LevelInfo},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _, _ := forcing(t)
			s := *m.portsModel.stop

			m, _ = step(t, m, portsKillResultMsg{gen: s.gen + 1000, pid: "812", process: "nginx", err: tt.err})

			if got := m.portsModel.footer.Text(); got != tt.want {
				t.Errorf("footer = %q, want %q: a SIGKILL that was sent has to be announced", got, tt.want)
			}
			if got := m.portsModel.footer.Level(); got != tt.level {
				t.Errorf("footer level = %v, want %v", got, tt.level)
			}
			if got := m.portsModel.stop; got == nil || *got != s {
				t.Errorf("stop = %+v, want the current stop %+v untouched", got, s)
			}
			if got := busyOf(m, "812"); got != "Killing nginx (812)" {
				t.Errorf("busy label = %q, want the mark of the current stop kept", got)
			}
		})
	}
}

// With no stop in progress (a rebuilt view) the result is announced too, and
// there is nothing of its own to clear.
func TestAKillResultWithNoStopIsAnnounced(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, _ := identityModel(t, false, stopRows()...)

	m, _ = step(t, m, portsKillResultMsg{gen: 7, pid: "812", process: "nginx"})

	if got := m.portsModel.footer.Text(); got != "Process 812 terminated" {
		t.Errorf("footer = %q, want %q", got, "Process 812 terminated")
	}
	if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
		t.Errorf("stop = %+v, busy = %v, want none", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
	}
}
