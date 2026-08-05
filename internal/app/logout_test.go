package app

import (
	"testing"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/gitlab/auth"
)

// signedIn puts the router in the state a successful login leaves it in.
func signedIn(t *testing.T) *App {
	t.Helper()
	a := newWithSize(testConfig(), 120, 40)
	a.setAuthenticated(&gitlabclient.Client{}, &gitlabclient.User{Username: "anthoni"})
	a.sharedState.CachedGroups = []*gitlabclient.Group{{Name: "platform"}}
	a.sharedState.CachedProjects = []*gitlabclient.Project{{Name: "devdesk"}}
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
	if a.sharedState.GitLabClient != nil {
		t.Error("the GitLab client survived the logout, so views can still call the API")
	}
	if a.sharedState.CurrentUser != nil {
		t.Error("CurrentUser survived, so the header still names a signed-out user")
	}
}

// The caches were read through the client that just stopped being valid.
func TestLoggingOutDropsTheCachedGitLabData(t *testing.T) {
	a := signedIn(t)

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if a.sharedState.CachedGroups != nil || a.sharedState.CachedProjects != nil {
		t.Errorf("cached groups=%d projects=%d survived the logout",
			len(a.sharedState.CachedGroups), len(a.sharedState.CachedProjects))
	}
}

// Clearing sharedState does not empty a table the explorer already loaded, so
// the view itself has to go.
func TestLoggingOutDropsTheExplorer(t *testing.T) {
	a := signedIn(t)
	a.createView(command.ViewGitlabExplorer)
	if _, ok := a.views[command.ViewGitlabExplorer]; !ok {
		t.Fatal("the explorer was not built, so the test proves nothing")
	}

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if _, ok := a.views[command.ViewGitlabExplorer]; ok {
		t.Error("the explorer survived the logout with whatever it had loaded")
	}
}

// The auth view is kept: it is on screen and has just written "Logged out
// successfully", which rebuilding would throw away.
func TestLoggingOutKeepsTheAuthView(t *testing.T) {
	a := signedIn(t)
	a.createView(command.ViewGitlabAuth)
	before := a.views[command.ViewGitlabAuth]

	_, _ = a.Update(auth.LogoutCompleteMsg{})

	if a.views[command.ViewGitlabAuth] != before {
		t.Error("the auth view was rebuilt, discarding the logout confirmation")
	}
}

// The message still has to reach the view, or the form never resets.
func TestTheLogoutMessageStillReachesTheView(t *testing.T) {
	a := signedIn(t)
	a.currentView = command.ViewGitlabAuth
	a.createView(command.ViewGitlabAuth)

	_, cmd := a.Update(auth.LogoutCompleteMsg{})

	if cmd == nil {
		t.Fatal("no command returned, so the view was never told")
	}
}
