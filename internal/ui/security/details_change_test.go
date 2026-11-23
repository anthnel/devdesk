package security

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-753, lot 2, step 4 (the section) and the help of step 6. The states a
// stored preview can be in are built in details_change_state_test.go; here the
// text is read: the lines of the diff, the cap, the cut, the none and the delta.

// ── Reading the section ──────────────────────────────────────────────────────

// ansiPlain is the detail without its styling.
func ansiPlain(m Model) string { return ansi.Strip(m.buildDetailsContent()) }

// textLines are the lines of the detail without styling, the padding cell on the
// left and the spaces on the right gone. A marker of a hunk keeps its two spaces.
func textLines(m Model) []string {
	lines := plainLines(m)
	for i, l := range lines {
		lines[i] = strings.TrimRight(strings.TrimPrefix(l, " "), " ")
	}
	return lines
}

// blockFrom is the labelled line at index at and the lines that continue it, up
// to the next blank line or the next label of the detail.
func blockFrom(lines []string, at int) []string {
	out := []string{lines[at]}
	for _, next := range lines[at+1:] {
		if strings.TrimSpace(next) == "" || startsBlockLabel(next) {
			break
		}
		out = append(out, next)
	}
	return out
}

// changeBlock is the Change section: its first line, which starts with "Change:",
// and what follows it. Nil when the detail has none.
func changeBlock(m Model) []string {
	lines := textLines(m)
	for i, l := range lines {
		if strings.HasPrefix(l, "Change:") {
			return blockFrom(lines, i)
		}
	}
	return nil
}

// isDiffRow says whether a line of the section is a row of the diff: the marker
// of a change, or the two spaces of an indented hunk marker.
func isDiffRow(l string) bool {
	return strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") || strings.HasPrefix(l, "  ")
}

// diffRows are the rows of the diff in a block, without the label line, the
// "more lines" marker and the git line.
func diffRowsOf(block []string) []string {
	var rows []string
	for _, l := range block[min(1, len(block)):] {
		// The git advice may wrap under its label, on lines that start with spaces.
		if moreLinesMarker.MatchString(l) || strings.HasPrefix(l, "Git:") {
			break
		}
		if isDiffRow(l) {
			rows = append(rows, l)
		}
	}
	return rows
}

// moreLinesMarker is the line that says how many rows the section leaves out.
var moreLinesMarker = regexp.MustCompile(`^… (\d+) more lines?$`)

// the closed vocabulary of the first line of the section (plan, "Shapes").
var closedChange = regexp.MustCompile(`^Change: (\S+|none: DevDesk cannot write this fix|stale preview: \S+ changed after the scan|preview failed: .+|preparing the preview\.\.\.|\S+ \S+ -> .+)$`)

const noChange = "Change: none: DevDesk cannot write this fix"

// ── The section of a rule the catalog fixes ──────────────────────────────────

// A diff of at most eight rows is shown whole, under the file, then the git
// advice the modal shows.
func TestTheSectionShowsTheFileTheDiffAndTheGitAdvice(t *testing.T) {
	s := openPreview(t, fixtureFor(t, "AVD-DS-0021"), -time.Hour).answered(t)
	s.m = feed(t, s.m, testutil.Resize(120, 28))
	block := changeBlock(s.m)

	if len(block) < 2 || block[0] != "Change: Dockerfile" {
		t.Fatalf("the section reads %q, want it to start with \"Change: Dockerfile\"", block)
	}
	lines := s.msg.Prepared.Lines
	if len(lines) == 0 || len(lines) > 8 {
		t.Fatalf("the fixture has %d lines, want 1 to 8", len(lines))
	}
	if got := block[1 : 1+len(lines)]; !slices.Equal(got, lines) {
		t.Errorf("the rows of the section are not the lines of the preview:\n got: %q\nwant: %q", got, lines)
	}
	for _, l := range block[1:] {
		if moreLinesMarker.MatchString(l) {
			t.Errorf("a diff that fits says there are more lines: %q", l)
		}
	}
	git := strings.Join(strings.Fields(strings.Join(block[1+len(lines):], " ")), " ")
	if want := "Git: " + gitAdvice(s.msg.Prepared.File); git != want {
		t.Errorf("after the rows the section reads %q, want %q", git, want)
	}
}

