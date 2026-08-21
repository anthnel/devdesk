package netdiag

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── View states ──────────────────────────────────────────────────────────────

func TestViewRendersTheForm(t *testing.T) {
	out := newTestModel(t).View()
	for _, want := range []string{"Target", "Port", "DNS Server", "Run Diagnostics"} {
		if !strings.Contains(out, want) {
			t.Errorf("the form has no %q", want)
		}
	}
}

// TestTheFormNoLongerOffersAToolPicker — the checkboxes are the thing this
// change removes, so their absence is asserted rather than assumed.
func TestTheFormNoLongerOffersAToolPicker(t *testing.T) {
	out := newTestModel(t).View()
	for _, gone := range []string{"Diagnostic Tests", "Netcat", "Traceroute", "SSL Certificate", "Curl"} {
		if strings.Contains(out, gone) {
			t.Errorf("the form still offers %q", gone)
		}
	}
}

// TestTheTableStaysOnScreenWhileTheRunWalks is Rule 139: a body that swaps
// itself for a spinner loses its header and its columns, and here it would also
// throw away the rows already answered.
func TestTheTableStaysOnScreenWhileTheRunWalks(t *testing.T) {
	m := runningModel(t, "example.com")
	m = feed(t, m, stageDoneMsg{
		gen: m.runGen, stage: netcheck.StageResolve, next: 1,
		results: netcheck.ResultsOf(check(netcheck.CheckResolve, netcheck.OK, "example.com resolves")),
	})

	out := m.View()
	if !strings.Contains(out, "Check") || !strings.Contains(out, "Verdict") {
		t.Error("the table header is not on screen mid-run")
	}
	if !strings.Contains(out, "DNS resolution") {
		t.Error("a check that already landed is not shown")
	}
}

// TestTheProgressLineNamesTheQuestionBeingAsked — an unreachable host spends
// its timeouts one after another, and a spinner with nothing beside it is
// indistinguishable from a hang.
func TestTheProgressLineNamesTheQuestionBeingAsked(t *testing.T) {
	m := runningModel(t, "example.com")
	footer := m.RenderFooter(120)

	if !strings.Contains(footer, netcheck.StageTitle(netcheck.StageResolve)) {
		t.Errorf("the footer does not name the stage:\n%s", footer)
	}
	if !strings.Contains(footer, "1/") {
		t.Error("the footer does not say how far along the run is")
	}
}

func TestTheTableShowsTheVerdictAndTheObservation(t *testing.T) {
	out := resultsModel(t).View()
	for _, want := range []string{"DNS resolution", "TCP connect", "OK", "FAIL", "N/A",
		"Port 443 does not accept connections"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table has no %q", want)
		}
	}
}

func TestTheEmptyMessageWaitsForTheRunToFinish(t *testing.T) {
	running := runningModel(t, "example.com")
	if strings.Contains(running.View(), "No checks") {
		t.Error("the table announces the absence of what it is fetching")
	}
}

func TestFilteringToNothingSaysWhy(t *testing.T) {
	m := deliver(t, runningModel(t, "example.com"),
		check(netcheck.CheckResolve, netcheck.OK, "resolves"),
		check(netcheck.CheckTCP, netcheck.OK, "open"))
	m = feed(t, m, testutil.Key("p"))

	if !strings.Contains(m.View(), "every check came back clean") {
		t.Errorf("an empty problems view says nothing useful:\n%s", m.View())
	}
}

// ── Header ───────────────────────────────────────────────────────────────────

func TestGetTitleNamesTheOpenCheck(t *testing.T) {
	m := resultsModel(t)
	if strings.Contains(m.GetTitle(), theme_chevron) {
		t.Error("the title carries a suffix with no detail open")
	}
	m.checksTable.SetCursor(1)
	m = feed(t, m, testutil.Key("enter"))
	if !strings.Contains(m.GetTitle(), "TCP connect") {
		t.Errorf("GetTitle = %q, want the open check named", m.GetTitle())
	}
}

func TestGetIconIsPopulated(t *testing.T) {
	if New(testConfig()).GetIcon() == "" {
		t.Fatal("GetIcon is empty")
	}
}

// TestTheHeaderCarriesTheVerdict — it is the answer to the question the view
// exists for, and reading it off ten rows is what a header is supposed to save.
func TestTheHeaderCarriesTheVerdict(t *testing.T) {
	form := newTestModel(t)
	for _, info := range form.GetHeaderInfo("default") {
		if info.Key == "Verdict" {
			t.Fatal("the form advertises a verdict before anything has run")
		}
	}

	m := resultsModel(t)
	var got string
	for _, info := range m.GetHeaderInfo("default") {
		if info.Key == "Verdict" {
			got = info.Value
		}
	}
	if got != netcheck.Fail.String() {
		t.Fatalf("header verdict = %q, want %q", got, netcheck.Fail)
	}
}

