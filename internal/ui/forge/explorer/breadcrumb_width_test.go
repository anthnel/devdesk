package explorer

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── DEVDESK-769: the breadcrumb fits the width ───────────────────────────────

// atDepth returns a model sized to width, showing its tree, whose breadcrumb is
// `parents` groups deep and ends on a group called current.
func atDepth(t *testing.T, width, parents int, current string) Model {
	t.Helper()
	m := feed(t, drilledModel(t), tea.WindowSizeMsg{Width: width, Height: 30})
	if !m.showsTree() {
		t.Fatal("the fixture does not show the tree, so the breadcrumb is not rendered")
	}
	m.navigationStack = nil
	for i := 1; i <= parents; i++ {
		m.navigationStack = append(m.navigationStack, &TreeNode{
			ID: fmt.Sprint(i), Name: fmt.Sprintf("group-%02d", i), Type: NodeTypeGroup,
		})
	}
	m.currentGroupNode = &TreeNode{ID: "999", Name: current, Type: NodeTypeGroup}
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
				t.Errorf("breadcrumb = %q, want the overlong current group cut with %q", plain, theme.TrailEllipsis)
			}
			cur, grp := strings.LastIndex(trimmed, name), strings.LastIndex(trimmed, "group-")
			if cur < 0 || cur < grp {
				t.Errorf("breadcrumb = %q, want the current group last", plain)
			}
		})

		t.Run(fmt.Sprintf("trail that fits, width %d", width), func(t *testing.T) {
			footer := atDepth(t, width, 2, "devdesk").RenderFooter(width)

			line := strings.Split(footer, "\n")[0]
			want := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs([]theme.TabItem{
				{Label: theme.IconHome + " home"},
				{Label: "group-01"},
				{Label: "group-02"},
				{Label: "devdesk"},
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
