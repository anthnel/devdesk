package netdiag

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
	dockerpkg "gitlab.com/anthnell/devsecops/devdesk/internal/docker"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/help"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/shortcut"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// ViewState represents the current state of the netdiag view
type ViewState int

const (
	StateInput   ViewState = iota // Form: target, port, checkboxes, button
	StateRunning                  // Tests running in parallel
	StateResults                  // Summary results table
	StateDetails                  // Full log output for selected test
)

// Tab indices
const (
	tabDiagnostics = 0
	tabPorts       = 1
	tabTopology    = 2
)

// Form field indices
const (
	fieldTarget        = 0
	fieldPort          = 1
	fieldDNSServer     = 2
	fieldPing          = 3
	fieldDNS           = 4
	fieldTraceroute    = 5
	fieldTCPTraceroute = 6
	fieldNetcat        = 7
	fieldCurl          = 8
	fieldSSL           = 9
	fieldButton        = 10
	fieldCount         = 11
)

// testDef defines a diagnostic test
type testDef struct {
	name     string
	enabled  bool
	fieldIdx int
}

// testResult holds the result of a single test
type testResult struct {
	name      string
	success   bool
	output    string
	done      bool
	cancelled bool
}

// testCompleteMsg is sent when a single test finishes
type testCompleteMsg struct {
	gen     int // must match Model.runGen; stale results are discarded
	name    string
	success bool
	output  string
}

// clearFooterMsg clears the footer message after 3 seconds
type clearFooterMsg struct{}

func clearFooterCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearFooterMsg{}
	})
}

// Model represents the network diagnostics view
type Model struct {
	config *config.Config
	width  int
	height int

	// Active tab (tabDiagnostics, tabPorts, or tabTopology)
	activeTab     int
	portsModel    *PortsModel
	topologyModel *TopologyModel

	state ViewState

	// Form inputs
	targetInput    textinput.Model
	portInput      textinput.Model
	dnsServerInput textinput.Model
	focusedField   int

	// Test definitions (order matches fieldPing..fieldSSL)
	tests []testDef

	// Running state
	spinner    spinner.Model
	results    map[string]testResult
	totalTests int
	doneTests  int
	runGen     int // incremented each run; stale testCompleteMsgs are discarded

	// Results table
	resultsTable table.Model
	resultOrder  []string // test names in display order

	// Details view
	detailsViewport viewport.Model
	selectedTest    string
	rawDetails      bool // true = raw output, false = formatted (traceroute only)

	// Footer
	footerError string
	footerInfo  string
}

// isIPAddress reports whether s is a valid IPv4 or IPv6 address.
func isIPAddress(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}

// maxHostnameLen is the maximum total length of a DNS name (RFC 1035).
const maxHostnameLen = 253

// defaultPort is used when the port field is left empty.
const defaultPort = "443"

// capitalize upper-cases the first letter, so lowercase error strings can be
// reused as footer messages (Rule 137).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// isHostname reports whether s is a syntactically valid DNS hostname:
// dot-separated labels of 1-63 chars, each made of letters, digits and hyphens,
// and not starting or ending with a hyphen.
func isHostname(s string) bool {
	if s == "" || len(s) > maxHostnameLen {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !isAlphaNum && r != '-' {
				return false
			}
		}
	}
	return true
}

// validateTarget checks that the target is a usable IP address or hostname.
// Targets reach external tools as command arguments, so rejecting anything that
// is not a plain host keeps shell metacharacters out of the diagnostic commands.
func validateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("target is required")
	}
	if isIPAddress(target) || isHostname(target) {
		return nil
	}
	return fmt.Errorf("target must be a hostname or IP address")
}

// validatePort checks that the port is a decimal number in the 1-65535 range.
func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	return nil
}

