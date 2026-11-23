package security

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/remediation"
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
	return openDetailOn(t, f, detailsTarget, scan.TargetDirectory, width, height)
}

// openDetailOnImage is openDetail for a finding of an image scan, whose files
// are inside the image and cannot be written.
func openDetailOnImage(t *testing.T, f scan.Finding, width, height int) Model {
	t.Helper()
	return openDetailOn(t, f, "app:1.0", scan.TargetImage, width, height)
}

func openDetailOn(t *testing.T, f scan.Finding, target string, kind scan.TargetType, width, height int) Model {
	t.Helper()
	result := &scan.Result{Target: target, TargetType: kind, Findings: []scan.Finding{f}}
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

// fullFinding is a finding with something under every section of the Fix block
// and after it: a package and a file (Where), a Resolution (Do), a command (Run),
// a fixed version (Effect), a description (Why), a long Message (Output) and
// references (Refs). Every text is long enough to wrap at 175 columns.
func fullFinding() scan.Finding {
	f := cveWith("gomod", "go get golang.org/x/net@v0.7.0")
	f.File = "go.mod"
	f.Class = scan.ClassLangPackages
	f.Resolution = strings.Repeat("Upgrade the module to the fixed release and rebuild every binary that links it. ", 4)
	f.Description = strings.Repeat("The HTTP/2 server does not bound the number of streams a peer may open. ", 4)
	f.Message = strings.Repeat("alpha beta gamma delta ", 80)
	return f
}

// plumberControl is a plumber control: a job and the script line that fired it.
func plumberControl() scan.Finding {
	return scan.Finding{
		ID: "PLUMBER-7", Title: "Unpinned download in a script", Severity: scan.SeverityHigh, Source: scan.SourcePlumber,
		File: ".gitlab-ci.yml", Job: "build", ScriptLine: "curl -sSL https://example.test/install.sh | sh",
		Description: "A script pipes a download into a shell.", Resolution: "Pin the download to a checksum.",
	}
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
		"full":           fullFinding(),
		"ci":             plumberControl(),
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

// ── The Output (the scanner's Message) is capped ──────────────────────────────────────────────

func TestTheOutputShowsEightLinesThenSaysHowManyAreLeft(t *testing.T) {
	f := renderFailureFixture()
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			section := sectionAfter(plainLines(openDetail(t, f, size.width, size.height)), "Output:")
			if len(section) != maxMessageLines+1 {
				t.Fatalf("Output section = %d lines, want %d then the marker:\n%s", len(section), maxMessageLines, strings.Join(section, "\n"))
			}
			if got, want := section[maxMessageLines], moreLines(7); got != want {
				t.Errorf("marker = %q, want %q", got, want)
			}
			if !strings.HasPrefix(section[7], "line 08") {
				t.Errorf("the eighth line = %q, want line 08", section[7])
			}
			if joined := strings.Join(section, "\n"); strings.Contains(joined, "line 09") {
				t.Errorf("a ninth line leaked into the Output:\n%s", joined)
			}
		})
	}
}

func TestTheOutputIsCappedAtItsWidth(t *testing.T) {
	msg := strings.Repeat("alpha beta gamma ", 60)
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			section := sectionAfter(plainLines(openDetail(t, ksv0001(msg), size.width, size.height)), "Output:")
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
				t.Errorf("an Output line spans %d cells, limit %d", widest, limit)
			}
			// At 175 the text could take 171 cells and the cap is the reason it
			// does not: the lines are well past what 80 columns allow.
			if size.width == 175 && widest <= 100 {
				t.Errorf("at 175 the Output wraps at %d cells, want up to %d", widest, maxMessageWidth)
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
			if strings.Contains(content, "Output:") {
				t.Errorf("an Output heading is left over for a repeated text:\n%s", content)
			}
		})
	}
}

// ── The Fix block (DEVDESK-751, AC1, AC2) ────────────────────────────────────

// blockLabels are the labels of the Fix block and of the sections after it, in
// the order the detail shows them.
var blockLabels = []string{"Fix:", "Where:", "Do:", "Run:", "Effect:", "Warning:", "Change:", "Why:", "Output:", "Refs:"}

// labelAt is the index of the first line that starts with label, or -1.
func labelAt(lines []string, label string) int {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), label) {
			return i
		}
	}
	return -1
}

