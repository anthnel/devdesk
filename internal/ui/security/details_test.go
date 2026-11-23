package security

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-750 (step 4): the finding detail is readable. Nothing overflows the
// viewport, the title is one line, the Message is capped, the fix status and the
// remediation come before the facts, and no empty line is left behind by a
// section that has nothing to say.

// detailsTarget is the directory the fixtures were scanned from. It is only a
// string: a directory result is never read from disk by the detail.
const detailsTarget = "/work/repo"

// detailsSizes are the three terminals of the plan's measured layout table: the
// width the router hands the view and the height of its viewport.
var detailsSizes = []struct{ width, height int }{{80, 12}, {120, 28}, {175, 38}}

// textWidth is what a line of text may span: the viewport is two cells narrower
// than the view, and the padding takes one cell on each side.
func textWidth(width int) int { return width - 4 }

// tabOf is the tab that lists f.
func tabOf(t *testing.T, f scan.Finding) int {
	t.Helper()
	for tab, category := range tabCategory {
		if category == scan.Categorize(f) {
			return tab
		}
	}
	t.Fatalf("no tab lists %s", f.ID)
	return -1
}

// openDetail opens the detail of f the way a user does: a result holding f, the
// tab that lists it, enter.
func openDetail(t *testing.T, f scan.Finding, width, height int) Model {
	t.Helper()
	result := &scan.Result{Target: detailsTarget, TargetType: scan.TargetDirectory, Findings: []scan.Finding{f}}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(width, height))
	m.switchTab(tabOf(t, f))
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil {
		t.Fatalf("enter on %s did not open its details (state %v)", f.ID, m.state)
	}
	return m
}

// plainLines is the built content without its styling, one entry per line.
func plainLines(m Model) []string { return strings.Split(ansi.Strip(m.buildDetailsContent()), "\n") }

// sectionAfter is the lines under a heading, up to the next blank line.
func sectionAfter(lines []string, heading string) []string {
	for i, line := range lines {
		if strings.TrimSpace(line) != heading {
			continue
		}
		var out []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				break
			}
			out = append(out, strings.TrimSpace(next))
		}
		return out
	}
	return nil
}

func indexOfLine(lines []string, contains string) int {
	for i, line := range lines {
		if strings.Contains(line, contains) {
			return i
		}
	}
	return -1
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

const renderFirstLine = "Error: lstat " + detailsTarget + "/k8s/base: no such file or directory"

// renderFailureFixture is built like scan.renderFailure builds one: the title
// carries the first line of the renderer's stderr, the Message all of it, and
// the paths are absolute, under the scanned directory or under the container
// mount.
func renderFailureFixture() scan.Finding {
	lines := []string{renderFirstLine}
	for i := 2; i <= 15; i++ {
		root := detailsTarget
		if i%2 == 0 {
			root = "/scan"
		}
		lines = append(lines, fmt.Sprintf("line %02d: open %s/k8s/overlays/prod/p%02d.yaml: denied", i, root, i))
	}
	return scan.Finding{
		ID: scan.K8sRenderID, Title: "Overlay does not build: " + renderFirstLine,
		Description: "The directory could not be rendered, so none of its resources were validated.",
		Message:     strings.Join(lines, "\n"),
		Resolution:  "Build it locally with kustomize build (or kubectl kustomize) and fix what it reports.",
		Severity:    scan.SeverityHigh, Source: scan.SourceKustomize, File: "k8s/overlays/prod", IaCType: "kustomize",
	}
}

func ksv0001(message string) scan.Finding {
	return scan.Finding{
		ID: "KSV-0001", Title: "Process can elevate its own privileges", Severity: scan.SeverityHigh,
		Description: "A program inside the container can elevate its own privileges and run as root.",
		Message:     message, Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", Line: 10, IaCType: "kubernetes",
		References: []string{"https://avd.aquasec.com/misconfig/ksv001"},
	}
}

func cveWith(ecosystem, fixCommand string) scan.Finding {
	return scan.Finding{
		ID: "CVE-2026-1111", Title: "A flaw in a dependency", Severity: scan.SeverityHigh, Source: scan.SourceTrivy,
		PkgName: "golang.org/x/net", Ecosystem: ecosystem, Version: "0.1.0", FixedIn: "0.7.0",
		FixCommand: fixCommand, References: []string{"https://nvd.example/CVE-2026-1111"},
	}
}

func longSecret() scan.Finding {
	return scan.Finding{
		ID: "generic-api-key", Title: "A key in the tree", Severity: scan.SeverityHigh, Source: scan.SourceGitleaks,
		File: "config/prod.env", Line: 12, Match: strings.Repeat("A", 300),
	}
}

func longCVE() scan.Finding {
	f := cveWith("gomod", "go get "+strings.Repeat("p", 200))
	f.PkgName = strings.Repeat("q", 200)
	f.Version = strings.Repeat("1", 200)
	f.FixedIn = strings.Repeat("2", 200)
	return f
}

func minimalFinding() scan.Finding {
	return scan.Finding{ID: "X-1", Title: "Only a title", Severity: scan.SeverityLow}
}

// detailsFixtures are the findings every layout assertion runs over.
func detailsFixtures() map[string]scan.Finding {
	return map[string]scan.Finding{
		"render failure": renderFailureFixture(),
		"long message":   ksv0001(strings.Repeat("alpha beta gamma ", 60)),
		"known cve":      cveWith("gomod", "go get golang.org/x/net@v0.7.0"),
		"unknown cve":    func() scan.Finding { f := cveWith("conda", "Update foo to 1.2"); f.PkgName = "foo"; return f }(),
		"long secret":    longSecret(),
		"long cve":       longCVE(),
		"minimal":        minimalFinding(),
	}
}

// ── Nothing overflows (AC1) ──────────────────────────────────────────────────

func requireWithin(t *testing.T, label string, lines []string, limit int) {
	t.Helper()
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > limit {
			t.Errorf("%s: line %d spans %d cells, limit %d: %q", label, i, got, limit, ansi.Strip(line))
			return
		}
	}
}

