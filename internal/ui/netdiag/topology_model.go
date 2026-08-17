package netdiag

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	dockerpkg "github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// topoState is the state machine for the Topology tab.
type topoState int

const (
	topoStateLoading topoState = iota // fetching network data
	topoStateReady                    // data loaded, viewport scrollable
)

// topoDataMsg carries the parsed network topology data.
type topoDataMsg struct {
	interfaces  []InterfaceInfo
	routes      []RouteInfo
	neighbours  []NeighbourInfo
	firewall    []FirewallChain
	firewallSrc string // "iptables", "nftables", or "unavailable"
	err         error
}

// InterfaceInfo represents a network interface parsed from ip addr show / ip -s link show.
type InterfaceInfo struct {
	Name      string   // e.g. "eth0", "lo"
	State     string   // "UP", "DOWN", "LOOP"
	Addresses []string // CIDR notation, e.g. "192.168.1.5/24"
	MTU       int      // maximum transmission unit
	RxErrors  int64    // receive errors from ip -s link show
	TxErrors  int64    // transmit errors from ip -s link show
}

// RouteInfo represents an IP route parsed from ip route show.
type RouteInfo struct {
	Destination string // e.g. "default", "192.168.1.0/24"
	Gateway     string // empty for directly connected routes
	Interface   string // e.g. "eth0"
}

// NeighbourInfo represents an ARP/neighbour cache entry from ip neigh show.
type NeighbourInfo struct {
	IP        string // e.g. "192.168.1.1"
	MAC       string // e.g. "aa:bb:cc:dd:ee:ff", empty when INCOMPLETE
	Interface string // e.g. "eth0"
	State     string // "REACHABLE", "STALE", "DELAY", "FAILED", "PERMANENT", "INCOMPLETE"
}

// FirewallChain represents a summary of a firewall chain.
type FirewallChain struct {
	Name   string // e.g. "INPUT", "FORWARD", "OUTPUT"
	Policy string // "ACCEPT", "DROP", "REJECT", or "n/a"
	Rules  int    // count of rules in the chain
}

func fetchTopoDataCmd(image string) tea.Cmd {
	return func() tea.Msg {
		var (
			wg                                          sync.WaitGroup
			addrRes, linkRes, routeRes, neighRes, fwRes dockerpkg.DiagResult
		)
		wg.Add(5)
		go func() { defer wg.Done(); addrRes = dockerpkg.RunIPAddr(image) }()
		go func() { defer wg.Done(); linkRes = dockerpkg.RunIPLink(image) }()
		go func() { defer wg.Done(); routeRes = dockerpkg.RunIPRoute(image) }()
		go func() { defer wg.Done(); neighRes = dockerpkg.RunIPNeigh(image) }()
		go func() { defer wg.Done(); fwRes = dockerpkg.RunFirewallRules(image) }()
		wg.Wait()

		var ifaces []InterfaceInfo
		var err error
		if addrRes.Success {
			ifaces = parseIPAddr(addrRes.Output)
		} else {
			err = fmt.Errorf("%s", addrRes.Output)
		}

		// Merge MTU and error stats into ifaces (best-effort — failures are logged, not fatal)
		if linkRes.Success {
			ifaces = parseIPLink(linkRes.Output, ifaces)
		} else {
			log.Printf("WARN [netdiag/topology] ip -s link show failed: %s", linkRes.Output)
		}

		var routes []RouteInfo
		if routeRes.Success {
			routes = parseIPRoute(routeRes.Output)
		}

		neighbours := parseIPNeigh(neighRes.Output)
		firewall, fwSrc := parseFirewall(fwRes.Output)

		return topoDataMsg{
			interfaces:  ifaces,
			routes:      routes,
			neighbours:  neighbours,
			firewall:    firewall,
			firewallSrc: fwSrc,
			err:         err,
		}
	}
}

