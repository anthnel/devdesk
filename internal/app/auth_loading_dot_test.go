package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/forge/auth"
)

// DEVDESK-375: the forge login shows "Authenticating..." and the shared dot in
// the footer. The seed below feeds footerLoadingSeeds; no Cmd is executed.

// seedGitAuthLoading opens the auth view and starts a login (its Cmds are dropped).
func seedGitAuthLoading(t *testing.T, a *App) {
	t.Helper()
	delete(a.views, command.ViewGitAuth)
	_ = a.switchView(command.ViewGitAuth)
	a.resize(a.width, a.height)
	a.Update(auth.AuthStartMsg{})
}

func TestAuthLoginAsksTheRouterForTheBlink(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedGitAuthLoading(t, a)

	if !a.wantsBlink() {
		t.Error("the router does not want the animation while a login is in flight")
	}
	if got := ansi.Strip(viewFooter(t, a, command.ViewGitAuth)); !strings.Contains(got, "Authenticating...") {
		t.Errorf("the footer does not say Authenticating...\n%s", got)
	}
}
