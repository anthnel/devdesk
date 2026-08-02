package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// sectionsToLines splits rendered sections into individual lines with background separators
func sectionsToLines(sections []string, colWidth int) []string {
	emptyLine := theme.EmptyLineBg(colWidth)
	var lines []string
	for i, s := range sections {
		lines = append(lines, strings.Split(s, "\n")...)
		if i < len(sections)-1 {
			lines = append(lines, emptyLine)
		}
	}
	return lines
}

// View renders the dashboard as a two-column layout
func (m Model) View() string {
	colWidth := m.columnWidth()

	// Left column: GitLab, Services, Certificates, Workspaces
	leftSections := []string{
		m.renderGitLabSection(colWidth),
		m.renderServicesSection(colWidth),
		m.renderCertificatesSection(colWidth),
		m.renderWorkspacesSection(colWidth),
	}

	// Right column: OCI, Containers, Tools
	rightSections := []string{
		m.renderOCISection(colWidth),
		m.renderContainersSection(colWidth),
		m.renderToolsSection(colWidth),
	}

	// Split sections into lines
	leftLines := sectionsToLines(leftSections, colWidth)
	rightLines := sectionsToLines(rightSections, colWidth)

	// Equalize heights
	emptyLine := theme.EmptyLineBg(colWidth)
	maxH := len(leftLines)
	if len(rightLines) > maxH {
		maxH = len(rightLines)
	}
	for len(leftLines) < maxH {
		leftLines = append(leftLines, emptyLine)
	}
	for len(rightLines) < maxH {
		rightLines = append(rightLines, emptyLine)
	}

	// Combine columns line by line (no JoinHorizontal)
	var rows []string
	for i := 0; i < maxH; i++ {
		rows = append(rows, theme.PadWithBg(leftLines[i], colWidth)+theme.PadWithBg(rightLines[i], colWidth))
	}

	content := strings.Join(rows, "\n")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(content),
		lipgloss.WithWhitespaceBackground(theme.ColorBackground))
}

// renderGitLabSection renders the GitLab card (without Projects/Groups)
func (m Model) renderGitLabSection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconGitlab+" GitLab") + "\n\n")

	if !m.shared.IsAuthenticated {
		b.WriteString(theme.DimStyle.Render("Not connected"))
		b.WriteString("\n")
		b.WriteString(theme.HelpStyle.Render("Authenticate with :gitlab-auth"))
		return m.renderSection(b.String(), width)
	}

	if m.loadingGitLab {
		b.WriteString(theme.DimStyle.Render("Loading..."))
		return m.renderSection(b.String(), width)
	}

	if m.shared.CurrentUser != nil {
		b.WriteString(theme.StatusOKStyle.Render("✓ Connected") + theme.Bg(" as ") + theme.PrimaryColorStyle.Bold(true).Render(m.shared.CurrentUser.Username))
		b.WriteString("\n\n")
	}

	if m.gitlabStats != nil {
		stats := m.gitlabStats
		b.WriteString(theme.Bg("Merge Requests:  ") + renderCount(stats.AssignedMRs) + theme.Bg(" assigned, ") + renderCount(stats.ReviewMRs) + theme.Bg(" to review"))
		b.WriteString("\n")
		b.WriteString(theme.Bg("Issues:          ") + renderCount(stats.AssignedIssues) + theme.Bg(" assigned"))
	}

	b.WriteString("\n")

	return m.renderSection(b.String(), width)
}

// renderServicesSection renders the services status card (non-SSL only)
func (m Model) renderServicesSection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconService+" Services") + "\n\n")

	if m.loadingServices {
		b.WriteString(theme.DimStyle.Render("Loading..."))
		return m.renderSection(b.String(), width)
	}

	services := m.filterComponents(false)

	if len(services) == 0 {
		b.WriteString(theme.DimStyle.Render("No monitors configured"))
		b.WriteString("\n")
		b.WriteString(theme.HelpStyle.Render("Configure in :status"))
		return m.renderSection(b.String(), width)
	}

	okCount, downCount, errorCount := countStatuses(services)

	b.WriteString(theme.StatusOKStyle.Render(fmt.Sprintf("✓ %d OK", okCount)))
	if downCount > 0 {
		b.WriteString(theme.Bg("  ") + theme.StatusDownStyle.Render(fmt.Sprintf("✗ %d Down", downCount)))
	}
	if errorCount > 0 {
		b.WriteString(theme.Bg("  ") + theme.StatusErrorStyle.Render(fmt.Sprintf("%s %d Error", theme.IconWarning, errorCount)))
	}

	b.WriteString("\n")

	return m.renderSection(b.String(), width)
}

