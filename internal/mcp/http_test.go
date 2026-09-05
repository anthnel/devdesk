package mcp

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The in-memory transport of tools_test proves what the server registers; it
// says nothing about whether any of it survives being carried over HTTP. That
// is the whole of §3.61's first phase, so it is checked over a real socket:
// httptest binds one, a real client speaks Streamable HTTP to it, and the tool
// list comes back.
//
// It is the same assertion as TestTheServerRegistersExactlyTheDeclaredTools,
// deliberately — what is under test here is the transport, and reusing the
// assertion is what makes the two comparable when one of them fails.
func TestTheDeclaredToolsSurviveTheHTTPTransport(t *testing.T) {
	handler, err := Handler(testEnv(nil))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("client connect over HTTP: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	got := listToolNames(t, cs)
	want := toolNames()

	if len(got) != len(want) {
		t.Fatalf("over HTTP the server serves %v, the table declares %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The allow-list is applied when the handler is built, so a name matching
// nothing has to stop the handler from existing at all. Refusing later — at the
// first request, say — would leave a port listening and an agent connected to a
// server that answers nothing.
func TestAnUnknownToolInTheAllowListYieldsNoHandler(t *testing.T) {
	handler, err := Handler(testEnv([]string{"scan_everything"}))

	if err == nil {
		t.Fatal("an allow-list naming no tool still produced a handler")
	}
	if handler != nil {
		t.Error("a refused allow-list still produced a handler")
	}
}
