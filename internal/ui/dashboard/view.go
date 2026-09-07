package dashboard

import (
	"strings"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/version"
)

// View renders the dashboard as a grid of titled boxes. There is no outer
// frame: every other view frames one object (a table, a form), the
// dashboard is made of heterogeneous groups that a single frame wouldn't
// help read.
func (m Model) View() string {
	t := layoutTier(m.width, m.height)
	colWidth := t.columnWidth(m.width)
	columns := m.columnsFor(t)

	m.chartLines = m.fitCharts(columns, colWidth, t)
	inner := m.innerHeights(columns, colWidth, t)

	rendered := make([][]string, len(columns))
	height := 0
	for i, col := range columns {
		rendered[i] = m.renderColumn(col, colWidth, inner, t)
		if len(rendered[i]) > height {
			height = len(rendered[i])
		}
	}

	pad := theme.EmptyLineBg(sidePadding)
	gap := theme.EmptyLineBg(columnGap)
	blank := theme.EmptyLineBg(colWidth)

	// A blank line separates the title rule from the first row: without it,
	// the boxes' top border touches the title's and the two read as a single
	// frame.
	lines := make([]string, 0, height+1)
	lines = append(lines, theme.EmptyLineBg(m.width))
	for i := range height {
		var line strings.Builder
		line.WriteString(pad)
		for c, col := range rendered {
			if c > 0 {
				line.WriteString(gap)
			}
			if i < len(col) {
				line.WriteString(col[i])
			} else {
				line.WriteString(blank)
			}
		}
		lines = append(lines, theme.PadWithBg(line.String(), m.width))
	}
	return strings.Join(lines, "\n")
}

// columnsFor distributes the active tab's sections over the tier's columns.
// At `wide`, the third column carries the Resources tab's content: the tier
// decides where a fact is, never whether it exists.
func (m Model) columnsFor(t tier) [][]section {
	overview, resources := overviewSections(m.forgeType()), resourceSections()

	if m.activeTab == tabResources {
		return distribute(resources, t.columns())
	}
	if t.columns() == 3 {
		// At three columns, the layout is explicit rather than split into equal
		// shares: **the three chart boxes fill the first row**. They share the
		// same height — two curves each — and a row is framed on its tallest
		// box, so mixing them with the text boxes would leave gaps in both
		// rows.
		code, health, host, dockerBox := overview[0], overview[1], overview[2], overview[3]
		network, storage := resources[0], resources[1]
		return [][]section{
			{host, code},
			{network, health},
			{dockerBox, storage},
		}
	}
	return distribute(overview, t.columns())
}

// distribute lays sections into `cols` columns, filling each column top to
// bottom — reading order down a column, which is how a stack of boxes is read.
func distribute(sections []section, cols int) [][]section {
	if cols < 1 {
		cols = 1
	}
	perColumn := (len(sections) + cols - 1) / cols

	out := make([][]section, 0, cols)
	for start := 0; start < len(sections); start += perColumn {
		out = append(out, sections[start:min(start+perColumn, len(sections))])
	}
	return out
}

// fitCharts measures how many lines a chart can take.
//
// The grid is rendered once **with no curve at all**: what's left between
// this skeleton and the available height is exactly what the curves can
// occupy. They all live in the same row and there are two per box, hence
// the split in two.
//
// Measuring rather than deriving is what makes the calculation insensitive
// to the boxes: Health gained eight lines by becoming a tree, and no
// constant had to be updated.
func (m Model) fitCharts(columns [][]section, width int, t tier) int {
	if t != tierWide {
		return 1
	}

	bare := m
	bare.chartLines = 0
	used := leadingBlank
	for _, height := range bare.innerHeights(columns, width, t) {
		used += height + theme.BoxChrome
	}

	// The floor is **one** line, not three: a braille curve needs three lines
	// to be worth more than a sparkline, but forcing three lines when there
	// are only two available overflows the grid — and what overflows is
	// lost, not pushed down. A short curve beats a cut-off line.
	free := (m.height - used) / chartsPerBox
	return min(max(free, 1), maxChartHeight)
}

// chartHeightAt reports the chart height this model's size would produce, which
// is what the tests assert on: View() measures it on a copy, so it never
// survives on the model itself.
func (m Model) chartHeightAt(t tier) int {
	return m.fitCharts(m.columnsFor(t), t.columnWidth(m.width), t)
}

