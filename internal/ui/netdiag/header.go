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
		if label := m.selected.Title; label != "" {
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

	if m.activeTab == tabInterfaces {
		if m.interfacesModel.loading {
			return tabShortcuts
		}
		return append(tabShortcuts, shortcut.Shortcuts{
			{Key: "ctrl+r", Description: "Refresh"},
			{Key: "/", Description: "Filter"},
			{Key: ".", Description: "Sort"},
			{Key: "?", Description: "Help"},
		}...)
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
	if m.activeTab == tabInterfaces {
		return append(info, shortcut.HeaderInfo{
			Key:   "Interfaces",
			Value: m.interfacesModel.summaryLine(),
			Style: theme.HeaderValueStyle,
		})
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
	if m.activeTab == tabInterfaces {
		return 3 + m.interfacesModel.table.FilterBar().ExtraHeight()
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
	case m.activeTab == tabInterfaces && m.interfacesModel.table.FilterBar().IsVisible():
		parts = append(parts, m.interfacesModel.table.FilterBar().View())
	}

	tabs := theme.RenderTabs([]theme.TabItem{
		{Label: "Diagnostics"},
		{Label: "Ports"},
		{Label: "Interfaces"},
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
	case tabInterfaces:
		return &m.interfacesModel.footer, m.interfacesModel.statusLine()
	}
	return &m.footer, m.statusLine()
}

// statusLine is what the diagnostics tab derives on every frame: a progress
// label while the pipeline walks, and nothing once it has an answer. It is a
// status rather than a footer message because it is a state, not an event —
// a message expires after three seconds and a run outlives that (Rule 128).
func (m *Model) statusLine() components.Status {
	if m.state == StateRunning {
		return components.Status{Text: m.progressLabel(), Spinner: true}
	}
	return components.Status{}
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
			{Key: "n", Description: "Toggle numeric addresses / reverse-DNS host names (Ports tab)"},
			{Key: "z", Description: "Reset all active filters (Ports tab)"},
			{Key: "/", Description: "Search ports by address, process or PID (Ports tab)"},
			{Key: "space (Ports)", Description: "Pause / resume auto-refresh (Ports tab)"},
			{Key: "K", Description: "Send SIGKILL to the selected process, after confirmation (Ports tab)"},
			// Interfaces tab
			{Key: "/", Description: "Search interfaces by name, MAC or address (Interfaces tab)"},
			{Key: ".", Description: "Cycle the sort column (Interfaces tab)"},
			{Key: "ctrl+r", Description: "Re-read the interfaces (Interfaces tab)"},
		},
		Sections: []help.Section{
			{
				Title: "Diagnostics Tab — What it asks",
				Body: "One target, one run. The checks follow from the target and from what has " +
					"already failed, so there is nothing to select.\n\n" +
					"DNS resolution     - the name, against the resolver your own traffic uses\n" +
					"Reverse DNS        - the name an address publishes (literal targets only)\n" +
					"Local route        - which interface carries this target, and from which address\n" +
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
				Body: "Reads this machine's TCP and UDP sockets every 2 seconds, in this process — no " +
					"container, no Docker. Shows the owning process name and PID where the system will " +
					"name one. K terminates that process, after a confirmation, with the rights DevDesk " +
					"itself has: another user's process or a service comes back refused rather than " +
					"silently killed elsewhere.\n\n" +
					"n toggles reverse DNS on the addresses. Host names only — port numbers stay " +
					"numeric, because a service name would be DevDesk's guess and not the system's.",
			},
			{
				Title: "Interfaces Tab — How it works",
				Body: "Lists this machine's network interfaces, read in this process — no container, " +
					"no Docker. Name, state, MTU, hardware address, error counters and every address " +
					"the interface carries. ctrl+r re-reads them.\n\n" +
					"A dash is not a zero. RX err and TX err show a dash when the counters could not " +
					"be read at all, which is a different thing from an interface that has dropped " +
					"nothing; MTU shows one where the platform reports no figure, as Windows does " +
					"for its loopback.\n\n" +
					"This was the Topology tab. Its routing table, ARP cache and firewall summary " +
					"were read by running ip and iptables in a container on the Docker Desktop VM, " +
					"so they described the VM and not this machine. Nothing portable produces them, " +
					"so they were removed rather than translated — and the route question moved to " +
					"the Diagnostics tab, where it is asked about a target: the Local route check " +
					"names the interface your traffic to that host actually leaves by.",
			},
			{
				Title: "Where the checks run",
				Body: "Everything here runs in this process, on this machine's network stack: the " +
					"diagnostic checks, the socket table and the interfaces alike. They answer for " +
					"the resolver, the routing table, the sockets and the VPN you are actually on, " +
					"and nothing needs Docker.\n\n" +
					"The route trace used to be the exception. It ran traceroute in a container " +
					"started with --network host, which on Docker Desktop is the Linux VM's " +
					"namespace — so it traced the path from the VM and not from here, and the two " +
					"are not the same network. It was removed rather than kept with a caveat.\n\n" +
					"What replaced it, for the question people actually asked: the Local route " +
					"check names the interface and source address your traffic to this target " +
					"leaves by. That is the split-tunnel answer, read from this machine.\n\n" +
					"One image setting is left, network.connectivity_image, and the netdiag view " +
					"does not use it: it belongs to the OCI connectivity test, which runs ping, nc " +
					"or wget inside a Docker network you pick. busybox covers all three in 6,81 MB.",
			},
		},
	}
}
