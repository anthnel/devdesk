package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ThemeListMsg contient la liste des thèmes disponibles
type ThemeListMsg struct {
	Themes  []string
	Current string
}

// ThemeAppliedMsg signale qu'un thème a été appliqué avec succès
type ThemeAppliedMsg struct {
	ThemeName string
}

// ThemeErrorMsg signale une erreur liée aux thèmes
type ThemeErrorMsg struct {
	Error error
}

// listThemes retourne la liste des thèmes disponibles
func (a *App) listThemes() tea.Cmd {
	return func() tea.Msg {
		log.Printf("Listing available themes")
		themes, err := theme.ListThemes()
		if err != nil {
			log.Printf("ERROR: Failed to list themes: %v", err)
			return ThemeErrorMsg{Error: err}
		}

		return ThemeListMsg{Themes: themes, Current: theme.CurrentThemeName}
	}
}

// applyTheme charge et applique un thème par nom
func (a *App) applyTheme(name string) tea.Cmd {
	cfgRef := a.config
	return func() tea.Msg {
		log.Printf("Applying theme: %s", name)
		t, err := theme.LoadTheme(name)
		if err != nil {
			log.Printf("ERROR: Failed to load theme '%s': %v", name, err)
			return ThemeErrorMsg{Error: err}
		}

		theme.ApplyTheme(t)
		theme.CurrentThemeName = name

		// Sauvegarder le choix de thème dans la config
		cfgRef.App.Theme = name
		if err := config.Save(cfgRef); err != nil {
			log.Printf("ERROR: Failed to save theme preference: %v", err)
		}

		log.Printf("Theme '%s' applied successfully", name)
		return ThemeAppliedMsg{ThemeName: name}
	}
}

// handleThemeList opens the picker with the cursor on the theme in use.
func (a *App) handleThemeList(msg ThemeListMsg) (tea.Model, tea.Cmd) {
	a.showThemeList = true
	a.themeList = msg.Themes
	a.currentTheme = msg.Current
	a.themeSelectedIdx = indexOf(msg.Themes, msg.Current)
	return a, nil
}

// handleThemeApplied refreshes the UI: the styles the theme changed are baked
// into every rendered line, so the whole tree has to be laid out again.
func (a *App) handleThemeApplied(msg ThemeAppliedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Theme applied: %s, refreshing UI", msg.ThemeName)
	return a, a.requestResize()
}

// handleThemeListKeyMsg gère les touches clavier dans l'overlay de sélection de thème
func (a *App) handleThemeListKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.showThemeList = false
	case "up", "k":
		a.themeSelectedIdx = max(a.themeSelectedIdx-1, 0)
	case "down", "j":
		a.themeSelectedIdx = min(a.themeSelectedIdx+1, len(a.themeList)-1)
	case "enter":
		if len(a.themeList) > 0 {
			a.showThemeList = false
			return a, a.applyTheme(a.themeList[a.themeSelectedIdx])
		}
	}
	return a, nil
}

// renderThemeListOverlay affiche un overlay avec la liste des thèmes
func (a *App) renderThemeListOverlay() string {
	return renderPickerOverlay("Select Theme", a.themeList, a.currentTheme, a.themeSelectedIdx)
}