// AC: the first eight rows, then how many more; the modal has all of them.
func TestALongDiffIsCappedAtEightRowsAndTheModalHasAllOfThem(t *testing.T) {
	s := openPreview(t, ds0002(t), -time.Hour).answered(t)
	s.m = feed(t, s.m, testutil.Resize(120, 28))
	lines := s.msg.Prepared.Lines
	if len(lines) <= 8 {
		t.Fatalf("the fixture has %d lines, it must have more than 8", len(lines))
	}
	block := changeBlock(s.m)
	if block[0] != "Change: Dockerfile" {
		t.Fatalf("the section reads %q", block)
	}
	if got := block[1:9]; !slices.Equal(got, lines[:8]) {
		t.Errorf("the first eight rows are not the first eight lines:\n got: %q\nwant: %q", got, lines[:8])
	}
	want := fmt.Sprintf("… %d more lines", len(lines)-8)
	if block[9] != want {
		t.Errorf("after the eighth row the section reads %q, want %q", block[9], want)
	}
	if rows := diffRowsOf(block[:10]); len(rows) != 8 {
		t.Errorf("the section shows %d rows, want 8", len(rows))
	}

	// ctrl+o, from this very detail: the confirmation has the whole diff.
	m, prepared := previewed(t, s.m)
	if m.confirmModal == nil {
		t.Fatalf("no confirmation; footer %q", m.footer.Text())
	}
	if !slices.Equal(prepared.Lines, lines) {
		t.Errorf("ctrl+o computed other lines than the preview:\n got: %q\nwant: %q", prepared.Lines, lines)
	}
	if indexOfBlock(modalLines(m), lines) < 0 {
		t.Errorf("the modal does not hold the %d lines of the diff:\n%s", len(lines), strings.Join(modalLines(m), "\n"))
	}
}

// The section sits in the Fix block's neighbourhood: after Effect (and Warning),
// before Why.
func TestTheSectionComesAfterTheEffectAndBeforeWhy(t *testing.T) {
	fx := ds0002(t)
	fx.finding.Description = "Running as root lets a compromised process write anywhere in the container."
	s := openPreview(t, fx, -time.Hour).answered(t)
	lines := textLines(s.m)

	order := []string{"Fix:", "Where:", "Effect:", "Change:", "Why:"}
	last := -1
	for _, label := range order {
		at := labelAt(lines, label)
		if at < 0 {
			t.Fatalf("no %s line:\n%s", label, strings.Join(lines, "\n"))
		}
		if at <= last {
			t.Errorf("%s is at line %d, not after the previous label (%d)", label, at, last)
		}
		last = at
	}
	if w := labelAt(lines, "Warning:"); w >= 0 && w > labelAt(lines, "Change:") {
		t.Error("Warning comes after Change")
	}
}

// ── Every rule, at the three widths ──────────────────────────────────────────

// For every rule of the catalog: the section names the file the fix edits, every
// row starts with +, - or two spaces, nothing is wider than the column, and the
// git advice is there.
func TestEveryRuleOfTheCatalogShowsItsChangeAtEveryWidth(t *testing.T) {
	for _, fx := range changeFixtures(t) {
		t.Run(fx.id, func(t *testing.T) {
			s := openPreview(t, fx, -time.Hour).answered(t)
			if s.msg.Prepared.Err != nil {
				t.Fatalf("the preview failed: %v", s.msg.Prepared.Err)
			}
			for _, size := range detailsSizes {
				m := feed(t, s.m, testutil.Resize(size.width, size.height))
				block := changeBlock(m)
				if len(block) < 3 || block[0] != "Change: "+fx.edited {
					t.Fatalf("%d columns: the section reads %q, want it to start with %q", size.width, block, "Change: "+fx.edited)
				}
				rows := diffRowsOf(block)
				if len(rows) == 0 || len(rows) > 8 {
					t.Errorf("%d columns: %d rows, want 1 to 8", size.width, len(rows))
				}
				limit := min(maxColumnWidth, textWidth(size.width))
				for _, l := range block {
					if w := ansi.StringWidth(l); w > limit {
						t.Errorf("%d columns: %q is %d cells wide, the column is %d", size.width, l, w, limit)
					}
				}
				if !strings.Contains(strings.Join(block, " "), "Git: ") {
					t.Errorf("%d columns: no git advice in %q", size.width, block)
				}
				if !closedChange.MatchString(block[0]) {
					t.Errorf("the first line %q is not in the closed vocabulary", block[0])
				}
			}
		})
	}
}

