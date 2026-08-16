package ociresources

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The connectivity form is driven entirely through Update, and the one command
// it returns shells out to Docker — so it is asserted on, never executed.

func connectivityContainers() []docker.NetworkContainer {
	return []docker.NetworkContainer{
		{Name: "api", IPv4: "172.18.0.2"},
		{Name: "db", IPv4: "172.18.0.3"},
		{Name: "sidecar-with-no-address"},
	}
}

func newConnectivityForm(t *testing.T) *ConnectivityTestForm {
	t.Helper()
	return newConnectivityTestForm("web", "net22222", "netshoot", connectivityContainers(), 120, 30)
}

func key(t *testing.T, f *ConnectivityTestForm, names ...string) (*ConnectivityTestForm, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, name := range names {
		f, cmd = f.Update(testutil.Key(name))
	}
	return f, cmd
}

// onTypeField moves the focus to the test type. It goes backwards on purpose:
// ↓ from the target field walks the container suggestions first, so the number
// of presses would depend on how many containers the fixture has.
func onTypeField(t *testing.T, f *ConnectivityTestForm) *ConnectivityTestForm {
	t.Helper()
	f, _ = key(t, f, "up", "up")
	if f.focusedField != cFieldType {
		t.Fatalf("focus = %d, want the type field", f.focusedField)
	}
	return f
}

// ── Field layout ─────────────────────────────────────────────────────────────

// The port only means something for curl and netcat, and hiding it moves the
// submit button up a slot. Getting that wrong makes Enter do nothing on the
// button the user is looking at.
func TestThePortFieldAndTheSubmitSlotMoveWithTheTestType(t *testing.T) {
	f := newConnectivityForm(t)

	if f.isPortActive() {
		t.Error("ping was given a port field")
	}
	if f.numFields() != 3 || f.submitIdx() != cFieldPort {
		t.Errorf("ping: %d fields, submit at %d — want 3 and the third slot", f.numFields(), f.submitIdx())
	}

	f = onTypeField(t, f)
	f, _ = key(t, f, "right")
	if !f.isPortActive() {
		t.Fatalf("testType = %v, want curl after one step right", f.testType)
	}
	if f.numFields() != 4 || f.submitIdx() != cFieldSubmit {
		t.Errorf("curl: %d fields, submit at %d — want 4 and the fourth slot", f.numFields(), f.submitIdx())
	}
}

// Rule 132: a closed set cycles with ← / →, and wraps in both directions.
func TestTheTestTypeCyclesInBothDirections(t *testing.T) {
	f := onTypeField(t, newConnectivityForm(t))

	f, _ = key(t, f, "right")
	if f.testType != testCurl {
		t.Errorf("testType = %v, want curl", f.testType)
	}
	f, _ = key(t, f, "right", "right")
	if f.testType != testPing {
		t.Errorf("testType = %v, want it to have wrapped back to ping", f.testType)
	}
	f, _ = key(t, f, "left")
	if f.testType != testNetcat {
		t.Errorf("testType = %v, want it to wrap backwards to netcat", f.testType)
	}
}

// The arrows only cycle the field that owns them; on any other field they are
// navigation and must leave the type alone.
func TestTheArrowsDoNotCycleTheTypeFromAnotherField(t *testing.T) {
	f := newConnectivityForm(t)

	f, _ = key(t, f, "right", "left")

	if f.testType != testPing {
		t.Errorf("testType = %v, want it untouched from the target field", f.testType)
	}
}

// D21, pinning the invariant rather than the code: cycling the type can shrink
// the field count, and the clamp that guarded that in the left/right handlers
// could not fire — cycling only happens while cFieldType is focused, and that
// index is below numFields() in every mode. Both clamps are gone (the shape D5
// records for CreationForm); this test is what keeps the invariant they were
// guarding true without them.
func TestCyclingTheTypeNeverStrandsTheFocus(t *testing.T) {
	f := onTypeField(t, newConnectivityForm(t))

	for i, step := range []string{"right", "right", "right", "left", "left", "left"} {
		f, _ = key(t, f, step)
		if f.focusedField >= f.numFields() {
			t.Fatalf("after %d cycles the focus is on field %d of %d", i+1, f.focusedField, f.numFields())
		}
	}
}

