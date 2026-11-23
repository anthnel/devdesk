package app

import (
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	uitestutil "github.com/anthnel/devdesk/internal/ui/testutil"
)

// floorNeed reads the row count out of the dashboard's floor warning, or
// returns 0 when the footer carries none.
var floorNeed = regexp.MustCompile(`Terminal too short for the dashboard \(need (\d+) rows\)`)

func floorRows(footer string) int {
	match := floorNeed.FindStringSubmatch(ansi.Strip(footer))
	if match == nil {
		return 0
	}
	n, _ := strconv.Atoi(match[1])
	return n
}

// dashboardBoxes is how many boxes the dashboard lays out at this size: six at
// wide, the only tier without a tab bar, four otherwise.
func dashboardBoxes(a *App) int {
	if a.lastFooterHeight == 2 {
		return 6
	}
	return 4
}

// TestTheDashboardClosesEveryBoxAtEveryRealisticSize (DEVDESK-743): the router's
// viewport does not scroll, so a grid taller than the viewport used to lose its
// last lines, the bottom border of the lowest boxes. From 24 rows up, every box
// the dashboard shows is closed; when it shows fewer than it lays out, the
// footer says so and how many rows it needs, and that figure is exact.
func TestTheDashboardClosesEveryBoxAtEveryRealisticSize(t *testing.T) {
	for _, width := range []int{80, 100, 120, 175, 200} {
		for height := 24; height <= 60; height++ {
			a := dashboardAt(t, width, height)
			screen := a.Render()
			if n := unclosedBoxes(screen); n != 0 {
				t.Errorf("%dx%d: %d box(es) lost their bottom border:\n%s", width, height, n, ansi.Strip(screen))
				continue
			}
			shown := strings.Count(ansi.Strip(screen), "┘")
			need := floorRows(a.renderViewFooter())
			switch {
			case shown < dashboardBoxes(a) && need == 0:
				t.Errorf("%dx%d: %d of %d boxes shown and no floor warning", width, height, shown, dashboardBoxes(a))
			case shown == dashboardBoxes(a) && need != 0:
				t.Errorf("%dx%d: every box shown, yet the footer warns (need %d)", width, height, need)
			case need != 0:
				checkFloorFigure(t, width, height, need)
			}
		}
	}
}

// checkFloorFigure: at the announced height nothing is hidden, one row less
// and the warning is still there.
func checkFloorFigure(t *testing.T, width, height, need int) {
	t.Helper()
	if need <= height {
		t.Errorf("%dx%d: the footer asks for %d rows, no more than it has", width, height, need)
		return
	}
	at := dashboardAt(t, width, need)
	if got := floorRows(at.renderViewFooter()); got != 0 {
		t.Errorf("%dx%d asks for %d rows, and %dx%d still warns (need %d)", width, height, need, width, need, got)
	}
	if shown := strings.Count(ansi.Strip(at.Render()), "┘"); shown != dashboardBoxes(at) {
		t.Errorf("%dx%d: %d of %d boxes at the announced height", width, need, shown, dashboardBoxes(at))
	}
	if before := dashboardAt(t, width, need-1); floorRows(before.renderViewFooter()) == 0 {
		t.Errorf("%dx%d asks for %d rows, but %d already fit", width, height, need, need-1)
	}
}

// TestTheResourcesTabStillFits: the second tab (16 lines) needs no tightening
// at the sizes of the report.
func TestTheResourcesTabStillFits(t *testing.T) {
	for _, sz := range [][2]int{{175, 38}, {120, 30}} {
		a := dashboardAt(t, sz[0], sz[1])
		view := a.views[a.currentView]
		next, _ := view.Update(uitestutil.Key("tab"))
		a.views[a.currentView] = next.(Screen)
		screen := ansi.Strip(a.Render())
		if !strings.Contains(screen, "Storage") || strings.Count(screen, "┘") != 2 {
			t.Errorf("%dx%d: the Resources tab does not show its two boxes closed:\n%s", sz[0], sz[1], screen)
		}
		if need := floorRows(a.renderViewFooter()); need != 0 {
			t.Errorf("%dx%d: the Resources tab warns (need %d)", sz[0], sz[1], need)
		}
	}
}

// TestATallDashboardIsUnchanged: on a tall terminal the grid keeps every blank
// line it had before DEVDESK-743. The golden files are main's rendering
// (6a9cc0a9) of the same data. Two differences are expected, and only those:
// the blank line main drew above the boxes is gone with the title rule, and at
// wide the two rows freed that way go to the curves, whose height fitCharts
// measures from the room left (each chart one line taller, its braille
// redrawn at that height). So at standard the rendering is main's, line for
// line; at wide every line of main's that is not a curve is still there, in
// order, and the only lines added are curve lines.
func TestATallDashboardIsUnchanged(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Host box names the OS; the golden files were rendered on Linux")
	}
	for _, tc := range []struct {
		width, height int
		wide          bool
	}{{175, 60, false}, {200, 60, true}} {
		golden, err := os.ReadFile("testdata/dashboard743/main-" +
			strconv.Itoa(tc.width) + "x" + strconv.Itoa(tc.height) + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		want := strings.SplitN(string(golden), "\n", 2)[1] // main's leading blank line
		a := dashboardAt(t, tc.width, tc.height)
		got := ansi.Strip(a.views[a.currentView].Render())
		if !tc.wide {
			if got != want {
				t.Errorf("%dx%d: the dashboard changed on a tall terminal:\n got:\n%s\nwant:\n%s",
					tc.width, tc.height, got, want)
			}
			continue
		}
		wantText, gotText := withoutCurves(want), withoutCurves(got)
		if strings.Join(wantText, "\n") != strings.Join(gotText, "\n") {
			t.Errorf("%dx%d: a line other than a curve changed:\n got:\n%s\nwant:\n%s",
				tc.width, tc.height, strings.Join(gotText, "\n"), strings.Join(wantText, "\n"))
		}
		if added := strings.Count(got, "\n") - strings.Count(want, "\n"); added != 2 {
			t.Errorf("%dx%d: %d lines added, want 2 (one per curve of a chart box)", tc.width, tc.height, added)
		}
	}
}

// withoutCurves drops the lines of a rendering that hold a curve: any line
// with a braille cell, and the blank box lines of a chart area, which differ
// from a box's own blank line only by where the curve happens to fall. What is
// left is every label, value, border and gap.
func withoutCurves(rendering string) []string {
	var kept []string
	for _, line := range strings.Split(rendering, "\n") {
		if strings.ContainsFunc(line, func(r rune) bool { return r >= 0x2800 && r <= 0x28FF }) ||
			isBlankBoxLine(line) {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// isBlankBoxLine reports whether a line holds only box sides and spaces.
func isBlankBoxLine(line string) bool {
	return strings.Trim(line, " │") == ""
}