// A line longer than the column is cut with the ellipsis, never wider than the
// column, and nothing in the detail is wider than the viewport allows.
func TestALongRowIsCutToTheColumnAtEveryWidth(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install " + strings.Repeat("a-package-with-a-long-name ", 14) + "\n"
	fx := dockerfileFixture("AVD-DS-0021", src, 2, 2)
	s := openPreview(t, fx, -time.Hour).answered(t)
	if s.msg.Prepared.Err != nil {
		t.Fatalf("the preview failed: %v", s.msg.Prepared.Err)
	}
	for _, size := range detailsSizes {
		t.Run(fmt.Sprintf("%d", size.width), func(t *testing.T) {
			m := feed(t, s.m, testutil.Resize(size.width, size.height))
			limit := min(maxColumnWidth, textWidth(size.width))
			cut := 0
			for _, l := range diffRowsOf(changeBlock(m)) {
				if w := ansi.StringWidth(l); w > limit {
					t.Errorf("%q is %d cells wide, the column is %d", l, w, limit)
				}
				if strings.HasSuffix(l, theme.Ellipsis) {
					cut++
				}
			}
			if cut == 0 {
				t.Errorf("no row was cut with %q, the fixture's rows are over %d cells", theme.Ellipsis, limit)
			}
			for _, l := range plainLines(m) {
				if w := ansi.StringWidth(l); w > size.width-2 {
					t.Errorf("a line of the detail is %d cells wide, the viewport has %d: %q", w, size.width-2, l)
				}
			}
		})
	}
}

// ── Nothing from the file reaches the terminal ───────────────────────────────

var sgrOnly = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func TestAHostileFileNeverReachesTheSection(t *testing.T) {
	const hostile = "\x1b]0;pwned\x07\x1b[31mred\x1b[0m"
	src := "FROM debian:12\nRUN apt-get install -y curl # " + hostile + "\n"
	s := openPreview(t, dockerfileFixture("AVD-DS-0029", src, 2, 2), -time.Hour).answered(t)
	if s.m.changePreview.State != previewReady {
		t.Fatalf("state = %v (Err %v), want ready", s.m.changePreview.State, s.msg.Prepared.Err)
	}
	raw := s.m.buildDetailsContent()
	if strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\x07") {
		t.Errorf("the detail holds an OSC or a bell: %q", raw)
	}
	if left := sgrOnly.ReplaceAllString(raw, ""); strings.Contains(left, "\x1b") {
		t.Errorf("an escape sequence other than the theme's colours is in the detail: %q", left)
	}
	block := strings.Join(changeBlock(s.m), "\n")
	if strings.Contains(block, "pwned") || !strings.Contains(block, "red") {
		t.Errorf("the section should keep the visible text only:\n%s", block)
	}
}

