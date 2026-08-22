package auth

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func (m Model) InEditMode() bool {
	// En mode édition seulement si un textinput a le focus
	// et que l'utilisateur n'est pas en train d'authentifier
	// Si authentifié, on n'est plus en mode édition
	if m.authenticated || m.authenticating {
		return false
	}
	// En mode édition si on est sur un des champs de texte
	return m.currentField == fieldToken
}

func (m Model) GetShortcuts() shortcut.Shortcuts {
	return []shortcut.Shortcut{
		{Key: "enter", Description: "Submit / Advance field"},
		{Key: "ctrl+p", Description: "Command mode"},
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
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Where the token is stored",
				Body: "The token goes to the host's own secret manager — the Windows Credential Manager, the macOS Keychain, or a Secret Service implementation on Linux. It is never written to a configuration file.\n\n" +
					"When no such store answers, DevDesk falls back to git's credential helper, provided git is configured with one that does not itself write plaintext. Failing that, the token is kept for this session only and you will be asked for it again next launch — the view says so when that happens.\n\n" +
					"Set app.secret_backend in the context configuration to \"keyring\" or \"git-credential\" to pin one of them instead of letting DevDesk choose.",
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

	// Ce que la migration hors du fichier de config a fait, le cas échéant
	if len(m.notices) > 0 {
		sections = append(sections, m.renderNotices())
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
	if m.authenticated {
		return m.renderLoggedInView()
	}

	// The URL is shown, not edited: it is configuration, and the configuration
	// view owns it. Both views used to write it, so neither was authoritative.
	b.WriteString(m.renderConfiguredURL() + "\n\n")

	// Token
	labelStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Bold(true)
	if m.currentField == fieldToken {
		labelStyle = labelStyle.Foreground(theme.ColorPrimary)
	}
	b.WriteString(labelStyle.Render("Personal Access Token") + "\n")
	b.WriteString(m.tokenInput.View() + "\n\n")

	// Destination du token. Ce n'est pas un choix — c'est le seul chemin — mais
	// l'utilisateur doit pouvoir lire où part son secret, et surtout constater
	// quand rien n'est enregistré (§3.9).
	b.WriteString(m.renderSecretDestination() + "\n\n")

	// Button
	b.WriteString(theme.RenderButton("Login", m.currentField == fieldSubmit, "primary"))

	return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1, 2).Render(b.String())
}

// renderSecretDestination names the store the token goes to, or warns that
// there is none. The warning is deliberately the louder of the two.
func (m *Model) renderSecretDestination() string {
	if m.secrets.Persists() {
		return theme.DimStyle.Render(m.secrets.Detail)
	}
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorWarn).
		Render(theme.IconWarning + " " + m.secrets.Detail)
}

// renderNotices reports what the migration off plaintext configuration did.
// Empty in every normal run — it only has something to say the first time a
// user launches a build that no longer keeps secrets in config.yaml.
func (m *Model) renderNotices() string {
	style := lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorHighlight).
		Padding(1, 2)
	return style.Render(theme.IconLock + " " + strings.Join(m.notices, "\n  "))
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
	b.WriteString(theme.RenderButton("Logout", m.currentField == fieldToken, "danger"))

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

// renderConfiguredURL shows the server this view will authenticate against, and
// says where to change it. Empty is the case worth naming: without a URL there
// is nothing to log into, and the user would otherwise be told the token is
// missing for a problem that is not the token.
func (m Model) renderConfiguredURL() string {
	label := lipgloss.NewStyle().Background(theme.ColorBackground).Bold(true).Render("GitLab URL")

	if m.config == nil || m.config.GitLab.URL == "" {
		return label + "\n" + theme.StatusErrorStyle.Render(
			theme.IconWarning+" not configured — set it in :config, gitlab tab")
	}
	value := lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorPrimary).
		Render(m.config.GitLab.URL)
	return label + "\n" + value + theme.DimStyle.Render("   change it in :config")
}
