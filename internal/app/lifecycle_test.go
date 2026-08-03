package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/ui/gitlab/auth"
	"github.com/anthnel/devdesk/internal/ui/gitlab/explorer"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// ── Construction ─────────────────────────────────────────────────────────────

// newWithSize is what New() becomes once the terminal size is known. It must
// come back laid out: Init() does not resize, so a router that skipped it would
// render its first frame against a zero-height viewport.
func TestTheRouterComesBackLaidOut(t *testing.T) {
	a := newWithSize(testConfig(), 160, 44)

	if a.width != 160 || a.height != 44 {
		t.Errorf("the router is %dx%d, want 160x44", a.width, a.height)
	}
	if a.viewport.Height <= 0 {
		t.Errorf("viewport height = %d; the router was never laid out", a.viewport.Height)
	}
	if a.commandMode {
		t.Error("the router starts with the command line open")
	}
}

// The dashboard and the status view are built eagerly — the dashboard is the
// usual landing view and the status view holds the monitors that start polling.
func TestTheRouterBuildsItsStartingViews(t *testing.T) {
	a := newWithSize(testConfig(), 160, 44)

	for _, view := range []command.ViewType{command.ViewDashboard, command.ViewStatus} {
		if _, built := a.views[view]; !built {
			t.Errorf("%s was not built at startup", view)
		}
	}
	if _, built := a.views[command.ViewOCIResources]; built {
		t.Error("the OCI view was built at startup; views are meant to be lazy")
	}
}

// The configured landing view is honoured, and a setting that no longer parses
// falls back to the dashboard rather than leaving the router on no view at all.
func TestTheLandingViewComesFromTheConfig(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		want       command.ViewType
	}{
		{"a named view", "security", command.ViewSecurity},
		{"an alias", "ws", command.ViewWorkspaces},
		{"unset", "", command.ViewDashboard},
		{"no longer valid", "does-not-exist", command.ViewDashboard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.App.DefaultView = tt.configured

			if got := defaultView(cfg); got != tt.want {
				t.Errorf("defaultView() = %s, want %s", got, tt.want)
			}

			a := newWithSize(cfg, 120, 40)
			if _, built := a.views[tt.want]; !built {
				t.Errorf("the landing view %s was not built", tt.want)
			}
		})
	}
}

// Init starts the active view. Without an auto-login to attempt that is all it
// has to do, and the view's own Init is what loads its data.
func TestInitStartsTheActiveView(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)

	a.Init()

	if view.inits != 1 {
		t.Errorf("the active view was initialised %d times, want once", view.inits)
	}
}

// A context switch rebuilds every view against the new config, so none of them
// keeps serving the previous context's data.
func TestReinitializingDropsEveryView(t *testing.T) {
	a := router(t, &fakeView{})
	stale := &fakeView{}
	a.views[command.ViewSecurity] = stale
	a.views[command.ViewContainers] = &fakeView{}

	a.reinitializeViews()

	if _, kept := a.views[command.ViewContainers]; kept {
		t.Error("a view from the previous context survived the switch")
	}
	if a.views[a.currentView] == tea.Model(stale) {
		t.Error("the active view was reused rather than rebuilt")
	}
	if _, rebuilt := a.views[a.currentView]; !rebuilt {
		t.Error("the active view was dropped and not rebuilt")
	}
}

// ── Context switching ────────────────────────────────────────────────────────

// The new context has its own credentials, so anything cached from the previous
// one has to go — otherwise the explorer would list the old instance's groups.
func TestSwitchingContextClearsTheGitLabSession(t *testing.T) {
	a := router(t, &fakeView{})
	a.sharedState.IsAuthenticated = true
	a.sharedState.CachedGroups = []*gitlabclient.Group{{ID: 1, Name: "old"}}
	a.sharedState.CurrentUser = &gitlabclient.User{Username: "before"}

	a.Update(ContextSwitchCompleteMsg{ContextName: "work", Config: testConfig()})

	if a.sharedState.IsAuthenticated {
		t.Error("the previous context's session survived the switch")
	}
	if a.sharedState.CachedGroups != nil {
		t.Error("the previous context's groups survived the switch")
	}
	if a.currentContext != "work" {
		t.Errorf("current context = %q, want work", a.currentContext)
	}
}

