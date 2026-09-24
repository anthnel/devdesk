package configuration

import (
	"log"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// mcpTab is the title of the tab the connect command is copied from.
const mcpTab = "mcp"

// The reasons Y is refused (Rule 130). Named once, so the header, the footer
// and the tests read the same words.
const (
	reasonNotOnMCPTab   = "The MCP connect command is copied from the mcp tab"
	reasonMCPNotRunning = "MCP server is not running"
	reasonTypingAValue  = "Leave the field first — Y is being typed into it"
)

// writeClipboard is the clipboard write, a variable so a test can take it: the
// real one writes to the machine's clipboard, which is neither the test's to
// take nor available on a headless runner.
var writeClipboard = clipboard.WriteAll

// MCPCommandCopiedMsg reports the clipboard write of the connect command.
//
// It deliberately carries no copy of the command: the command holds the
// token, and a message is what ends up in a log line when something goes
// wrong (§3.77, question 4).
type MCPCommandCopiedMsg struct {
	Err error
}

// connectCommand is the one-line `claude mcp add` for the server as it is
// actually bound (§3.77).
//
// The address is the listener's, not `mcp.listen` as typed: a port `0` would
// be copied as a port nobody listens on. Claude Code only, because it is the
// one client whose setup is a single command — the others want a file, and a
// menu of formats would be a second feature.
//
// The token is base64url (internal/mcp/token.go), so double quotes are enough
// in sh, PowerShell and cmd alike.
func connectCommand(addr, token string) string {
	return "claude mcp add --transport http devdesk http://" + addr +
		` --header "Authorization: Bearer ` + token + `"`
}

// copyAvailability is whether Y copies the connect command now, or why not. One
// computation, read by the header to grey and by the handler to refuse.
//
// A text field with focus comes first: the letter goes into the field there,
// so announcing it as an action would be announcing a key that types.
func (m Model) copyAvailability() shortcut.Availability {
	switch {
	case !m.onMCPTab():
		return shortcut.Unavailable(reasonNotOnMCPTab)
	case m.current().takesText():
		return shortcut.Unavailable(reasonTypingAValue)
	case m.mcp.Addr == "" || m.mcp.Token == "":
		return shortcut.Unavailable(reasonMCPNotRunning)
	}
	return shortcut.Availability{}
}

// onMCPTab reports whether the tab on screen is the mcp tab.
func (m Model) onMCPTab() bool {
	return m.activeTab >= 0 && m.activeTab < len(m.sections) && m.sections[m.activeTab].Title == mcpTab
}

// copyConnectCommand puts the connect command on the clipboard, or says in the
// footer why it cannot (Rule 130: a refusal is never silent).
func (m Model) copyConnectCommand() (tea.Model, tea.Cmd) {
	if a := m.copyAvailability(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	command := connectCommand(m.mcp.Addr, m.mcp.Token)
	return m, func() tea.Msg {
		return MCPCommandCopiedMsg{Err: writeClipboard(command)}
	}
}

// handleMCPCommandCopied reports the write (Rule 128).
//
// Success is announced as an Info that names the token: the clipboard is
// readable by every process in the session, and putting a secret there is an
// act the user should see happen rather than infer (§3.77, question 3).
//
// The log line names the error and nothing else — never the command, which
// carries the token.
func (m Model) handleMCPCommandCopied(msg MCPCommandCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [configuration] copy MCP connect command to clipboard: %v", msg.Err)
		return m, m.footer.Error("Failed to copy the connect command — check logs")
	}
	return m, m.footer.Info("Claude Code connect command copied — it contains the token")
}