func startsBlockLabel(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, label := range blockLabels {
		if strings.HasPrefix(trimmed, label) {
			return true
		}
	}
	return false
}

// labelledLines is the line that starts with label and the ones that continue it,
// up to the next label or the next blank line.
func labelledLines(lines []string, label string) []string {
	at := labelAt(lines, label)
	if at < 0 {
		return nil
	}
	out := []string{lines[at]}
	for _, next := range lines[at+1:] {
		if strings.TrimSpace(next) == "" || startsBlockLabel(next) {
			break
		}
		out = append(out, next)
	}
	return out
}

// squash joins words with single spaces, so a text compares equal however it
// was wrapped.
func squash(text string) string { return strings.Join(strings.Fields(text), " ") }

// labelledText is the wrapped text of one labelled line, squashed.
func labelledText(lines []string, label string) string {
	return squash(strings.Join(labelledLines(lines, label), " "))
}

func leadingSpaces(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// columnWidth is the text width at a terminal width: one column, never wider
// than maxColumnWidth.
func columnWidth(width int) int { return min(width-4, 110) }

func TestTheTextIsOneColumnOfAtMost110Cells(t *testing.T) {
	for width, want := range map[int]int{80: 76, 120: 110, 175: 110} {
		m := openDetail(t, fullFinding(), width, 28)
		if got := m.detailsTextWidth(); got != want {
			t.Errorf("detailsTextWidth at %d columns = %d, want %d", width, got, want)
		}
	}
}

// At 120 and 175 columns no line of any fixture is wider than the column plus
// the padding cell on its left, and a long text does fill the column.
func TestNoLineOfTheDetailIsWiderThanTheColumn(t *testing.T) {
	for name, f := range detailsFixtures() {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", name, size.width), func(t *testing.T) {
				widest := 0
				for _, line := range plainLines(openDetail(t, f, size.width, size.height)) {
					widest = max(widest, ansi.StringWidth(strings.TrimRight(line, " ")))
				}
				if limit := columnWidth(size.width) + 1; widest > limit {
					t.Errorf("a line spans %d cells, the column plus padding is %d", widest, limit)
				}
			})
		}
	}
	widest := 0
	for _, line := range plainLines(openDetail(t, fullFinding(), 175, 38)) {
		widest = max(widest, ansi.StringWidth(strings.TrimRight(line, " ")))
	}
	if widest < 100 {
		t.Errorf("at 175 the widest line of a long finding spans %d cells: the column is not used up to 110", widest)
	}
}

// Fix is the first section, one blank line after the title, on every finding:
// a finding with nothing to say says so ("No guidance available") instead of
// leaving the reader to wonder. Its sub-fields follow with no blank line, and
// the sections after the block are each separated by one blank line.
func TestTheFixBlockComesFirstForEveryFinding(t *testing.T) {
	for name, f := range detailsFixtures() {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", name, size.width), func(t *testing.T) {
				lines := plainLines(openDetail(t, f, size.width, size.height))
				fix := labelAt(lines, "Fix:")
				if fix < 0 {
					t.Fatalf("no Fix line:\n%s", strings.Join(lines, "\n"))
				}
				// padding, header, title, one blank line, Fix.
				if fix != 4 || strings.TrimSpace(lines[fix-1]) != "" || strings.TrimSpace(lines[fix-2]) == "" {
					t.Errorf("Fix is on line %d, want 4, one blank line after the title:\n%s", fix, strings.Join(lines[:min(len(lines), 8)], "\n"))
				}
				end := fix
				for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
					end++
				}
				for _, label := range []string{"Where:", "Do:", "Run:", "Effect:", "Warning:"} {
					if at := labelAt(lines, label); at >= end {
						t.Errorf("%s is on line %d, after the blank line that ends the block (%d)", label, at, end)
					}
				}
				for _, label := range []string{"Why:", "Output:", "Refs:"} {
					if at := labelAt(lines, label); at >= 0 && at < end {
						t.Errorf("%s is on line %d, inside the Fix block that ends at %d", label, at, end)
					}
				}
				if why := labelAt(lines, "Why:"); why >= 0 && why < fix {
					t.Errorf("Why (line %d) comes before Fix (line %d)", why, fix)
				}
			})
		}
	}
}

