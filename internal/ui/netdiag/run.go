package netdiag

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dockerpkg "github.com/anthnel/devdesk/internal/docker"
)

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
