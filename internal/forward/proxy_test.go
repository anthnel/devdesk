package forward

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// backend starts an HTTP server that answers with its own label and the Host it
// was asked for, and returns its host:port.
func backend(t *testing.T, label string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s host=%s path=%s", label, r.Host, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// routedRegistry returns a registry serving on a free port.
func routedRegistry(t *testing.T) (*Registry, int) {
	t.Helper()
	r := New()
	t.Cleanup(r.CloseAll)
	port := freePort(t)
	if err := r.SetProxyPort(port); err != nil {
		t.Fatalf("SetProxyPort: %v", err)
	}
	return r, port
}

// get asks the proxy for a path under a Host, and returns the status and body.
func get(t *testing.T, port int, host, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s (Host %s): %v", path, host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestTwoRoutesShareOnePortAndAreToldApartByHost(t *testing.T) {
	r, port := routedRegistry(t)
	api, app := backend(t, "api-server"), backend(t, "app-server")
	if _, err := r.OpenRoute("api.localhost", api); err != nil {
		t.Fatalf("OpenRoute api: %v", err)
	}
	if _, err := r.OpenRoute("app.localhost", app); err != nil {
		t.Fatalf("OpenRoute app: %v", err)
	}

	for host, want := range map[string]string{
		"api.localhost": "api-server",
		"app.localhost": "app-server",
	} {
		_, body := get(t, port, host+":"+strconv.Itoa(port), "/x")
		if !strings.HasPrefix(body, want) {
			t.Errorf("Host %s answered %q, want the %s backend", host, body, want)
		}
	}
}

func TestTheInboundHostReachesTheBackend(t *testing.T) {
	r, port := routedRegistry(t)
	if _, err := r.OpenRoute("api.localhost", backend(t, "b")); err != nil {
		t.Fatal(err)
	}
	host := "api.localhost:" + strconv.Itoa(port)
	if _, body := get(t, port, host, "/"); !strings.Contains(body, "host="+host) {
		t.Errorf("the backend saw %q, want the Host the client sent (%s)", body, host)
	}
}

func TestTheHostIsMatchedWithoutItsPortOrCase(t *testing.T) {
	r, port := routedRegistry(t)
	if _, err := r.OpenRoute("api.localhost", backend(t, "b")); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"API.Localhost", "api.localhost.", "api.localhost:9"} {
		if code, _ := get(t, port, host, "/"); code != http.StatusOK {
			t.Errorf("Host %q got %d, want 200", host, code)
		}
	}
}

func TestAnUnknownHostGetsA404ThatListsTheRoutes(t *testing.T) {
	r, port := routedRegistry(t)
	target := backend(t, "b")
	if _, err := r.OpenRoute("api.localhost", target); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, port, "nope.localhost", "/")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	for _, want := range []string{"nope.localhost", "http://api.localhost:" + strconv.Itoa(port), target} {
		if !strings.Contains(body, want) {
			t.Errorf("the 404 does not mention %q:\n%s", want, body)
		}
	}
}

func TestABareLocalhostGetsTheSame404(t *testing.T) {
	r, port := routedRegistry(t)
	if _, err := r.OpenRoute("api.localhost", backend(t, "b")); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, port, "localhost:"+strconv.Itoa(port), "/"); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestABackendThatStopsAnswersWithA502AndTheRouteSaysWhy(t *testing.T) {
	r, port := routedRegistry(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	target := strings.TrimPrefix(srv.URL, "http://")
	f, err := r.OpenRoute("api.localhost", target)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()

	code, body := get(t, port, "api.localhost", "/")
	if code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", code)
	}
	if !strings.Contains(body, target) {
		t.Errorf("the 502 does not name the target:\n%s", body)
	}
	if got := stateOf(t, r, f.ID).LastErr; got == "" {
		t.Error("the route carries no error after a 502")
	}
}

