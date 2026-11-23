package security

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-754 (SC1): "Group by fix" on the CVE tab. `g` swaps the body of the
// results between the findings table and one row per cause (the grouped list);
// enter on a group opens StateGroup, the findings of that group, and enter there
// opens the same detail as from the flat table. The findings table is never
// rebuilt by any of it, so its filter, sort, query and cursor are what the user
// left. Groups are derived from the result on screen and never stored as done.

// groupToggleKey switches the results between the findings table and the grouped
// list. A lowercase letter is a display toggle, declared in keymap.localToggles.
const groupToggleKey = "g"

// Why `g`, `Y` and the keys that act on one finding are greyed in the grouped
// list (Rule 130).
const (
	reasonGroupNotHere    = "Group by fix covers the CVE tab"
	reasonNothingToGroup  = "No vulnerability to group"
	reasonOpenTheGroup    = "Open the group to act on one finding"
	reasonNoGroupSelected = "No group selected"
	reasonNoGroupCommand  = "This group has no command to run"
	reasonGroupHasNoFix   = "No fix is published for this group"
	reasonCopyNeedsGroup  = "Copy the command from a group (g), or from a finding's detail"
)

// The two words of the toggle, named for the display it switches to.
const (
	labelGroupByFix    = "Group by fix"
	labelListByFinding = "List by finding"
)

// isGrouped reports whether the active tab shows the grouped list.
func (m Model) isGrouped() bool { return m.groupedTabs[m.activeTab] }

// canGroup says whether `g` applies here. In the grouped list it always does,
// even with no group left: it is the way back.
func (m Model) canGroup() shortcut.Availability {
	switch {
	case m.activeTab != TabCVE:
		return shortcut.Unavailable(reasonGroupNotHere)
	case m.isGrouped():
		return shortcut.Availability{}
	case !m.hasVulnerability():
		return shortcut.Unavailable(reasonNothingToGroup)
	}
	return shortcut.Availability{}
}

// hasVulnerability says whether the result holds a CVE at all, whatever the
// filters hide: a search that matches none does not make them vanish.
func (m Model) hasVulnerability() bool {
	if m.result == nil {
		return false
	}
	for _, f := range m.result.Findings {
		if scan.Categorize(f) == scan.CategoryVulnerability {
			return true
		}
	}
	return false
}

