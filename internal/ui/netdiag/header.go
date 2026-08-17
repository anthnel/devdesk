package netdiag

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// GetTitle implements HeaderView
func (m *Model) GetTitle() string {
	base := theme.IconNetwork + " Network"
	if m.state == StateDetails && m.selectedTest != "" {
		detailStyle := lipgloss.NewStyle().Foreground(theme.ColorPrimary).Background(theme.ColorBackground)
		suffix := detailStyle.Render(" " + theme.IconChevronRight + " " + m.selectedTest)
		return base + suffix
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
			{Key: "space", Description: "Toggle checkbox"},
			{Key: "enter", Description: "Run (on button)"},
			{Key: "?", Description: "Help"},
		}...)
	case StateRunning:
		return shortcut.Shortcuts{
			{Key: "esc", Description: "Cancel & show results"},
		}
	case StateResults:
		return append(tabShortcuts, shortcut.Shortcuts{
			{Key: "enter", Description: "View logs"},
			{Key: "esc", Description: "New diagnostic"},
			{Key: "?", Description: "Help"},
		}...)
	case StateDetails:
		sc := shortcut.Shortcuts{
			{Key: "esc", Description: "Back to results"},
		}
		res, ok := m.results[m.selectedTest]
		isDNS := ok && (res.name == "DNS Resolution" || res.name == "Reverse DNS")
		isTrace := ok && (res.name == "Traceroute" || res.name == "TCP Traceroute")
		if isDNS || isTrace {
			label := "Raw output"
			if m.rawDetails {
				label = "Formatted output"
			}
			sc = append(sc, shortcut.Shortcut{Key: "f", Description: label})
		}
		return sc
	}
	return tabShortcuts
}

// GetHeaderInfo implements HeaderView
func (m *Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
}

// GetFooterHeight implements FooterView — tab bar + empty + info + ports filter bar when visible (Rule 124)
func (m *Model) GetFooterHeight() int {
	if m.activeTab == tabPorts {
		return 3 + m.portsModel.table.FilterBar().ExtraHeight()
	}
	return 3
}

// RenderFooter implements FooterView
func (m *Model) RenderFooter(width int) string {
	var parts []string

	// Ports filter bar renders above the tab bar when visible
	if m.activeTab == tabPorts && m.portsModel.table.FilterBar().IsVisible() {
		parts = append(parts, m.portsModel.table.FilterBar().View())
	}

	tabs := theme.RenderTabs([]theme.TabItem{
		{Label: "Diagnostics"},
		{Label: "Ports"},
		{Label: "Topology"},
	}, m.activeTab)
	tabBar := theme.PadWithBg(theme.Bg(" ")+tabs, width)

	// Show footer error/info from the active tab
	footerErr, footerInfo := m.footerError, m.footerInfo
	switch m.activeTab {
	case tabPorts:
		footerErr = m.portsModel.footerError
		footerInfo = m.portsModel.footerInfo
	case tabTopology:
		footerErr = m.topologyModel.footerError
		footerInfo = m.topologyModel.footerInfo
	}

	infoLine := theme.EmptyLineBg(width)
	if footerErr != "" {
		infoLine = theme.PadWithBg(theme.StatusErrorStyle.Render(footerErr), width)
	} else if footerInfo != "" {
		infoLine = lipgloss.NewStyle().
			Foreground(theme.ColorHighlight).
			Background(theme.ColorBackground).
			Width(width).
			Align(lipgloss.Center).
			Render(footerInfo)
	}
	parts = append(parts, tabBar, theme.EmptyLineBg(width), infoLine)
	return strings.Join(parts, "\n")
}

// GetHelpContent implements help.Provider
func (m *Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Network",
		Description: "Three-tab view: run diagnostic tests (Diagnostics), monitor live ports (Ports), or inspect network topology (Topology).",
		KeyBindings: []help.KeyBinding{
			// Tab navigation
			{Key: "tab / shift+tab", Description: "Cycle between Diagnostics, Ports, and Topology tabs"},
			// Diagnostics form
			{Key: "↑ / ↓", Description: "Navigate between fields (Diagnostics tab)"},
			{Key: "space", Description: "Toggle checkbox (Diagnostics tab)"},
			{Key: "enter", Description: "Run diagnostics (on button)"},
			// Running
			{Key: "esc (running)", Description: "Cancel run and show partial results"},
			// Results
			{Key: "enter (results)", Description: "View full log output for selected test"},
			{Key: "esc", Description: "New diagnostic (back to form)"},
			// Details
			{Key: "esc (details)", Description: "Back to results table"},
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
				Title: "Diagnostics Tab — Available Tests",
				Body: "Ping           - ICMP reachability check\n" +
					"DNS Resolution  - Resolve hostname to IP (auto: Reverse DNS for IP targets)\n" +
					"Traceroute      - ICMP network path to target\n" +
					"TCP Traceroute  - TCP network path to target:port\n" +
					"Netcat          - TCP port connectivity check\n" +
					"HTTP/HTTPS      - Curl request with status code\n" +
					"SSL Certificate - Certificate validity and expiry",
			},
			{
				Title: "Ports Tab — How it works",
				Body: "Runs ss -tupan inside an ephemeral Docker container with --net=host --pid=host " +
					"every 2 seconds. Shows all active TCP/UDP sockets on the host including the owning " +
					"process name and PID. ctrl+k runs kill -9 via a --privileged container.",
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
				Title: "Docker image",
				Body: "All tabs use the image configured at docker.network_tool_image in your config. " +
					"The image must include ss (iproute2), ping, traceroute, nc, curl, openssl, ip, and " +
					"iptables or nft for firewall inspection.",
			},
		},
	}
}