func TestARouteCountsTheRequestsItCarried(t *testing.T) {
	r, port := routedRegistry(t)
	f, err := r.OpenRoute("api.localhost", backend(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		get(t, port, "api.localhost", "/")
	}
	got := stateOf(t, r, f.ID)
	if got.Total != 3 || got.Active != 0 {
		t.Errorf("Total/Active = %d/%d, want 3/0", got.Total, got.Active)
	}
}

// A WebSocket is an HTTP upgrade followed by raw bytes. This does the handshake
// by hand and then echoes, which is what hot reload rides on.
func TestAnUpgradedConnectionCarriesBytesBothWays(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				_ = req.Body.Close()
				_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
				_, _ = io.Copy(conn, br)
			}()
		}
	}()

	r, port := routedRegistry(t)
	if _, err := r.OpenRoute("hot.localhost", ln.Addr().String()); err != nil {
		t.Fatal(err)
	}

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: hot.localhost\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("reading the handshake: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	_, _ = io.WriteString(conn, "ping")
	got := make([]byte, 4)
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "ping" {
		t.Errorf("echo = %q, %v; want ping", got, err)
	}
}

func TestRouteNamesAreValidated(t *testing.T) {
	r, _ := routedRegistry(t)
	target := backend(t, "b")
	for _, name := range []string{
		"", "api", "localhost", ".localhost", "api.local", "api.example.com",
		"-api.localhost", "api-.localhost", "a_b.localhost", "a b.localhost",
		"a..localhost", strings.Repeat("a", 64) + ".localhost",
	} {
		if _, err := r.OpenRoute(name, target); !errors.Is(err, ErrBadRouteName) {
			t.Errorf("OpenRoute(%q) = %v, want ErrBadRouteName", name, err)
		}
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("a refused name left %d routes behind", len(got))
	}
}

