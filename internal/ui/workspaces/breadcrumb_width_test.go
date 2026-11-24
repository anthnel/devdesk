package workspaces

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── DEVDESK-769: the breadcrumb fits the width ───────────────────────────────

// atDepth returns a model sized to width whose breadcrumb is `parents` folders
// deep, ending on a folder called current.
func atDepth(t *testing.T, width, parents int, current string) Model {
	t.Helper()
	m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: width, Height: 30})
	m.navigationStack = nil
	for i := 1; i <= parents; i++ {
		m.navigationStack = append(m.navigationStack, fmt.Sprintf("/tmp/workspaces/dir-%02d", i))
	}
	m.currentPath = "/tmp/workspaces/" + current
	m.activeTabIndex = m.tabCount() - 1
	return m
}

func TestBreadcrumbFitsTheWidth(t *testing.T) {
	const name = "current-"
	long := name + strings.Repeat("n", 200-len(name))

	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("deep trail, width %d", width), func(t *testing.T) {
			footer := atDepth(t, width, 40, long).RenderFooter(width)

			lines := strings.Split(footer, "\n")
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("footer line %d is %d cells wide in %d: %q", i, w, width, ansi.Strip(line))
				}
			}
			// The breadcrumb is the first line of the footer (Rule 124).
			plain := ansi.Strip(lines[0])
			if !strings.HasPrefix(strings.TrimLeft(plain, " "), theme.TrailEllipsis) {
				t.Errorf("breadcrumb = %q, want it to start with %q (the head was cut)", plain, theme.TrailEllipsis)
			}
			if strings.Contains(plain, "home") {
				t.Errorf("breadcrumb = %q, want the home pill dropped on a trail this long", plain)
			}
			trimmed := strings.TrimRight(plain, " ")
			if !strings.HasSuffix(trimmed, theme.TrailEllipsis) {
				t.Errorf("breadcrumb = %q, want the overlong current folder cut with %q", plain, theme.TrailEllipsis)
			}
			cur, dir := strings.LastIndex(trimmed, name), strings.LastIndex(trimmed, "dir-")
			if cur < 0 || cur < dir {
				t.Errorf("breadcrumb = %q, want the current folder last", plain)
			}
		})

		t.Run(fmt.Sprintf("trail that fits, width %d", width), func(t *testing.T) {
			footer := atDepth(t, width, 2, "devdesk").RenderFooter(width)

			line := strings.Split(footer, "\n")[0]
			want := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs([]theme.TabItem{
				{Label: theme.IconHome + " home"},
				{Label: theme.IconDirectory + " dir-01"},
				{Label: theme.IconDirectory + " dir-02"},
				{Label: theme.IconDirectory + " devdesk"},
			}, 3), width)
			if line != want {
				t.Errorf("a trail that fits changed\n got: %q\nwant: %q", ansi.Strip(line), ansi.Strip(want))
			}
			if strings.Contains(ansi.Strip(line), theme.TrailEllipsis) {
				t.Errorf("breadcrumb = %q, want nothing cut", ansi.Strip(line))
			}
		})
	}
}