func TestGetHeaderInfoCarriesTheContext(t *testing.T) {
	info := New(testConfig()).GetHeaderInfo("work")
	if len(info) == 0 || info[0].Key != "Context" || info[0].Value != "work" {
		t.Fatalf("header info = %+v", info)
	}
}

// ── Shortcuts ────────────────────────────────────────────────────────────────

func TestShortcutsFollowTheState(t *testing.T) {
	keysOf := func(sc shortcut.Shortcuts) string {
		var b strings.Builder
		for _, s := range sc {
			b.WriteString(s.Key + " ")
		}
		return b.String()
	}

	if got := keysOf(newTestModel(t).GetShortcuts()); !strings.Contains(got, "enter") {
		t.Errorf("the form does not advertise enter: %s", got)
	}
	if got := keysOf(runningModel(t, "example.com").GetShortcuts()); !strings.Contains(got, "esc") {
		t.Errorf("a running pipeline does not advertise esc: %s", got)
	}

	results := keysOf(resultsModel(t).GetShortcuts())
	for _, want := range []string{"enter", "p", "/", "ctrl+r", "esc"} {
		if !strings.Contains(results, want) {
			t.Errorf("the results state does not advertise %q: %s", want, results)
		}
	}
}

// TestTheTraceShortcutIsAdvertisedOnlyWhereItApplies is Rule 130.
func TestTheTraceShortcutIsAdvertisedOnlyWhereItApplies(t *testing.T) {
	advertised := func(m *Model) bool {
		for _, s := range m.GetShortcuts() {
			if s.Key == keymap.Trace {
				return true
			}
		}
		return false
	}

	refused := deliver(t, runningModel(t, "example.com"),
		check(netcheck.CheckTCP, netcheck.Fail, "refused"))
	if !advertised(refused) {
		t.Error("a refused port does not offer the trace")
	}

	reachable := deliver(t, runningModel(t, "example.com"),
		check(netcheck.CheckTCP, netcheck.OK, "open"))
	if advertised(reachable) {
		t.Error("a reachable host offers a trace nobody needs")
	}
}

func TestTheProblemsShortcutLabelFlips(t *testing.T) {
	label := func(m *Model) string {
		for _, s := range m.GetShortcuts() {
			if s.Key == "p" {
				return s.Description
			}
		}
		return ""
	}

	m := resultsModel(t)
	if got := label(m); !strings.Contains(got, "problems") {
		t.Errorf("label = %q", got)
	}
	m = feed(t, m, testutil.Key("p"))
	if got := label(m); !strings.Contains(got, "every") {
		t.Errorf("the label did not flip: %q", got)
	}
}

func TestShortcutsFollowTheActiveTab(t *testing.T) {
	m := newTestModel(t)
	m.activeTab = tabPorts
	var keys string
	for _, s := range m.GetShortcuts() {
		keys += s.Key + " "
	}
	if !strings.Contains(keys, "K") {
		t.Errorf("the ports tab lost its shortcuts: %s", keys)
	}
}

// TestShortcutDescriptionsAreCapitalised is Rule 137.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	models := []*Model{newTestModel(t), runningModel(t, "example.com"), resultsModel(t)}

	details := resultsModel(t)
	details = feed(t, details, testutil.Key("enter"))
	models = append(models, details)

	ports := newTestModel(t)
	ports.activeTab = tabPorts
	models = append(models, ports)

	for _, m := range models {
		for _, s := range m.GetShortcuts() {
			r, _ := utf8.DecodeRuneInString(s.Description)
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ", r) {
				t.Errorf("description %q does not start with a capital", s.Description)
			}
		}
	}
}

// ── Footer ───────────────────────────────────────────────────────────────────

// TestFooterHeightMatchesWhatRenderFooterEmits — the router takes
// GetFooterHeight() lines off the viewport, so a mismatch either clips the
// footer or leaves a band nothing fills (D45).
func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func(*testing.T) *Model
	}{
		{"form", newTestModel},
		{"running", func(t *testing.T) *Model { return runningModel(t, "example.com") }},
		{"results", resultsModel},
		{"results with the filter bar open", func(t *testing.T) *Model {
			return feed(t, resultsModel(t), testutil.Key("p"))
		}},
		{"results searching", func(t *testing.T) *Model {
			return feed(t, resultsModel(t), testutil.Key("/"))
		}},
		{"ports", func(t *testing.T) *Model {
			m := newTestModel(t)
			m.activeTab = tabPorts
			return m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model(t)
			got := len(strings.Split(m.RenderFooter(120), "\n"))
			if got != m.GetFooterHeight() {
				t.Fatalf("RenderFooter emits %d lines, GetFooterHeight says %d", got, m.GetFooterHeight())
			}
		})
	}
}

