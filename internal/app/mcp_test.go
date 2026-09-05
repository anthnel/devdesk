package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// persistingStore is a Selection that keeps secrets in this process while
// claiming a backend that survives it. The claim is what is under test
// elsewhere in the file — ResolveToken refuses a store that does not persist —
// so a real keyring is not needed to exercise everything around it, and a test
// that reached for the developer's own keyring would be one every worktree
// shares.
func testSharedState() *shared.State {
	return &shared.State{Secrets: persistingStore()}
}

func persistingStore() credentials.Selection {
	return credentials.Selection{
		Storage: credentials.NewMemoryStorage(),
		Backend: credentials.BackendKeyring,
		Detail:  "under test",
	}
}

// startMCP runs the command the router issues at startup and returns what it
// reported. The command opens a listener, so every test that gets one closes it.
func startMCP(t *testing.T, cfg *config.Config) MCPServerStartedMsg {
	t.Helper()

	a := &App{
		config:         cfg,
		currentContext: "test",
		sharedState:    testSharedState(),
	}
	msg, ok := a.startMCPCmd()().(MCPServerStartedMsg)
	if !ok {
		t.Fatalf("startMCPCmd returned %T, want MCPServerStartedMsg", msg)
	}
	if msg.Server != nil {
		t.Cleanup(func() { _ = msg.Server.Close() })
	}
	return msg
}

// enabledConfig is a context that serves, on a port the OS picks — a fixed one
// would make two packages of tests run in parallel fight over it, and the
// failure would look like the bind failure this file is here to check.
func enabledConfig() *config.Config {
	cfg := config.Default()
	cfg.MCP.Enabled = true
	cfg.MCP.Listen = "127.0.0.1:0"
	return cfg
}

// `mcp.enabled` is false by default and that is the whole safety story: turning
// it on is the moment the user decides an agent may reach this context. A
// config written before the key existed must therefore not serve, and the zero
// value of a bool is what makes that true without a migration.
//
// The refusal travels on the message rather than being logged and dropped:
// "off" and "could not bind" look identical from outside, and only one of them
// is a problem.
func TestAContextThatHasNotEnabledTheServerDoesNotBind(t *testing.T) {
	msg := startMCP(t, config.Default())

	if msg.Err == nil {
		t.Fatal("a context with mcp.enabled unset bound a listener")
	}
	if msg.Server != nil {
		t.Error("a refused context still returned a running server")
	}
	if !strings.Contains(msg.Err.Error(), "mcp.enabled") {
		t.Errorf("refusal does not name the setting: %v", msg.Err)
	}
	if !strings.Contains(msg.Err.Error(), "test") {
		t.Errorf("refusal does not name the context: %v", msg.Err)
	}
}

func TestAnEnabledContextBinds(t *testing.T) {
	msg := startMCP(t, enabledConfig())

	if msg.Err != nil {
		t.Fatalf("enabled context did not serve: %v", msg.Err)
	}
	if msg.Server == nil {
		t.Fatal("enabled context reported no server")
	}
	// The address is what the listener actually bound, not what the setting
	// asked for: with :0 the two differ, and it is the bound one an agent has
	// to be pointed at.
	if msg.Addr == "" || strings.HasSuffix(msg.Addr, ":0") {
		t.Errorf("Addr = %q, want the port the OS actually assigned", msg.Addr)
	}
}

// An allow-list naming a tool that does not exist is refused rather than
// ignored: silently exposing less than was asked for is the failure nobody
// notices. It has to stop the bind, not merely log — a server listening with a
// tool missing is the same screen as a server listening correctly.
func TestAnUnknownToolInTheAllowListStopsTheBind(t *testing.T) {
	cfg := enabledConfig()
	cfg.MCP.Expose = []string{"scan_everything"}

	msg := startMCP(t, cfg)

	if msg.Err == nil {
		t.Fatal("an allow-list naming no tool still bound a listener")
	}
	if msg.Server != nil {
		t.Error("a refused allow-list still returned a running server")
	}
	if !strings.Contains(msg.Err.Error(), "scan_everything") {
		t.Errorf("refusal does not name the unknown tool: %v", msg.Err)
	}
}

// A file written before `listen` existed must bind the default, never nothing.
// The zero value of a string cannot be allowed to read as a choice.
func TestAContextWithoutAListenAddressBindsTheDefault(t *testing.T) {
	cfg := config.Default()
	cfg.MCP.Enabled = true
	cfg.MCP.Listen = ""

	msg := startMCP(t, cfg)

	if msg.Err != nil {
		t.Fatalf("a context with no listen address did not serve: %v", msg.Err)
	}
	if msg.Addr != config.DefaultMCPListen {
		t.Errorf("Addr = %q, want the default %q", msg.Addr, config.DefaultMCPListen)
	}
}