// TopologyModel manages the network topology sub-view.
type TopologyModel struct {
	image  string
	width  int
	height int

	state topoState

	interfaces  []InterfaceInfo
	routes      []RouteInfo
	neighbours  []NeighbourInfo
	firewall    []FirewallChain
	firewallSrc string
	loadErr     string

	viewport viewport.Model
	spinner  spinner.Model

	// footer is this tab's own message line (see PortsModel.footer).
	footer components.FooterMessage
}

func newTopologyModel(image string) *TopologyModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	return &TopologyModel{
		image:   image,
		state:   topoStateLoading,
		spinner: sp,
	}
}

// initTopology returns the initial commands: fetch network data and start spinner.
func (tm *TopologyModel) initTopology() tea.Cmd {
	return tea.Batch(fetchTopoDataCmd(tm.image), tm.spinner.Tick)
}

// InEditMode returns false — the topology tab has no text inputs.
func (tm *TopologyModel) InEditMode() bool {
	return false
}

// resize updates terminal dimensions and rebuilds the internal viewport.
func (tm *TopologyModel) resize(width, height int) {
	tm.width = width
	tm.height = height
	vw := max(width-2, 20)
	vh := max(height-2, 3)
	tm.viewport = viewport.New(vw, vh)
	tm.viewport.Style = lipgloss.NewStyle().Background(theme.ColorBackground)
	if tm.state != topoStateLoading {
		tm.viewport.SetContent(tm.buildViewportContent())
	}
}

func (tm *TopologyModel) update(msg tea.Msg) (*TopologyModel, tea.Cmd) {
	switch msg := msg.(type) {
	case topoDataMsg:
		return tm.handleData(msg)
	case spinner.TickMsg:
		if tm.state == topoStateLoading {
			var cmd tea.Cmd
			tm.spinner, cmd = tm.spinner.Update(msg)
			// The load is reported in the footer, so the frame has to reach it
			// — a spinner stuck on frame zero reads as a hang.
			tm.footer.SetSpinnerFrame(tm.spinner.View())
			return tm, cmd
		}
		return tm, nil
	case tea.KeyMsg:
		return tm.handleKey(msg)
	}
	return tm, nil
}

func (tm *TopologyModel) handleData(msg topoDataMsg) (*TopologyModel, tea.Cmd) {
	if msg.err != nil {
		log.Printf("ERROR [netdiag/topology] fetch: %v", msg.err)
		tm.loadErr = "Failed to load network data — check logs"
	} else {
		tm.interfaces = msg.interfaces
		tm.routes = msg.routes
		tm.neighbours = msg.neighbours
		tm.firewall = msg.firewall
		tm.firewallSrc = msg.firewallSrc
		tm.loadErr = ""
	}
	tm.state = topoStateReady
	tm.viewport.SetContent(tm.buildViewportContent())
	tm.viewport.GotoTop()
	return tm, nil
}

func (tm *TopologyModel) handleKey(msg tea.KeyMsg) (*TopologyModel, tea.Cmd) {
	if tm.state != topoStateReady {
		return tm, nil
	}
	switch msg.String() {
	case "up":
		tm.viewport.ScrollUp(1)
	case "down":
		tm.viewport.ScrollDown(1)
	case "pgup":
		tm.viewport.HalfPageUp()
	case "pgdown":
		tm.viewport.HalfPageDown()
	case "home":
		tm.viewport.GotoTop()
	case "end":
		tm.viewport.GotoBottom()
	case "ctrl+r":
		tm.state = topoStateLoading
		tm.loadErr = ""
		return tm, tea.Batch(fetchTopoDataCmd(tm.image), tm.spinner.Tick)
	}
	return tm, nil
}