func TestFooterShowsTheTabs(t *testing.T) {
	out := newTestModel(t).RenderFooter(120)
	for _, want := range []string{"Diagnostics", "Ports", "Topology"} {
		if !strings.Contains(out, want) {
			t.Errorf("the tab bar has no %q", want)
		}
	}
}

func TestFooterMessageIsPerTab(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("enter")) // refused: no target
	m.activeTab = tabPorts
	if strings.Contains(m.RenderFooter(120), "required") {
		t.Error("one tab's message leaked onto another")
	}
}

func TestTheFilterBarIsDrawnWhenItIsCounted(t *testing.T) {
	m := feed(t, resultsModel(t), testutil.Key("p"))
	if !m.filterBar.IsVisible() {
		t.Fatal("an active token did not make the bar visible")
	}
	if !strings.Contains(m.RenderFooter(120), problemsToken) {
		t.Error("the bar is counted but not drawn (D45)")
	}
}

// ── Help ─────────────────────────────────────────────────────────────────────

func TestGetHelpContentIsPopulated(t *testing.T) {
	c := New(testConfig()).GetHelpContent()
	if c.Title == "" || c.Description == "" {
		t.Fatal("the help has no title or description")
	}
	if len(c.KeyBindings) == 0 || len(c.Sections) == 0 {
		t.Fatal("the help has no bindings or sections")
	}
}

// TestTheHelpDescribesTheChecksItRuns — Rule 114: the help is updated in the
// same commit as the behaviour, and the old list named seven tools that no
// longer exist.
func TestTheHelpDescribesTheChecksItRuns(t *testing.T) {
	c := New(testConfig()).GetHelpContent()
	var body strings.Builder
	for _, s := range c.Sections {
		body.WriteString(s.Body)
	}
	text := body.String()

	for _, want := range []string{"Certificate chain", "Hostname match", "UNKNOWN", "N/A"} {
		if !strings.Contains(text, want) {
			t.Errorf("the help does not mention %q", want)
		}
	}
	for _, gone := range []string{"Netcat", "SSL Certificate -", "Curl request"} {
		if strings.Contains(text, gone) {
			t.Errorf("the help still describes %q", gone)
		}
	}
}

// ── Table invariants ─────────────────────────────────────────────────────────

// TestCheckColumnsHoldTheWidthInvariant is Rule 116 at the narrow end.
func TestCheckColumnsHoldTheWidthInvariant(t *testing.T) {
	for _, width := range []int{46, 80, 120, 200} {
		// The resize comes after the run: runningModel lays out at 120 columns,
		// so sizing first would be overwritten and every width would pass.
		m := deliver(t, runningModel(t, "example.com"),
			check(netcheck.CheckTCP, netcheck.Fail, "Port 443 does not accept connections"))
		m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: 30})

		for _, line := range strings.Split(m.View(), "\n") {
			if got := utf8.RuneCountInString(stripANSI(line)); got > width {
				t.Fatalf("at %d columns a line is %d wide", width, got)
			}
		}
	}
}

// TestALateStageKeepsTheCursorWhereItWas — the table is refilled on every stage,
// and a cursor that jumped back to the top on each would be unusable.
func TestALateStageKeepsTheCursorWhereItWas(t *testing.T) {
	m := resultsModel(t)
	m.checksTable.SetCursor(2)
	before := m.checksTable.Cursor()

	m.rebuildChecksTable()
	if got := m.checksTable.Cursor(); got != before {
		t.Fatalf("cursor moved from %d to %d on a refill", before, got)
	}
}

// TestTheVerdictCellIsPlainText is Rule 122: a Render() inside Cell is measured
// with its escape bytes, truncated mid-sequence, and bleeds over every row below.
func TestTheVerdictCellIsPlainText(t *testing.T) {
	for _, v := range []netcheck.Verdict{netcheck.OK, netcheck.Warn, netcheck.Fail,
		netcheck.NotApplicable, netcheck.Unknown} {
		cell := verdictCell(netcheck.Check{Verdict: v})
		if strings.Contains(cell, "\x1b") {
			t.Errorf("the %v cell carries an escape sequence", v)
		}
		if cell == "" {
			t.Errorf("the %v cell is empty", v)
		}
	}
}

const theme_chevron = ""

// stripANSI removes escape sequences so a rendered line can be measured.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