func TestTheSectionsComeInTheDocumentedOrder(t *testing.T) {
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			lines := plainLines(openDetail(t, fullFinding(), size.width, size.height))
			last := -1
			for _, label := range []string{"Fix:", "Where:", "Do:", "Run:", "Effect:", "Why:", "Output:", "Refs:"} {
				at := labelAt(lines, label)
				if at < 0 {
					t.Fatalf("the detail has no %s:\n%s", label, strings.Join(lines, "\n"))
				}
				if at <= last {
					t.Errorf("%s is on line %d, not after the previous section (line %d)", label, at, last)
				}
				last = at
			}
		})
	}
}

// The old headings are gone: the sections are the ones of the new layout.
func TestTheOldHeadingsAreGone(t *testing.T) {
	for name, f := range detailsFixtures() {
		content := ansi.Strip(openDetail(t, f, 120, 28).buildDetailsContent())
		for _, old := range []string{"Remediation:", "Message:", "Description:", "References:", "Fix status:"} {
			if strings.Contains(content, old) {
				t.Errorf("%s: the detail still has the heading %q", name, old)
			}
		}
	}
}

// ── The Fix line says the status in words (AC1) ──────────────────────────────

func TestTheFixLineSaysTheStatusInWords(t *testing.T) {
	declines := unconfinedSeccomp()
	rule, ok := remediation.FixFor(declines)
	if !ok || rule.Declines == nil || rule.Declines(declines) == "" {
		t.Fatal("KSV-0104 unconfined must decline from the finding alone")
	}
	cve := func(fixedIn string) scan.Finding { f := cveWith("gomod", ""); f.FixedIn = fixedIn; return f }

	tests := []struct {
		name  string
		f     scan.Finding
		image bool
		want  string
	}{
		{"a catalog rule on a directory", misconfigOf("DS002"), false, "Fix: Built-in fix available"},
		{"a catalog rule that declines this instance", declines, false, "Fix: Built-in fix unavailable: " + rule.Declines(declines)},
		{"a catalog rule on an image", misconfigOf("DS002"), true, "Fix: Cannot fix here"},
		{"a rule with no catalog fix, on an image", misconfigOf("KSV-0003"), true, "Fix: Cannot fix here"},
		{"a rule with a missing text", misconfigOf("KSV-0012"), false, "Fix: Manual"},
		{"a rule with a Resolution", withResolution(misconfigOf("KSV-0003")), false, "Fix: Manual"},
		{"a rule with nothing to say", misconfigOf("KSV-0003"), false, "Fix: No guidance available"},
		{"a CVE with a fixed version", cve("0.7.0"), false, "Fix: Manual"},
		{"a CVE with no fixed version", cve(""), false, "Fix: No fix published"},
		{"a secret with a Resolution", withResolution(longSecret()), false, "Fix: Manual"},
		{"a secret with nothing to say", longSecret(), false, "Fix: No guidance available"},
		{"a render failure with a Resolution", renderFailureFixture(), false, "Fix: Manual"},
		{"a minimal finding", minimalFinding(), false, "Fix: No fix published"},
	}
	for _, tc := range tests {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", tc.name, size.width), func(t *testing.T) {
				open := openDetail
				if tc.image {
					open = openDetailOnImage
				}
				lines := plainLines(open(t, tc.f, size.width, size.height))
				if got := labelledText(lines, "Fix:"); got != squash(tc.want) {
					t.Errorf("the Fix line reads %q, want %q", got, squash(tc.want))
				}
			})
		}
	}
}

// ── Where, Do, Run, Effect (AC2) ─────────────────────────────────────────────

func TestWhereSaysWhereTheFindingIs(t *testing.T) {
	tests := []struct {
		name string
		f    scan.Finding
		want []string
	}{
		{"a file and a line", ksv0001("A message"), []string{"deploy.yaml:10"}},
		{"a package and a file", fullFinding(), []string{"go.mod", "golang.org/x/net 0.1.0", "Application dependency"}},
		{"a CI job and its script", plumberControl(), []string{".gitlab-ci.yml", "build", "curl -sSL https://example.test/install.sh | sh"}},
		{"a secret", longSecret(), []string{"config/prod.env:12", strings.Repeat("A", 40)}},
	}
	for _, tc := range tests {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", tc.name, size.width), func(t *testing.T) {
				lines := plainLines(openDetail(t, tc.f, size.width, size.height))
				where := labelAt(lines, "Where:")
				if where < 0 {
					t.Fatalf("no Where line:\n%s", strings.Join(lines, "\n"))
				}
				if fix := labelAt(lines, "Fix:"); where <= fix {
					t.Errorf("Where (line %d) is not after Fix (line %d)", where, fix)
				}
				text := labelledText(lines, "Where:")
				for _, want := range tc.want {
					if !strings.Contains(text, squash(want)) {
						t.Errorf("Where does not say %q:\n%s", want, strings.Join(labelledLines(lines, "Where:"), "\n"))
					}
				}
			})
		}
	}
}

