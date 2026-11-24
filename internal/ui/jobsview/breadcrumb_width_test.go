package jobsview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── DEVDESK-769: the breadcrumb fits the width ───────────────────────────────

// openedAt returns a model sized to width with a run labelled label open at
// the item level, where the breadcrumb is drawn.
func openedAt(t *testing.T, width int, label string) Model {
	t.Helper()
	m := withRuns(t, run(1, jobs.KindScan, label, jobs.ItemDone, jobs.ItemRunning))
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: 20})
	m, _ = step(t, m, testutil.Key("right"))
	if m.level != levelItems {
		t.Fatal("→ did not open the run, so no breadcrumb is rendered")
	}
	return m
}

func TestBreadcrumbFitsTheWidth(t *testing.T) {
	const name = "label-"
	long := name + strings.Repeat("l", 200-len(name))

	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("long label, width %d", width), func(t *testing.T) {
			footer := openedAt(t, width, long).RenderFooter(width)

			lines := strings.Split(footer, "\n")
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("footer line %d is %d cells wide in %d: %q", i, w, width, ansi.Strip(line))
				}
			}
			// The breadcrumb is the first line of the footer at the item level.
			plain := ansi.Strip(lines[0])
			if !strings.HasPrefix(strings.TrimLeft(plain, " "), theme.TrailEllipsis) {
				t.Errorf("breadcrumb = %q, want it to start with %q (the head was cut)", plain, theme.TrailEllipsis)
			}
			// "Runs" is what falls; "scan > label…" is the level on screen and stays.
			if strings.Contains(plain, "Runs") {
				t.Errorf("breadcrumb = %q, want the Runs pill dropped", plain)
			}
			if !strings.Contains(plain, "scan "+theme.IconChevronRight+" "+name) {
				t.Errorf("breadcrumb = %q, want %q kept", plain, "scan "+theme.IconChevronRight+" "+name+"…")
			}
			if !strings.HasSuffix(strings.TrimRight(plain, " "), theme.TrailEllipsis) {
				t.Errorf("breadcrumb = %q, want the overlong label cut with %q", plain, theme.TrailEllipsis)
			}
		})

		t.Run(fmt.Sprintf("label that fits, width %d", width), func(t *testing.T) {
			footer := openedAt(t, width, "~/work").RenderFooter(width)

			line := strings.Split(footer, "\n")[0]
			want := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs([]theme.TabItem{
				{Label: "Runs"},
				{Label: "scan " + theme.IconChevronRight + " ~/work"},
			}, 1), width)
			if line != want {
				t.Errorf("a trail that fits changed\n got: %q\nwant: %q", ansi.Strip(line), ansi.Strip(want))
			}
		})
	}
}