// defaultDNSServer reads the first nameserver entry from /etc/resolv.conf.
// Returns an empty string if the file is unavailable or has no nameserver line.
func defaultDNSServer() string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only file, close error is irrelevant
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// New creates a new netdiag view
func New(cfg *config.Config) *Model {
	targetIn := textinput.New()
	theme.StyleTextInput(&targetIn)
	targetIn.Placeholder = "hostname or IP address"
	targetIn.CharLimit = 256
	targetIn.Focus()

	portIn := textinput.New()
	theme.StyleTextInput(&portIn)
	portIn.Placeholder = "443"
	portIn.CharLimit = 10

	dnsIn := textinput.New()
	theme.StyleTextInput(&dnsIn)
	dnsIn.Placeholder = "1.1.1.1"
	dnsIn.CharLimit = 64
	dnsIn.SetValue(defaultDNSServer())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	tests := []testDef{
		{name: "Ping", enabled: true, fieldIdx: fieldPing},
		{name: "DNS Resolution", enabled: true, fieldIdx: fieldDNS},
		{name: "Traceroute", enabled: true, fieldIdx: fieldTraceroute},
		{name: "TCP Traceroute", enabled: true, fieldIdx: fieldTCPTraceroute},
		{name: "Netcat", enabled: true, fieldIdx: fieldNetcat},
		{name: "HTTP/HTTPS (Curl)", enabled: true, fieldIdx: fieldCurl},
		{name: "SSL Certificate", enabled: true, fieldIdx: fieldSSL},
	}

	return &Model{
		config:         cfg,
		state:          StateInput,
		activeTab:      tabDiagnostics,
		portsModel:     newPortsModel(cfg.Docker.NetworkToolImage),
		topologyModel:  newTopologyModel(cfg.Docker.NetworkToolImage),
		targetInput:    targetIn,
		portInput:      portIn,
		dnsServerInput: dnsIn,
		focusedField:   fieldTarget,
		tests:          tests,
		spinner:        sp,
		results:        make(map[string]testResult),
	}
}

// Init implements tea.Model
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.portsModel.initPorts(), m.topologyModel.initTopology())
}

// AllowCommandMode implements app.CommandModeView — signals that pressing ":"
// should enter command mode when the topology tab is active and data is loaded.
func (m *Model) AllowCommandMode() bool {
	return m.activeTab == tabTopology && m.topologyModel.state == topoStateReady
}

// InEditMode implements FormView — true when a text input is active, tests are running,
// or the detail view is open (so esc is forwarded to the view instead of consumed by the app).
func (m *Model) InEditMode() bool {
	switch m.activeTab {
	case tabPorts:
		return m.portsModel.InEditMode()
	case tabTopology:
		return false
	}
	switch m.state {
	case StateRunning, StateDetails:
		return true
	case StateInput:
		return m.focusedField == fieldTarget || m.focusedField == fieldPort || m.focusedField == fieldDNSServer
	}
	return false
}

// FilterBarVisible returns true when the ports filter bar is visible (implements app.FilterBarView).
func (m *Model) FilterBarVisible() bool {
	return m.activeTab == tabPorts && m.portsModel.filterBar.IsVisible()
}