func TestAFindingWithNothingToLocateHasNoWhere(t *testing.T) {
	for _, f := range []scan.Finding{minimalFinding(), {ID: "X-2", Title: "Only a title", Severity: scan.SeverityLow, Source: scan.SourcePlumber}} {
		if content := ansi.Strip(openDetail(t, f, 120, 28).buildDetailsContent()); strings.Contains(content, "Where:") {
			t.Errorf("%s has nothing to locate and still has a Where heading:\n%s", f.ID, content)
		}
	}
}

// A Resolution is the scanner's own paragraph: it is shown as it came, under Do,
// whatever the width wraps it to.
func TestTheResolutionIsShownVerbatimUnderDo(t *testing.T) {
	for name, f := range map[string]scan.Finding{
		"a render failure": renderFailureFixture(),
		"a long finding":   fullFinding(),
		"a CI control":     plumberControl(),
	} {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", name, size.width), func(t *testing.T) {
				lines := plainLines(openDetail(t, f, size.width, size.height))
				if labelAt(lines, "Do:") < 0 {
					t.Fatalf("no Do section:\n%s", strings.Join(lines, "\n"))
				}
				if got := labelledText(lines, "Do:"); !strings.Contains(got, squash(f.Resolution)) {
					t.Errorf("Do does not hold the Resolution %q:\n%s", f.Resolution, got)
				}
			})
		}
	}
}

// The steps are numbered, and a step that wraps hangs under its own text. The
// Resolution paragraph comes after them.
func TestDoNumbersItsStepsAndHangsTheRest(t *testing.T) {
	f := withResolution(misconfigOf("KSV-0012"))
	missing, ok := remediation.MissingFor(f)
	if !ok {
		t.Fatal("KSV-0012 must have a MissingFor text")
	}
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			lines := plainLines(openDetail(t, f, size.width, size.height))
			section := labelledLines(lines, "Do:")
			if len(section) == 0 {
				t.Fatalf("no Do section:\n%s", strings.Join(lines, "\n"))
			}
			if first := strings.TrimSpace(section[0]); !strings.HasPrefix(first, "Do: 1. ") {
				t.Errorf("the first step is not numbered: %q", first)
			}
			text := squash(strings.Join(section, " "))
			step, resolution := strings.Index(text, squash(missing)), strings.Index(text, squash(f.Resolution))
			if step < 0 || resolution < 0 || step > resolution {
				t.Errorf("Do must read the step then the Resolution (step at %d, Resolution at %d):\n%s", step, resolution, text)
			}
			if len(section) < 2 {
				t.Fatalf("the fixture does not wrap at %d columns:\n%s", size.width, strings.Join(section, "\n"))
			}
			for _, line := range section[1:] {
				if leadingSpaces(line)-leadingSpaces(section[0]) < 4 {
					t.Errorf("a continuation line does not hang under Do: %q", line)
				}
			}
		})
	}
}

func TestRunIsOneLineStartingWithADollar(t *testing.T) {
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			lines := plainLines(openDetail(t, cveWith("gomod", "go get golang.org/x/net@v0.7.0"), size.width, size.height))
			got := labelledLines(lines, "Run:")
			if len(got) != 1 || strings.TrimSpace(got[0]) != "Run: $ go get golang.org/x/net@v0.7.0" {
				t.Errorf("Run = %q, want the single line %q", got, "Run: $ go get golang.org/x/net@v0.7.0")
			}
		})
	}
}

