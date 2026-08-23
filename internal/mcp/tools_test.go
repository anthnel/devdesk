package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The declared table and what the server actually serves have to agree, in both
// directions: a def whose closure registers under another name, or registers two
// tools, or forgets to register at all, is invisible from the table alone. The
// only thing that can tell is a real round trip, so the harness connects a real
// client to a real server over the SDK's in-memory transport.
func TestTheServerRegistersExactlyTheDeclaredTools(t *testing.T) {
	cs := connect(t, testEnv(nil))

	got := listToolNames(t, cs)
	want := toolNames()

	if len(got) != len(want) {
		t.Fatalf("the server serves %v, the table declares %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A def with no register closure would panic at startup rather than be reported,
// so it is checked before anything is built.
func TestEveryDeclaredToolCanRegisterItself(t *testing.T) {
	for _, def := range tools() {
		if def.Name == "" {
			t.Errorf("a tool is declared with no name")
		}
		if def.Description == "" {
			t.Errorf("tool %q is declared with no description — it is what an agent reads to decide whether to call it", def.Name)
		}
		if def.register == nil {
			t.Errorf("tool %q declares no register closure", def.Name)
		}
	}
}

// The description an agent reads comes off the same table as the name, or the
// two drift into saying different things about one tool.
func TestARegisteredToolCarriesItsDeclaredDescription(t *testing.T) {
	cs := connect(t, testEnv(nil))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	declared := make(map[string]string)
	for _, def := range tools() {
		declared[def.Name] = def.Description
	}
	for _, tool := range res.Tools {
		if tool.Description != declared[tool.Name] {
			t.Errorf("tool %q is served as %q, declared as %q", tool.Name, tool.Description, declared[tool.Name])
		}
	}
}

// `expose` is an allow-list: it narrows, and empty means everything. An opt-in
// would make enabling the server at all require naming every tool.
func TestAnEmptyExposeServesEveryTool(t *testing.T) {
	got, err := exposedTools(nil)
	if err != nil {
		t.Fatalf("exposedTools: %v", err)
	}
	if len(got) != len(tools()) {
		t.Errorf("an empty allow-list served %d tools, want all %d", len(got), len(tools()))
	}
}

func TestExposeNarrowsToWhatItNames(t *testing.T) {
	cs := connect(t, testEnv([]string{"context_list"}))

	got := listToolNames(t, cs)

	if len(got) != 1 || got[0] != "context_list" {
		t.Errorf("the server serves %v, want only context_list", got)
	}
}

// A typo in an allow-list exposes less than the user asked for and nothing
// fails — everything works, quietly, with a tool missing. So it is refused.
func TestExposeRefusesANameThatMatchesNoTool(t *testing.T) {
	_, err := exposedTools([]string{"context_list", "scan_everything"})

	if err == nil {
		t.Fatal("an allow-list naming a tool that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "scan_everything") {
		t.Errorf("the error does not name the offending entry: %v", err)
	}
}

// The refusal names the setting and the context both: activating it in the wrong
// context is otherwise an hour of looking at a server that will not start.
func TestTheRefusalNamesTheSettingAndTheContext(t *testing.T) {
	err := Refused("work")

	if !strings.Contains(err.Error(), "work") {
		t.Errorf("the refusal does not name the context: %v", err)
	}
	if !strings.Contains(err.Error(), "mcp.enabled") {
		t.Errorf("the refusal does not name the setting: %v", err)
	}
}

// context_list says which context is readable through this server, because a
// process serves exactly one and an agent reading the list alone would assume
// otherwise.
func TestContextListSaysWhichContextItServes(t *testing.T) {
	cs := connect(t, testEnv(nil))

	var out contextListOut
	callTool(t, cs, "context_list", nil, &out)

	if out.Served != "work" {
		t.Errorf("served = %q, want the context the server was opened on", out.Served)
	}
}

// ── harness ─────────────────────────────────────────────────────────────────

func testEnv(expose []string) *Env {
	cfg := config.Default()
	cfg.MCP.Enabled = true
	cfg.MCP.Expose = expose
	return &Env{Config: cfg, Context: "work"}
}

// connect drives a real server over the SDK's in-memory transport, which is what
// makes the registration itself observable rather than only the table.
func connect(t *testing.T, env *Env) *sdk.ClientSession {
	t.Helper()

	s, err := newServer(env)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := s.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func listToolNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

// callTool calls a tool and decodes its structured output, which is what an
// agent consumes — the text content is the same JSON rendered for a human.
func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s reported a tool error: %+v", name, res.Content)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured output of %s: %v", name, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode structured output of %s: %v", name, err)
	}
}
