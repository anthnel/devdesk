package explorer

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// visibleItems returns the nodes the table is showing: the current level, sorted
// and filtered. Every action resolves the cursor through this, so the row the
// user is looking at is the row that gets acted on.
func (m *Model) visibleItems() []*TreeNode {
	items := m.sortedItems(m.currentItems())
	query := strings.ToLower(m.filterBar.SearchQuery())
	if query == "" {
		return items
	}

	visible := make([]*TreeNode, 0, len(items))
	for _, node := range items {
		nameMatch := strings.Contains(strings.ToLower(node.Name), query)
		pathMatch := strings.Contains(strings.ToLower(node.FullPath), query)
		if nameMatch || pathMatch {
			visible = append(visible, node)
		}
	}
	return visible
}

// updateTableRows rebuilds table rows from the visible items
func (m *Model) updateTableRows() {
	items := m.visibleItems()
	rows := make([]table.Row, 0, len(items))
	for _, node := range items {
		rows = append(rows, table.Row{
			nodeTypeLabel(node),
			node.Name,
			nodeSlug(node.FullPath),
			visibilityLabel(node),
			node.AccessLevelName(),
			timeAgo(node.CreatedAt),
			timeAgo(node.LastActivityAt),
			pipelineStatusLabel(node),
		})
	}
	m.table.SetRows(rows)

	// bubbles does not clamp the cursor when the row count shrinks, so narrowing
	// the filter would leave it past the end. Nothing is highlighted then, and
	// every action that resolves the selection silently does nothing.
	if m.table.Cursor() >= len(rows) {
		m.table.SetCursor(max(len(rows)-1, 0))
	}

	m.updateSortIndicators()
}

// cycleSort cycles through sort options: each column asc then desc, then next column
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	if m.sortAsc {
		m.sortAsc = false
	} else {
		m.sortAsc = true
		nextIdx := 0
		for i, col := range sortableColumns {
			if col == m.sortColumn {
				nextIdx = (i + 1) % len(sortableColumns)
				break
			}
		}
		m.sortColumn = sortableColumns[nextIdx]
	}
	m.updateTableRows()
	return m, nil
}

// sortedItems returns nodes sorted by the current sort column and direction
func (m *Model) sortedItems(items []*TreeNode) []*TreeNode {
	if len(items) == 0 {
		return items
	}

	sorted := make([]*TreeNode, len(items))
	copy(sorted, items)

	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		var less bool
		switch m.sortColumn {
		case sortByName:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case sortByVisibility:
			less = strings.ToLower(a.Visibility) < strings.ToLower(b.Visibility)
		case sortByCreated:
			less = timeBefore(a.CreatedAt, b.CreatedAt)
		case sortByActivity:
			less = timeBefore(a.LastActivityAt, b.LastActivityAt)
		default: // sortByType
			less = string(a.Type) < string(b.Type)
		}

		if m.sortAsc {
			return less
		}
		return !less
	})

	return sorted
}

// timeBefore compares two nullable times (nil is considered "oldest")
func timeBefore(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return true
	}
	if b == nil {
		return false
	}
	return a.Before(*b)
}

// updateSortIndicators updates column headers with ▲/▼ sort arrows
func (m *Model) updateSortIndicators() {
	sortColIndex := map[sortField]int{
		sortByType:       0,
		sortByName:       1,
		sortByVisibility: 3,
		sortByCreated:    5,
		sortByActivity:   6,
	}
	baseTitles := map[int]string{
		0: "Type",
		1: "Name",
		3: "Visibility",
		5: "Created",
		6: "Activity",
	}

	columns := m.table.Columns()
	if len(columns) < numColumns {
		return
	}

	// Reset sortable column titles
	for idx, title := range baseTitles {
		columns[idx].Title = title
	}

	// Add arrow to active sort column
	if idx, ok := sortColIndex[m.sortColumn]; ok {
		arrow := " ▲"
		if !m.sortAsc {
			arrow = " ▼"
		}
		columns[idx].Title = baseTitles[idx] + arrow
	}

	m.table.SetColumns(columns)
}
