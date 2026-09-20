package app

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forward"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// routerWithStore is a router whose forwards file lives in the test's own
// directory. newWithSize sets no store, so nothing else in this package can
// reach the developer's ~/.devdesk.
func routerWithStore(t *testing.T) (*App, *forward.Store) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)
	store := forward.NewStore(filepath.Join(t.TempDir(), forward.FileName))
	a.forwardStore = store
	return a, store
}

// footersPosted feeds a message to the router and returns what it asked the
// footer to say, running the batch it answers with.
func footersPosted(t *testing.T, a *App, msg tea.Msg) []components.PostFooterMsg {
	t.Helper()
	_, cmd := a.Update(msg)
	var out []components.PostFooterMsg
	for _, m := range testutil.Msgs(cmd) {
		if p, ok := m.(components.PostFooterMsg); ok {
			out = append(out, p)
		}
	}
	return out
}

func TestAnOpenedForwardIsSaved(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	port := freePort(t)

	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{LocalPort: port, Target: target}))).(forward.OpenedMsg)
	footersPosted(t, a, opened)

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := forward.Entry{LocalPort: port, Target: target}
	if len(got) != 1 || got[0] != want {
		t.Errorf("the file holds %+v, want [%+v]", got, want)
	}
}

func TestARefusedOpenSavesNothing(t *testing.T) {
	a, store := routerWithStore(t)

	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{LocalPort: 80, Target: "127.0.0.1:1"}))).(forward.OpenedMsg)
	footersPosted(t, a, opened)

	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Errorf("a refused open wrote %s (stat err: %v)", store.Path(), err)
	}
}