// buildViewportContent builds the full scrollable content for the topology viewport.
func (tm *TopologyModel) buildViewportContent() string {
	w := max(tm.viewport.Width, 20)
	sep := strings.Repeat("─", max(w-2, 10))

	var lines []string
	lines = append(lines, theme.EmptyLineBg(w))

	// --- Section: Network Interfaces ---
	lines = append(lines, theme.PadWithBg(theme.SubTitleStyle.Render("  "+theme.IconNetwork+" Network Interfaces"), w))
	lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  "+sep), w))

	if tm.loadErr != "" {
		lines = append(lines, theme.PadWithBg(theme.StatusErrorStyle.Render("  "+tm.loadErr), w))
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  ctrl+r — retry"), w))
	} else if len(tm.interfaces) == 0 {
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  No interfaces found"), w))
	} else {
		for _, iface := range tm.interfaces {
			stateStr, stateStyle := ifaceStateStyle(iface.State)
			namePart := topopad(iface.Name, 14)
			statePart := stateStyle.Render(topopad(stateStr, 6))
			mtuPart := theme.DimStyle.Render(fmt.Sprintf("MTU %-5d", iface.MTU))
			firstIP := ""
			if len(iface.Addresses) > 0 {
				firstIP = iface.Addresses[0]
			}
			line := theme.Bg("  "+theme.IconNetwork+"  "+namePart+"  ") + statePart + theme.Bg("  ") + mtuPart + theme.Bg("  "+firstIP)
			lines = append(lines, theme.PadWithBg(line, w))
			for i := 1; i < len(iface.Addresses); i++ {
				lines = append(lines, theme.PadWithBg(theme.DimStyle.Render(strings.Repeat(" ", 40)+iface.Addresses[i]), w))
			}
		}
	}

	// --- Section: Network Errors (conditional) ---
	hasErrors := false
	for _, iface := range tm.interfaces {
		if iface.RxErrors > 0 || iface.TxErrors > 0 {
			hasErrors = true
			break
		}
	}
	if hasErrors {
		lines = append(lines, theme.EmptyLineBg(w))
		lines = append(lines, theme.PadWithBg(theme.SubTitleStyle.Render("  "+theme.IconWarning+" Network Errors"), w))
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  "+sep), w))
		for _, iface := range tm.interfaces {
			if iface.RxErrors == 0 && iface.TxErrors == 0 {
				continue
			}
			rxStr := fmt.Sprintf("RX: %d", iface.RxErrors)
			txStr := fmt.Sprintf("TX: %d", iface.TxErrors)
			namePart := topopad(iface.Name, 14)
			rxPart := theme.StatusWarningStyle.Render(rxStr)
			txPart := theme.StatusWarningStyle.Render(txStr)
			line := theme.Bg("  "+namePart+"  ") + rxPart + theme.Bg("   ") + txPart
			lines = append(lines, theme.PadWithBg(line, w))
		}
	}

	lines = append(lines, theme.EmptyLineBg(w))

	// --- Section: Routing Table ---
	lines = append(lines, theme.PadWithBg(theme.SubTitleStyle.Render("  "+theme.IconArrowRight+" Routing Table"), w))
	lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  "+sep), w))

	if len(tm.routes) == 0 && tm.loadErr == "" {
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  No routes found"), w))
	}
	for _, route := range tm.routes {
		destPart := topopad(route.Destination, 22)
		var gwPart string
		if route.Gateway != "" {
			gwPart = "via " + topopad(route.Gateway, 18)
		} else {
			gwPart = topopad("direct", 22)
		}
		devPart := "dev " + route.Interface
		line := theme.Bg("  " + theme.IconArrowRight + "  " + destPart + "  " + gwPart + "  " + devPart)
		lines = append(lines, theme.PadWithBg(line, w))
	}

	lines = append(lines, theme.EmptyLineBg(w))

	// --- Section: ARP / Neighbours ---
	lines = append(lines, theme.PadWithBg(theme.SubTitleStyle.Render("  "+theme.IconNetwork+" ARP / Neighbours"), w))
	lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  "+sep), w))

	if len(tm.neighbours) == 0 {
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  No neighbours found"), w))
	} else {
		for _, n := range tm.neighbours {
			ipPart := topopad(n.IP, 18)
			mac := n.MAC
			if mac == "" {
				mac = "(incomplete)"
			}
			macPart := topopad(mac, 20)
			ifacePart := topopad(n.Interface, 10)
			statePart := neighStateStyle(n.State)
			line := theme.Bg("  "+ipPart+"  "+macPart+"  "+ifacePart+"  ") + statePart
			lines = append(lines, theme.PadWithBg(line, w))
		}
	}

	lines = append(lines, theme.EmptyLineBg(w))

	// --- Section: Firewall Summary ---
	fwTitle := "  " + theme.IconTarget + " Firewall Summary"
	if tm.firewallSrc == "iptables" || tm.firewallSrc == "nftables" {
		fwTitle += theme.DimStyle.Render("  [" + tm.firewallSrc + "]")
	}
	lines = append(lines, theme.PadWithBg(theme.SubTitleStyle.Render(fwTitle), w))
	lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  "+sep), w))

	switch tm.firewallSrc {
	case "unavailable", "":
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  Firewall status unavailable (iptables/nft not found in image)"), w))
	default:
		if len(tm.firewall) == 0 {
			lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  No chains found"), w))
		} else {
			for _, chain := range tm.firewall {
				namePart := topopad(chain.Name, 28)
				policyPart := renderFWPolicy(chain.Policy)
				var rulesStr string
				if chain.Rules == 0 {
					rulesStr = theme.DimStyle.Render("0 rules")
				} else {
					rulesStr = fmt.Sprintf("%d rules", chain.Rules)
				}
				line := theme.Bg("  "+namePart+"  ") + policyPart + theme.Bg("  "+rulesStr)
				lines = append(lines, theme.PadWithBg(line, w))
			}
		}
	}

	lines = append(lines, theme.EmptyLineBg(w))
	return strings.Join(lines, "\n")
}