// The block is written in ColorText alone: the markers + and - carry the
// meaning, never a red and a green (the contrast of a dim or a coloured text
// falls under 4.5:1 on several shipped themes, as the Fix block's test says).
func TestTheSectionUsesNoColourButTheTextColour(t *testing.T) {
	text := strings.ToLower(theme.Hex(theme.ColorText))
	scenes := previewScenes(t)
	scenes["none"] = previewScene{m: func() Model {
		m, _ := openOn(t, findingFixtures()[1], detailsTarget, scan.TargetDirectory, 120, 28)
		return m
	}()}
	scenes["delta"] = previewScene{m: func() Model {
		m, _ := openOn(t, versionedCVE("0.8.0", "0.17.0"), detailsTarget, scan.TargetDirectory, 120, 28)
		return m
	}()}
	for name, s := range scenes {
		t.Run(name, func(t *testing.T) {
			styled := strings.Split(s.m.buildDetailsContent(), "\n")
			plain := textLines(s.m)
			at := labelAt(plain, "Change:")
			if at < 0 {
				t.Fatal("no Change section")
			}
			rows := len(blockFrom(plain, at))
			for i := at; i < at+rows; i++ {
				for hex := range foregrounds(styled[i]) {
					if hex != text {
						t.Errorf("%q carries the colour %s, want only ColorText %s", plain[i], hex, text)
					}
				}
			}
		})
	}
}

// ── The states of the section ────────────────────────────────────────────────

func TestTheSectionOfEachPreviewState(t *testing.T) {
	scenes := previewScenes(t)
	tests := []struct {
		state string
		want  []string
	}{
		{"pending", []string{"Change: preparing the preview..."}},
		{"expired", []string{"Change: stale preview: Dockerfile changed after the scan"}},
		{"declined", []string{noChange}},
		{"failed", []string{"Change: preview failed: Dockerfile no longer exists"}},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			block := changeBlock(scenes[tt.state].m)
			if !slices.Equal(block, tt.want) {
				t.Errorf("the section reads %q, want %q", block, tt.want)
			}
			if rows := diffRowsOf(block); len(rows) != 0 {
				t.Errorf("a diff is shown with no preview: %q", rows)
			}
		})
	}
	for state, s := range scenes {
		if block := changeBlock(s.m); len(block) > 0 && !closedChange.MatchString(block[0]) {
			t.Errorf("%s: the first line %q is not in the closed vocabulary", state, block[0])
		}
	}
}

// A stale file shows no diff (H4): computed on content the scan never read, with
// the scan's line numbers, it could be wrong. The reason is all it says.
func TestAStalePreviewShowsNoDiffAndNoGitAdvice(t *testing.T) {
	s := expiredScene(t)
	content := ansiPlain(s.m)
	for _, bad := range []string{"@@", "Git:", "\n+", "\n-"} {
		if strings.Contains(content, bad) {
			t.Errorf("a stale preview shows %q:\n%s", bad, content)
		}
	}
}

// ── What DevDesk cannot write: no invented diff ──────────────────────────────

func versionedCVE(version, fixedIn string) scan.Finding {
	return scan.Finding{
		ID: "CVE-2026-4242", Title: "A flaw in libfoo", Severity: scan.SeverityHigh, Source: "trivy",
		PkgName: "libfoo", Version: version, FixedIn: fixedIn, Class: scan.ClassLangPackages,
	}
}

func TestWhatDevDeskCannotWriteSaysNoneAndShowsNoDiff(t *testing.T) {
	tests := []struct {
		name   string
		f      scan.Finding
		target string
		kind   scan.TargetType
	}{
		{"a CVE with no fixed version", findingFixtures()[1], detailsTarget, scan.TargetDirectory},
		{"a secret", findingFixtures()[2], detailsTarget, scan.TargetDirectory},
		{"a licence", findingFixtures()[3], detailsTarget, scan.TargetDirectory},
		{"a render failure", renderFailureFixture(), detailsTarget, scan.TargetDirectory},
		{"a schema failure", scan.Finding{ID: scan.K8sSchemaID, Title: "Invalid field", Severity: scan.SeverityHigh,
			Source: scan.SourceKubeconform, File: "deploy.yaml", IaCType: "kubernetes"}, detailsTarget, scan.TargetDirectory},
		{"a Helm lint", scan.Finding{ID: scan.HelmLintID, Title: "Chart lint", Severity: scan.SeverityMedium,
			Source: scan.SourceHelm, File: "chart/Chart.yaml", IaCType: "helm"}, detailsTarget, scan.TargetDirectory},
		{"a rule outside the catalog", misconfigOf("KSV-0003"), detailsTarget, scan.TargetDirectory},
		{"a catalog rule on an image", misconfigOf("AVD-DS-0002"), "app:1.0", scan.TargetImage},
		{"a refusal the finding settles", unconfinedSeccomp(), detailsTarget, scan.TargetDirectory},
	}
	for _, tt := range tests {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", tt.name, size.width), func(t *testing.T) {
				m, _ := openOn(t, tt.f, tt.target, tt.kind, size.width, size.height)
				if block := changeBlock(m); !slices.Equal(block, []string{noChange}) {
					t.Errorf("the section reads %q, want %q", block, noChange)
				}
			})
		}
	}
}

