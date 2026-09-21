package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

func monitorsEnv(components ...config.ComponentConfig) *Env {
	env := testEnv(nil)
	env.Config.Status.Timeout = 2
	env.Config.Status.Components = components
	return env
}

func TestMonitorsStatusReportsEachMonitorsState(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer up.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	goneURL := gone.URL
	gone.Close()

	env := monitorsEnv(
		config.ComponentConfig{Name: "up", Type: "http", Target: up.URL},
		config.ComponentConfig{Name: "broken", Type: "http", Target: broken.URL},
		config.ComponentConfig{Name: "gone", Type: "http", Target: goneURL},
	)

	var out monitorsStatusOut
	callTool(t, connect(t, env), "monitors_status", nil, &out)

	if out.Context != "work" {
		t.Errorf("context = %q, want the serving one", out.Context)
	}
	if len(out.Monitors) != 3 {
		t.Fatalf("got %d monitors, want 3: %+v", len(out.Monitors), out.Monitors)
	}
	want := []struct{ name, status string }{{"up", "OK"}, {"broken", "ERROR"}, {"gone", "DOWN"}}
	for i, w := range want {
		m := out.Monitors[i]
		if m.Name != w.name || m.Status != w.status {
			t.Errorf("monitor %d = %s/%s, want %s/%s", i, m.Name, m.Status, w.name, w.status)
		}
		if m.CheckedAt == "" {
			t.Errorf("%s has no checked_at", m.Name)
		}
	}
	if out.Monitors[1].Error != "HTTP 500" || out.Monitors[2].Error == "" {
		t.Errorf("a monitor that is not OK must say why: %+v", out.Monitors[1:])
	}
	if out.Monitors[0].CertState != "" || out.Monitors[0].DaysLeft != nil {
		t.Errorf("an http monitor carries certificate fields: %+v", out.Monitors[0])
	}
}

// The SSL probe verifies the chain, so a test server's self-signed certificate
// is a certificate that could not be read: the state is "error" — an absence of
// a measurement, not a verdict on the certificate — and no expiry is invented.
func TestMonitorsStatusReportsAnUnreadableCertificateAsAnAbsence(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	env := monitorsEnv(config.ComponentConfig{Name: "tls", Type: "ssl", Target: strings.TrimPrefix(srv.URL, "https://")})

	var out monitorsStatusOut
	callTool(t, connect(t, env), "monitors_status", map[string]any{"type": "ssl"}, &out)

	if len(out.Monitors) != 1 {
		t.Fatalf("got %d monitors, want 1", len(out.Monitors))
	}
	m := out.Monitors[0]
	if m.CertState != "error" || m.DaysLeft != nil || m.Expires != "" || m.Error == "" {
		t.Errorf("an unreadable certificate must be an error with a reason and no dates: %+v", m)
	}
}

func TestMonitorsStatusProbesOnlyTheTypeAsked(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	env := monitorsEnv(
		config.ComponentConfig{Name: "web", Type: "http", Target: srv.URL},
		// A legacy entry with only a URL is an http monitor, as CheckOne reads it.
		config.ComponentConfig{Name: "legacy", URL: srv.URL},
	)

	var out monitorsStatusOut
	callTool(t, connect(t, env), "monitors_status", map[string]any{"type": "ssl"}, &out)
	if len(out.Monitors) != 0 || hits != 0 {
		t.Errorf("type ssl probed %d monitors and made %d requests, want none", len(out.Monitors), hits)
	}

	callTool(t, connect(t, env), "monitors_status", map[string]any{"type": "HTTP"}, &out)
	if len(out.Monitors) != 2 {
		t.Errorf("type http returned %d monitors, want the typed one and the legacy one", len(out.Monitors))
	}
}

func TestMonitorsStatusRefusesAnUnknownType(t *testing.T) {
	msg := callToolExpectingError(t, connect(t, monitorsEnv()), "monitors_status", map[string]any{"type": "smtp"})
	if !strings.Contains(msg, "smtp") || !strings.Contains(msg, "http, https, icmp, dns, ssl") {
		t.Errorf("error = %s, want the type and the valid ones named", msg)
	}
}

func TestMonitorsStatusWithNoMonitorsIsAnEmptyList(t *testing.T) {
	raw := callToolRaw(t, connect(t, monitorsEnv()), "monitors_status", nil)
	if !strings.Contains(raw, `"monitors":[]`) {
		t.Errorf("answer = %s, want an empty monitors array", raw)
	}
}

// The SSL probe takes no context, so the deadline is what bounds the call when
// a monitor is slow. A partial list read as complete would be worse than none.
func TestMonitorsStatusGivesUpAtTheDeadline(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)

	old := monitorsDeadline
	monitorsDeadline = 100 * time.Millisecond
	defer func() { monitorsDeadline = old }()

	env := monitorsEnv(config.ComponentConfig{Name: "slow", Type: "http", Target: slow.URL, Timeout: 5})

	start := time.Now()
	msg := callToolExpectingError(t, connect(t, env), "monitors_status", nil)
	if time.Since(start) > 2*time.Second {
		t.Errorf("the call took %s, the deadline did not bound it", time.Since(start))
	}
	if !strings.Contains(msg, "did not all answer") {
		t.Errorf("error = %s", msg)
	}
}
