package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/security"
)

// DEVDESK-372: the :sec inventory load shows the shared dot, and a scan job
// running beside it does not add a second one.

func TestSecLoadWithAScanJobShowsExactlyOneDot(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedSecurityLoading(t, a)
	startRunning(t, a, jobs.NewRun(jobs.KindScan, command.ViewSecurity, "", "inventory", "apirepo"), "apirepo")

	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	for _, phase := range []bool{true, false} {
		a.anim.blinkOn = phase
		_ = a.sendFrameTo(command.ViewSecurity)
		plain := ansi.Strip(viewFooter(t, a, command.ViewSecurity))
		if n := strings.Count(plain, on) + strings.Count(plain, off); n != 1 {
			t.Errorf("phase on=%v: %d dots, want exactly one\n%s", phase, n, plain)
		}
		if !strings.Contains(plain, "Loading scan inventory...") {
			t.Errorf("the load text is missing (it wins over the jobs line)\n%s", plain)
		}
	}
}

func TestSecInventoryLoadedMsgEndsTheDot(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedSecurityLoading(t, a)
	a.Update(security.InventoryLoadedMsg{})
	plain := ansi.Strip(viewFooter(t, a, command.ViewSecurity))
	if strings.ContainsAny(plain, "●○") {
		t.Errorf("a dot after the inventory loaded\n%s", plain)
	}
}
