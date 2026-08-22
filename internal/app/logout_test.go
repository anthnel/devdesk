package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/ui/forge/auth"
)

// signedIn puts the router in the state a successful login leaves it in.
func signedIn(t *testing.T) *App {
	t.Helper()
	a := newWithSize(testConfig(), 120, 40)
	a.setAuthenticated(gitlabforge.NewWithClient(nil, "https://gitlab.example.com"), forge.User{Username: "anthoni"})
	a.sharedState.ForgeStats = &forge.DashboardStats{Repositories: forge.Count(12)}
	return a
}

// Logging out has to clear what logging in set. LogoutCompleteMsg had no router
// handler at all: the auth view reset its own three fields and sharedState kept
// the client, the user and the caches — so the explorer went on browsing and
// the header went on naming a signed-out user.
func TestLoggingOutClearsTheSharedSession(t *testing.T) {
	a := signedIn(t)

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if a.sharedState.IsAuthenticated {
		t.Error("IsAuthenticated is still true after logging out")
	}
	if a.sharedState.Forge != nil {
		t.Error("the forge survived the logout, so views can still call the API")
	}
	if a.sharedState.CurrentUser.Username != "" {
		t.Error("CurrentUser survived, so the header still names a signed-out user")
	}
}

// The dashboard's counters were read through the client that just stopped being
// valid, so they go with it — otherwise the dashboard keeps reporting a
// signed-out user's project count.
//
// This used to assert on CachedGroups and CachedProjects too. They are gone
// (D36): nothing ever wrote to them, so the assertion held for a cache that was
// nil at every moment of its life. ForgeStats is the one of the three the
// dashboard actually fills.
func TestLoggingOutDropsTheCachedGitLabData(t *testing.T) {
	a := signedIn(t)

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if a.sharedState.ForgeStats != nil {
		t.Errorf("ForgeStats survived the logout: %+v", a.sharedState.ForgeStats)
	}
}

// Clearing sharedState does not empty a table the explorer already loaded, so
// the view itself has to go.
func TestLoggingOutDropsTheExplorer(t *testing.T) {
	a := signedIn(t)
	a.createView(command.ViewGitExplorer)
	if _, ok := a.views[command.ViewGitExplorer]; !ok {
		t.Fatal("the explorer was not built, so the test proves nothing")
	}

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if _, ok := a.views[command.ViewGitExplorer]; ok {
		t.Error("the explorer survived the logout with whatever it had loaded")
	}
}

// The auth view is kept: it is on screen and has just written "Logged out
// successfully", which rebuilding would throw away.
func TestLoggingOutKeepsTheAuthView(t *testing.T) {
	a := signedIn(t)
	a.createView(command.ViewGitAuth)
	before := a.views[command.ViewGitAuth]

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if a.views[command.ViewGitAuth] != before {
		t.Error("the auth view was rebuilt, discarding the logout confirmation")
	}
}

// The message still has to reach the view, or the form never resets.
func TestTheLogoutMessageStillReachesTheView(t *testing.T) {
	a := signedIn(t)
	a.currentView = command.ViewGitAuth
	a.createView(command.ViewGitAuth)

	_, cmd := a.Update(auth.LogoutCompleteMsg{})

	if cmd == nil {
		t.Fatal("no command returned, so the view was never told")
	}
}