// Update implements tea.Model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInputs()
		m.rebuildResultsTable()
		m.resizeDetailsViewport()
		m.portsModel.resize(msg.Width, msg.Height)
		m.topologyModel.resize(msg.Width, msg.Height)
		return m, nil

	case spinner.TickMsg:
		if m.state == StateRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		if m.activeTab == tabTopology {
			var cmd tea.Cmd
			m.topologyModel, cmd = m.topologyModel.update(msg)
			return m, cmd
		}
		return m, nil

	case testCompleteMsg:
		return m.handleTestComplete(msg)

	case clearFooterMsg:
		m.footerError = ""
		m.footerInfo = ""
		return m, nil

	// Ports sub-model messages
	case portsTickMsg, portsDataMsg, portsKillResultMsg, portsClearFooterMsg:
		var cmd tea.Cmd
		m.portsModel, cmd = m.portsModel.update(msg)
		return m, cmd

	// Topology sub-model messages
	case topoDataMsg:
		var cmd tea.Cmd
		m.topologyModel, cmd = m.topologyModel.update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleTestComplete(msg testCompleteMsg) (*Model, tea.Cmd) {
	if msg.gen != m.runGen {
		return m, nil // stale result from a cancelled run
	}
	m.results[msg.name] = testResult{
		name:    msg.name,
		success: msg.success,
		output:  msg.output,
		done:    true,
	}
	m.doneTests++
	log.Printf("INFO [netdiag] test %q done: success=%v", msg.name, msg.success)

	if m.doneTests >= m.totalTests {
		m.state = StateResults
		m.rebuildResultsTable()
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	// Tab / Shift+Tab cycles between the three tabs (Rule 135)
	switch msg.String() {
	case "tab":
		m.activeTab = (m.activeTab + 1) % 3
		return m, nil
	case "shift+tab":
		m.activeTab = (m.activeTab + 2) % 3
		return m, nil
	}

	// Route to active tab
	if m.activeTab == tabPorts {
		var cmd tea.Cmd
		m.portsModel, cmd = m.portsModel.update(msg)
		return m, cmd
	}
	if m.activeTab == tabTopology {
		var cmd tea.Cmd
		m.topologyModel, cmd = m.topologyModel.update(msg)
		return m, cmd
	}

	switch m.state {
	case StateInput:
		return m.handleKeyInput(msg)
	case StateRunning:
		return m.handleKeyRunning(msg)
	case StateResults:
		return m.handleKeyResults(msg)
	case StateDetails:
		return m.handleKeyDetails(msg)
	}
	return m, nil
}

func (m *Model) handleKeyRunning(msg tea.KeyMsg) (*Model, tea.Cmd) {
	if msg.String() != "esc" {
		return m, nil
	}
	// Invalidate the current run so arriving testCompleteMsgs are discarded
	m.runGen++
	// Mark all non-done tests as cancelled
	for _, name := range m.resultOrder {
		if res := m.results[name]; !res.done {
			m.results[name] = testResult{name: name, done: true, cancelled: true, output: "cancelled"}
			m.doneTests++
		}
	}
	m.state = StateResults
	m.rebuildResultsTable()
	return m, nil
}

func (m *Model) handleKeyInput(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "down":
		m.moveFocus(1)
		return m, nil
	case "up":
		m.moveFocus(-1)
		return m, nil
	case "enter":
		if m.focusedField == fieldButton {
			return m.startTests()
		}
		return m, nil
	case " ":
		// Space is the only key that toggles checkboxes (Rule 135)
		m.toggleFocusedCheckbox()
		return m, nil
	case "esc":
		return m, nil
	}

	// Forward to active text input
	return m.updateActiveInput(msg)
}

func (m *Model) moveFocus(delta int) {
	// Blur current input if leaving a text field
	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Blur()
	case fieldPort:
		m.portInput.Blur()
	case fieldDNSServer:
		m.dnsServerInput.Blur()
	}

	m.focusedField = (m.focusedField + delta + fieldCount) % fieldCount

	// Focus new input if entering a text field
	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Focus()
	case fieldPort:
		m.portInput.Focus()
	case fieldDNSServer:
		m.dnsServerInput.Focus()
	}
}

func (m *Model) toggleFocusedCheckbox() {
	for i := range m.tests {
		if m.tests[i].fieldIdx == m.focusedField {
			m.tests[i].enabled = !m.tests[i].enabled
			return
		}
	}
}

func (m *Model) updateActiveInput(msg tea.KeyMsg) (*Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focusedField {
	case fieldTarget:
		m.targetInput, cmd = m.targetInput.Update(msg)
	case fieldPort:
		m.portInput, cmd = m.portInput.Update(msg)
	case fieldDNSServer:
		m.dnsServerInput, cmd = m.dnsServerInput.Update(msg)
	}
	return m, cmd
}

func (m *Model) handleKeyResults(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.openDetails()
	case "ctrl+r", "r":
		return m.resetToForm()
	case "up", "k":
		m.resultsTable.MoveUp(1)
	case "down", "j":
		m.resultsTable.MoveDown(1)
	case "g":
		m.resultsTable.GotoTop()
	case "G":
		m.resultsTable.GotoBottom()
	}
	return m, nil
}