// statusLine is what the Topology tab derives on every frame: the load, which
// is reported in the footer with a spinner rather than replacing the pane.
func (tm *TopologyModel) statusLine() components.Status {
	if tm.state == topoStateLoading {
		return components.Status{Text: "Loading network data...", Spinner: true}
	}
	return components.Status{}
}

func (tm *TopologyModel) view() string {
	w := max(tm.width-2, 20)

	// The load says so in the footer, with a spinner (statusLine), so the pane
	// stays blank rather than carrying a second copy of the same message.
	if tm.state == topoStateLoading {
		lines := make([]string, 0, tm.height)
		for range tm.height {
			lines = append(lines, theme.EmptyLineBg(w))
		}
		return strings.Join(lines, "\n")
	}

	return tm.viewport.View()
}

// --- Parsers ---

// parseIPAddr parses the output of `ip addr show` into a slice of InterfaceInfo.
func parseIPAddr(raw string) []InterfaceInfo {
	var result []InterfaceInfo
	var current *InterfaceInfo

	for line := range strings.SplitSeq(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		// Interface header: first field is a number with colon (e.g., "1:", "2:")
		if numStr, ok := strings.CutSuffix(fields[0], ":"); ok {
			if _, err := strconv.Atoi(numStr); err == nil && len(fields) >= 3 {
				if current != nil {
					result = append(result, *current)
				}
				name := strings.TrimSuffix(fields[1], ":")
				// Strip @<parent> suffix for VLAN/tunnel interfaces (e.g., "eth0@if5")
				if idx := strings.Index(name, "@"); idx != -1 {
					name = name[:idx]
				}
				current = &InterfaceInfo{Name: name, State: parseIfaceState(fields[2])}
				// Extract MTU and operational state from the header line
				// (e.g., "mtu 1500 qdisc noqueue state DOWN").
				for i := 3; i+1 < len(fields); i++ {
					switch fields[i] {
					case "mtu":
						if v, err := strconv.Atoi(fields[i+1]); err == nil {
							current.MTU = v
						}
					case "state":
						// The flags only carry the admin state, so an unplugged NIC
						// still advertises UP. The operational state is authoritative
						// when the driver reports one. Loopback keeps its own label.
						if current.State != "LOOP" {
							if s, ok := operState(fields[i+1]); ok {
								current.State = s
							}
						}
					}
				}
				continue
			}
		}

		// IPv4/IPv6 address lines (indented in the output)
		if current != nil && len(fields) >= 2 && (fields[0] == "inet" || fields[0] == "inet6") {
			current.Addresses = append(current.Addresses, fields[1])
		}
	}

	if current != nil {
		result = append(result, *current)
	}
	return result
}

