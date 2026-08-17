package security

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// handleDetailsState processes input in details state
func (m Model) handleDetailsState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// backspace was an alias of esc, and the only one in the application.
	case "esc":
		m.state = StateResults
		return m, nil
	case keymap.Web:
		return m.handleDetailsOpenReference()
	case "up":
		m.detailsViewport.ScrollUp(1)
	case "down":
		m.detailsViewport.ScrollDown(1)
	case "pgup":
		m.detailsViewport.HalfPageUp()
	case "pgdown":
		m.detailsViewport.HalfPageDown()
	case "home":
		m.detailsViewport.GotoTop()
	case "end":
		m.detailsViewport.GotoBottom()
	}
	return m, nil
}

// handleDetailsOpenReference opens the first reference URL in the default browser.
func (m Model) handleDetailsOpenReference() (tea.Model, tea.Cmd) {
	if m.selectedFinding == nil {
		return m, nil
	}
	f := *m.selectedFinding
	if len(f.References) == 0 {
		m.statusMessage = "No references available"
		return m, clearStatusCmd()
	}
	url := f.References[0]
	return m, func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			log.Printf("ERROR [security] open URL %s: %v", url, err)
		}
		return nil
	}
}

// buildDetailsContent builds the full scrollable content for the details view.
func (m Model) buildDetailsContent() string {
	if m.selectedFinding == nil {
		return theme.Bg("No finding selected")
	}

	f := *m.selectedFinding
	var b strings.Builder

	// Available width inside padding (1 left + 1 right)
	contentWidth := max(m.width-6, 40)
	textStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	wrapStyle := textStyle.Width(contentWidth)

	// Header: severity + ID on same line with proper background
	severityStyle := m.getSeverityStyle(f.Severity)
	b.WriteString(severityStyle.Render(string(f.Severity)) + theme.Bg(" ") + theme.TitleStyle.Render(f.ID) + "\n\n")

	// Title with wrapping
	b.WriteString(theme.SubTitleStyle.Render("Title:") + "\n")
	b.WriteString(wrapStyle.Render(f.Title) + "\n\n")

	// Description with wrapping
	if f.Description != "" {
		b.WriteString(theme.SubTitleStyle.Render("Description:") + "\n")
		b.WriteString(wrapStyle.Render(f.Description) + "\n\n")
	}

	b.WriteString(theme.SubTitleStyle.Render("Source: ") + textStyle.Render(f.Source) + "\n")

	// File path with wrapping (can be long)
	if f.File != "" {
		b.WriteString(theme.SubTitleStyle.Render("File: ") + textStyle.Width(contentWidth-6).Render(f.File) + "\n")
	}

	if f.Line > 0 {
		b.WriteString(theme.SubTitleStyle.Render("Line: ") + textStyle.Render(fmt.Sprintf("%d", f.Line)) + "\n")
	}

	if f.PkgName != "" {
		b.WriteString(theme.SubTitleStyle.Render("Package: ") + textStyle.Render(f.PkgName) + "\n")
		if f.Version != "" {
			b.WriteString(theme.SubTitleStyle.Render("Version: ") + textStyle.Render(f.Version) + "\n")
		}
		if f.FixedIn != "" {
			b.WriteString(theme.SubTitleStyle.Render("Fixed In: ") + theme.StatusOKStyle.Render(f.FixedIn) + "\n")
		}
	}

	if f.Match != "" {
		b.WriteString(theme.SubTitleStyle.Render("Match: ") + textStyle.Render(f.Match) + "\n")
	}

	// Remediation section
	if f.Resolution != "" || f.FixCommand != "" {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Render("Remediation:") + "\n")
		if f.Resolution != "" {
			b.WriteString(wrapStyle.Render(f.Resolution) + "\n")
		}
		if f.FixCommand != "" {
			b.WriteString(theme.DimStyle.Render("Run: ") + textStyle.Render(f.FixCommand) + "\n")
		}
	}

	// References section
	if len(f.References) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Render("References:") + "\n")
		for _, ref := range f.References {
			b.WriteString(textStyle.Width(contentWidth-2).Render("  "+ref) + "\n")
		}
	}

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(b.String())
}

// renderDetailsView renders the scrollable details viewport.
func (m Model) renderDetailsView() string {
	return m.detailsViewport.View()
}

// getSeverityStyle returns style for severity level (Rule 102: the palette
// lives in the theme, not here).
func (m Model) getSeverityStyle(sev scan.SeverityLevel) lipgloss.Style {
	return theme.SeverityTextStyle(string(sev))
}

// HeaderView interface implementation
