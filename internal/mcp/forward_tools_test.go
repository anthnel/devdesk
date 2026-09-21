package mcp

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/forward"
)

func TestForwardsListReportsWhatTheSessionHolds(t *testing.T) {
	opened := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	env := testEnv(nil)
	env.Dispatch = &fakeSession{forwards: []forward.Forward{
		{ID: "1", LocalPort: 8080, Target: "db:5432", State: forward.StateLive, Active: 2, Total: 9, Opened: opened},
		{ID: "2", Name: "api.localhost", LocalPort: 80, Target: "127.0.0.1:3000", State: forward.StatePaused},
		{ID: "3", LocalPort: 9000, Target: "gone:1", State: forward.StateUnbound, LastErr: "address already in use"},
	}}

	var out forwardsListOut
	callTool(t, connect(t, env), "forwards_list", nil, &out)

	if len(out.Forwards) != 3 {
		t.Fatalf("got %d forwards, want 3", len(out.Forwards))
	}
	plain, named, unbound := out.Forwards[0], out.Forwards[1], out.Forwards[2]

	if plain.State != "live" || plain.Address != "127.0.0.1:8080" || plain.URL != "" || plain.Total != 9 || plain.Active != 2 {
		t.Errorf("plain forward = %+v", plain)
	}
	if plain.OpenedAt != "2026-09-21T10:00:00Z" {
		t.Errorf("opened_at = %q", plain.OpenedAt)
	}
	if named.State != "paused" || named.Name != "api.localhost" || !strings.HasPrefix(named.URL, "http://api.localhost") {
		t.Errorf("named route = %+v", named)
	}
	if unbound.State != "unbound" || unbound.LastError == "" {
		t.Errorf("an unbound forward must say why: %+v", unbound)
	}
}

// No forward is an empty list, not a missing key: an agent reads a null as
// "unknown", and here it is known.
func TestForwardsListWithNoForwardsIsAnEmptyList(t *testing.T) {
	env := testEnv(nil)
	env.Dispatch = &fakeSession{}

	out := callToolRaw(t, connect(t, env), "forwards_list", nil)
	if !strings.Contains(out, `"forwards":[]`) {
		t.Errorf("answer = %s, want an empty forwards array", out)
	}
}

func TestForwardsListRefusesWithoutASession(t *testing.T) {
	msg := callToolExpectingError(t, connect(t, testEnv(nil)), "forwards_list", nil)
	if !strings.Contains(msg, ErrNoSession.Error()) {
		t.Errorf("error = %s, want ErrNoSession", msg)
	}
}

func TestForwardsListPassesTheSessionsErrorOn(t *testing.T) {
	env := testEnv(nil)
	env.Dispatch = &fakeSession{forwardsErr: errors.New("session gone")}

	if msg := callToolExpectingError(t, connect(t, env), "forwards_list", nil); !strings.Contains(msg, "session gone") {
		t.Errorf("error = %s", msg)
	}
}
