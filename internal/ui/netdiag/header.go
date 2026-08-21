package netdiag

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// GetTitle implements HeaderView
func (m *Model) GetTitle() string {
	base := theme.IconNetwork + " Network"
	if m.state == StateDetails {
		label := m.selected.Title
		if m.traceOutput != "" {
			label = "Route trace"
		}
		if label != "" {
			detailStyle := lipgloss.NewStyle().Foreground(theme.ColorPrimary).Background(theme.ColorBackground)
			return base + detailStyle.Render(" "+theme.IconChevronRight+" "+label)
		}
	}
	return base
}

// GetIcon implements HeaderView
func (m *Model) GetIcon() string {
	return theme.IconNetwork
}

// GetShortcuts implements HeaderView
func (m *Model) GetShortcuts() shortcut.Shortcuts {
	tabShortcuts := shortcut.Shortcuts{
		{Key: "tab", Description: "Switch tab"},
	}

	if m.activeTab == tabTopology {
		switch m.topologyModel.state {
		case topoStateLoading:
			return tabShortcuts
		case topoStateReady:
			return append(tabShortcuts, shortcut.Shortcuts{
				{Key: "ctrl+r", Description: "Refresh"},
				{Key: "?", Description: "Help"},
			}...)
		}
		return tabShortcuts
	}

	if m.activeTab == tabPorts {
		if m.portsModel.table.FilterBar().InEditMode() {
			return shortcut.Shortcuts{
				{Key: "enter/esc", Description: "Confirm / Cancel search"},
			}
		}
		sc := shortcut.Shortcuts{
			{Key: "t", Description: "Toggle TCP"},
			{Key: "u", Description: "Toggle UDP"},
			{Key: "l", Description: "Toggle LISTEN"},
			{Key: "e", Description: "Toggle ESTAB"},
			{Key: "n", Description: "Toggle numeric"},
			{Key: "z", Description: "Reset filters"},
			{Key: "/", Description: "Search"},
			{Key: "space", Description: "Pause/Resume"},
			{Key: "K", Description: "Kill process"},
			{Key: "?", Description: "Help"},
		}
		return append(tabShortcuts, sc...)
	}

	switch m.state {
	case StateInput:
		return append(tabShortcuts, shortcut.Shortcuts{
			{Key: "enter", Description: "Run the checks"},
			{Key: "?", Description: "Help"},
		}...)
	case StateRunning:
		return shortcut.Shortcuts{
			{Key: "esc", Description: "Cancel and keep what completed"},
		}
	case StateResults:
		if m.filterBar.InEditMode() {
			return shortcut.Shortcuts{
				{Key: "enter/esc", Description: "Confirm / Cancel search"},
			}
		}
		sc := shortcut.Shortcuts{
			{Key: "enter", Description: "Explain the check"},
		}
		// Rule 130: the trace is offered only when something points at the
		// path. A certificate that does not verify is not a routing problem.
		if m.traceWorthOffering() {
			sc = append(sc, shortcut.Shortcut{Key: "H", Description: "Trace the route"})
		}
		label := "Show problems only"
		if m.filterBar.IsTokenActive(problemsToken) {
			label = "Show every check"
		}
		sc = append(sc,
			shortcut.Shortcut{Key: "p", Description: label},
			shortcut.Shortcut{Key: "/", Description: "Search"},
			shortcut.Shortcut{Key: "ctrl+r", Description: "Run again"},
			shortcut.Shortcut{Key: "esc", Description: "New diagnostic"},
			shortcut.Shortcut{Key: "?", Description: "Help"},
		)
		return append(tabShortcuts, sc...)
	case StateDetails:
		return shortcut.Shortcuts{
			{Key: "esc", Description: "Go back to the checks"},
		}
	}
	return tabShortcuts
}

// GetHeaderInfo implements HeaderView.
//
// The verdict is the one line worth spending: it is the answer to the question
// the view exists for, and reading it off ten rows is what the header is
// supposed to save. It appears only once there is a run to summarise.
func (m *Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	info := []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
	if m.activeTab != tabDiagnostics || m.state == StateInput {
		return info
	}
	if len(m.results.All()) == 0 {
		return info
	}
	return append(info, shortcut.HeaderInfo{
		Key:   "Verdict",
		Value: m.verdict.String(),
		Style: verdictHeaderStyle(m.verdict),
	})
}

