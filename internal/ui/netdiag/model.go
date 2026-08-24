package netdiag

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ViewState represents the current state of the netdiag view
type ViewState int

const (
	StateInput   ViewState = iota // Target, port and resolver
	StateRunning                  // The pipeline is walking its stages
	StateResults                  // The checks and their verdicts
	StateDetails                  // One check explained, or a route trace
)

// Tab indices
const (
	tabDiagnostics = 0
	tabPorts       = 1
	tabInterfaces  = 2
)

// Form field indices.
//
// There were eleven, seven of them checkboxes selecting which tools to run.
// They asked the user to configure something the pipeline derives: the checks
// follow from the target and from what has already failed, so a question that
// was never worth asking is no longer asked.
const (
	fieldTarget    = 0
	fieldPort      = 1
	fieldDNSServer = 2
	fieldButton    = 3
	fieldCount     = 4
)

// problemsToken filters the table down to what is not settled.
//
// One token rather than one per verdict: a run produces about ten rows, and
// four cumulative filters over ten rows is machinery nobody needs. What a
// reader wants is to drop the settled rows and see what went wrong.
const problemsToken = "problems"

// stageDoneMsg carries the results after one stage of the pipeline.
//
// The accumulated Results travels *in the message* rather than being mutated in
// place: the model holds the previous value while the next stage runs on a
// goroutine, so sharing one would be a data race on exactly what Rule 110
// forbids. netcheck.RunStep copies for this reason.
type stageDoneMsg struct {
	gen     int // must match Model.runGen; a superseded run's results are discarded
	stage   netcheck.StageID
	next    int // index into netcheck.Steps() of the stage still to run
	results netcheck.Results
}

// traceDoneMsg carries the output of a route trace.
type traceDoneMsg struct {
	gen    int
	output string
	tcp    bool
}

// Model represents the network diagnostics view
type Model struct {
	config *config.Config
	width  int
	height int

	// Active tab (tabDiagnostics, tabPorts, or tabInterfaces)
	activeTab       int
	portsModel      *PortsModel
	interfacesModel *InterfacesModel

	state ViewState

	// Form inputs
	targetInput    textinput.Model
	portInput      textinput.Model
	dnsServerInput textinput.Model
	focusedField   int

	// Running state
	spinner    spinner.Model
	runGen     int // incremented each run; results from a superseded run are dropped
	runStage   netcheck.StageID
	runStep    int
	totalSteps int

	// Results
	results     netcheck.Results
	verdict     netcheck.Verdict
	checksTable datatable.Model[netcheck.Check]
	filterBar   components.FilterBar

	// Details: one check explained, or a route trace
	detailsViewport viewport.Model
	selected        netcheck.Check
	traceOutput     string
	traceTCP        bool
	tracing         bool

	// footer is the one line of transient state below the tab bar (Rule 128).
	footer components.FooterMessage
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
	portIn.Placeholder = defaultPort
	portIn.CharLimit = 10

	dnsIn := textinput.New()
	theme.StyleTextInput(&dnsIn)
	// Empty means the system resolver, and that is the answer the user cares
	// about: it is the one their own traffic uses. The old default read the
	// first nameserver out of /etc/resolv.conf, which does not exist on Windows.
	dnsIn.Placeholder = "system resolver"
	dnsIn.CharLimit = 64

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	return &Model{
		config:          cfg,
		state:           StateInput,
		activeTab:       tabDiagnostics,
		portsModel:      newPortsModel(time.Duration(cfg.Network.PortsRefreshInterval) * time.Second),
		interfacesModel: newInterfacesModel(),
		targetInput:     targetIn,
		portInput:       portIn,
		dnsServerInput:  dnsIn,
		focusedField:    fieldTarget,
		spinner:         sp,
		filterBar:       components.NewFilterBarWithTokens([]components.FilterToken{{Label: problemsToken}}),
		checksTable: datatable.New(datatable.Config[netcheck.Check]{
			Columns: checkColumns(),
			// Pipeline order is content: resolve, reach, connect, TLS, HTTP is
			// the order the questions depend on one another. A table that
			// reorders itself destroys the one thing the layout says.
			SortColumn: -1,
		}),
	}
}

// buildTarget assembles what the form describes.
func (m *Model) buildTarget() (netcheck.Target, error) {
	host := strings.TrimSpace(m.targetInput.Value())
	if err := validateTarget(host); err != nil {
		return netcheck.Target{}, err
	}
	port := strings.TrimSpace(m.portInput.Value())
	if port == "" {
		port = defaultPort
	}
	if err := validatePort(port); err != nil {
		return netcheck.Target{}, err
	}
	n, _ := strconv.Atoi(port) // validatePort has already accepted it
	return netcheck.Target{
		Host:     host,
		Port:     n,
		Resolver: strings.TrimSpace(m.dnsServerInput.Value()),
	}, nil
}

// visibleChecks applies the filter bar to the results.
//
// The view filters and calls SetItems rather than leaning on the table's own
// search, for the reason security and status do: this decides which rows exist
// at all, where a FilterBar query narrows a list that is already settled.
func (m *Model) visibleChecks() []netcheck.Check {
	all := m.results.All()
	query := strings.ToLower(m.filterBar.SearchQuery())
	onlyProblems := m.filterBar.IsTokenActive(problemsToken)

	out := make([]netcheck.Check, 0, len(all))
	for _, c := range all {
		if onlyProblems && (c.Verdict == netcheck.OK || c.Verdict == netcheck.NotApplicable) {
			continue
		}
		if query != "" && !matchesQuery(c, query) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func matchesQuery(c netcheck.Check, query string) bool {
	return strings.Contains(strings.ToLower(c.Title), query) ||
		strings.Contains(strings.ToLower(c.Summary), query)
}

// tracedTarget reports whether a failed check makes a route trace worth
// offering, and it is the only rule that decides (Rule 130).
//
// Tracing is the one expensive probe left — thirty hops at a second each — and
// cost was the only honest reason a checkbox ever existed. So it is offered
// when the path is in question and hidden when it is not: a certificate that
// does not verify is not a routing problem, and a trace would be thirty seconds
// spent answering something nobody asked.
func (m *Model) traceWorthOffering() bool {
	if m.state != StateResults {
		return false
	}
	switch {
	case m.results.VerdictOf(netcheck.CheckTCP) == netcheck.Fail:
		return true
	case m.results.VerdictOf(netcheck.CheckICMP) == netcheck.Warn:
		return true
	default:
		return false
	}
}

// checkSettings is what the config says the pipeline should wait for and send.
//
// Read on every run rather than captured at construction: the configuration
// view rebuilds this model on save, but a run already in flight holds its own
// copy through the message chain, and taking the values here keeps the two from
// disagreeing halfway down the pipeline.
//
// netcheck.Settings.normalize fills in anything non-positive, so a hand-edited
// zero becomes the default rather than a dial with no deadline.
func (m *Model) checkSettings() netcheck.Settings {
	return netcheck.Settings{
		CheckTimeout:     time.Duration(m.config.Network.CheckTimeout) * time.Second,
		PingCount:        m.config.Network.PingCount,
		ExpiryWarnWindow: time.Duration(m.config.Network.CertExpiryWarnDays) * 24 * time.Hour,
	}
}
