package ociresources

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// viewportOverhead is the number of lines consumed by the top padding and table borders in the viewport.
const viewportOverhead = 3 // EmptyLine + table header + table border

// NetworkInspectForm displays the containers connected to a Docker network in the viewport.
type NetworkInspectForm struct {
	networkID   string
	networkName string
	containers  []docker.NetworkContainer
	table       datatable.Model[docker.NetworkContainer]
	loading     bool
	width       int
	height      int
}

// containerColumns describes the network inspect table. Nothing sorts or
// searches: a network holds a handful of containers, and the list is the
// network's own order.
func containerColumns() []datatable.Column[docker.NetworkContainer] {
	return []datatable.Column[docker.NetworkContainer]{
		{
			Title: "Container", MinWidth: 20, Flex: 1,
			Cell: func(c docker.NetworkContainer) string { return c.Name },
		},
		{
			Title: "IPv4", MinWidth: 16,
			Cell: func(c docker.NetworkContainer) string { return c.IPv4 },
		},
		{
			Title: "MAC Address", MinWidth: 20,
			Cell: func(c docker.NetworkContainer) string { return c.MacAddress },
		},
	}
}

// newNetworkInspectForm creates a NetworkInspectForm for the given network.
func newNetworkInspectForm(networkID, networkName string, width, height int) *NetworkInspectForm {
	f := &NetworkInspectForm{
		networkID:   networkID,
		networkName: networkName,
		loading:     true,
		width:       width,
		height:      height,
		table: datatable.New(datatable.Config[docker.NetworkContainer]{
			Columns:    containerColumns(),
			SortColumn: -1,
		}),
	}
	f.resize(width, height)
	return f
}

// SetContainers updates the table with the fetched containers.
func (f *NetworkInspectForm) SetContainers(containers []docker.NetworkContainer) {
	f.loading = false
	f.containers = containers
	f.table.SetItems(containers)
}

// SelectedContainer returns the currently selected container or nil.
func (f *NetworkInspectForm) SelectedContainer() *docker.NetworkContainer {
	c, ok := f.table.Selected()
	if !ok {
		return nil
	}
	return &c
}

// Update handles input for the network inspect form.
func (f *NetworkInspectForm) Update(msg tea.Msg) (*NetworkInspectForm, tea.Cmd) {
	return f, f.table.Update(msg)
}

// resize adjusts the table columns and height to fit the viewport.
//
// f.width is already the viewport's content width — the caller subtracted the
// borders — and datatable.Resize subtracts them itself, so they are added back
// here. The arithmetic written out before subtracted them a second time, which
// left the selected row two cells short of the right border.
func (f *NetworkInspectForm) resize(width, height int) {
	f.width = width
	f.height = height
	f.table.Resize(width+2, max(height-viewportOverhead, 3))
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