// With no credentials for the new context the user lands on the auth view,
// rather than on a view that can only show an error.
func TestSwitchingWithoutCredentialsLandsOnTheAuthView(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(ContextSwitchCompleteMsg{ContextName: "work", Config: testConfig()})

	if a.currentView != command.ViewGitlabAuth {
		t.Errorf("current view = %s, want the auth view", a.currentView)
	}
}

// When the switch already authenticated, the session is adopted and the user
// stays where they were.
func TestSwitchingWithCredentialsKeepsTheView(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(ContextSwitchCompleteMsg{
		ContextName:  "work",
		Config:       testConfig(),
		GitLabClient: &gitlabclient.Client{},
		GitLabUser:   &gitlabclient.User{Username: "anthnel"},
	})

	if !a.sharedState.IsAuthenticated {
		t.Error("the session carried by the switch was not adopted")
	}
	if a.currentView == command.ViewGitlabAuth {
		t.Error("the user was sent to the auth view despite being authenticated")
	}
}

// Each context has its own secrets, so a switch has to bring its own store.
// Keeping the previous one would hand the new context the old context's token.
func TestSwitchingContextAdoptsTheNewStore(t *testing.T) {
	a := router(t, &fakeView{})
	before := a.sharedState.Secrets.Storage

	arrived := credentials.SessionOnly("the work context's store")
	a.Update(ContextSwitchCompleteMsg{
		ContextName: "work",
		Config:      testConfig(),
		Secrets:     arrived,
		Notices:     []string{"The GitLab token moved out of the configuration file."},
	})

	if a.sharedState.Secrets.Storage == before {
		t.Error("the previous context's secret store survived the switch")
	}
	if a.sharedState.Secrets.Storage != arrived.Storage {
		t.Error("the store carried by the switch was not adopted")
	}
	if len(a.sharedState.SecretNotices) != 1 {
		t.Errorf("notices = %v, want the one carried by the switch", a.sharedState.SecretNotices)
	}
}

// The Cmd that performs a switch resolves the new context's store itself and
// carries it back in the message. Assigning it from inside the Cmd would be a
// write to the model off the Update goroutine (Rule 110).
func TestTheSwitchCommandCarriesAStore(t *testing.T) {
	a := router(t, &fakeView{})

	done, ok := testutil.MsgOf[ContextSwitchCompleteMsg](a.switchContext("secretsctx"))
	if !ok {
		t.Fatalf("the switch produced %T, want a completed switch", testutil.Msg(a.switchContext("secretsctx")))
	}
	if done.Secrets.Storage == nil {
		t.Error("the switch carried no secret store, so the new context would have nowhere to keep a token")
	}
	if done.Secrets.Detail == "" {
		t.Error("the switch carried no description of where secrets go")
	}
}

// useSecrets is the startup path New() takes. It has to rebuild the auth view,
// which holds the store it was constructed with.
func TestUseSecretsRebuildsTheAuthView(t *testing.T) {
	a := router(t, &fakeView{})
	a.createView(command.ViewGitlabAuth)
	before := a.views[command.ViewGitlabAuth]

	a.useSecrets(credentials.SessionOnly("under test"))

	if a.views[command.ViewGitlabAuth] == before {
		t.Error("the auth view kept the store it was built with")
	}
	if a.sharedState.Secrets.Backend != credentials.BackendMemory {
		t.Errorf("Backend = %q, want the one just installed", a.sharedState.Secrets.Backend)
	}
}

// A switch that fails leaves the router alone — the context it is on still
// works.
func TestAFailedSwitchChangesNothing(t *testing.T) {
	a := router(t, &fakeView{})
	a.currentContext = "default"

	a.Update(ContextSwitchErrorMsg{Error: errors.New("permission denied")})

	if a.currentContext != "default" {
		t.Errorf("current context = %q after a failed switch, want default", a.currentContext)
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("current view = %s after a failed switch", a.currentView)
	}
}

// ── GitLab authentication ────────────────────────────────────────────────────

