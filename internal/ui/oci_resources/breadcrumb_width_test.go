package ociresources

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── DEVDESK-769: the breadcrumb fits the width ───────────────────────────────

// drilledAt returns a model sized to width, on the Registries tab, inside the
// group "prod" whose alias is alias. The trail below the table names it.
func drilledAt(t *testing.T, width int, alias string) Model {
	t.Helper()
	m := feed(t, registriesTab(t), tea.WindowSizeMsg{Width: width, Height: 30})
	m.registries[0].Alias = alias
	m.registryGroupSlug = m.registries[0].Slug
	if m.drilledGroup() == nil {
		t.Fatal("the fixture is not inside a group, so no breadcrumb is rendered")
	}
	return m
}

func TestBreadcrumbFitsTheWidth(t *testing.T) {
	const name = "alias-"
	long := name + strings.Repeat("a", 200-len(name))

	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("long alias, width %d", width), func(t *testing.T) {
			footer := drilledAt(t, width, long).RenderFooter(width)

			lines := strings.Split(footer, "\n")
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("footer line %d is %d cells wide in %d: %q", i, w, width, ansi.Strip(line))
				}
			}
			// The breadcrumb is the first line of the footer, above the tab bar.
			plain := ansi.Strip(lines[0])
			if !strings.HasPrefix(strings.TrimLeft(plain, " "), theme.TrailEllipsis) {
				t.Errorf("breadcrumb = %q, want it to start with %q (the head was cut)", plain, theme.TrailEllipsis)
			}
			if strings.Contains(plain, "Registries") {
				t.Errorf("breadcrumb = %q, want the Registries pill dropped", plain)
			}
			trimmed := strings.TrimRight(plain, " ")
			if !strings.HasSuffix(trimmed, theme.TrailEllipsis) || !strings.Contains(trimmed, name) {
				t.Errorf("breadcrumb = %q, want the group alias last, cut with %q", plain, theme.TrailEllipsis)
			}
		})

		t.Run(fmt.Sprintf("alias that fits, width %d", width), func(t *testing.T) {
			footer := drilledAt(t, width, "prod").RenderFooter(width)

			line := strings.Split(footer, "\n")[0]
			want := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs([]theme.TabItem{
				{Label: "Registries"},
				{Label: "prod"},
			}, 1), width)
			if line != want {
				t.Errorf("a trail that fits changed\n got: %q\nwant: %q", ansi.Strip(line), ansi.Strip(want))
			}
		})
	}
}