// toggleGroup switches the CVE tab between the findings table and the grouped
// list, or says why it cannot (Rule 130). Neither way rebuilds the findings
// table: going in selects the group of the selected finding, coming out puts the
// findings cursor on the first visible member of the group under the cursor.
func (m Model) toggleGroup() (tea.Model, tea.Cmd) {
	if a := m.canGroup(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	if m.isGrouped() {
		m.leaveGroupedList()
		return m, nil
	}
	m.enterGroupedList()
	return m, nil
}

// enterGroupedList builds the groups from the result and puts the cursor on the
// group of the selected finding.
func (m *Model) enterGroupedList() {
	m.groupedTabs[TabCVE] = true
	m.groupsAll = cveGroups(m.result.Findings)
	m.groups.SetItems(visibleGroups(m.groupsAll, m.findingsTable.Visible()))
	m.groups.GotoTop()
	m.groups.Remeasure()
	if f, ok := m.findingsTable.Selected(); ok {
		if i := groupIndexOf(m.groups.Visible(), f); i >= 0 {
			m.groups.SetCursor(i)
		}
	}
}

// leaveGroupedList shows the findings table again, its cursor on the first
// visible member of the group that was selected.
func (m *Model) leaveGroupedList() {
	var group *fixGroup
	if g, ok := m.groups.Selected(); ok {
		group = &g
	}
	var prev *scan.Finding
	if f, ok := m.findingsTable.Selected(); ok {
		prev = &f
	}
	m.groupedTabs[TabCVE] = false
	if i := nearestFinding(m.findingsTable.Visible(), group, prev, m.findingsTable.Cursor()); i >= 0 {
		m.findingsTable.SetCursor(i)
	}
}

// groupsFollowResult rebuilds the groups after the result changed. Nothing is
// built while the CVE tab is flat: the next `g` builds them from the result then
// on screen.
func (m *Model) groupsFollowResult() {
	m.groupsAll = nil
	if m.groupedTabs[TabCVE] && m.result != nil {
		m.groupsAll = cveGroups(m.result.Findings)
	}
	m.refreshGroups()
}

// refreshGroups lists the groups that hold a finding the findings table shows,
// keeping the cursor on its group by key, else at the nearest position. It runs
// after anything that changes what the findings table shows.
func (m *Model) refreshGroups() {
	if m.activeTab != TabCVE || !m.isGrouped() {
		return
	}
	prev, hadPrev := m.groups.Selected()
	prevCursor := m.groups.Cursor()
	m.groups.SetItems(visibleGroups(m.groupsAll, m.findingsTable.Visible()))
	m.groups.Remeasure()
	if !hadPrev {
		return
	}
	if i := groupIndexByKey(m.groups.Visible(), prev.Key, prevCursor); i >= 0 {
		m.groups.SetCursor(i)
	}
}

// currentGroup is the group an action on a group acts on: the one under the
// cursor of the grouped list, or the one the group level shows.
func (m Model) currentGroup() (fixGroup, bool) {
	switch {
	case m.state == StateGroup && m.openGroup != nil:
		return groupByKey(m.groupsAll, *m.openGroup)
	case m.state == StateResults && m.isGrouped():
		return m.groups.Selected()
	}
	return fixGroup{}, false
}

// groupByKey is the group with key in groups.
func groupByKey(groups []fixGroup, key groupKey) (fixGroup, bool) {
	for _, g := range groups {
		if g.Key == key {
			return g, true
		}
	}
	return fixGroup{}, false
}

// canOpenGroup says whether enter has a group to open.
func (m Model) canOpenGroup() shortcut.Availability {
	if _, ok := m.groups.Selected(); !ok {
		return shortcut.Unavailable(reasonNoGroupSelected)
	}
	return shortcut.Availability{}
}

// openSelectedGroup opens the group under the cursor: the list of all its
// findings, the hidden ones included, since one move closes them all.
func (m Model) openSelectedGroup() (tea.Model, tea.Cmd) {
	if a := m.canOpenGroup(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	g, _ := m.groups.Selected()
	key := g.Key
	m.openGroup = &key
	m.state = StateGroup
	m.groupTable.SetItems(g.Members)
	m.groupTable.GotoTop()
	m.groupTable.Remeasure()
	return m, nil
}

// refreshGroupLevel follows a new result in the group level, and in a detail
// opened from it: the members are read again by key. A group that is gone takes
// the level and the detail with it, back to the grouped list.
func (m *Model) refreshGroupLevel() {
	if m.openGroup == nil {
		return
	}
	g, ok := groupByKey(m.groupsAll, *m.openGroup)
	if !ok {
		m.openGroup = nil
		if m.state == StateGroup || m.state == StateDetails && m.detailsParent == StateGroup {
			m.state = StateResults
			m.selectedFinding = nil
		}
		return
	}
	prev, hadPrev := m.groupTable.Selected()
	m.groupTable.SetItems(g.Members)
	m.groupTable.Remeasure()
	if !hadPrev {
		return
	}
	for i, f := range m.groupTable.Visible() {
		if sameFinding(f, prev) {
			m.groupTable.SetCursor(i)
			return
		}
	}
}

// handleGroupedKey takes the keys the grouped list answers itself. The others
// fall through to the results' own handling: the severity tokens, the search and
// the tabs drive the findings table, which the list is a view of.
func (m Model) handleGroupedKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		next, cmd := m.openSelectedGroup()
		return next, cmd, true
	case keymap.Space, writeRemediationKey, keymap.Scan:
		// They act on one finding, and a group is not one.
		return m, m.footer.Warn(reasonOpenTheGroup), true
	case ".", "up", "down", "pgup", "pgdown", "home", "end":
		return m, m.groups.Update(msg), true
	}
	return m, nil, false
}

// handleGroupState processes input in the group level. Only the keys of its four
// shortcuts act: the tabs, the filters and the sort belong to the list behind it.
func (m Model) handleGroupState(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeGroup()
	case "enter":
		f, ok := m.groupTable.Selected()
		if !ok {
			return m, m.footer.Warn(reasonNothingToSee)
		}
		return m.openDetail(f, StateGroup)
	case keymap.Copy:
		return m.copyGroupCommand()
	case "up", "down", "pgup", "pgdown", "home", "end":
		return m, m.groupTable.Update(msg)
	}
	return m, nil
}