// fakeGitLab answers the one call an authentication makes: GET /api/v4/user.
func fakeGitLab(t *testing.T, username string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/v4/user") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"username":"` + username + `","name":"Test User"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// With no GitLab URL configured there is nothing to log in to, and the router
// must not issue a command that would fail on every start.
func TestAutoLoginIsSkippedWithoutAURL(t *testing.T) {
	a := router(t, &fakeView{})

	if cmd := a.tryAutoLogin(); cmd != nil {
		t.Error("an auto-login was attempted with no GitLab URL configured")
	}
}

// The saved token is used without asking, which is what makes the explorer
// usable straight from a cold start.
func TestAutoLoginUsesTheSavedToken(t *testing.T) {
	srv := fakeGitLab(t, "anthnel")
	a := router(t, &fakeView{})
	a.config.GitLab.URL = srv.URL
	if err := a.sharedState.Secrets.Storage.Save(srv.URL, "saved-token"); err != nil {
		t.Fatalf("seeding the secret store: %v", err)
	}

	result, ok := testutil.MsgOf[GitLabAutoLoginMsg](a.tryAutoLogin())
	if !ok {
		t.Fatal("the auto-login produced no result")
	}
	if result.Error != nil {
		t.Fatalf("the auto-login failed: %v", result.Error)
	}
	if result.User == nil || result.User.Username != "anthnel" {
		t.Errorf("logged in as %v, want anthnel", result.User)
	}
}

// A stored token the server rejects reports the failure rather than a session,
// so the router can send the user to the auth view.
func TestAutoLoginReportsARejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	a := router(t, &fakeView{})
	a.config.GitLab.URL = srv.URL
	if err := a.sharedState.Secrets.Storage.Save(srv.URL, "revoked"); err != nil {
		t.Fatalf("seeding the secret store: %v", err)
	}

	result, ok := testutil.MsgOf[GitLabAutoLoginMsg](a.tryAutoLogin())
	if !ok {
		t.Fatal("the auto-login produced no result")
	}
	if result.Error == nil {
		t.Error("a rejected token was reported as a successful login")
	}
	if result.Client != nil {
		t.Error("a rejected token produced a client")
	}
}

// A failed auto-login is not an error the user has to dismiss: it leaves the
// session unauthenticated and says nothing.
func TestAFailedAutoLoginIsIgnored(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(GitLabAutoLoginMsg{Error: errors.New("401 unauthorized")})

	if a.sharedState.IsAuthenticated {
		t.Error("a failed auto-login left the session marked authenticated")
	}
}

func TestASuccessfulAutoLoginPopulatesTheSession(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(GitLabAutoLoginMsg{
		Client: &gitlabclient.Client{},
		User:   &gitlabclient.User{Username: "anthnel"},
	})

	if !a.sharedState.IsAuthenticated {
		t.Fatal("a successful auto-login did not mark the session authenticated")
	}
	if a.sharedState.CurrentUser.Username != "anthnel" {
		t.Errorf("the session user is %q, want anthnel", a.sharedState.CurrentUser.Username)
	}
}

// A manual authentication is intercepted so the URL is persisted; without it
// the next start has nothing to auto-login with.
func TestAManualAuthenticationPersistsTheConfig(t *testing.T) {
	a := router(t, &fakeView{})
	saved := testConfig()
	saved.GitLab.URL = "https://gitlab.example.com"

	a.Update(auth.AuthResultMsg{
		Client:       &gitlabclient.Client{},
		User:         &gitlabclient.User{Username: "anthnel"},
		ConfigToSave: saved,
	})

	if !a.sharedState.IsAuthenticated {
		t.Error("the session was not marked authenticated")
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reading back the config: %v", err)
	}
	if reloaded.GitLab.URL != "https://gitlab.example.com" {
		t.Errorf("the persisted URL is %q, want the one just authenticated", reloaded.GitLab.URL)
	}
}

// The view still needs the message: it owns the error banner and the form state.
func TestAFailedAuthenticationReachesTheView(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)

	a.Update(auth.AuthResultMsg{Error: errors.New("invalid token")})

	if _, ok := receivedOf[auth.AuthResultMsg](view); !ok {
		t.Error("the auth view was not told the authentication failed")
	}
	if a.sharedState.IsAuthenticated {
		t.Error("a failed authentication marked the session authenticated")
	}
}

// ── Scans delegated between views ────────────────────────────────────────────

func TestWorkspaceScanDetailsAskTheCache(t *testing.T) {
	a := router(t, &fakeView{})

	_, cmd := a.Update(workspaces.ScanDetailsRequestMsg{RepoPath: "/repos/devdesk"})

	loaded, ok := testutil.MsgOf[WorkspaceScanResultLoadedMsg](cmd)
	if !ok {
		t.Fatalf("the request produced %T, want a cache load", testutil.Msg(cmd))
	}
	if loaded.RepoPath != "/repos/devdesk" {
		t.Errorf("the cache was asked for %q, want /repos/devdesk", loaded.RepoPath)
	}
}