// verdictHeaderStyle colours the headline the way the table colours a cell.
func verdictHeaderStyle(v netcheck.Verdict) lipgloss.Style {
	switch v {
	case netcheck.Fail:
		return theme.SeverityTextStyle("CRITICAL")
	case netcheck.Warn:
		return theme.SeverityTextStyle("MEDIUM")
	case netcheck.OK:
		return theme.StatusOKStyle
	default:
		return theme.DimStyle
	}
}

// GetFooterHeight implements FooterView — tab bar + empty + info + ports filter bar when visible (Rule 124)
func (m *Model) GetFooterHeight() int {
	if m.activeTab == tabPorts {
		return 3 + m.portsModel.table.FilterBar().ExtraHeight()
	}
	if m.activeTab == tabDiagnostics {
		return 3 + m.filterBar.ExtraHeight()
	}
	return 3
}

// RenderFooter implements FooterView
func (m *Model) RenderFooter(width int) string {
	var parts []string

	// The active tab's filter bar renders above the tab bar when visible.
	switch {
	case m.activeTab == tabPorts && m.portsModel.table.FilterBar().IsVisible():
		parts = append(parts, m.portsModel.table.FilterBar().View())
	case m.activeTab == tabDiagnostics && m.filterBar.IsVisible():
		parts = append(parts, m.filterBar.View())
	}

	tabs := theme.RenderTabs([]theme.TabItem{
		{Label: "Diagnostics"},
		{Label: "Ports"},
		{Label: "Topology"},
	}, m.activeTab)
	tabBar := theme.PadWithBg(theme.Bg(" ")+tabs, width)

	// The message belongs to the tab that raised it: each sub-model keeps its
	// own, so switching away does not carry one tab's notice onto another.
	footer, status := m.activeFooter()
	parts = append(parts, tabBar, theme.EmptyLineBg(width), footer.View(width, status))
	return strings.Join(parts, "\n")
}

// activeFooter resolves the message line of whichever tab is on screen, and the
// status that tab derives on every frame.
func (m *Model) activeFooter() (*components.FooterMessage, components.Status) {
	switch m.activeTab {
	case tabPorts:
		return &m.portsModel.footer, m.portsModel.statusLine()
	case tabTopology:
		return &m.topologyModel.footer, m.topologyModel.statusLine()
	}
	return &m.footer, m.statusLine()
}

// statusLine is what the diagnostics tab derives on every frame: a progress
// label while the pipeline walks, and nothing once it has an answer. It is a
// status rather than a footer message because it is a state, not an event —
// a message expires after three seconds and a run outlives that (Rule 128).
func (m *Model) statusLine() components.Status {
	switch {
	case m.tracing:
		return components.Status{Text: "Tracing the route...", Spinner: true}
	case m.state == StateRunning:
		return components.Status{Text: m.progressLabel(), Spinner: true}
	default:
		return components.Status{}
	}
}

