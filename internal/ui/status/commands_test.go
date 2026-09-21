package status

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
)

// status.timeout is in seconds. A monitor with no timeout of its own falls back
// on it, so a checker built with the bare integer would wait five nanoseconds
// and report a healthy endpoint DOWN — which is what a hand-written monitor
// without `timeout:` used to show on every refresh.
func TestAMonitorWithNoTimeoutOfItsOwnUsesTheConfiguredSeconds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Status.Timeout = 5
	cfg.Status.Components = []config.ComponentConfig{{Name: "web", Type: "http", Target: srv.URL}}

	msg, ok := checkComponents(cfg)().(CheckCompleteMsg)
	if !ok {
		t.Fatalf("checkComponents returned %T, want CheckCompleteMsg", msg)
	}
	if len(msg.Components) != 1 {
		t.Fatalf("got %d results, want 1", len(msg.Components))
	}
	if got := msg.Components[0]; got.Status != status.StatusOK {
		t.Errorf("a reachable endpoint reads %s (%q), want OK", got.Status, got.Error)
	}
}