// Rule 135: ↑ / ↓ navigate fields, and they wrap so every control is reachable
// in one direction.
func TestFieldNavigationWrapsAround(t *testing.T) {
	f := newConnectivityForm(t)

	f, _ = key(t, f, "up")
	if f.focusedField != f.submitIdx() {
		t.Errorf("focus = %d, want up from the first field to land on submit", f.focusedField)
	}
	f, _ = key(t, f, "down")
	if f.focusedField != cFieldTarget {
		t.Errorf("focus = %d, want it back on the target", f.focusedField)
	}
}

// Only the focused input takes typing — the port field must not swallow
// characters meant for the target.
func TestTypingReachesTheFocusedInputOnly(t *testing.T) {
	f := newConnectivityForm(t)

	for _, msg := range testutil.Type("10.0.0.5") {
		f, _ = f.Update(msg)
	}
	if f.targetInput.Value() != "10.0.0.5" {
		t.Errorf("target = %q, want what was typed", f.targetInput.Value())
	}

	f = onTypeField(t, f)
	f, _ = key(t, f, "right") // curl, so the port field appears
	f, _ = key(t, f, "down")  // onto the port
	for _, msg := range testutil.Type("8443") {
		f, _ = f.Update(msg)
	}
	if f.portInput.Value() != "8443" {
		t.Errorf("port = %q, want what was typed", f.portInput.Value())
	}
	if f.targetInput.Value() != "10.0.0.5" {
		t.Errorf("target = %q, want it left alone", f.targetInput.Value())
	}
}

// ── The container suggestions ────────────────────────────────────────────────

// The list exists so the user does not have to read an IP off another screen
// and retype it, so browsing it has to fill the field.
func TestBrowsingTheSuggestionsFillsTheTarget(t *testing.T) {
	f := newConnectivityForm(t)

	f, _ = key(t, f, "down")
	if f.targetInput.Value() != "172.18.0.2" {
		t.Fatalf("target = %q, want the first container's address", f.targetInput.Value())
	}
	f, _ = key(t, f, "down")
	if f.targetInput.Value() != "172.18.0.3" {
		t.Errorf("target = %q, want the second container's address", f.targetInput.Value())
	}
}

// Stepping back off the top of the list is how the user leaves it, and the
// selection has to be released rather than sticking on the first entry.
func TestSteppingBackOffTheSuggestionListReleasesIt(t *testing.T) {
	f := newConnectivityForm(t)
	f, _ = key(t, f, "down", "down") // into the list, second entry

	f, _ = key(t, f, "up", "up")

	if f.containerListIdx != -1 {
		t.Errorf("containerListIdx = %d, want nothing selected", f.containerListIdx)
	}
	if f.focusedField != cFieldTarget {
		t.Errorf("focus = %d, want it still on the target field", f.focusedField)
	}
}

// Once the user types, the highlight is stale — it refers to a value they have
// just replaced.
func TestTypingClearsTheSuggestionHighlight(t *testing.T) {
	f := newConnectivityForm(t)
	f, _ = key(t, f, "down")

	for _, msg := range testutil.Type("9") {
		f, _ = f.Update(msg)
	}

	if f.containerListIdx != -1 {
		t.Errorf("containerListIdx = %d, want the highlight dropped once typing started", f.containerListIdx)
	}
}

// With the list exhausted, ↓ has to go back to being navigation or the submit
// button is unreachable.
func TestDownPastTheLastSuggestionResumesNavigating(t *testing.T) {
	f := newConnectivityForm(t)

	for range len(connectivityContainers()) {
		f, _ = key(t, f, "down")
	}
	f, _ = key(t, f, "down")

	if f.focusedField != cFieldType {
		t.Errorf("focus = %d, want the next field once the list is exhausted", f.focusedField)
	}
}

// A form opened on a network with nothing attached has no list, and ↓ is plain
// navigation from the first keystroke.
func TestWithNoContainersTheArrowsOnlyNavigate(t *testing.T) {
	f := newConnectivityTestForm("web", "net22222", "netshoot", nil, 120, 30)

	f, _ = key(t, f, "down")

	if f.focusedField != cFieldType {
		t.Errorf("focus = %d, want the type field", f.focusedField)
	}
}

// ── The command that gets run ────────────────────────────────────────────────