func TestNoLineOfTheDetailOverflowsTheViewport(t *testing.T) {
	for name, f := range detailsFixtures() {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", name, size.width), func(t *testing.T) {
				m := openDetail(t, f, size.width, size.height)
				limit := size.width - 2
				requireWithin(t, "buildDetailsContent", strings.Split(m.buildDetailsContent(), "\n"), limit)
				requireWithin(t, "viewport", strings.Split(m.renderDetailsView(), "\n"), limit)
			})
		}
	}
}

// Under 46 columns the old floor of 40 cells for the text overflowed.
func TestNoLineOfTheDetailOverflowsANarrowTerminal(t *testing.T) {
	for name, f := range detailsFixtures() {
		for _, width := range []int{30, 40, 45} {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				m := openDetail(t, f, width, 12)
				requireWithin(t, "buildDetailsContent", strings.Split(m.buildDetailsContent(), "\n"), width-2)
				requireWithin(t, "viewport", strings.Split(m.renderDetailsView(), "\n"), width-2)
			})
		}
	}
}

// The viewport has two border cells to give back, from the enter key and from a
// resize, whichever state the view is in when the terminal changes.
func TestTheDetailsViewportIsTwoCellsNarrowerThanTheView(t *testing.T) {
	for _, size := range detailsSizes {
		m := openDetail(t, longCVE(), size.width, size.height)
		if got, want := m.detailsViewport.Width(), size.width-2; got != want {
			t.Errorf("after enter at %d: viewport width = %d, want %d", size.width, got, want)
		}
		if got := m.detailsViewport.Height(); got != size.height {
			t.Errorf("after enter at %d: viewport height = %d, want %d", size.width, got, size.height)
		}
	}

	opened := openDetail(t, longCVE(), 160, 30)
	resized := feed(t, opened, testutil.Resize(100, 20))
	if got, want := resized.detailsViewport.Width(), 98; got != want {
		t.Errorf("resized while on the details: viewport width = %d, want %d", got, want)
	}
	requireWithin(t, "content after a resize", strings.Split(resized.buildDetailsContent(), "\n"), 98)

	away := feed(t, scannedModel(t), testutil.Resize(100, 20))
	if got, want := away.detailsViewport.Width(), 98; got != want {
		t.Errorf("resized on the results: viewport width = %d, want %d", got, want)
	}
}