// A command wider than the column is cut with an ellipsis on its one line: Y
// copies it whole (Q5).
func TestALongCommandIsCutOnItsOneLine(t *testing.T) {
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			lines := plainLines(openDetail(t, longCVE(), size.width, size.height))
			run := 0
			for _, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), "Run:") {
					run++
					trimmed := strings.TrimSpace(line)
					if ansi.StringWidth(trimmed) > columnWidth(size.width) || !strings.HasSuffix(trimmed, theme.Ellipsis) {
						t.Errorf("the Run line %q must be cut to %d cells with %q", trimmed, columnWidth(size.width), theme.Ellipsis)
					}
				}
				if strings.Contains(line, "ppp") && !strings.HasPrefix(strings.TrimSpace(line), "Run:") {
					t.Errorf("the command goes on over a second line: %q", line)
				}
			}
			if run != 1 {
				t.Errorf("%d Run lines, want 1", run)
			}
		})
	}
}

func TestAnInstructionIsAStepNotACommand(t *testing.T) {
	f := cveWith("conda", "Update foo to 1.2")
	f.PkgName = "foo"
	lines := plainLines(openDetail(t, f, 175, 38))
	if labelAt(lines, "Run:") >= 0 {
		t.Errorf("a sentence is presented as a command to run:\n%s", strings.Join(lines, "\n"))
	}
	if got := labelledText(lines, "Do:"); !strings.Contains(got, "1. Update foo to 1.2") {
		t.Errorf("Do = %q, want the instruction as its first step", got)
	}
}

func TestEffectSaysWhatTheFixDoes(t *testing.T) {
	hostNS, ok := remediation.FixFor(misconfigOf("KSV-0009"))
	if !ok || hostNS.Warning == "" {
		t.Fatal("KSV-0009 must be a catalog rule with a Warning")
	}
	builtIn := misconfigOf("KSV-0009")
	builtIn.Description = "Sharing the host network namespace exposes the node's network."

	t.Run("a built-in fix and its warning", func(t *testing.T) {
		lines := plainLines(openDetail(t, builtIn, 120, 28))
		if got := labelledText(lines, "Effect:"); got != squash("Effect: "+hostNS.Title) {
			t.Errorf("Effect = %q, want the rule's title %q", got, hostNS.Title)
		}
		if got := labelledText(lines, "Warning:"); got != squash("Warning: "+hostNS.Warning) {
			t.Errorf("Warning = %q, want %q", got, "Warning: "+hostNS.Warning)
		}
		effect, warning, why := labelAt(lines, "Effect:"), labelAt(lines, "Warning:"), labelAt(lines, "Why:")
		if effect < 0 || warning <= effect || why <= warning {
			t.Errorf("Effect (%d), Warning (%d) and Why (%d) are not in that order", effect, warning, why)
		}
	})
	t.Run("a fixed version", func(t *testing.T) {
		lines := plainLines(openDetail(t, cveWith("gomod", "go get golang.org/x/net@v0.7.0"), 120, 28))
		if got := labelledText(lines, "Effect:"); got != "Effect: Fixed in 0.7.0" {
			t.Errorf("Effect = %q, want %q", got, "Effect: Fixed in 0.7.0")
		}
		if labelAt(lines, "Warning:") >= 0 {
			t.Error("a CVE has no warning")
		}
	})
}

// ── Why, Output, Refs ────────────────────────────────────────────────────────

func TestWhyHoldsTheDescriptionTheSourceAndTheType(t *testing.T) {
	f := ksv0001("")
	for _, size := range detailsSizes {
		t.Run(fmt.Sprint(size.width), func(t *testing.T) {
			lines := plainLines(openDetail(t, f, size.width, size.height))
			why := labelAt(lines, "Why:")
			if why < 0 || strings.TrimSpace(lines[why]) != "Why:" {
				t.Fatalf("Why is not a heading on its own line:\n%s", strings.Join(lines, "\n"))
			}
			section := squash(strings.Join(sectionAfter(lines, "Why:"), " "))
			for _, want := range []string{f.Description, "Source: " + f.Source, "Type: " + f.IaCType} {
				if !strings.Contains(section, squash(want)) {
					t.Errorf("Why does not say %q:\n%s", want, section)
				}
			}
			for _, label := range []string{"Source:", "Type:"} {
				if at := labelAt(lines, label); at < why {
					t.Errorf("%s (line %d) is before Why (line %d)", label, at, why)
				}
			}
		})
	}
}