func TestVersionDelta(t *testing.T) {
	tests := []struct {
		name string
		f    scan.Finding
		want string
		ok   bool
	}{
		{"one fixed version", versionedCVE("0.8.0", "0.17.0"), "libfoo 0.8.0 -> 0.17.0", true},
		{"the branch of the installed major, the lowest", versionedCVE("0.8.0", "0.8.5, 0.17.0, 1.0.0"), "libfoo 0.8.0 -> 0.8.5", true},
		{"another major only", versionedCVE("2.1.0", "1.4.9, 3.0.1"), "libfoo 2.1.0 -> 1.4.9", true},
		{"versions that cannot be compared: the raw text, as the catalog does", versionedCVE("1.0", "alpha, beta"), "libfoo 1.0 -> alpha, beta", true},
		{"no fixed version", versionedCVE("0.8.0", ""), "", false},
		{"no installed version", versionedCVE("", "0.17.0"), "", false},
		{"no package", func() scan.Finding { f := versionedCVE("0.8.0", "0.17.0"); f.PkgName = ""; return f }(), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := versionDelta(tt.f)
			if got != tt.want || ok != tt.ok {
				t.Errorf("versionDelta = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
	// The same pick as the command DevDesk offers: one computation.
	f := versionedCVE("0.8.0", "0.8.5, 0.17.0")
	if got, _ := versionDelta(f); !strings.HasSuffix(got, "-> "+scan.PickFixed(f.Version, f.FixedIn)) {
		t.Errorf("versionDelta = %q, not the version scan.PickFixed chooses", got)
	}
}

// A vulnerability with a fixed version shows the move, one line, never a diff of
// a lockfile DevDesk does not write.
func TestAVulnerabilityWithAFixedVersionShowsTheVersionMove(t *testing.T) {
	for _, size := range detailsSizes {
		t.Run(fmt.Sprintf("%d", size.width), func(t *testing.T) {
			m, cmd := openOn(t, versionedCVE("0.8.0", "0.17.0"), detailsTarget, scan.TargetDirectory, size.width, size.height)
			if cmd != nil {
				t.Error("a vulnerability started a preview")
			}
			want := []string{"Change: libfoo 0.8.0 -> 0.17.0"}
			if block := changeBlock(m); !slices.Equal(block, want) {
				t.Errorf("the section reads %q, want %q", block, want)
			}
			if !strings.Contains(ansiPlain(m), "0.8.0 -> 0.17.0") {
				t.Error("the move is not in the detail")
			}
		})
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func TestHelpDescribesTheChangeSection(t *testing.T) {
	body := helpSection(t, "Finding details")
	for _, want := range []string{
		"Change", // the section
		"diff",   // what it shows
		"Git",    // the advice next to it
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Finding details help does not mention %q:\n%s", want, body)
		}
	}
	// The order the help promises is the order of the screen.
	if fix, change, why := strings.Index(body, "Effect"), strings.Index(body, "Change"), strings.Index(body, "Why holds"); fix < 0 || change < fix || (why >= 0 && change > why) {
		t.Errorf("the help does not put Change after Effect and before Why (Effect %d, Change %d, Why %d)", fix, change, why)
	}
}