// closeGroup goes back to the grouped list, on the group that was open.
func (m Model) closeGroup() (tea.Model, tea.Cmd) {
	if m.openGroup != nil {
		if i := groupIndexByKey(m.groups.Visible(), *m.openGroup, m.groups.Cursor()); i >= 0 {
			m.groups.SetCursor(i)
		}
	}
	m.state = StateResults
	m.openGroup = nil
	return m, nil
}

// canCopyGroupCommand says whether Y has a group command to copy. In the flat
// list there is no group, and the reason says where Y works.
func (m Model) canCopyGroupCommand() shortcut.Availability {
	if m.state == StateResults && !m.isGrouped() {
		return shortcut.Unavailable(reasonCopyNeedsGroup)
	}
	g, ok := m.currentGroup()
	switch {
	case !ok:
		return shortcut.Unavailable(reasonNoGroupSelected)
	case g.Key.Unfixed:
		return shortcut.Unavailable(reasonGroupHasNoFix)
	case g.Command == "":
		return shortcut.Unavailable(reasonNoGroupCommand)
	}
	return shortcut.Availability{}
}

// copyGroupCommand puts the group's command on the clipboard, whole. It never
// runs it, and the write is in the Cmd (Rule 110). The command is plain words by
// construction (cveGroups).
func (m Model) copyGroupCommand() (tea.Model, tea.Cmd) {
	if a := m.canCopyGroupCommand(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	g, _ := m.currentGroup()
	command := g.Command
	return m, func() tea.Msg {
		return FixCommandCopiedMsg{Command: command, Err: writeClipboard(command)}
	}
}

// groupShortcuts is the group level's column: four keys.
func (m Model) groupShortcuts() shortcut.Shortcuts {
	_, hasMember := m.groupTable.Selected()
	return []shortcut.Shortcut{
		{Key: "esc", Description: "Back"},
		{Key: "enter", Description: "Details", Disabled: !hasMember},
		{Key: keymap.Copy, Description: "Copy cmd", Disabled: !m.canCopyGroupCommand().Enabled()},
		{Key: "?", Description: "Help"},
	}
}

// groupToggleLabel names the display `g` switches to.
func (m Model) groupToggleLabel() string {
	if m.isGrouped() {
		return labelListByFinding
	}
	return labelGroupByFix
}

// groupTitle is the group level's title suffix: the cause it lists.
func groupTitle(g fixGroup) string {
	return g.Subject + " " + g.Change
}

// groupStatus is the footer's sentence about the selected group, a state of the
// screen derived on every frame (Rule 128).
func (m Model) groupStatus() sharedcomponents.Status {
	if g, ok := m.currentGroup(); ok {
		return sharedcomponents.Status{Text: groupSentence(g)}
	}
	return sharedcomponents.Status{}
}

// groupSentence is what the footer says about the selected group.
func groupSentence(g fixGroup) string {
	count := cveCount(len(g.Members))
	switch {
	case g.Key.Unfixed:
		return "No fix published for " + count
	case g.Target != "":
		return count + " closed by one move to " + g.Target
	}
	// Several candidate versions, none preferred: no single move is promised.
	_, options, _ := strings.Cut(g.Change, " -> ")
	if options == "" || options == "none" {
		return count + ", no single version to move to"
	}
	return count + ", fixed in " + options
}

// cveCount is "1 CVE" or "N CVEs".
func cveCount(n int) string {
	if n == 1 {
		return "1 CVE"
	}
	return strconv.Itoa(n) + " CVEs"
}

// newGroupsTable is the grouped list's table.
func newGroupsTable() datatable.Model[fixGroup] {
	return datatable.New(datatable.Config[fixGroup]{
		Columns:    cveGroupColumns(),
		SortColumn: -1,
		SelectedStyles: func(g fixGroup) table.Styles {
			return theme.TableStylesForSeverity(string(g.Severity))
		},
	})
}

// newGroupFindingsTable is the table of the findings of one group: the findings
// table's columns, without its tokens and without the File column, which only the
// Misconfigurations tab shows.
func newGroupFindingsTable(_ *config.Config) datatable.Model[scan.Finding] {
	t := datatable.New(datatable.Config[scan.Finding]{
		Columns:        findingColumns(),
		SortColumn:     -1,
		SelectedStyles: findingSelectedStyles,
	})
	t.SetColumnHidden(fileColumn, true)
	return t
}