func TestRefsListsTheReferences(t *testing.T) {
	f := ksv0001("")
	f.References = []string{"https://avd.aquasec.com/misconfig/ksv001", "https://example.test/second"}
	section := sectionAfter(plainLines(openDetail(t, f, 120, 28)), "Refs:")
	if !reflect.DeepEqual(section, f.References) {
		t.Errorf("Refs = %q, want %q", section, f.References)
	}
}

// ── Visible at the smallest terminal ─────────────────────────────────────────

// At 80x24 the viewport shows 11 lines of text: the Fix line and the Where line
// are in them, on a finding with a long title and a long message.
func TestTheFixAndWhereAreVisibleAtTheSmallestTerminal(t *testing.T) {
	for name, f := range map[string]scan.Finding{
		"a render failure": renderFailureFixture(),
		"a misconfig":      ksv0001(strings.Repeat("alpha beta gamma ", 60)),
		"a CVE":            fullFinding(),
	} {
		t.Run(name, func(t *testing.T) {
			visible := ansi.Strip(openDetail(t, f, 80, 12).detailsViewport.View())
			for _, want := range []string{"Fix:", "Where:"} {
				if !strings.Contains(visible, want) {
					t.Errorf("%s is below the fold at 80x24:\n%s", want, visible)
				}
			}
		})
	}
}

// ── Contrast (AC7) ───────────────────────────────────────────────────────────

var foregroundSGR = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)

// foregrounds are the distinct "#rrggbb" foreground colours in a styled text.
func foregrounds(styled string) map[string]bool {
	out := map[string]bool{}
	for _, m := range foregroundSGR.FindAllStringSubmatch(styled, -1) {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		out[fmt.Sprintf("#%02x%02x%02x", r, g, b)] = true
	}
	return out
}

// Q1-A: the block is written in ColorText alone. ColorDim, the sub-title colour
// and the warning colour fall under 4.5:1 on several shipped themes (measured
// on 2026-11-23), so none of them is used inside the block; the word "Warning:"
// carries the warning. The 13 themes are checked in internal/ui/theme.
func TestTheFixBlockUsesNoColourButTheTextColour(t *testing.T) {
	text := strings.ToLower(theme.Hex(theme.ColorText))
	for _, other := range []struct {
		name string
		hex  string
	}{{"ColorDim", theme.Hex(theme.ColorDim)}, {"ColorSubTitleFg", theme.Hex(theme.ColorSubTitleFg)}, {"ColorWarn", theme.Hex(theme.ColorWarn)}} {
		if strings.EqualFold(other.hex, text) {
			t.Fatalf("the default theme gives %s the colour of ColorText: this test would prove nothing", other.name)
		}
	}
	hostNS := misconfigOf("KSV-0009")
	hostNS.Description = "Sharing the host network namespace exposes the node's network."
	for name, f := range map[string]scan.Finding{
		"a built-in fix with a warning": hostNS, "a CVE with a command": fullFinding(),
		"a misconfig with a Resolution": ksv0001("A message"), "a CI control": plumberControl(), "a minimal finding": minimalFinding(),
	} {
		t.Run(name, func(t *testing.T) {
			lines := strings.Split(openDetail(t, f, 120, 28).buildDetailsContent(), "\n")
			plain := make([]string, len(lines))
			for i, line := range lines {
				plain[i] = ansi.Strip(line)
			}
			fix := labelAt(plain, "Fix:")
			if fix < 0 {
				t.Fatalf("no Fix line:\n%s", strings.Join(plain, "\n"))
			}
			for i := fix; i < len(lines) && strings.TrimSpace(plain[i]) != ""; i++ {
				for hex := range foregrounds(lines[i]) {
					if hex != text {
						t.Errorf("line %q of the Fix block carries the colour %s, want only ColorText %s", plain[i], hex, text)
					}
				}
			}
		})
	}
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

	for _, heading := range []string{"Where:", "Do:", "Run:", "Effect:", "Warning:", "Why:", "Output:", "Refs:"} {
		if strings.Contains(content, heading) {
			t.Errorf("a finding with nothing to put under %q still has the heading:\n%s", heading, content)
		}
	}
	if !strings.Contains(content, "Fix: No fix published") {
		t.Errorf("a finding with nothing to say has no Fix line:\n%s", content)
	}
	if strings.Contains(content, "\n\n\n") {
		t.Errorf("a stray blank line is left behind:\n%q", content)
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