// innerHeight is the content height every box on screen fills, borders
// excluded: the tallest section of the frame.
//
// It is **derived from the sections**, not declared by the tier. A constant
// could truncate a section that grows — a chart added in phase 3, a line
// added to Health in phase 4 — and the truncation doesn't show: the box
// stays well-formed, it just loses its last line. What stays fixed, and
// that's phase 1's contract, is that a section renders the same number of
// lines whatever the state of its data.
// It is computed **per row**, not for the whole grid: text boxes fit in six
// lines and chart boxes in twenty, so a single height would leave thirteen
// blank lines in Health because Host, two columns over, carries two curves.
// The rows stay aligned with each other, which a per-box height would
// lose.
func (m Model) innerHeights(columns [][]section, width int, t tier) []int {
	rows := 0
	for _, col := range columns {
		rows = max(rows, len(col))
	}

	heights := make([]int, rows)
	for i := range heights {
		content := 0
		for _, col := range columns {
			if i >= len(col) {
				continue
			}
			content = max(content, len(col[i].render(m, width, t)))
		}
		heights[i] = max(nominalInnerHeight, content+trailingBlank)
	}
	return heights
}

// renderColumn stacks a column's boxes. No blank line between them: their
// borders already separate them, and at 30 lines the budget is exactly two
// boxes.
func (m Model) renderColumn(sections []section, width int, inner []int, t tier) []string {
	var lines []string
	for i, s := range sections {
		height := nominalInnerHeight
		if i < len(inner) {
			height = inner[i]
		}
		content := padTo(s.render(m, width, t), height, theme.BoxContentWidth(width))
		lines = append(lines, theme.RenderTitledBox(s.title, content, width)...)
	}
	return lines
}

// padTo fills a section's content out to the frame's box height. It does
// not truncate: innerHeight is computed on these same sections, so a line
// lost here would be a calculation bug, not an overflow to absorb.
func padTo(content []string, height, innerWidth int) []string {
	out := make([]string, 0, height)
	out = append(out, content...)
	for len(out) < height {
		out = append(out, theme.EmptyLineBg(innerWidth))
	}
	return out
}

// filterComponents returns service components filtered by SSL type
func (m Model) filterComponents(sslOnly bool) []status.ComponentStatus {
	var result []status.ComponentStatus
	for _, c := range m.serviceComponents {
		isSSL := c.Type == status.TypeSSL
		if sslOnly == isSSL {
			result = append(result, c)
		}
	}
	return result
}

// countStatuses returns OK, Down, Error counts
func countStatuses(components []status.ComponentStatus) (ok, down, errCount int) {
	for _, c := range components {
		switch c.Status {
		case status.StatusOK:
			ok++
		case status.StatusDown:
			down++
		default:
			errCount++
		}
	}
	return
}

// HeaderView interface

// Frameless tells the router to draw no viewport border for this view. The
// dashboard is the only view that doesn't frame a single object — see
// View().
func (m Model) Frameless() bool {
	return true
}

// GetFooterHeight returns the footer height for this view (Rule 124): an empty
// line, an info line, and the tab bar when there is more than one tab.
func (m Model) GetFooterHeight() int {
	if m.showsTabBar() {
		return 3
	}
	return 2
}

// showsTabBar reports whether a tab bar is worth a line. A single tab is
// not a choice: the bar would say "you are here", which the screen already
// says.
func (m Model) showsTabBar() bool {
	return tabCountFor(layoutTier(m.width, m.height)) > 1
}

// RenderFooter draws the tab bar (Rule 124).
//
// **There is no "Updated" line**, and its absence is a choice. It used to
// exist to date values stuck at `-`, but the dashboard's three clocks run
// at the second, the five-second, and the thirty-second mark: the oldest
// fact on screen is never a minute old, so TimeAgo permanently answered
// `now`. A line whose value never changes tells nothing, and it was costing
// the view's only info line.
//
// The info line is still rendered even empty: the router budgets on
// GetFooterHeight (Rule 124). It's also what carries the refusal of a
// greyed key (Rule 130) — the line already existed, it was just always
// empty.
func (m Model) RenderFooter(width int) string {
	info := m.footer.View(width, m.status())

	if !m.showsTabBar() {
		return theme.EmptyLineBg(width) + "\n" + info
	}

	tabs := []theme.TabItem{{Label: "Overview"}, {Label: "Resources"}}
	tabBar := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, int(m.activeTab)), width)
	return tabBar + "\n" + theme.EmptyLineBg(width) + "\n" + info
}

// status is the line the dashboard derives on every frame (Rule 128).
//
// The dashboard launches nothing, so it passes no origin and always gets D9's
// degraded form — "2 jobs running — :jobs for details". That is not a
// limitation here but the right sentence: it is the screen a user is most
// likely to be watching while a batch runs somewhere else, and what it can
// honestly say is how much is going and where to look.
func (m Model) status() sharedcomponents.Status {
	line := sharedcomponents.JobsStatusLine(m.jobs, "")
	if line == "" {
		return sharedcomponents.Status{}
	}
	return sharedcomponents.Status{Text: line, Spinner: true}
}