// renderCertificatesSection renders SSL certificate status
func (m Model) renderCertificatesSection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconCertificate+" Certificates") + "\n\n")

	if m.loadingServices {
		b.WriteString(theme.DimStyle.Render("Loading..."))
		return m.renderSection(b.String(), width)
	}

	certs := m.filterComponents(true)

	if len(certs) == 0 {
		b.WriteString(theme.DimStyle.Render("No SSL monitors"))
		return m.renderSection(b.String(), width)
	}

	okCount, downCount, errorCount := countStatuses(certs)

	b.WriteString(theme.StatusOKStyle.Render(fmt.Sprintf("✓ %d valid", okCount)))
	if downCount > 0 {
		b.WriteString(theme.Bg("  ") + theme.StatusDownStyle.Render(fmt.Sprintf("✗ %d expired", downCount)))
	}
	if errorCount > 0 {
		b.WriteString(theme.Bg("  ") + theme.StatusErrorStyle.Render(fmt.Sprintf("%s %d warning", theme.IconWarning, errorCount)))
	}

	b.WriteString("\n")

	return m.renderSection(b.String(), width)
}

// renderWorkspacesSection renders the workspaces card with disk usage
func (m Model) renderWorkspacesSection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconWorkspace+" Workspaces") + "\n\n")

	if m.loadingWorkspaces {
		b.WriteString(theme.DimStyle.Render("Loading..."))
		return m.renderSection(b.String(), width)
	}

	b.WriteString(theme.Bg(fmt.Sprintf("%d workspaces", m.workspaceCount)))
	if m.workspaceDisk != "" {
		b.WriteString(theme.DimStyle.Render(fmt.Sprintf("  (%s)", m.workspaceDisk)))
	}
	b.WriteString("\n")
	b.WriteString(theme.DimStyle.Render(m.config.App.WorkspacesDir))

	return m.renderSection(b.String(), width)
}

// renderOCISection renders OCI resource counts and disk usage
func (m Model) renderOCISection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconDocker+" OCI Resources") + "\n\n")

	if m.loadingOCI {
		b.WriteString(theme.DimStyle.Render("Loading..."))
		return m.renderSection(b.String(), width)
	}

	if m.ociStats == nil || !m.ociStats.Available {
		b.WriteString(theme.DimStyle.Render("Docker not available"))
		return m.renderSection(b.String(), width)
	}

	s := m.ociStats
	b.WriteString(theme.Bg("Images:      ") + renderCountWithSize(s.ImagesCount, s.ImagesSize))
	b.WriteString("\n")
	b.WriteString(theme.Bg("Containers:  ") + renderCountWithSize(s.ContainersCount, s.ContainersSize))
	b.WriteString("\n")
	b.WriteString(theme.Bg("Volumes:     ") + renderCountWithSize(s.VolumesCount, s.VolumesSize))

	b.WriteString("\n")

	return m.renderSection(b.String(), width)
}

// renderContainersSection renders container counts by state
func (m Model) renderContainersSection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconContainer+" Containers") + "\n\n")

	if m.loadingDocker {
		b.WriteString(theme.DimStyle.Render("Loading..."))
		return m.renderSection(b.String(), width)
	}

	if m.dockerStats == nil || !m.dockerStats.Available {
		b.WriteString(theme.DimStyle.Render("Docker not available"))
		return m.renderSection(b.String(), width)
	}

	d := m.dockerStats
	total := d.Running + d.Stopped + d.Paused

	b.WriteString(theme.Bg(fmt.Sprintf("%d containers", total)))
	b.WriteString("\n")
	b.WriteString(theme.StatusOKStyle.Render(fmt.Sprintf("%s %d running", theme.IconPlay, d.Running)))
	if d.Stopped > 0 {
		b.WriteString(theme.Bg("  ") + theme.DimStyle.Render(fmt.Sprintf("%s %d stopped", theme.IconStop, d.Stopped)))
	}
	if d.Paused > 0 {
		b.WriteString(theme.Bg("  ") + theme.StatusWarningStyle.Render(fmt.Sprintf("%s %d paused", theme.IconPause, d.Paused)))
	}

	b.WriteString("\n")

	return m.renderSection(b.String(), width)
}

