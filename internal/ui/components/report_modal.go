package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// PullReport contains the results of a recursive pull operation
type PullReport struct {
	Cloned  []string
	Skipped []string
	Errors  []string
}

// ReportModal displays the results of an operation
type ReportModal struct {
	title        string
	report       PullReport
	scrollOffset int
	maxVisible   int
	activeTab    int // 0 = Cloned, 1 = Skipped, 2 = Errors
	width        int
	height       int
}

// NewReportModal creates a new report modal
func NewReportModal(title string, report PullReport) *ReportModal {
	return &ReportModal{
		title:        title,
		report:       report,
		scrollOffset: 0,
		maxVisible:   10,
		activeTab:    0,
	}
}

// ReportModalCloseMsg is sent when the modal is closed
type ReportModalCloseMsg struct{}

// Update handles messages
func (m *ReportModal) Update(msg tea.Msg) (*ReportModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "right", "l":
			// Switch tab
			m.activeTab = (m.activeTab + 1) % 3
			m.scrollOffset = 0

		case "shift+tab", "left", "h":
			// Switch tab backwards
			m.activeTab = (m.activeTab + 2) % 3
			m.scrollOffset = 0

		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}

		case "down", "j":
			currentList := m.getCurrentList()
			if m.scrollOffset < len(currentList)-m.maxVisible {
				m.scrollOffset++
			}

		case "enter", "esc", "q":
			return m, func() tea.Msg {
				return ReportModalCloseMsg{}
			}
		}
	}

	return m, nil
}

// getCurrentList returns the currently active list
func (m *ReportModal) getCurrentList() []string {
	switch m.activeTab {
	case 0:
		return m.report.Cloned
	case 1:
		return m.report.Skipped
	case 2:
		return m.report.Errors
	default:
		return []string{}
	}
}

// View renders the modal
func (m *ReportModal) View() string {
	var b strings.Builder

	// Title
	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Summary
	summary := fmt.Sprintf("✓ %d cloned  •  ⊘ %d skipped  •  ✗ %d errors",
		len(m.report.Cloned),
		len(m.report.Skipped),
		len(m.report.Errors),
	)
	summaryStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Bold(true)
	b.WriteString(summaryStyle.Render(summary))
	b.WriteString("\n\n")

	// Tabs
	tabs := m.renderTabs()
	b.WriteString(tabs)
	b.WriteString("\n\n")

	// Current list content
	currentList := m.getCurrentList()
	if len(currentList) == 0 {
		emptyMsg := theme.DimStyle.Render("(empty)")
		b.WriteString(emptyMsg)
	} else {
		// Render visible items
		endIdx := m.scrollOffset + m.maxVisible
		if endIdx > len(currentList) {
			endIdx = len(currentList)
		}

		for i := m.scrollOffset; i < endIdx; i++ {
			item := currentList[i]
			// Truncate long paths
			if len(item) > 55 {
				item = "..." + item[len(item)-52:]
			}
			b.WriteString("  • " + item + "\n")
		}

		// Scroll indicator
		if len(currentList) > m.maxVisible {
			scrollInfo := theme.DimStyle.Render(
				fmt.Sprintf("  [%d-%d of %d] ↑↓ to scroll",
					m.scrollOffset+1,
					endIdx,
					len(currentList)),
			)
			b.WriteString("\n" + scrollInfo)
		}
	}

	b.WriteString("\n\n")

	// Help
	// help := theme.HelpStyle.Render("[tab/←→] switch tab  [↑↓] scroll  [enter/esc] close")
	// b.WriteString(help)

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorPrimary).
		BorderBackground(theme.ColorBackground).
		Padding(1, 2).
		Width(65).
		Render(b.String())
}

// renderTabs renders the tab bar
func (m *ReportModal) renderTabs() string {
	return theme.RenderTabs([]theme.TabItem{
		{Label: fmt.Sprintf("Cloned (%d)", len(m.report.Cloned))},
		{Label: fmt.Sprintf("Skipped (%d)", len(m.report.Skipped))},
		{Label: fmt.Sprintf("Errors (%d)", len(m.report.Errors))},
	}, m.activeTab)
}
