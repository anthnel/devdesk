package auth

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func (m Model) InEditMode() bool {
	// En mode édition seulement si un textinput a le focus (champs 0 ou 1)
	// et que l'utilisateur n'est pas en train d'authentifier
	// Si authentifié, on n'est plus en mode édition
	if m.authenticated || m.authenticating {
		return false
	}
	// En mode édition si on est sur un des champs de texte
	return m.currentField == 0 || m.currentField == 1
}

func (m Model) GetShortcuts() shortcut.Shortcuts {
	return []shortcut.Shortcut{
		{Key: "enter", Description: "Submit / Advance field"},
		{Key: "space", Description: "Select option"},
		{Key: "ctrl+s", Description: "Toggle save to helper"},
		{Key: "ctrl+f", Description: "Toggle save to config"},
		{Key: "alt+:", Description: "Command mode"},
		{Key: "?", Description: "Help"},
	}
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	return theme.EmptyLineBg(width) + "\n" + theme.EmptyLineBg(width)
}

func (m Model) GetTitle() string {
	return theme.IconUser + " Gitlab Authentication"
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
}

// GetHelpContent retourne le contenu d'aide de la vue GitLab Auth
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "GitLab Authentication",
		Description: "This view allows you to connect to a GitLab instance. Once authenticated, you can explore groups and projects, clone repositories, and manage your GitLab resources.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑ / ↓", Description: "Navigate between form fields"},
			{Key: "enter", Description: "Submit form / advance to next field"},
			{Key: "space", Description: "Select save option"},
			{Key: "ctrl+s", Description: "Toggle save to Git Credential Manager"},
			{Key: "ctrl+f", Description: "Toggle save to config file"},
			{Key: "alt+:", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Save Options",
				Body:  "Git Credential Manager (recommended): stores the token securely via Git's credential helper. The token does not appear in configuration files.\n\nConfig file: saves the token in ~/.devdesk/contexts/<context>/config.yaml. Less secure but works without a credential helper.",
			},
			{
				Title: "Personal Access Token",
				Body:  "Create a token in GitLab > Settings > Access Tokens. Required scopes: api, read_user. The token must start with 'glpat-'.",
			},
		},
	}
}

// View rend la vue
func (m *Model) View() string {
	var sections []string

	// Status (only show when not authenticated, to avoid duplication with logged-in view)
	if m.success != "" && !m.authenticated {
		sections = append(sections, m.renderSuccess())
	}

	// Warning (non-bloquant, ex: échec sauvegarde credentials)
	if m.warning != "" {
		sections = append(sections, m.renderWarning())
	}

	// Form
	sections = append(sections, m.renderForm())

	// Error
	if m.error != "" {
		sections = append(sections, m.renderError())
	}

	return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(strings.Join(sections, "\n"))
}

func (m *Model) renderForm() string {
	var b strings.Builder

	// Spinner si en cours d'authentification
	if m.authenticating {
		b.WriteString(theme.SpinnerMessage(m.spinner.View(), "Authenticating...") + "\n\n")
		return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1, 2).Render(b.String())
	}

	// Si authentifié, afficher la vue "Logged in"
	if m.authenticated && m.user != nil {
		return m.renderLoggedInView()
	}

	// Sinon, afficher le formulaire de login
	// URL
	labelStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Bold(true)
	if m.currentField == 0 {
		labelStyle = labelStyle.Foreground(theme.ColorPrimary)
	}
	b.WriteString(labelStyle.Render("GitLab URL") + "\n")
	b.WriteString(m.urlInput.View() + "\n\n")

	// Token
	labelStyle = lipgloss.NewStyle().Background(theme.ColorBackground).Bold(true)
	if m.currentField == 1 {
		labelStyle = labelStyle.Foreground(theme.ColorPrimary)
	}
	b.WriteString(labelStyle.Render("Personal Access Token") + "\n")
	b.WriteString(m.tokenInput.View() + "\n\n")

	// Options
	b.WriteString(theme.DimStyle.Render("Save options:") + "\n")

	b.WriteString(theme.RenderRadioButton(m.saveOption == SaveToHelper, "Save to Git Credential Manager (secure)", m.currentField == 2) + "\n")
	b.WriteString(theme.RenderRadioButton(m.saveOption == SaveToConfig, "Save token to config file (less secure)", m.currentField == 3) + "\n\n")

	// Button
	b.WriteString(theme.RenderButton("Login", m.currentField == 4, "primary"))

	return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1, 2).Render(b.String())
}

func (m *Model) renderLoggedInView() string {
	var b strings.Builder

	// Afficher les infos de l'utilisateur
	b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Bold(true).Render("Authenticated as: "))
	b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorPrimary).Bold(true).Render(m.user.Username))
	if m.user.Name != "" {
		b.WriteString(theme.Bg(" (" + m.user.Name + ")"))
	}
	b.WriteString("\n")

	// Afficher l'URL GitLab
	if m.config != nil && m.config.GitLab.URL != "" {
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Bold(true).Render("GitLab URL: "))
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorPrimary).Bold(true).Render(m.config.GitLab.URL) + "\n")
	}
	b.WriteString("\n")

	// Bouton Logout
	b.WriteString(theme.RenderButton("Logout", m.currentField == 0, "danger"))

	return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1, 2).Render(b.String())
}

func (m *Model) renderSuccess() string {
	style := lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorOK).
		Padding(1, 2)
	return style.Render(m.success)
}

func (m *Model) renderWarning() string {
	style := lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorError).
		Padding(1, 2).
		Width(m.width - 8)
	return style.Render(theme.IconWarning + " " + m.warning)
}

func (m *Model) renderError() string {
	style := lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorError).
		Padding(1, 2)
	return style.Render("✗ " + m.error)
}