// renderToolsSection renders detected DevSecOps tools
func (m Model) renderToolsSection(width int) string {
	var b strings.Builder

	b.WriteString(theme.SubTitleStyle.Render(theme.IconTools+" Tools") + "\n\n")

	if m.loadingTools {
		b.WriteString(theme.DimStyle.Render("Detecting..."))
		return m.renderSection(b.String(), width)
	}

	if len(m.tools) == 0 {
		b.WriteString(theme.DimStyle.Render("No tools detected"))
		return m.renderSection(b.String(), width)
	}

	// Find max tool name length for alignment
	maxNameLen := 0
	for _, tool := range m.tools {
		if len(tool.Name) > maxNameLen {
			maxNameLen = len(tool.Name)
		}
	}

	for i, tool := range m.tools {
		paddedName := tool.Name + strings.Repeat(" ", maxNameLen-len(tool.Name))
		if tool.Available {
			b.WriteString(theme.StatusOKStyle.Render("✓") + theme.Bg(" "+paddedName))
		} else {
			b.WriteString(theme.DimStyle.Render("✗ " + paddedName))
		}
		if i < len(m.tools)-1 {
			b.WriteString("\n")
		}
	}

	return m.renderSection(b.String(), width)
}

// renderSection wraps content in a styled section with background
func (m Model) renderSection(content string, width int) string {
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Padding(0, 1).
		Width(width).
		Render(content)
}

// columnWidth returns width for each column
func (m Model) columnWidth() int {
	w := (m.width - 6) / 2
	if w < 30 {
		w = 30
	}
	return w
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

// renderCount formats a count with highlight if > 0
func renderCount(n int) string {
	if n > 0 {
		return theme.PrimaryColorStyle.Bold(true).Render(fmt.Sprintf("%d", n))
	}
	return theme.DimStyle.Render("0")
}

// renderCountWithSize formats "N (size)" with styling
func renderCountWithSize(count int, size string) string {
	countStr := renderCount(count)
	if size != "" {
		return countStr + theme.DimStyle.Render(fmt.Sprintf("  (%s)", size))
	}
	return countStr
}

// HeaderView interface

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	return theme.EmptyLineBg(width) + "\n" + theme.EmptyLineBg(width)
}

// GetShortcuts returns the keyboard shortcuts for the header
func (m Model) GetShortcuts() shortcut.Shortcuts {
	shortcuts := []shortcut.Shortcut{
		{Key: "ctrl+r", Description: "Refresh"},
	}
	if m.shared.IsAuthenticated {
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "m", Description: "Open MRs"},
			shortcut.Shortcut{Key: "i", Description: "Open issues"},
		)
	}
	shortcuts = append(shortcuts,
		shortcut.Shortcut{Key: ":", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
	return shortcuts
}

// GetTitle returns the view title
func (m Model) GetTitle() string {
	return theme.IconDashboard + " Dashboard"
}

// GetIcon returns the view icon
func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
}

// GetHelpContent returns help content for the dashboard (Rule 114)
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Dashboard",
		Description: "The dashboard provides an overview of all your DevSecOps resources in a two-column layout. Left column shows GitLab activity, service health, SSL certificates, and workspaces. Right column shows OCI resources, container states, and available tools.",
		KeyBindings: []help.KeyBinding{
			{Key: "ctrl+r", Description: "Refresh all dashboard data"},
			{Key: "m", Description: "Open assigned merge requests in browser (requires authentication)"},
			{Key: "i", Description: "Open assigned issues in browser (requires authentication)"},
			{Key: ":", Description: "Open command mode to navigate to other views"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "GitLab",
				Body:  "Displays your GitLab activity: assigned merge requests, MRs awaiting your review, and assigned issues. Requires authentication via :gitlab-auth.",
			},
			{
				Title: "Services & Certificates",
				Body:  "Shows the health of monitored services (HTTP, ICMP, DNS) separately from SSL certificate monitors. Green when all OK, yellow when degraded, red when all down. Configure in :status view.",
			},
			{
				Title: "Workspaces",
				Body:  "Shows the number of local workspaces and total disk usage of the workspaces directory.",
			},
			{
				Title: "OCI Resources",
				Body:  "Displays counts and disk usage for Docker images, containers, and volumes via 'docker system df'.",
			},
			{
				Title: "Containers",
				Body:  "Shows container counts by state: running, stopped, and paused. Requires the docker CLI.",
			},
			{
				Title: "Tools",
				Body:  "Lists detected DevSecOps tools (Docker, Trivy, Gitleaks, Git) with their versions and source (binary or docker image).",
			},
			{
				Title: "Navigation",
				Body:  "Use command mode (:) to switch to specific views: :status for monitors, :gitlab-auth for authentication, :gitlab-explorer for browsing projects, :workspaces for file management, :containers for Docker management, :oci-resources for OCI resource management, :security for scanning.",
			},
		},
	}
}
