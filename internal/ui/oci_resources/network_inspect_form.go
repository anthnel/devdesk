package ociresources

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// viewportOverhead is the number of lines consumed by the top padding and table borders in the viewport.
const viewportOverhead = 3 // EmptyLine + table header + table border

// NetworkInspectForm displays the containers connected to a Docker network in the viewport.
type NetworkInspectForm struct {
	networkID   string
	networkName string
	containers  []docker.NetworkContainer
	table       table.Model
	loading     bool
	width       int
	height      int
}

// newNetworkInspectForm creates a NetworkInspectForm for the given network.
func newNetworkInspectForm(networkID, networkName string, width, height int) *NetworkInspectForm {
	columns := []table.Column{
		{Title: "Container", Width: 30},
		{Title: "IPv4", Width: 18},
		{Title: "MAC Address", Width: 18},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(theme.DefaultTableStyles())

	f := &NetworkInspectForm{
		networkID:   networkID,
		networkName: networkName,
		loading:     true,
		width:       width,
		height:      height,
		table:       t,
	}
	f.resize(width, height)
	return f
}

// SetContainers updates the table with the fetched containers.
func (f *NetworkInspectForm) SetContainers(containers []docker.NetworkContainer) {
	f.loading = false
	f.containers = containers
	rows := make([]table.Row, len(containers))
	for i, c := range containers {
		rows[i] = table.Row{c.Name, c.IPv4, c.MacAddress}
	}
	f.table.SetRows(rows)
}

// SelectedContainer returns the currently selected container or nil.
func (f *NetworkInspectForm) SelectedContainer() *docker.NetworkContainer {
	cursor := f.table.Cursor()
	if cursor < 0 || cursor >= len(f.containers) {
		return nil
	}
	c := f.containers[cursor]
	return &c
}

// Update handles input for the network inspect form.
func (f *NetworkInspectForm) Update(msg tea.Msg) (*NetworkInspectForm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			f.table.MoveUp(1)
		case "down", "j":
			f.table.MoveDown(1)
		case "g", "home":
			f.table.GotoTop()
		case "G", "end":
			f.table.GotoBottom()
		}
	}
	return f, nil
}

// resize adjusts the table columns and height to fit the viewport.
func (f *NetworkInspectForm) resize(width, height int) {
	f.width = width
	f.height = height

	// Rule 116: available = width - 2 (viewport borders) - numCols × 2 (cell padding)
	numCols := 3
	available := width - 2 - numCols*2
	fixedIPv4 := 16
	fixedMAC := 20
	flexName := max(available-fixedIPv4-fixedMAC, 10)
	columns := f.table.Columns()
	if len(columns) >= 3 {
		columns[0].Width = flexName
		columns[1].Width = fixedIPv4
		columns[2].Width = available - flexName - fixedIPv4
		f.table.SetColumns(columns)
	}

	tableH := max(height-viewportOverhead, 3)
	f.table.SetHeight(tableH)
}

// View renders the network inspect form in the viewport (Rule 112).
func (f *NetworkInspectForm) View() string {
	w := f.width
	lines := []string{theme.EmptyLineBg(w)} // Rule 131: top padding

	if f.loading {
		lines = append(lines,
			theme.PadWithBg(theme.DimStyle.Render("  Loading containers..."), w),
		)
	} else if len(f.containers) == 0 {
		lines = append(lines,
			theme.PadWithBg(theme.DimStyle.Render("  No containers connected to this network."), w),
		)
	} else {
		lines = append(lines, f.table.View())
	}

	return strings.Join(lines, "\n")
}