func TestAClosedForwardIsRemovedFromTheFile(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Save(a.sharedState.Forwards.Entries()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	footersPosted(t, a, forward.CloseMsg{ID: f.ID})

	if got, _ := store.Load(); len(got) != 0 {
		t.Errorf("the file still holds %+v after a delete", got)
	}
}

func TestAPauseIsSavedAsPaused(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	toggled := runCmd(t, mustCmd(a.Update(forward.ToggleMsg{ID: f.ID}))).(forward.ToggledMsg)
	if toggled.Err != nil {
		t.Fatalf("Toggle: %v", toggled.Err)
	}
	footersPosted(t, a, toggled)

	got, _ := store.Load()
	if len(got) != 1 || !got[0].Paused {
		t.Errorf("the file holds %+v, want the one entry paused", got)
	}
}

func TestAResumeThatCannotBindWarnsAndKeepsTheEntry(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.sharedState.Forwards.Toggle(f.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	squatter := squat(t, f.Addr())
	defer func() { _ = squatter.Close() }()

	toggled := runCmd(t, mustCmd(a.Update(forward.ToggleMsg{ID: f.ID}))).(forward.ToggledMsg)
	posted := footersPosted(t, a, toggled)

	if len(posted) != 1 || posted[0].Level != components.LevelWarning ||
		!strings.Contains(posted[0].Text, "already taken") {
		t.Errorf("footer = %+v, want one warning naming the taken port", posted)
	}
	if got, _ := store.Load(); len(got) != 1 {
		t.Errorf("the file holds %+v, want the entry kept", got)
	}
}

func TestStartupReopensWhatWasSavedAndSaysSo(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	port := freePort(t)
	if err := store.Save([]forward.Entry{{LocalPort: port, Target: target}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	if restored.Err != nil || restored.Summary.Live != 1 {
		t.Fatalf("restore = %+v, want one live", restored)
	}
	posted := footersPosted(t, a, restored)

	if len(posted) != 1 || posted[0].Level != components.LevelInfo ||
		!strings.Contains(posted[0].Text, "Restored 1 forward") {
		t.Errorf("footer = %+v, want an info saying one forward was restored", posted)
	}
	if got := a.sharedState.Forwards.List(); len(got) != 1 || got[0].LocalPort != port {
		t.Errorf("the registry holds %+v, want the saved forward", got)
	}
}

func TestStartupWarnsWhenSomethingCouldNotBeBound(t *testing.T) {
	a, store := routerWithStore(t)
	silent := "127.0.0.1:" + itoa(freePort(t))
	if err := store.Save([]forward.Entry{{LocalPort: freePort(t), Target: silent}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	posted := footersPosted(t, a, restored)

	if len(posted) != 1 || posted[0].Level != components.LevelWarning ||
		!strings.Contains(posted[0].Text, "1 of 1 forwards could not be bound") {
		t.Errorf("footer = %+v, want a warning counting the unbound one", posted)
	}
	// Restoring must not rewrite the file: nothing changed.
	if got, _ := store.Load(); len(got) != 1 {
		t.Errorf("the file holds %+v, want the entry kept", got)
	}
}

func TestAnUnreadableFileIsReportedAndNotOverwrittenByStartup(t *testing.T) {
	a, store := routerWithStore(t)
	const garbage = "forwards: [not: yaml\n"
	if err := os.WriteFile(store.Path(), []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	if restored.Err == nil {
		t.Fatal("an unreadable file was reported as fine")
	}
	posted := footersPosted(t, a, restored)
	if len(posted) != 1 || posted[0].Level != components.LevelError {
		t.Errorf("footer = %+v, want one error", posted)
	}
	if got, _ := os.ReadFile(store.Path()); string(got) != garbage {
		t.Errorf("startup touched the unreadable file: %q", got)
	}
}

func TestARouterBuiltByATestNeverTouchesTheHomeDirectory(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	if a.forwardStore != nil {
		t.Fatal("newWithSize set a forwards store; only New may, or a test would write ~/.devdesk")
	}
	if a.restoreForwardsCmd() != nil || a.saveForwardsCmd() != nil {
		t.Error("without a store the router still produced I/O")
	}
}

func TestAFailedSaveReachesTheFooter(t *testing.T) {
	a, _ := routerWithStore(t)
	posted := footersPosted(t, a, forward.SavedMsg{Err: os.ErrPermission})
	if len(posted) != 1 || posted[0].Level != components.LevelError {
		t.Errorf("footer = %+v, want one error", posted)
	}
}

// ── Named routes (§3.74) ─────────────────────────────────────────────────────

// httpBackend answers every request with its label, so a route can be told
// from another by what comes back.
func httpBackend(t *testing.T, label string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, label)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func routerServingOn(t *testing.T, port int) (*App, *forward.Store) {
	t.Helper()
	a, store := routerWithStore(t)
	a.config.Network.ProxyPort = port
	if err := a.sharedState.Forwards.SetProxyPort(port); err != nil {
		t.Fatalf("SetProxyPort: %v", err)
	}
	return a, store
}

func TestARouteRequestOpensARouteAndSavesItByName(t *testing.T) {
	a, store := routerServingOn(t, freePort(t))
	target := httpBackend(t, "hello")

	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{Name: "api.localhost", Target: target}))).(forward.OpenedMsg)
	if opened.Err != nil {
		t.Fatalf("the route was refused: %v", opened.Err)
	}
	posted := footersPosted(t, a, opened)

	if len(posted) != 1 || !strings.Contains(posted[0].Text, "http://api.localhost:") {
		t.Errorf("footer = %+v, want it to say the URL the route is served on", posted)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []forward.Entry{{Name: "api.localhost", Target: target}}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("the file holds %+v, want %+v", got, want)
	}
}

func TestEveryRouteRefusalIsNamedRatherThanRelayed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"bad name", fmt.Errorf("%w: x", forward.ErrBadRouteName), ".localhost"},
		{"name taken", fmt.Errorf("%w: x", forward.ErrRouteNameTaken), "already exists"},
		{"proxy port in use", fmt.Errorf("%w: 8080: bind", forward.ErrProxyPortInUse), "network.proxy_port"},
		{"proxy port requested", fmt.Errorf("%w: 8080", forward.ErrProxyPortTaken), "network.proxy_port"},
		{"no proxy port", forward.ErrNoProxyPort, "network.proxy_port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := forwardRefusal(tc.err)
			if !strings.Contains(got, tc.want) {
				t.Errorf("forwardRefusal(%v) = %q, want it to mention %q", tc.err, got, tc.want)
			}
			if strings.Contains(got, "bind") || strings.Contains(got, "listen") {
				t.Errorf("forwardRefusal(%v) relayed the OS wording: %q", tc.err, got)
			}
		})
	}
}

// The proxy's port is not one the user typed, so the sentence a TCP forward
// gets for a taken port ("pick another") would send them to a field that is
// not there.
func TestATakenProxyPortDoesNotTellTheUserToPickAnotherForwardPort(t *testing.T) {
	got := forwardRefusal(fmt.Errorf("%w: 8080", forward.ErrProxyPortInUse))
	if strings.Contains(got, "pick another") {
		t.Errorf("the proxy-port refusal is %q, which points at a field a route does not have", got)
	}
}