// ── The title is one line, with short paths (AC1) ────────────────────────────

func TestTheRenderFailureTitleIsOneLineWithRelativePaths(t *testing.T) {
	f := renderFailureFixture()
	want := "Overlay does not build: Error: lstat k8s/base: no such file or directory"
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			m := openDetail(t, f, size.width, size.height)
			lines := plainLines(m)

			title := indexOfLine(lines, "Overlay does not build")
			header := indexOfLine(lines, scan.K8sRenderID)
			if title < 0 || header < 0 {
				t.Fatalf("no header or title in:\n%s", strings.Join(lines, "\n"))
			}
			if title != header+1 && title != header+2 {
				t.Errorf("the title is on line %d, the header on %d: it must follow the header", title, header)
			}
			if got := strings.TrimSpace(lines[title]); got != want {
				t.Errorf("title line = %q, want %q", got, want)
			}
			if next := lines[title+1]; strings.TrimSpace(next) != "" {
				t.Errorf("the title wraps onto a second line: %q", next)
			}
			if n := strings.Count(strings.Join(lines, "\n"), "Overlay does not build"); n != 1 {
				t.Errorf("the title is shown %d times, want once", n)
			}
			// The finding keeps its text: only the display is shortened.
			if m.selectedFinding.Title != f.Title || m.result.Findings[0].Title != f.Title {
				t.Errorf("the finding's own title was rewritten: %q", m.selectedFinding.Title)
			}
		})
	}
}

func TestALongTitleIsCutOnOneLine(t *testing.T) {
	f := ksv0001("")
	f.Title = strings.Repeat("a long title ", 30)
	for _, size := range detailsSizes {
		m := openDetail(t, f, size.width, size.height)
		lines := plainLines(m)
		at := indexOfLine(lines, "a long title")
		if at < 0 {
			t.Fatalf("at %d: the title is not shown", size.width)
		}
		title := strings.TrimSpace(lines[at])
		if ansi.StringWidth(title) > textWidth(size.width) || !strings.HasSuffix(title, theme.Ellipsis) {
			t.Errorf("at %d: title %q must be cut to %d cells with %q", size.width, title, textWidth(size.width), theme.Ellipsis)
		}
		if strings.TrimSpace(lines[at+1]) != "" {
			t.Errorf("at %d: the title wraps: %q", size.width, lines[at+1])
		}
	}
}

// A tab counts for no cell in x/ansi but lipgloss renders it as four spaces, so
// a title or an ID made of tab-separated words used to span far more cells than
// the viewport has.
func TestATitleOrAnIDWithTabsNeverOverflowsTheViewport(t *testing.T) {
	spaced := strings.Join(strings.Split("abcdefghijklmnopqrstuvwxyz", ""), "\t")
	tests := map[string]func(f *scan.Finding){
		"title":           func(f *scan.Finding) { f.Title = spaced },
		"id":              func(f *scan.Finding) { f.ID = spaced },
		"title with CR":   func(f *scan.Finding) { f.Title = strings.ReplaceAll(spaced, "\t", "\r") },
		"id then newline": func(f *scan.Finding) { f.ID = spaced + "\nsecond line" },
	}
	for name, mutate := range tests {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", name, size.width), func(t *testing.T) {
				f := ksv0001("")
				mutate(&f)
				m := openDetail(t, f, size.width, size.height)
				limit := size.width - 2
				requireWithin(t, "buildDetailsContent", strings.Split(m.buildDetailsContent(), "\n"), limit)
				requireWithin(t, "viewport", strings.Split(m.renderDetailsView(), "\n"), limit)
			})
		}
	}
}

// ── The Message is capped (AC2) ──────────────────────────────────────────────