// GetHelpContent implements help.Provider
func (m *Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Network",
		Description: "Three-tab view: check whether a host is reachable and its certificate chain sound (Diagnostics), monitor live ports (Ports), or inspect network topology (Topology).",
		KeyBindings: []help.KeyBinding{
			// Tab navigation
			{Key: "tab / shift+tab", Description: "Cycle between Diagnostics, Ports, and Topology tabs"},
			// Diagnostics
			{Key: "↑ / ↓", Description: "Navigate between fields (Diagnostics tab)"},
			{Key: "enter (form)", Description: "Run the checks against the target"},
			{Key: "esc (running)", Description: "Cancel the run and keep what completed"},
			{Key: "enter (results)", Description: "Explain the selected check"},
			{Key: "H", Description: "Trace the route — offered only when a check points at the path"},
			{Key: "p", Description: "Show problems only / show every check (Diagnostics tab)"},
			{Key: "/", Description: "Search checks by name or observation (Diagnostics tab)"},
			{Key: "ctrl+r", Description: "Run the checks again"},
			{Key: "esc (results)", Description: "Back to the target form"},
			{Key: "esc (details)", Description: "Back to the checks"},
			{Key: "pgup / pgdown", Description: "Scroll half page up / down"},
			// Ports tab
			{Key: "t", Description: "Toggle TCP filter — cumulative with u (Ports tab)"},
			{Key: "u", Description: "Toggle UDP filter — cumulative with t (Ports tab)"},
			{Key: "l", Description: "Toggle LISTEN state filter — cumulative with e (Ports tab)"},
			{Key: "e", Description: "Toggle ESTAB state filter — cumulative with l (Ports tab)"},
			{Key: "n", Description: "Toggle numeric addresses / DNS names (Ports tab)"},
			{Key: "z", Description: "Reset all active filters (Ports tab)"},
			{Key: "/", Description: "Search ports by address, process or PID (Ports tab)"},
			{Key: "space (Ports)", Description: "Pause / resume auto-refresh (Ports tab)"},
			{Key: "K", Description: "Send SIGKILL to the selected process, after confirmation (Ports tab)"},
		},
		Sections: []help.Section{
			{
				Title: "Diagnostics Tab — What it asks",
				Body: "One target, one run. The checks follow from the target and from what has " +
					"already failed, so there is nothing to select.\n\n" +
					"DNS resolution     - the name, against the resolver your own traffic uses\n" +
					"Reverse DNS        - the name an address publishes (literal targets only)\n" +
					"ICMP echo          - reachability; a filtered echo is a warning, never a failure\n" +
					"TCP connect        - the port you named, and the answer that decides reachability\n" +
					"TLS handshake      - whether the port speaks TLS at all\n" +
					"Certificate chain  - verified against this machine's store, intermediates included\n" +
					"Hostname match     - whether the certificate names the host you dialled\n" +
					"Certificate expiry - the validity window, in days\n" +
					"TLS version        - the negotiated protocol version\n" +
					"HTTP response      - whether the service answers, over the scheme actually spoken",
			},
			{
				Title: "Diagnostics Tab — Reading the verdicts",
				Body: "OK      - the objective is met\n" +
					"WARN    - met, but something is off: a certificate near expiry, a deprecated\n" +
					"          version, a chain that verifies here and will not elsewhere\n" +
					"FAIL    - the objective is not met\n" +
					"N/A     - no meaning for this target, or blocked by a check above it\n" +
					"UNKNOWN - the check could not look, which is not the same as a pass\n\n" +
					"A stage whose dependency failed does not run: its checks come back N/A naming " +
					"what blocked them, so a broken run reads as one failure and a named point of " +
					"rupture rather than as ten red rows.\n\n" +
					"Press enter on any row for what it means and what to do about it.",
			},
			{
				Title: "Ports Tab — How it works",
				Body: "Runs ss -tupan inside an ephemeral Docker container with --net=host --pid=host " +
					"every 2 seconds. Shows all active TCP/UDP sockets on the host including the owning " +
					"process name and PID. K runs kill -9 via a --privileged container, after a confirmation.",
			},
			{
				Title: "Topology Tab — How it works",
				Body: "Fetches five data sources concurrently on load: network interfaces with MTU (ip addr show + " +
					"ip -s link show), routing table (ip route show), ARP/neighbour cache (ip neigh show), and " +
					"firewall rules (iptables or nft). Network errors section is shown only when RX/TX errors are " +
					"non-zero. Firewall summary shows chain names, default policy, and rule count. " +
					"Press ctrl+r to reload all sections.\n\n" +
					"ARP / Neighbours states:\n" +
					"  REACHABLE  — entry confirmed reachable recently (green)\n" +
					"  PERMANENT  — static entry, never expires (green)\n" +
					"  STALE      — entry not confirmed recently, will be re-probed on next use (dim)\n" +
					"  DELAY      — waiting for confirmation after sending a probe (dim)\n" +
					"  INCOMPLETE — ARP request sent, no reply yet (dim)\n" +
					"  FAILED     — unreachable, ARP probe received no reply (red)",
			},
			{
				Title: "Where the checks run",
				Body: "The diagnostic checks run in this process, on this machine's network stack. " +
					"They therefore answer for the resolver, the routing table and the VPN you are " +
					"actually on.\n\n" +
					"The route trace (H), the Ports tab and the Topology tab still run in the image " +
					"configured at docker.network_tool_image. On Docker Desktop that container lives " +
					"in a Linux VM with its own network namespace, so a trace can legitimately " +
					"disagree with the checks above it. The trace pane says so.\n\n" +
					"The image must include traceroute, tcptraceroute, ss (iproute2), ip, and " +
					"iptables or nft for firewall inspection.",
			},
		},
	}
}