// GetShortcuts returns the keyboard shortcuts for the header.
//
// Rule 130: the two deep links need a session and `tab` needs a second tab.
// Neither is a different screen, so the entries stay where they are and are
// greyed — the column is the same six lines at every width and either side of
// a sign-in.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	links := m.forgeLinks()
	return []shortcut.Shortcut{
		// At `wide` the Resources boxes are already on screen, so there is no
		// second tab to switch to.
		{Key: "tab", Description: "Switch tab", Disabled: !m.showsTabBar()},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: keymap.Requests, Description: "Open MRs", Disabled: !links.Enabled()},
		{Key: keymap.Issues, Description: "Open issues", Disabled: !links.Enabled()},
		{Key: "ctrl+p", Description: "Command"},
		{Key: "?", Description: "Help"},
	}
}

// GetTitle returns the view title
func (m Model) GetTitle() string {
	return theme.IconDashboard + " Dashboard"
}

// GetIcon returns the view icon
func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header.
//
// The version is here and on no other screen: the dashboard is the landing
// view, so the only one where the information is read without having been
// sought. Repeating it everywhere would cost a header column on every view
// for a value that never changes during a session; `:about` breaks it
// down.
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
		{Key: "Version", Value: version.Get().Short(), Style: theme.HeaderValueStyle},
	}
}

// GetHelpContent returns help content for the dashboard (Rule 114)
func (m Model) GetHelpContent() help.Content {
	v := m.vocab()
	return help.Content{
		Title:       "Dashboard",
		Description: "The dashboard groups everything it knows into four titled boxes: Code, Health, Host and Docker. The layout follows the terminal's size — one column when it is narrow, a grid when it is not, and a third column on a large terminal, where it shows the Resources tab inline. A value that has not been measured yet reads '-', a source that is absent reads 'n/a', and a measured zero reads '0'.",
		KeyBindings: []help.KeyBinding{
			{Key: "tab", Description: "Switch between the Overview and Resources tabs"},
			{Key: "ctrl+r", Description: "Refresh all dashboard data"},
			{Key: keymap.Requests, Description: "Open assigned merge requests in browser (requires authentication)"},
			{Key: keymap.Issues, Description: "Open assigned issues in browser (requires authentication)"},
			{Key: "ctrl+p", Description: "Open command mode to navigate to other views"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Code",
				Body: "Your " + v.Name + " activity — assigned " + strings.ToLower(v.ChangeRequests) + ", " +
					strings.ToLower(v.ChangeRequests) + " awaiting your review, assigned issues —" +
					" and the local clones under the workspaces directory. The two belong together: the explorer " +
					"creates what does not exist, workspaces reconciles what does. Requires authentication via :" +
					string(command.ViewGitAuth) + ".",
			},
			{
				Title: "Health",
				Body:  "Monitored services (HTTP, ICMP, DNS) and SSL certificates, each with its up and down counts, plus the security posture read from the scan caches. Monitors are configured in :status, and the findings themselves live in :security — this box only counts them.",
			},
			{
				Title: "Host and Docker",
				Body:  "Two measurement points, and they are not on one axis: the Host box reads the machine dk runs on, while the Docker box reads inside the Docker Desktop VM, whose footprint is a subset of the host's. Both are true and they do not add up, which is why the box titles name where the number was measured.\n\nThe Host box ends on the tooling: one line when every tool is there, one node per missing tool otherwise — the name is what you need to install it. The Docker box counts what the daemon holds under a Resources root: images, volumes and networks. The sizes are not there, they are in Storage.",
			},
			{
				Title: "Resources",
				Body:  "The second tab carries the host and Docker series in detail — throughput, disk per volume, reclaimable space. It exists because nothing else shows them: there is no host view, and :containers shows one container rather than the machine. On a large terminal it is inline in a third column and the tab is a bigger version of it.",
			},
			{
				Title: "Navigation",
				Body: "Press ctrl+p to open command mode, then type a view name: " +
					string(command.ViewStatus) + " for monitors, " +
					string(command.ViewGitAuth) + " for authentication, " +
					string(command.ViewGitExplorer) + " for browsing " + strings.ToLower(v.Repositories) + ", " +
					string(command.ViewWorkspaces) + " for file management, " +
					string(command.ViewContainers) + " for Docker management, " +
					string(command.ViewOCIResources) + " for OCI resource management, " +
					string(command.ViewSecurity) + " for scanning.\n\n" +
					"A bare : opens command mode too, but only when no text field has focus — inside one it " +
					"types a colon, which values like https://trivy-server:4954 need. ctrl+p always works.",
			},
		},
	}
}