func (m *Model) handleKeyDetails(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = StateResults
		return m, nil
	case "f":
		m.rawDetails = !m.rawDetails
		res := m.results[m.selectedTest]
		m.detailsViewport.SetContent(m.renderDetailsContent(res, m.detailsViewport.Width))
		m.detailsViewport.GotoTop()
	case "up", "k":
		m.detailsViewport.ScrollUp(1)
	case "down", "j":
		m.detailsViewport.ScrollDown(1)
	case "pgup":
		m.detailsViewport.HalfPageUp()
	case "pgdown":
		m.detailsViewport.HalfPageDown()
	case "g":
		m.detailsViewport.GotoTop()
	case "G":
		m.detailsViewport.GotoBottom()
	}
	return m, nil
}

func (m *Model) openDetails() (*Model, tea.Cmd) {
	idx := m.resultsTable.Cursor()
	if idx < 0 || idx >= len(m.resultOrder) {
		return m, nil
	}
	name := m.resultOrder[idx]
	res, ok := m.results[name]
	if !ok {
		return m, nil
	}
	m.selectedTest = name
	m.rawDetails = false
	m.state = StateDetails
	m.resizeDetailsViewport()
	m.detailsViewport.SetContent(m.renderDetailsContent(res, m.detailsViewport.Width))
	m.detailsViewport.GotoTop()
	return m, nil
}

func (m *Model) resetToForm() (*Model, tea.Cmd) {
	m.state = StateInput
	m.focusedField = fieldTarget
	m.targetInput.Focus()
	m.results = make(map[string]testResult)
	m.doneTests = 0
	m.totalTests = 0
	m.resultOrder = nil
	return m, nil
}