func TestANameIsStoredInLowerCaseAndIsUniqueWhateverItsState(t *testing.T) {
	r, _ := routedRegistry(t)
	target := backend(t, "b")
	f, err := r.OpenRoute("API.localhost", target)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "api.localhost" {
		t.Errorf("Name = %q, want it lower-cased", f.Name)
	}
	if _, err := r.OpenRoute("api.LOCALHOST", target); !errors.Is(err, ErrRouteNameTaken) {
		t.Errorf("a duplicate = %v, want ErrRouteNameTaken", err)
	}
	// A paused route still owns its name, or resuming it would find it gone.
	if _, err := r.Toggle(f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenRoute("api.localhost", target); !errors.Is(err, ErrRouteNameTaken) {
		t.Errorf("a duplicate of a paused route = %v, want ErrRouteNameTaken", err)
	}
}

func TestARouteToASilentTargetIsRefused(t *testing.T) {
	r, _ := routedRegistry(t)
	silent := "127.0.0.1:" + strconv.Itoa(freePort(t))
	if _, err := r.OpenRoute("api.localhost", silent); !errors.Is(err, ErrTargetUnreachable) {
		t.Errorf("OpenRoute = %v, want ErrTargetUnreachable", err)
	}
	if len(r.List()) != 0 {
		t.Error("a refused route left a row behind")
	}
}

func TestARouteBeforeTheProxyPortIsKnownIsRefused(t *testing.T) {
	r := New()
	t.Cleanup(r.CloseAll)
	if _, err := r.OpenRoute("api.localhost", backend(t, "b")); !errors.Is(err, ErrNoProxyPort) {
		t.Errorf("OpenRoute = %v, want ErrNoProxyPort", err)
	}
}

func TestTheProxyBindsWithTheFirstRouteAndReleasesItWithTheLast(t *testing.T) {
	r, port := routedRegistry(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// Nothing serves before the first route: the port is free.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the proxy holds %s with no route: %v", addr, err)
	}
	_ = ln.Close()

	f, err := r.OpenRoute("api.localhost", backend(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := net.Listen("tcp", addr); err == nil {
		t.Fatal("the proxy is not listening with a live route")
	}

	if err := r.Close(f.ID); err != nil {
		t.Fatal(err)
	}
	ln, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("closing the last route left the proxy on %s: %v", addr, err)
	}
	_ = ln.Close()
}

func TestPausingTheLastRouteReleasesTheProxyAndResumingBindsItAgain(t *testing.T) {
	r, port := routedRegistry(t)
	f, err := r.OpenRoute("api.localhost", backend(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.Toggle(f.ID); err != nil || got.State != StatePaused {
		t.Fatalf("pause = %+v, %v", got, err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("a paused route keeps the proxy bound: %v", err)
	}
	_ = ln.Close()

	if got, err := r.Toggle(f.ID); err != nil || got.State != StateLive {
		t.Fatalf("resume = %+v, %v", got, err)
	}
	if code, _ := get(t, port, "api.localhost", "/"); code != http.StatusOK {
		t.Errorf("the resumed route answers %d, want 200", code)
	}
}

func TestAPausedRouteIsNotServed(t *testing.T) {
	r, port := routedRegistry(t)
	a, err := r.OpenRoute("a.localhost", backend(t, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenRoute("b.localhost", backend(t, "b")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Toggle(a.ID); err != nil {
		t.Fatal(err)
	}

	if code, _ := get(t, port, "a.localhost", "/"); code != http.StatusNotFound {
		t.Errorf("a paused route answered %d, want 404", code)
	}
	if code, _ := get(t, port, "b.localhost", "/"); code != http.StatusOK {
		t.Errorf("the other route answered %d, want 200", code)
	}
}

func TestARouteShowsTheProxyPortInPlaceOfItsOwn(t *testing.T) {
	r, port := routedRegistry(t)
	f, err := r.OpenRoute("api.localhost", backend(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	if f.LocalPort != port || stateOf(t, r, f.ID).LocalPort != port {
		t.Errorf("LocalPort = %d, want the proxy's %d", f.LocalPort, port)
	}
}

func TestATCPForwardCannotTakeTheProxyPort(t *testing.T) {
	r, port := routedRegistry(t)
	if _, err := r.Open(port, echoServer(t)); !errors.Is(err, ErrProxyPortTaken) {
		t.Errorf("Open on the proxy port = %v, want ErrProxyPortTaken", err)
	}
}

func TestAProxyPortThatIsTakenIsRefusedAsTheProxysAndNotAsAForwards(t *testing.T) {
	r := New()
	t.Cleanup(r.CloseAll)
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	if err := r.SetProxyPort(held.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatal(err)
	}

	_, err = r.OpenRoute("api.localhost", backend(t, "b"))
	if !errors.Is(err, ErrProxyPortInUse) {
		t.Errorf("OpenRoute = %v, want ErrProxyPortInUse", err)
	}
	if errors.Is(err, ErrPortInUse) {
		t.Error("the proxy's port was reported as a forward's, whose sentence says to pick another")
	}
}

func TestChangingTheProxyPortMovesTheRoutes(t *testing.T) {
	r, oldPort := routedRegistry(t)
	f, err := r.OpenRoute("api.localhost", backend(t, "b"))
	if err != nil {
		t.Fatal(err)
	}

	newPort := freePort(t)
	if err := r.SetProxyPort(newPort); err != nil {
		t.Fatalf("SetProxyPort: %v", err)
	}
	if code, _ := get(t, newPort, "api.localhost", "/"); code != http.StatusOK {
		t.Errorf("the route answers %d on the new port, want 200", code)
	}
	if ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(oldPort)); err != nil {
		t.Errorf("the old port %d is still held: %v", oldPort, err)
	} else {
		_ = ln.Close()
	}
	if got := stateOf(t, r, f.ID); got.LocalPort != newPort || got.State != StateLive {
		t.Errorf("route = %+v, want live on %d", got, newPort)
	}
}

func TestMovingTheProxyToATakenPortLeavesTheRoutesUnboundNotDeleted(t *testing.T) {
	r, _ := routedRegistry(t)
	f, err := r.OpenRoute("api.localhost", backend(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	err = r.SetProxyPort(held.Addr().(*net.TCPAddr).Port)
	if !errors.Is(err, ErrProxyPortInUse) {
		t.Fatalf("SetProxyPort = %v, want ErrProxyPortInUse", err)
	}
	got := stateOf(t, r, f.ID)
	if got.State != StateUnbound || got.LastErr == "" {
		t.Errorf("route = %+v, want unbound with a reason", got)
	}
	if len(r.Entries()) != 1 {
		t.Error("the route was dropped from what is saved")
	}
}

func TestRoutesAreSavedByNameAndRestoredWithoutAPort(t *testing.T) {
	r, _ := routedRegistry(t)
	target := backend(t, "b")
	if _, err := r.OpenRoute("api.localhost", target); err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "api.localhost", Target: target}}
	if got := r.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries = %+v, want %+v", got, want)
	}

	again, port := routedRegistry(t)
	if sum := again.Restore(want); sum != (Restored{Live: 1}) {
		t.Fatalf("Restore = %+v, want one live", sum)
	}
	if code, body := get(t, port, "api.localhost", "/"); code != http.StatusOK || !strings.HasPrefix(body, "b ") {
		t.Errorf("the restored route answers %d %q", code, body)
	}
}

func TestARestoredRouteWhoseTargetIsNotUpIsUnboundAndNothingIsBound(t *testing.T) {
	r, port := routedRegistry(t)
	silent := "127.0.0.1:" + strconv.Itoa(freePort(t))

	sum := r.Restore([]Entry{{Name: "api.localhost", Target: silent}})
	if sum != (Restored{Unbound: 1}) {
		t.Fatalf("Restore = %+v, want one unbound", sum)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("the proxy was bound for a route that is not served: %v", err)
	}
	_ = ln.Close()
	if len(r.Entries()) != 1 {
		t.Error("the route was dropped")
	}
}

func TestRestoreMixesTCPForwardsAndRoutes(t *testing.T) {
	r, port := routedRegistry(t)
	echo, web := echoServer(t), backend(t, "w")
	tcpPort := freePort(t)

	sum := r.Restore([]Entry{
		{LocalPort: tcpPort, Target: echo},
		{Name: "web.localhost", Target: web},
		{Name: "off.localhost", Target: web, Paused: true},
	})
	if sum != (Restored{Live: 2, Paused: 1}) {
		t.Fatalf("Restore = %+v, want 2 live and 1 paused", sum)
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(tcpPort), time.Second); err != nil {
		t.Errorf("the TCP forward does not listen: %v", err)
	} else {
		_ = conn.Close()
	}
	if code, _ := get(t, port, "web.localhost", "/"); code != http.StatusOK {
		t.Errorf("the route answers %d, want 200", code)
	}
	if code, _ := get(t, port, "off.localhost", "/"); code != http.StatusNotFound {
		t.Errorf("the paused route answers %d, want 404", code)
	}
}

// 0.0.0.0 and 127.0.0.1 collide on a port in both directions, so a second bind
// cannot show which one the proxy took. The listener's own address can.
func TestTheProxyListensOnLoopbackOnly(t *testing.T) {
	r, _ := routedRegistry(t)
	if _, err := r.OpenRoute("api.localhost", backend(t, "b")); err != nil {
		t.Fatal(err)
	}
	r.proxyMu.Lock()
	addr := r.proxyLn.Addr().(*net.TCPAddr)
	r.proxyMu.Unlock()
	if got := addr.IP.String(); got != "127.0.0.1" {
		t.Errorf("the proxy listens on %s, want 127.0.0.1", got)
	}
}
