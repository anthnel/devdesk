package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
)

// Everything else in this package calls the handlers directly, on the test's
// own goroutine — which is exactly the arrangement that cannot show the defect
// this design exists to avoid. The tool handler really runs on the HTTP
// server's goroutines, several at once, while Update() runs on Bubble Tea's;
// what holds them apart is that nothing in internal/mcp touches the model
// (Rule 110).
//
// So this drives the real thing: a real tea.Program looping, a real listener, a
// real HTTP client hammering the tool endpoint from several goroutines while
// keys and job transitions go through Update. Under -race it is the only test
// here that can fail for the right reason.
func TestTheServerAndTheLoopRunSideBySide(t *testing.T) {
	a := router(t, &bareView{})
	// The server is stood up here rather than through startMCPCmd, and the
	// context leaves `mcp.enabled` false, so the program's own Init starts
	// nothing: two listeners would trip the epoch guard, which closes the
	// superseded one — correctly, but it would be testing that instead of this.
	// What is under test is the dispatcher and Update running side by side.
	a.config = config.Default()

	// WithoutRenderer keeps the terminal out of it; WithInput(nil) stops Bubble
	// Tea reading stdin, which under `go test` is closed and would quit at once.
	p := tea.NewProgram(a, tea.WithoutRenderer(), tea.WithInput(nil))
	a.AttachProgram(p)

	handler, err := mcpserver.Handler(&mcpserver.Env{
		Config:   a.config,
		Context:  "test",
		Dispatch: a.mcpDispatch,
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(mcpserver.Authorize(handler, "the-token"))
	t.Cleanup(srv.Close)

	loop := make(chan struct{})
	go func() {
		defer close(loop)
		if _, err := p.Run(); err != nil {
			t.Errorf("program: %v", err)
		}
	}()
	t.Cleanup(func() {
		p.Quit()
		<-loop
	})

	endpoint := srv.URL + "/"
	client := &http.Client{Timeout: 10 * time.Second}

	var wg sync.WaitGroup

	// Eight callers asking the session what is running, through the whole
	// stack: HTTP, the SDK, the tool handler, p.Send, Update, the reply channel.
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if err := callJobsList(client, endpoint, "the-token"); err != nil {
					t.Errorf("jobs_list over HTTP: %v", err)
					return
				}
			}
		}()
	}

	// And the model being written all the while, from the loop's own goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 40 {
			p.Send(jobs.StartMsg{Run: jobs.Run{
				Kind:   jobs.KindScan,
				Origin: command.ViewWorkspaces,
				Items:  []jobs.Item{{Target: "/repo", State: jobs.ItemRunning}},
			}})
			if i%4 == 0 {
				p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
			}
		}
	}()

	wg.Wait()
}

// callJobsList speaks just enough of Streamable HTTP to invoke one tool: the
// SDK's client would do it too, but eight of them each holding a session is a
// different test — what is wanted here is load on the handler.
func callJobsList(client *http.Client, endpoint, token string) error {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return errStatus(res.StatusCode)
	}

	session := res.Header.Get("Mcp-Session-Id")
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"jobs_list","arguments":{}}}`
	req, err = http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(call))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Mcp-Session-Id", session)
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")

	res2, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res2.Body.Close() }()
	if res2.StatusCode != http.StatusOK {
		return errStatus(res2.StatusCode)
	}

	// The body is read and checked, not discarded: a call that reached the
	// handler and came back an error would otherwise pass, and this test would
	// be exercising the HTTP layer alone rather than the loop behind it.
	answer, err := io.ReadAll(res2.Body)
	if err != nil {
		return err
	}
	if !strings.Contains(string(answer), `"jobs"`) {
		return fmt.Errorf("jobs_list did not answer with a job list: %s", answer)
	}
	return nil
}

type errStatus int

func (e errStatus) Error() string { return "HTTP " + http.StatusText(int(e)) }