func TestTheCommandMatchesTheSelectedTest(t *testing.T) {
	cases := []struct {
		name     string
		testType connectivityTestType
		port     string
		want     string
	}{
		{"ping ignores the port", testPing, "8080", "ping -c 3 10.0.0.5"},
		{"curl builds a URL", testCurl, "8080", "curl -v -s -m 5 http://10.0.0.5:8080"},
		{"netcat probes the port", testNetcat, "5432", "nc -zv -w 5 10.0.0.5 5432"},
		{"an empty port falls back to 80", testCurl, "", "curl -v -s -m 5 http://10.0.0.5:80"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnectivityForm(t)
			f.targetInput.SetValue("  10.0.0.5  ") // the surrounding space is the user's, not ours
			f.portInput.SetValue(tc.port)
			f.testType = tc.testType

			if got := strings.Join(f.buildCommand(), " "); got != tc.want {
				t.Errorf("buildCommand() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubmittingRunsTheTestAndBlocksTheForm(t *testing.T) {
	f := newConnectivityForm(t)
	f.targetInput.SetValue("10.0.0.5")

	f, cmd := key(t, f, "up", "enter") // up lands on submit

	if cmd == nil {
		t.Fatal("submitting produced no command, so no test would run")
	}
	if f.state != connectivityStateRunning {
		t.Errorf("state = %v, want the form to show it is running", f.state)
	}
}

// A test against nothing has no meaning, and starting it would replace the form
// with a running state the user then has to wait out.
func TestSubmittingWithNoTargetDoesNothing(t *testing.T) {
	f := newConnectivityForm(t)
	f.targetInput.SetValue("   ")

	f, cmd := key(t, f, "up", "enter")

	if cmd != nil {
		t.Error("a test was started with no target")
	}
	if f.state != connectivityStateInput {
		t.Errorf("state = %v, want the form still accepting input", f.state)
	}
}

// Enter anywhere but the button is not a submission — Rule 135 reserves it for
// the focused action.
func TestEnterOnAFieldDoesNotSubmit(t *testing.T) {
	f := newConnectivityForm(t)
	f.targetInput.SetValue("10.0.0.5")

	f, cmd := key(t, f, "enter")

	if cmd != nil || f.state != connectivityStateInput {
		t.Errorf("enter on the target field started a test (state %v)", f.state)
	}
}

// The running state is what stops a second test being launched over the first.
func TestNoKeyIsAcceptedWhileATestRuns(t *testing.T) {
	f := newConnectivityForm(t)
	f.state = connectivityStateRunning

	f, cmd := key(t, f, "enter", "down", "right")

	if cmd != nil {
		t.Error("a key produced a command while a test was running")
	}
	if f.focusedField != cFieldTarget || f.testType != testPing {
		t.Errorf("the form moved under a running test: focus %d, type %v", f.focusedField, f.testType)
	}
}

// ── Results ──────────────────────────────────────────────────────────────────

// A non-zero exit with output is not a failed test: curl exiting 52 with a
// transcript is the answer. Only an exit with nothing to show is an error.
func TestAResultWithOutputAndAnExitCodeIsAWarningNotAFailure(t *testing.T) {
	f := newConnectivityForm(t)

	f.SetResult("* connected\n* empty reply from server", errors.New("diagnostic command failed: exit status 52"))

	if f.state != connectivityStateResults {
		t.Fatalf("state = %v, want the results shown", f.state)
	}
	if !f.resultExitCode {
		t.Error("output alongside an exit code was treated as a hard failure")
	}
	// The wrapper the docker layer adds is noise in a status line.
	if f.resultErr != "exit status 52" {
		t.Errorf("resultErr = %q, want the wrapper stripped", f.resultErr)
	}
	if !strings.Contains(f.View(), "empty reply") {
		t.Error("the output was not rendered alongside the warning")
	}
}

func TestAResultWithNoOutputIsAHardFailure(t *testing.T) {
	f := newConnectivityForm(t)

	f.SetResult("", errors.New("diagnostic command failed: no such image"))

	if f.resultExitCode {
		t.Error("an error with no output was treated as a warning")
	}
	if !strings.Contains(f.View(), "no such image") {
		t.Error("the failure was not shown")
	}
}

func TestACleanResultReportsCompletion(t *testing.T) {
	f := newConnectivityForm(t)

	f.SetResult("3 packets transmitted, 3 received", nil)

	if f.resultErr != "" || f.resultExitCode {
		t.Errorf("a clean run reported err=%q exitCode=%v", f.resultErr, f.resultExitCode)
	}
	if !strings.Contains(f.View(), "Test completed") {
		t.Error("a clean run did not say so")
	}
}

// A second run must start from a blank form rather than from the last result.
func TestEnterOnTheResultsReturnsToAnEmptyForm(t *testing.T) {
	f := newConnectivityForm(t)
	f.SetResult("some output", nil)
	f.resultScroll = 3

	f, _ = key(t, f, "enter")

	if f.state != connectivityStateInput {
		t.Fatalf("state = %v, want the input form back", f.state)
	}
	if f.result != "" || f.resultScroll != 0 || f.resultErr != "" {
		t.Errorf("the previous result survived: %q at offset %d", f.result, f.resultScroll)
	}
}

func TestTheResultScrollsAndStopsAtBothEnds(t *testing.T) {
	f := newConnectivityForm(t)
	f.height = 10 // 4 visible lines
	f.SetResult(strings.Repeat("line\n", 19)+"line", nil)

	f, _ = key(t, f, "up")
	if f.resultScroll != 0 {
		t.Errorf("resultScroll = %d, want it held at the top", f.resultScroll)
	}

	f, _ = key(t, f, "down", "down")
	if f.resultScroll != 2 {
		t.Errorf("resultScroll = %d, want 2", f.resultScroll)
	}

	f, _ = key(t, f, "end")
	bottom := f.resultScroll
	if bottom != 20-f.resultVisibleLines() {
		t.Errorf("resultScroll = %d, want the last screenful", bottom)
	}
	f, _ = key(t, f, "down")
	if f.resultScroll != bottom {
		t.Errorf("resultScroll = %d, want it held at the bottom", f.resultScroll)
	}

	f, _ = key(t, f, "home")
	if f.resultScroll != 0 {
		t.Errorf("resultScroll = %d, want it back at the top", f.resultScroll)
	}
}

// The indicator is the only thing telling the user there is more below.
func TestOverflowingOutputIsCounted(t *testing.T) {
	f := newConnectivityForm(t)
	f.height = 10
	f.SetResult(strings.Repeat("line\n", 19)+"line", nil)

	if !strings.Contains(f.View(), "/ 20 lines]") {
		t.Error("the scroll indicator does not report the total")
	}
}

func TestOutputThatFitsIsNotCounted(t *testing.T) {
	f := newConnectivityForm(t)
	f.SetResult("one\ntwo", nil)

	if strings.Contains(f.View(), "lines]") {
		t.Error("a result that fits was given a scroll indicator")
	}
}

// ── Rendering ────────────────────────────────────────────────────────────────

// Rule 134: the shortcuts live in the header, so nothing in the viewport may
// spell out a key.
func TestTheFormCarriesNoInlineHelp(t *testing.T) {
	f := newConnectivityForm(t)

	for _, view := range []string{f.View(), runningView(f), resultsView(f)} {
		if strings.Contains(view, "[enter]") || strings.Contains(view, "[esc]") {
			t.Error("the viewport spells out a keybinding")
		}
	}
}

func TestEveryRenderedLineFillsTheWidth(t *testing.T) {
	f := newConnectivityForm(t)
	f = onTypeField(t, f)
	f, _ = key(t, f, "right") // curl, so the port field renders too

	for _, view := range []string{f.View(), runningView(f), resultsView(f)} {
		for i, line := range strings.Split(view, "\n") {
			if width := lipgloss.Width(line); width != f.width {
				t.Errorf("line %d is %d columns wide, want %d", i, width, f.width)
			}
		}
	}
}

// A container with no address still has to be listed — it is on the network,
// and hiding it would suggest otherwise.
func TestAContainerWithNoAddressIsStillListed(t *testing.T) {
	f := newConnectivityForm(t)

	view := f.View() // the target field is focused, so the list is shown
	if !strings.Contains(view, "sidecar-with-no-address") {
		t.Error("a container with no address was left out of the list")
	}
	if !strings.Contains(view, "—") {
		t.Error("the missing address was not marked")
	}
}

// The list is a suggestion for the target field alone; leaving that field has
// to take it off screen or it crowds out the rest of the form.
func TestTheSuggestionsAreOnlyShownOnTheTargetField(t *testing.T) {
	f := newConnectivityForm(t)
	f, _ = key(t, f, "up") // onto submit

	if strings.Contains(f.View(), "Containers on this network") {
		t.Error("the suggestion list is shown while another field is focused")
	}
}

func TestTruncateResultLine(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"much longer than allowed", 10, "much long…"},
		{"anything", 0, ""},
		{"anything", -1, ""},
	}
	for _, tc := range cases {
		if got := truncateResultLine(tc.in, tc.max); got != tc.want {
			t.Errorf("truncateResultLine(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func runningView(f *ConnectivityTestForm) string {
	clone := *f
	clone.state = connectivityStateRunning
	return clone.View()
}

func resultsView(f *ConnectivityTestForm) string {
	clone := *f
	clone.SetResult("some output\nmore output", nil)
	return clone.View()
}