// operState maps the operational state token of `ip addr show` onto the label
// shown in the topology table. The boolean is false for UNKNOWN, which drivers
// report when they cannot determine a carrier state (tun/tap devices always do);
// callers should then fall back to the interface flags.
func operState(token string) (string, bool) {
	switch strings.ToUpper(token) {
	case "UP":
		return "UP", true
	case "UNKNOWN":
		return "", false
	default:
		return "DOWN", true
	}
}

// parseIfaceState extracts the interface state from its flags string (e.g., "<LOOPBACK,UP,LOWER_UP>").
func parseIfaceState(flags string) string {
	up := strings.ToUpper(flags)
	switch {
	case strings.Contains(up, "LOOPBACK"):
		return "LOOP"
	case strings.Contains(up, ",UP,") || strings.Contains(up, ",UP>") || up == "<UP>":
		return "UP"
	default:
		return "DOWN"
	}
}

// parseIPLink merges MTU and RX/TX error stats from `ip -s link show` into an existing
// slice of InterfaceInfo keyed by name. Unmatched interfaces are ignored.
func parseIPLink(raw string, ifaces []InterfaceInfo) []InterfaceInfo {
	// Build a name → index map for fast lookup
	idx := make(map[string]int, len(ifaces))
	for i, iface := range ifaces {
		idx[iface.Name] = i
	}

	var currentName string
	rxHeaderSeen := false

	for line := range strings.SplitSeq(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		// Interface header: "2: eth0: <FLAGS> mtu 1500 ..."
		if numStr, ok := strings.CutSuffix(fields[0], ":"); ok {
			if _, err := strconv.Atoi(numStr); err == nil && len(fields) >= 2 {
				rxHeaderSeen = false
				name := strings.TrimSuffix(fields[1], ":")
				if i := strings.Index(name, "@"); i != -1 {
					name = name[:i]
				}
				currentName = name
				// MTU is already parsed from ip addr show; skip here to avoid duplicates.
				continue
			}
		}

		if currentName == "" {
			continue
		}

		// "RX:  bytes   packets   errors   dropped ..." header line
		if fields[0] == "RX:" {
			rxHeaderSeen = true
			continue
		}
		// "TX:  bytes   packets   errors   dropped ..." header line
		if fields[0] == "TX:" {
			rxHeaderSeen = false
			// Next line holds TX stats
			continue
		}

		// Stat value lines follow the RX: / TX: headers
		// Format: "  bytes   packets   errors   dropped   missed   mcast"
		// errors is the 3rd column (index 2)
		if i, ok := idx[currentName]; ok && len(fields) >= 3 {
			errVal, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil {
				continue
			}
			if rxHeaderSeen {
				ifaces[i].RxErrors = errVal
				rxHeaderSeen = false // consumed
			} else {
				ifaces[i].TxErrors = errVal
			}
		}
	}
	return ifaces
}

// parseIPRoute parses the output of `ip route show` into a slice of RouteInfo.
func parseIPRoute(raw string) []RouteInfo {
	var result []RouteInfo
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		route := RouteInfo{Destination: fields[0]}
		for i := 1; i+1 < len(fields); i++ {
			switch fields[i] {
			case "via":
				route.Gateway = fields[i+1]
			case "dev":
				route.Interface = fields[i+1]
			}
		}
		result = append(result, route)
	}
	return result
}

// parseIPNeigh parses the output of `ip neigh show` into a slice of NeighbourInfo.
func parseIPNeigh(raw string) []NeighbourInfo {
	var result []NeighbourInfo
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		n := NeighbourInfo{IP: fields[0]}
		// Walk fields looking for "dev", "lladdr", and trailing state token
		for i := 1; i < len(fields); i++ {
			switch fields[i] {
			case "dev":
				if i+1 < len(fields) {
					n.Interface = fields[i+1]
					i++
				}
			case "lladdr":
				if i+1 < len(fields) {
					n.MAC = fields[i+1]
					i++
				}
			}
		}
		// Last field is the state token
		n.State = fields[len(fields)-1]
		result = append(result, n)
	}
	return result
}