func TestTheMessageShowsEightLinesThenSaysHowManyAreLeft(t *testing.T) {
	f := renderFailureFixture()
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			section := sectionAfter(plainLines(openDetail(t, f, size.width, size.height)), "Message:")
			if len(section) != maxMessageLines+1 {
				t.Fatalf("Message section = %d lines, want %d then the marker:\n%s", len(section), maxMessageLines, strings.Join(section, "\n"))
			}
			if got, want := section[maxMessageLines], moreLines(7); got != want {
				t.Errorf("marker = %q, want %q", got, want)
			}
			if !strings.HasPrefix(section[7], "line 08") {
				t.Errorf("the eighth line = %q, want line 08", section[7])
			}
			if joined := strings.Join(section, "\n"); strings.Contains(joined, "line 09") {
				t.Errorf("a ninth line leaked into the Message:\n%s", joined)
			}
		})
	}
}

func TestTheMessageIsCappedAtItsWidth(t *testing.T) {
	msg := strings.Repeat("alpha beta gamma ", 60)
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			section := sectionAfter(plainLines(openDetail(t, ksv0001(msg), size.width, size.height)), "Message:")
			_, hidden := cappedLines(msg, textWidth(size.width), maxMessageLines)
			if hidden == 0 {
				t.Fatal("the fixture does not overflow the cap")
			}
			if len(section) != maxMessageLines+1 || section[maxMessageLines] != moreLines(hidden) {
				t.Fatalf("section = %q, want %d lines then %q", section, maxMessageLines, moreLines(hidden))
			}
			widest := 0
			for _, line := range section[:maxMessageLines] {
				widest = max(widest, ansi.StringWidth(line))
			}
			limit := min(textWidth(size.width), maxMessageWidth)
			if widest > limit {
				t.Errorf("a Message line spans %d cells, limit %d", widest, limit)
			}
			// At 175 the text could take 171 cells and the cap is the reason it
			// does not: the lines are well past what 80 columns allow.
			if size.width == 175 && widest <= 100 {
				t.Errorf("at 175 the Message wraps at %d cells, want up to %d", widest, maxMessageWidth)
			}
		})
	}
}

// Two findings of the same rule in two containers say which container.
func TestTwoFindingsOfOneRuleAreToldApart(t *testing.T) {
	web := openDetail(t, ksv0001("Container 'web' of Deployment 'app' should set readOnlyRootFilesystem"), 120, 28)
	side := openDetail(t, ksv0001("Container 'sidecar' of Deployment 'app' should set readOnlyRootFilesystem"), 120, 28)

	a, b := ansi.Strip(web.buildDetailsContent()), ansi.Strip(side.buildDetailsContent())
	if a == b {
		t.Fatal("the details of two findings that differ by their Message are identical")
	}
	if !strings.Contains(a, "Container 'web'") || strings.Contains(a, "sidecar") {
		t.Errorf("the first detail does not name its container:\n%s", a)
	}
	if !strings.Contains(b, "Container 'sidecar'") || strings.Contains(b, "'web'") {
		t.Errorf("the second detail does not name its container:\n%s", b)
	}
}

// A Message that says what the title or the description already said adds
// nothing but length.
func TestAMessageThatRepeatsTheTitleOrTheDescriptionIsNotShownAgain(t *testing.T) {
	sameTitle := ksv0001("Same words as the title")
	sameTitle.Title = "Same words as the title"
	sameDescription := ksv0001("Same words as the description")
	sameDescription.Description = "Same words as the description"

	for name, tc := range map[string]struct {
		f    scan.Finding
		text string
	}{
		"title":       {sameTitle, "Same words as the title"},
		"description": {sameDescription, "Same words as the description"},
	} {
		t.Run(name, func(t *testing.T) {
			content := ansi.Strip(openDetail(t, tc.f, 120, 28).buildDetailsContent())
			if n := strings.Count(content, tc.text); n != 1 {
				t.Errorf("%q is shown %d times, want once:\n%s", tc.text, n, content)
			}
			if strings.Contains(content, "Message:") {
				t.Errorf("a Message heading is left over for a repeated text:\n%s", content)
			}
		})
	}
}

// ── What to do comes first (AC3, AC4) ────────────────────────────────────────

