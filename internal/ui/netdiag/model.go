package netdiag

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
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
	resultsTable datatable.Model[testResult]
	resultOrder  []string // test names in display order

	// Details view
	detailsViewport viewport.Model
	selectedTest    string
	rawDetails      bool // true = raw output, false = formatted (traceroute only)

	// Footer
	footerError string
	footerInfo  string
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
		resultsTable: datatable.New(datatable.Config[testResult]{
			Columns: resultColumns(),
			// The order the tests were started in, which is the order the form
			// lists them. `.` sorts on demand; a results list that reorders
			// itself is one the user has to re-read.
			SortColumn: -1,
		}),
	}
}
