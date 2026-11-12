package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-373: the :net diagnostics run and the interfaces read show the shared
// dot. The seeds below feed footerLoadingSeeds; no Cmd is executed.

// seedNetdiagDiagnosticsRun opens :net on a run against example.com:443. The
// pipeline's Cmd is dropped, so the run stays at its first stage.
func seedNetdiagDiagnosticsRun(t *testing.T, a *App) {
	t.Helper()
	delete(a.views, command.ViewNetdiag)
	a.Update(netdiag.OpenRequestMsg{Target: netcheck.Target{Host: "example.com", Port: 443}, AutoRun: true})
	a.resize(a.width, a.height)
}

// seedNetdiagInterfacesLoading puts :net on its Interfaces tab with the first
// read unanswered (its Init Cmd is dropped).
func seedNetdiagInterfacesLoading(t *testing.T, a *App) {
	t.Helper()
	delete(a.views, command.ViewNetdiag)
	_ = a.switchView(command.ViewNetdiag)
	a.resize(a.width, a.height)
	a.Update(testutil.Key("tab"))
	a.Update(testutil.Key("tab"))
}

func netdiagDots(t *testing.T, a *App) (on, off int) {
	t.Helper()
	plain := ansi.Strip(viewFooter(t, a, command.ViewNetdiag))
	return strings.Count(plain, indicator.Glyph(indicator.Running, true)),
		strings.Count(plain, indicator.Glyph(indicator.Running, false))
}

func TestNetdiagTabSwitchDuringALoadRearmsTheChainOnReturn(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedNetdiagInterfacesLoading(t, a)
	a.anim.ticking = false
	a.Update(testutil.Key("tab")) // Forward: nothing to blink
	a.Update(testutil.Key("shift+tab"))
	if !a.anim.ticking {
		t.Error("back on the loading Interfaces tab the animation chain was not armed again")
	}
	if on, off := netdiagDots(t, a); on+off != 1 {
		t.Errorf("want exactly one dot on return, got on=%d off=%d", on, off)
	}
}

func TestNetdiagFrameReachesTheInactiveInterfacesFooter(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedNetdiagInterfacesLoading(t, a)
	a.Update(testutil.Key("tab"))
	a.anim.blinkOn = false
	_ = a.sendFrameTo(command.ViewNetdiag)
	a.Update(testutil.Key("shift+tab"))
	if on, off := netdiagDots(t, a); on != 0 || off != 1 {
		t.Errorf("a frame sent while on Forward left the Interfaces footer frozen: on=%d off=%d", on, off)
	}
}