// parseFirewall dispatches to the right parser based on the output prefix.
// Returns (chains, source) where source is "iptables", "nftables", or "unavailable".
func parseFirewall(raw string) ([]FirewallChain, string) {
	if body, ok := strings.CutPrefix(raw, "IPTABLES:"); ok {
		return parseFirewallIPTables(strings.TrimSpace(body)), "iptables"
	}
	if body, ok := strings.CutPrefix(raw, "NFTABLES:"); ok {
		return parseFirewallNFTables(strings.TrimSpace(body)), "nftables"
	}
	return nil, "unavailable"
}

// parseFirewallIPTables parses `iptables -L -n --line-numbers` output.
func parseFirewallIPTables(raw string) []FirewallChain {
	var result []FirewallChain
	var current *FirewallChain

	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Chain header: "Chain INPUT (policy ACCEPT)" or "Chain DOCKER-USER (1 references)"
		if strings.HasPrefix(line, "Chain ") {
			if current != nil {
				result = append(result, *current)
			}
			fields := strings.Fields(line)
			chain := FirewallChain{Name: fields[1], Policy: "n/a"}
			// Extract policy from "(policy ACCEPT)"
			if len(fields) >= 4 && fields[2] == "(policy" {
				chain.Policy = strings.TrimSuffix(fields[3], ")")
			}
			current = &chain
			continue
		}
		// Skip the column header line ("num  target  prot ...")
		if strings.HasPrefix(line, "num") || strings.HasPrefix(line, "target") {
			continue
		}
		// Count rule lines (non-empty, non-header lines within a chain block)
		if current != nil {
			current.Rules++
		}
	}
	if current != nil {
		result = append(result, *current)
	}
	return result
}

// parseFirewallNFTables parses `nft list ruleset` output.
func parseFirewallNFTables(raw string) []FirewallChain {
	var result []FirewallChain
	var current *FirewallChain

	for line := range strings.SplitSeq(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Chain block: "\tchain input {"
		if strings.HasPrefix(trimmed, "chain ") {
			if current != nil {
				result = append(result, *current)
			}
			fields := strings.Fields(trimmed)
			current = &FirewallChain{Name: fields[1], Policy: "n/a"}
			continue
		}
		if current == nil {
			continue
		}
		// Policy line: "type filter hook input priority 0; policy accept;"
		if strings.HasPrefix(trimmed, "type ") {
			for part := range strings.SplitSeq(trimmed, ";") {
				part = strings.TrimSpace(part)
				if policy, ok := strings.CutPrefix(part, "policy "); ok {
					current.Policy = strings.ToUpper(policy)
				}
			}
			continue
		}
		// End of chain block
		if trimmed == "}" {
			result = append(result, *current)
			current = nil
			continue
		}
		// Count rule lines
		current.Rules++
	}
	if current != nil {
		result = append(result, *current)
	}
	return result
}

// --- Helpers ---

// topopad pads or truncates s to the given ASCII width.
func topopad(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// ifaceStateStyle returns a display label and lipgloss style for an interface state.
func ifaceStateStyle(state string) (string, lipgloss.Style) {
	switch state {
	case "UP":
		return "UP", theme.StatusOKStyle
	case "DOWN":
		return "DOWN", theme.StatusErrorStyle
	case "LOOP":
		return "LOOP", theme.DimStyle
	default:
		return state, theme.DimStyle
	}
}

// neighStateStyle returns a styled string for a neighbour state.
func neighStateStyle(state string) string {
	switch state {
	case "REACHABLE", "PERMANENT":
		return theme.StatusOKStyle.Render(state)
	case "FAILED":
		return theme.StatusErrorStyle.Render(state)
	default:
		return theme.DimStyle.Render(state)
	}
}

// renderFWPolicy returns a styled string for a firewall policy.
func renderFWPolicy(policy string) string {
	switch policy {
	case "DROP", "REJECT":
		return theme.StatusErrorStyle.Render(topopad(policy, 10))
	default:
		return theme.Bg(topopad(policy, 10))
	}
}