func TestTheFixStatusAndTheRemediationComeBeforeTheFacts(t *testing.T) {
	f := ksv0001("The container may escalate")
	f.ID, f.File, f.Line, f.IaCType = "DS002", "Dockerfile", 3, "dockerfile"
	f.Resolution = "Add a USER instruction with a non-root user."
	content := ansi.Strip(openDetail(t, f, 120, 28).buildDetailsContent())

	order := []string{labelBuiltIn, "Remediation:", "Message:", "Description:", "Source:", "Type:", "References:"}
	last := -1
	for _, want := range order {
		at := strings.Index(content, want)
		if at < 0 {
			t.Fatalf("the detail has no %q:\n%s", want, content)
		}
		if at < last {
			t.Errorf("%q comes before the previous section of %v:\n%s", want, order, content)
		}
		last = at
	}
}

// At 80x24 the viewport shows 11 lines of text: the status has to be in them,
// in words.
func TestTheFixStatusIsVisibleAtTheSmallestTerminal(t *testing.T) {
	m := openDetail(t, renderFailureFixture(), 80, 12)

	visible := ansi.Strip(m.detailsViewport.View())

	if !strings.Contains(visible, "No built-in fix") {
		t.Errorf("the status is below the fold at 80x24:\n%s", visible)
	}
}

func TestACommandIsRunAndAnInstructionIsNot(t *testing.T) {
	t.Run("a command", func(t *testing.T) {
		content := ansi.Strip(openDetail(t, cveWith("gomod", "go get golang.org/x/net@v0.7.0"), 175, 38).buildDetailsContent())
		if !strings.Contains(content, "Run: go get golang.org/x/net@v0.7.0") {
			t.Errorf("a real command is not shown under Run:\n%s", content)
		}
	})
	t.Run("an instruction", func(t *testing.T) {
		f := cveWith("conda", "Update foo to 1.2")
		f.PkgName = "foo"
		content := ansi.Strip(openDetail(t, f, 175, 38).buildDetailsContent())
		if !strings.Contains(content, "Update foo to 1.2") {
			t.Errorf("the instruction is not shown:\n%s", content)
		}
		if strings.Contains(content, "Run:") {
			t.Errorf("a sentence is presented as a command to run:\n%s", content)
		}
	})
}

// ── Nothing is left behind (AC7) ─────────────────────────────────────────────

// requireTidy fails on a blank line at either end of the text (the padding's own
// blank line is allowed once at each end) and on two blank lines in a row.
func requireTidy(t *testing.T, label, content string) {
	t.Helper()
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	lines := strings.Split(ansi.Strip(content), "\n")
	if len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	if len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		t.Errorf("%s: the content is empty", label)
		return
	}
	if blank(lines[0]) {
		t.Errorf("%s: the text starts with a blank line", label)
	}
	if blank(lines[len(lines)-1]) {
		t.Errorf("%s: the text ends with a blank line", label)
	}
	for i := 1; i < len(lines); i++ {
		if blank(lines[i]) && blank(lines[i-1]) {
			t.Errorf("%s: two blank lines in a row at text line %d:\n%s", label, i, strings.Join(lines, "\n"))
			return
		}
	}
}

func TestNoDetailHasAStrayBlankLine(t *testing.T) {
	for name, f := range detailsFixtures() {
		t.Run(name, func(t *testing.T) {
			requireTidy(t, name, openDetail(t, f, 120, 28).buildDetailsContent())
		})
	}
}

func TestAMinimalFindingHasNoEmptySection(t *testing.T) {
	content := ansi.Strip(openDetail(t, minimalFinding(), 120, 28).buildDetailsContent())

	for _, heading := range []string{"Remediation:", "Message:", "File:", "References:", "Description:", "Script:"} {
		if strings.Contains(content, heading) {
			t.Errorf("a finding with nothing to put under %q still has the heading:\n%s", heading, content)
		}
	}
	if !strings.Contains(content, "Only a title") || !strings.Contains(content, "X-1") {
		t.Errorf("the identity of the finding is gone:\n%s", content)
	}
}

func TestNoSelectedFindingIsNotAPanic(t *testing.T) {
	m := scannedModel(t)
	m.selectedFinding = nil

	content := m.buildDetailsContent()

	if !strings.Contains(ansi.Strip(content), "No finding selected") {
		t.Errorf("content = %q", ansi.Strip(content))
	}
}