// A port that answers without a token is what the whole step exists to prevent.
// stdio needed no authentication because the process *was* the user; a loopback
// port that clones and scans is reachable by every process on the machine.
func TestAServedContextRefusesARequestWithoutTheToken(t *testing.T) {
	msg := startMCP(t, enabledConfig())
	if msg.Err != nil {
		t.Fatalf("enabled context did not serve: %v", msg.Err)
	}

	res, err := http.Post("http://"+msg.Addr+"/", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST without a token: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("an unauthenticated request got %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}

// A store that does not survive the session would hand out a new token every
// launch, breaking the agent's configuration once per session and silently —
// and the failure would be read as the agent's, never as the store's. So the
// server does not start at all, and says why.
func TestAStoreThatDoesNotPersistStopsTheBind(t *testing.T) {
	a := &App{
		config:         enabledConfig(),
		currentContext: "test",
		sharedState:    &shared.State{Secrets: credentials.SessionOnly("no keyring under test")},
	}

	msg, ok := a.startMCPCmd()().(MCPServerStartedMsg)
	if !ok {
		t.Fatalf("startMCPCmd returned %T, want MCPServerStartedMsg", msg)
	}
	if msg.Server != nil {
		_ = msg.Server.Close()
		t.Fatal("a session-only secret store still bound a listener")
	}
	if msg.Err == nil {
		t.Fatal("a session-only secret store served without saying so")
	}
	if !strings.Contains(msg.Err.Error(), "survives the session") {
		t.Errorf("the refusal does not say what is missing: %v", msg.Err)
	}
}

// The router has no footer of its own — RenderFooter is the active view's — so
// the one thing it has to say reaches the screen as a broadcast every view's
// FooterMessage already consumes.
//
// Rule 128 calls this an Error and not a Warn: the bind was refused, which is
// the system saying no.
func TestAFailedBindIsReportedOnScreen(t *testing.T) {
	cfg := enabledConfig()
	cfg.MCP.Listen = "127.0.0.1:0"

	a := router(t, &bareView{})
	a.config = cfg

	_, cmd := a.handleMCPServerStarted(MCPServerStartedMsg{Err: errors.New("address already in use")})
	if cmd == nil {
		t.Fatal("a failed bind said nothing to the user")
	}

	post, ok := testutil.MsgOf[components.PostFooterMsg](cmd)
	if !ok {
		t.Fatal("the failure did not reach a footer")
	}
	if post.Level != components.LevelError {
		t.Errorf("level = %v, want LevelError — the system refused the bind", post.Level)
	}
	if !strings.Contains(post.Text, "address already in use") {
		t.Errorf("the message does not say why: %q", post.Text)
	}
}

// Nothing was attempted when the context did not ask for a server, so there is
// nothing to report — and a message announcing the failure of a server nobody
// asked for reads as a fault.
func TestADisabledContextSaysNothingOnScreen(t *testing.T) {
	a := router(t, &bareView{})
	a.config = config.Default()

	_, cmd := a.handleMCPServerStarted(MCPServerStartedMsg{Err: mcpserver.Refused("default")})
	if cmd != nil {
		t.Error("a context that never asked for a server still reported one failing")
	}
}

// The configuration view is built lazily and shows all three facts, so it has
// to be rebuilt when they change — the precedent is useSecrets rebuilding the
// auth view once the store is resolved.
func TestTheConfigurationViewIsRebuiltWhenTheServerReports(t *testing.T) {
	a := router(t, &bareView{})
	a.createView(command.ViewConfiguration)
	if _, ok := a.views[command.ViewConfiguration]; !ok {
		t.Fatal("the configuration view was not built; this test cannot say anything")
	}

	a.handleMCPServerStarted(MCPServerStartedMsg{Addr: "127.0.0.1:7777", Token: "t"})

	if _, ok := a.views[command.ViewConfiguration]; ok {
		t.Error("the configuration view was kept, so it still shows what was true before the server started")
	}
}

// The refusal shown on that screen is a rendered state, not an error value, and
// "the setting is off" is one of the answers rather than a fault.
func TestTheFactsShownOnScreenHideARefusalNobodyAskedFor(t *testing.T) {
	a := router(t, &bareView{})
	a.config = config.Default()
	a.mcpErr = mcpserver.Refused("default")

	if reason := a.mcpFacts().Reason; reason != "" {
		t.Errorf("Reason = %q for a context that never asked for a server, want it empty", reason)
	}

	a.config = enabledConfig()
	a.mcpErr = errors.New("address already in use")
	if reason := a.mcpFacts().Reason; reason == "" {
		t.Error("a real failure was not shown on the configuration screen")
	}
}