func (m *Model) startTests() (*Model, tea.Cmd) {
	target := strings.TrimSpace(m.targetInput.Value())
	if err := validateTarget(target); err != nil {
		m.footerError = capitalize(err.Error())
		return m, clearFooterCmd()
	}

	port := strings.TrimSpace(m.portInput.Value())
	if port == "" {
		port = defaultPort
	}
	if err := validatePort(port); err != nil {
		m.footerError = capitalize(err.Error())
		return m, clearFooterCmd()
	}

	enabled := m.enabledTests()
	if len(enabled) == 0 {
		m.footerError = "Select at least one test"
		return m, clearFooterCmd()
	}

	m.runGen++
	gen := m.runGen
	m.state = StateRunning
	m.results = make(map[string]testResult)
	m.resultOrder = nil
	m.doneTests = 0
	m.totalTests = len(enabled)

	dnsServer := strings.TrimSpace(m.dnsServerInput.Value())
	image := m.config.Docker.NetworkToolImage

	var cmds []tea.Cmd
	cmds = append(cmds, m.spinner.Tick)

	for _, t := range enabled {
		name := t.name
		m.results[name] = testResult{name: name, done: false}
		m.resultOrder = append(m.resultOrder, name)

		var cmd tea.Cmd
		switch name {
		case "Ping":
			cmd = runTestCmd(gen, name, func() dockerpkg.DiagResult {
				return dockerpkg.RunPing(image, target)
			})
		case "DNS Resolution":
			cmd = m.buildDNSTestCmd(gen, target, dnsServer, image)
		case "Traceroute":
			cmd = runTestCmd(gen, name, func() dockerpkg.DiagResult {
				return dockerpkg.RunTraceroute(image, target)
			})
		case "TCP Traceroute":
			cmd = runTestCmd(gen, name, func() dockerpkg.DiagResult {
				return dockerpkg.RunTCPTraceroute(image, target, port)
			})
		case "Netcat":
			cmd = runTestCmd(gen, name, func() dockerpkg.DiagResult {
				return dockerpkg.RunNetcat(image, target, port)
			})
		case "HTTP/HTTPS (Curl)":
			cmd = runTestCmd(gen, name, func() dockerpkg.DiagResult {
				return dockerpkg.RunCurl(image, target, port)
			})
		case "SSL Certificate":
			cmd = runTestCmd(gen, name, func() dockerpkg.DiagResult {
				return dockerpkg.RunSSLCert(image, target, port)
			})
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// buildDNSTestCmd builds the DNS or reverse DNS test command depending on the target.
// If the target is an IP address, it uses dig -x for PTR lookup and relabels the result row.
func (m *Model) buildDNSTestCmd(gen int, target, dnsServer, image string) tea.Cmd {
	effectiveName := "DNS Resolution"
	runner := func() dockerpkg.DiagResult {
		return dockerpkg.RunDNS(image, target, dnsServer)
	}
	if isIPAddress(target) {
		effectiveName = "Reverse DNS"
		runner = func() dockerpkg.DiagResult {
			return dockerpkg.RunReverseDNS(image, target, dnsServer)
		}
	}
	// Rename the key/order entry that was set before the switch in startTests()
	m.results[effectiveName] = m.results["DNS Resolution"]
	delete(m.results, "DNS Resolution")
	m.resultOrder[len(m.resultOrder)-1] = effectiveName
	return runTestCmd(gen, effectiveName, runner)
}

// runTestCmd creates a tea.Cmd that runs fn and returns a testCompleteMsg tagged with gen
func runTestCmd(gen int, name string, fn func() dockerpkg.DiagResult) tea.Cmd {
	return func() tea.Msg {
		res := fn()
		return testCompleteMsg{gen: gen, name: name, success: res.Success, output: res.Output}
	}
}

func (m *Model) enabledTests() []testDef {
	var out []testDef
	for _, t := range m.tests {
		if t.enabled {
			out = append(out, t)
		}
	}
	return out
}

// View implements tea.Model
func (m *Model) View() string {
	switch m.activeTab {
	case tabPorts:
		return m.portsModel.view()
	case tabTopology:
		return m.topologyModel.view()
	}
	switch m.state {
	case StateInput:
		return m.renderInputForm()
	case StateRunning:
		return m.renderRunning()
	case StateResults:
		return m.renderResults()
	case StateDetails:
		return m.detailsViewport.View()
	}
	return ""
}

func (m *Model) renderInputForm() string {
	var b strings.Builder

	// Target field
	b.WriteString(m.renderTextInputField("Target", m.targetInput, fieldTarget))
	b.WriteString("\n")

	// Port field
	b.WriteString(m.renderTextInputField("Port", m.portInput, fieldPort))
	b.WriteString("\n")

	// DNS Server field
	b.WriteString(m.renderTextInputField("DNS Server", m.dnsServerInput, fieldDNSServer))
	b.WriteString("\n\n")

	// Tests section
	b.WriteString(theme.SubTitleStyle.Render(theme.IconConfig + " Diagnostic Tests"))
	b.WriteString("\n\n")

	for _, t := range m.tests {
		b.WriteString(theme.RenderCheckbox(t.enabled, t.name, m.focusedField == t.fieldIdx))
		b.WriteString("\n")
	}

	// Start button
	b.WriteString("\n")
	b.WriteString("  " + theme.RenderButton("Run Diagnostics", m.focusedField == fieldButton, "primary"))

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(b.String())
}

func (m *Model) renderTextInputField(label string, input textinput.Model, fieldIdx int) string {
	prefix := "  "
	if m.focusedField == fieldIdx {
		prefix = theme.IconCircleSmall + " "
	}
	return theme.KeyStyle.Render(prefix+label+" "+theme.IconChevronRight+" ") + input.View()
}

func (m *Model) renderRunning() string {
	w := max(m.width-2, 20)

	var lines []string
	lines = append(lines, theme.EmptyLineBg(w))

	title := theme.SpinnerMessage(m.spinner.View(), "Running diagnostics...")
	lines = append(lines, theme.PadWithBg(theme.Bg("  ")+title, w))
	lines = append(lines, theme.EmptyLineBg(w))

	for _, name := range m.resultOrder {
		res := m.results[name]
		var label string
		switch {
		case !res.done:
			label = theme.Bg("  ") + theme.SpinnerMessage(m.spinner.View(), name)
		case res.cancelled:
			label = theme.Bg("  ") + theme.DimStyle.Render(theme.IconCanceled+" "+name)
		case res.success:
			label = theme.Bg("  ") + theme.StatusOKStyle.Render(theme.IconOK) + theme.Bg(" "+name)
		default:
			label = theme.Bg("  ") + theme.StatusErrorStyle.Render(theme.IconError) + theme.Bg(" "+name)
		}
		lines = append(lines, theme.PadWithBg(label, w))
	}

	lines = append(lines, theme.EmptyLineBg(w))
	prog := fmt.Sprintf("  %d / %d tests completed", m.doneTests, m.totalTests)
	lines = append(lines, theme.PadWithBg(theme.Bg(prog), w))

	return strings.Join(lines, "\n")
}

func (m *Model) renderResults() string {
	return m.resultsTable.View()
}

func (m *Model) renderDetailsContent(res testResult, width int) string {
	var lines []string
	lines = append(lines, theme.EmptyLineBg(width))

	output := res.output
	if output == "" {
		output = "(no output)"
	}

	switch {
	case !m.rawDetails && (res.name == "Traceroute" || res.name == "TCP Traceroute"):
		lines = append(lines, formatTracerouteOutput(output, width)...)
	case !m.rawDetails && (res.name == "DNS Resolution" || res.name == "Reverse DNS"):
		lines = append(lines, formatDNSOutput(output, width)...)
	default:
		lineStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
		for line := range strings.SplitSeq(output, "\n") {
			lines = append(lines, theme.PadWithBg(lineStyle.Render(line), width))
		}
	}

	return strings.Join(lines, "\n")
}

func (m *Model) rebuildResultsTable() {
	if m.state != StateResults {
		return
	}

	w := max(m.width-2, 30)

	numCols := 3
	available := w - numCols*2
	col1W := 22
	col2W := 6
	col3W := max(available-col1W-col2W, 10)

	cols := []table.Column{
		{Title: "Test", Width: col1W},
		{Title: "Status", Width: col2W},
		{Title: "Output", Width: col3W},
	}

	var rows []table.Row
	for _, name := range m.resultOrder {
		res, ok := m.results[name]
		if !ok {
			continue
		}
		var statusIcon string
		switch {
		case res.cancelled:
			statusIcon = theme.IconCanceled + " —"
		case res.success:
			statusIcon = theme.IconOK + " OK"
		default:
			statusIcon = theme.IconError + " FAIL"
		}
		firstLine := firstOutputLine(res.output, col3W)
		rows = append(rows, table.Row{name, statusIcon, firstLine})
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(max(m.height-13, 3)),
	)
	t.SetStyles(theme.DefaultTableStyles())
	m.resultsTable = t
}

func (m *Model) resizeInputs() {
	// Viewport border (2) + form padding (2) + prefix (2) + longest label "DNS Server " + chevron + spaces (~15)
	const labelOverhead = 21
	wide := max(m.width-labelOverhead, 20)
	m.targetInput.Width = wide
	m.dnsServerInput.Width = wide
	m.portInput.Width = 10
}

func (m *Model) resizeDetailsViewport() {
	// Header ~9 lines + tab footer 3 lines + viewport border 2 lines = 14 overhead
	m.detailsViewport = viewport.New(max(m.width-4, 20), max(m.height-13, 3))
	m.detailsViewport.Style = lipgloss.NewStyle().Background(theme.ColorBackground)
}

// firstOutputLine returns the first non-empty line truncated to maxLen
func firstOutputLine(output string, maxLen int) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > maxLen {
				return line[:maxLen-3] + "..."
			}
			return line
		}
	}
	return ""
}

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
		if m.portsModel.filterBar.InEditMode() {
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
			{Key: "ctrl+k", Description: "Kill process"},
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
			{Key: "r", Description: "New diagnostic"},
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
		return 3 + m.portsModel.filterBar.ExtraHeight()
	}
	return 3
}

// RenderFooter implements FooterView
func (m *Model) RenderFooter(width int) string {
	var parts []string

	// Ports filter bar renders above the tab bar when visible
	if m.activeTab == tabPorts && m.portsModel.filterBar.IsVisible() {
		parts = append(parts, m.portsModel.filterBar.View())
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
			{Key: "r", Description: "New diagnostic (back to form)"},
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
			{Key: "ctrl+k", Description: "Send SIGKILL to selected process (Ports tab)"},
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