func TestARouteOpenedBeforeAnyPortIsConfiguredIsRefusedByName(t *testing.T) {
	a, _ := routerWithStore(t)
	a.config.Network.ProxyPort = 0
	_ = a.sharedState.Forwards.SetProxyPort(0)

	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{Name: "api.localhost", Target: httpBackend(t, "x")}))).(forward.OpenedMsg)
	if !errors.Is(opened.Err, forward.ErrNoProxyPort) {
		t.Fatalf("Err = %v, want ErrNoProxyPort", opened.Err)
	}
}

func TestARestoredRouteIsServedAfterStartup(t *testing.T) {
	port := freePort(t)
	a, store := routerServingOn(t, port)
	target := httpBackend(t, "restored")
	if err := store.Save([]forward.Entry{{Name: "api.localhost", Target: target}}); err != nil {
		t.Fatal(err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	if restored.Err != nil || restored.Summary.Live != 1 {
		t.Fatalf("restore = %+v, want one live", restored)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+itoa(port), nil)
	req.Host = "api.localhost"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("asking the proxy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body, _ := io.ReadAll(resp.Body); string(body) != "restored" {
		t.Errorf("the restored route answered %q", body)
	}
}

// ── network.proxy_port follows the configuration ─────────────────────────────

func TestASavedConfigurationMovesTheProxyToTheNewPort(t *testing.T) {
	oldPort, newPort := freePort(t), freePort(t)
	a, _ := routerServingOn(t, oldPort)
	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{Name: "api.localhost", Target: httpBackend(t, "x")}))).(forward.OpenedMsg)
	if opened.Err != nil {
		t.Fatalf("OpenRoute: %v", opened.Err)
	}

	cfg := *a.config
	cfg.Network.ProxyPort = newPort
	_, cmd := a.Update(configuration.ConfigSavedMsg{Config: &cfg})

	var set *forward.ProxyPortSetMsg
	for _, msg := range testutil.Msgs(cmd) {
		if m, ok := msg.(forward.ProxyPortSetMsg); ok {
			set = &m
		}
	}
	if set == nil {
		t.Fatal("saving the configuration did not move the proxy")
	}
	if set.Err != nil || set.Port != newPort {
		t.Errorf("ProxyPortSetMsg = %+v, want port %d and no error", *set, newPort)
	}
	if got := a.sharedState.Forwards.ProxyPort(); got != newPort {
		t.Errorf("the registry serves on %d, want %d", got, newPort)
	}
}

func TestAProxyThatCannotFollowTheConfigurationWarnsAndKeepsTheRoutes(t *testing.T) {
	a, store := routerServingOn(t, freePort(t))
	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{Name: "api.localhost", Target: httpBackend(t, "x")}))).(forward.OpenedMsg)
	footersPosted(t, a, opened)

	held := squat(t, "127.0.0.1:0")
	defer func() { _ = held.Close() }()
	takenPort := held.Addr().(*net.TCPAddr).Port

	cfg := *a.config
	cfg.Network.ProxyPort = takenPort
	_, cmd := a.Update(configuration.ConfigSavedMsg{Config: &cfg})
	var set forward.ProxyPortSetMsg
	for _, msg := range testutil.Msgs(cmd) {
		if m, ok := msg.(forward.ProxyPortSetMsg); ok {
			set = m
		}
	}
	posted := footersPosted(t, a, set)

	if len(posted) != 1 || posted[0].Level != components.LevelWarning ||
		!strings.Contains(posted[0].Text, "network.proxy_port") {
		t.Errorf("footer = %+v, want a warning naming network.proxy_port", posted)
	}
	if list := a.sharedState.Forwards.List(); len(list) != 1 || list[0].State != forward.StateUnbound {
		t.Errorf("registry = %+v, want the route kept and unbound", list)
	}
	if got, _ := store.Load(); len(got) != 1 {
		t.Errorf("the file holds %+v, want the route kept", got)
	}
}

// Two moves can be in flight, and each applies the port as it is when it runs:
// whichever finishes last must leave the proxy on the latest one.
func TestTheLatestConfiguredPortWinsWhateverOrderTheMovesRunIn(t *testing.T) {
	a, _ := routerServingOn(t, freePort(t))
	first, second := freePort(t), freePort(t)

	a.config.Network.ProxyPort = first
	older := a.syncProxyPortCmd()
	a.config.Network.ProxyPort = second
	newer := a.syncProxyPortCmd()

	// The newer one runs first, the older one last: a stale value read at
	// creation time would leave the proxy on the first port.
	newer()
	older()

	if got := a.sharedState.Forwards.ProxyPort(); got != second {
		t.Errorf("the proxy is on %d, want the latest configured port %d", got, second)
	}
}