// The security view delegates scanning back to the OCI view, which owns the
// image list and the scan cache. The router switches there and forwards it.
func TestADelegatedScanSwitchesToTheOCIView(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"a batch", ociresources.LaunchBatchScanMsg{}},
		{"a single image", ociresources.LaunchSingleImageScanMsg{ImageName: "api:v1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oci := &fakeView{}
			a := router(t, &fakeView{})
			a.views[command.ViewOCIResources] = oci
			a.currentView = command.ViewSecurity

			a.Update(tt.msg)

			if a.currentView != command.ViewOCIResources {
				t.Errorf("current view = %s, want the OCI view running the scan", a.currentView)
			}
			if len(oci.received) == 0 {
				t.Error("the OCI view was never handed the scan")
			}
		})
	}
}

// The OCI view is built on demand: a scan can be delegated to it before the
// user has ever opened it.
func TestADelegatedScanBuildsTheOCIViewIfNeeded(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(ociresources.LaunchBatchScanMsg{})

	if _, built := a.views[command.ViewOCIResources]; !built {
		t.Error("the OCI view was not built to run the delegated scan")
	}
}

// ── Selection mode ───────────────────────────────────────────────────────────

// The explorer borrows the workspaces view to pick where to clone.
func TestTheExplorerBorrowsWorkspacesForAPullDestination(t *testing.T) {
	a := router(t, &fakeView{})
	a.currentView = command.ViewGitlabExplorer
	a.views[command.ViewGitlabExplorer] = &fakeView{}

	a.Update(explorer.PullSelectionRequestMsg{})

	if a.currentView != command.ViewWorkspaces {
		t.Errorf("current view = %s, want the workspaces browser", a.currentView)
	}
	if a.selectionReturnView != command.ViewGitlabExplorer {
		t.Errorf("return view = %s, want the explorer that asked", a.selectionReturnView)
	}
}

// Picking an image returns it to the security view as a scan target.
func TestPickingAnImageReturnsItToTheOrigin(t *testing.T) {
	origin := &fakeView{}
	oci := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewSecurity] = origin
	a.views[command.ViewOCIResources] = oci
	a.selectionReturnView = command.ViewSecurity
	a.currentView = command.ViewOCIResources

	a.Update(ociresources.ImageSelectedMsg{ImageName: "api:v1"})

	if a.currentView != command.ViewSecurity {
		t.Errorf("current view = %s, want the security view back", a.currentView)
	}
	got, ok := receivedOf[security.SelectionResultMsg](origin)
	if !ok {
		t.Fatal("the security view never received the chosen image")
	}
	if got.Path != "api:v1" {
		t.Errorf("received %q, want api:v1", got.Path)
	}
	if _, reset := receivedOf[ociresources.ResetSelectionMsg](oci); !reset {
		t.Error("the OCI view was left in selection mode")
	}
}

// Cancelling from the OCI view goes through the same path as cancelling from
// workspaces.
func TestCancellingFromTheImageBrowserReturnsToTheOrigin(t *testing.T) {
	origin := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewSecurity] = origin
	a.selectionReturnView = command.ViewSecurity
	a.currentView = command.ViewOCIResources

	a.Update(ociresources.SelectionCancelledMsg{})

	if a.currentView != command.ViewSecurity {
		t.Errorf("current view = %s, want the security view back", a.currentView)
	}
	if _, ok := receivedOf[security.SelectionCancelledMsg](origin); !ok {
		t.Error("the security view was not told the selection was cancelled")
	}
}

// Esc in the security results returns to whichever view opened them.
func TestLeavingTheSecurityResultsReturnsToTheOrigin(t *testing.T) {
	a := router(t, &fakeView{})
	a.currentView = command.ViewSecurity
	a.views[command.ViewSecurity] = &fakeView{}

	a.Update(security.BackToOriginMsg{Origin: command.ViewWorkspaces})

	if a.currentView != command.ViewWorkspaces {
		t.Errorf("current view = %s, want the workspaces view that opened the scan", a.currentView)
	}
}
