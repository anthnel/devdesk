package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anthnel/devdesk/internal/credentials"
)

// persisting is a store that keeps secrets in this process while claiming a
// backend that outlives it. The claim is what the refusal path is tested
// against below; reaching for the developer's own keyring would make these
// tests write to something every worktree shares.
func persisting() credentials.Selection {
	return credentials.Selection{
		Storage: credentials.NewMemoryStorage(),
		Backend: credentials.BackendKeyring,
		Detail:  "under test",
	}
}

// The token is created once and reused. A token regenerated on every launch
// would break the agent's configuration once per session, silently — and the
// failure would be attributed to the agent rather than to DevDesk.
func TestATokenIsCreatedOnceAndThenReused(t *testing.T) {
	sel := persisting()

	first, err := ResolveToken(sel)
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if first == "" {
		t.Fatal("ResolveToken returned an empty token")
	}

	second, err := ResolveToken(sel)
	if err != nil {
		t.Fatalf("ResolveToken again: %v", err)
	}
	if second != first {
		t.Error("a second call minted a new token — the agent's configuration would break every launch")
	}
}

// Two contexts must not share a token. The keyring namespaces the account by
// context on top of the URL, so what this checks is the layer below: a fresh
// store is a fresh token, never a value carried over.
func TestASecondStoreGetsItsOwnToken(t *testing.T) {
	a, err := ResolveToken(persisting())
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	b, err := ResolveToken(persisting())
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if a == b {
		t.Error("two independent stores produced the same token")
	}
}

// credentials.Select falls back to memory when no host store answers, so this
// is a real path. The refusal has to name what is missing: a server that simply
// does not start is an hour of looking at the wrong thing.
func TestAStoreThatDoesNotPersistIsRefused(t *testing.T) {
	_, err := ResolveToken(credentials.SessionOnly("no keyring under test"))

	if err == nil {
		t.Fatal("a session-only store minted a token that would not survive the session")
	}
	if !strings.Contains(err.Error(), "survives the session") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
}

// The bearer check is the transport's, not a tool's, and it refuses before the
// body is read.
func TestOnlyTheRightBearerGetsThrough(t *testing.T) {
	handler, err := Handler(testEnv(nil))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	srv := httptest.NewServer(Authorize(handler, "the-token"))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{"no header at all", "", http.StatusUnauthorized},
		{"the token without the scheme", "the-token", http.StatusUnauthorized},
		{"another scheme", "Basic the-token", http.StatusUnauthorized},
		{"a wrong token", "Bearer not-the-token", http.StatusUnauthorized},
		{"a prefix of the token", "Bearer the-tok", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != tc.want {
				t.Errorf("status %d, want %d", res.StatusCode, tc.want)
			}
			if res.Header.Get("WWW-Authenticate") == "" {
				t.Error("a refusal does not say what it wanted")
			}
		})
	}
}

// And the whole protocol still works behind the check: the tool list comes back
// for a client that carries the token.
func TestTheDeclaredToolsSurviveTheBearerCheck(t *testing.T) {
	handler, err := Handler(testEnv(nil))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	srv := httptest.NewServer(Authorize(handler, "the-token"))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:   srv.URL,
		HTTPClient: &http.Client{Transport: bearer{next: http.DefaultTransport, token: "the-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("client connect with the token: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	if got, want := len(listToolNames(t, cs)), len(toolNames()); got != want {
		t.Errorf("the server serves %d tools behind the check, the table declares %d", got, want)
	}
}

// bearer is what an agent's MCP client configuration does with a header.
type bearer struct {
	next  http.RoundTripper
	token string
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}
