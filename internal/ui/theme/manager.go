package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme représente un thème de couleurs pour l'application
type Theme struct {
	Name string `json:"name"`

	// Status colors
	ColorOK    string `json:"color_ok"`
	ColorError string `json:"color_error"`
	ColorWarn  string `json:"color_warn"`

	// UI colors
	ColorPrimary    string `json:"color_primary"`
	ColorSecondary  string `json:"color_secondary"`
	ColorBorder     string `json:"color_border"`
	ColorText       string `json:"color_text"`
	ColorDim        string `json:"color_dim"`
	ColorHighlight  string `json:"color_highlight"`
	ColorWhite      string `json:"color_white"`
	ColorBlack      string `json:"color_black"`
	ColorBackground string `json:"color_background"`

	// Component colors (optional, fallback to semantic colors)
	TitleFg    string `json:"title_fg,omitempty"`
	AppTitleFg string `json:"app_title_fg,omitempty"`
	SubTitleFg string `json:"subtitle_fg,omitempty"`

	ButtonFg         string `json:"button_fg,omitempty"`
	ButtonBg         string `json:"button_bg,omitempty"`
	ButtonInactiveFg string `json:"button_inactive_fg,omitempty"`
	ButtonInactiveBg string `json:"button_inactive_bg,omitempty"`

	TabActiveFg   string `json:"tab_active_fg,omitempty"`
	TabActiveBg   string `json:"tab_active_bg,omitempty"`
	TabInactiveFg string `json:"tab_inactive_fg,omitempty"`
	TabInactiveBg string `json:"tab_inactive_bg,omitempty"`

	TableHeaderFg   string `json:"table_header_fg,omitempty"`
	TableSelectedFg string `json:"table_selected_fg,omitempty"`
	TableSelectedBg string `json:"table_selected_bg,omitempty"`

	CmdLineFg         string `json:"cmd_line_fg,omitempty"`
	CmdLineBg         string `json:"cmd_line_bg,omitempty"`
	CmdLineInactiveFg string `json:"cmd_line_inactive_fg,omitempty"`

	ViewportBorder string `json:"viewport_border,omitempty"`

	// Severity colors (background + foreground pairs)
	SeverityCritical   string `json:"severity_critical,omitempty"`
	SeverityCriticalFg string `json:"severity_critical_fg,omitempty"`
	SeverityHigh       string `json:"severity_high,omitempty"`
	SeverityHighFg     string `json:"severity_high_fg,omitempty"`
	SeverityMedium     string `json:"severity_medium,omitempty"`
	SeverityMediumFg   string `json:"severity_medium_fg,omitempty"`
	SeverityLow        string `json:"severity_low,omitempty"`
	SeverityLowFg      string `json:"severity_low_fg,omitempty"`
	SeverityInfo       string `json:"severity_info,omitempty"`
	SeverityInfoFg     string `json:"severity_info_fg,omitempty"`
}

// DefaultTheme retourne le thème par défaut (Catppuccin Mocha)
func DefaultTheme() *Theme {
	return &Theme{
		Name:               "default",
		ColorOK:            "#a6e3a1",
		ColorError:         "#f38ba8",
		ColorWarn:          "#fab387",
		ColorPrimary:       "#cba6f7",
		ColorSecondary:     "#b4befe",
		ColorBorder:        "#6c7086",
		ColorText:          "#cdd6f4",
		ColorDim:           "#585b70",
		ColorHighlight:     "#f9e2af",
		ColorWhite:         "#cdd6f4",
		ColorBlack:         "#1e1e2e",
		ColorBackground:    "#1e1e2e",
		TitleFg:            "#cba6f7",
		AppTitleFg:         "#cba6f7",
		SubTitleFg:         "#b4befe",
		ButtonFg:           "#1e1e2e",
		ButtonBg:           "#cba6f7",
		ButtonInactiveFg:   "#cdd6f4",
		ButtonInactiveBg:   "#585b70",
		TabActiveFg:        "#1e1e2e",
		TabActiveBg:        "#b4befe",
		TabInactiveFg:      "#585b70",
		TabInactiveBg:      "#313244",
		TableHeaderFg:      "#b4befe",
		TableSelectedFg:    "#1e1e2e",
		TableSelectedBg:    "#b4befe",
		CmdLineFg:          "#f9e2af",
		CmdLineBg:          "#313244",
		CmdLineInactiveFg:  "#585b70",
		ViewportBorder:     "#6c7086",
		SeverityCritical:   "#f38ba8",
		SeverityCriticalFg: "#1e1e2e",
		SeverityHigh:       "#eba0ac",
		SeverityHighFg:     "#1e1e2e",
		SeverityMedium:     "#fab387",
		SeverityMediumFg:   "#1e1e2e",
		SeverityLow:        "#313244",
		SeverityLowFg:      "#cdd6f4",
		SeverityInfo:       "#45475a",
		SeverityInfoFg:     "#cdd6f4",
	}
}

// ThemeDir retourne le répertoire des thèmes (~/.devdesk/themes/)
func ThemeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".devdesk", "themes"), nil
}

// LoadTheme charge un thème depuis un fichier JSON
// Si name est vide, "dark" ou "default", retourne le thème par défaut intégré
func LoadTheme(name string) (*Theme, error) {
	if name == "" || name == "dark" || name == "default" {
		return DefaultTheme(), nil
	}

	dir, err := ThemeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get theme directory: %w", err)
	}

	path := filepath.Join(dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read theme '%s': %w", name, err)
	}

	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("failed to parse theme '%s': %w", name, err)
	}

	// Assurer que le nom est défini
	if t.Name == "" {
		t.Name = name
	}

	return &t, nil
}

// ListThemes retourne la liste des thèmes disponibles
// Inclut toujours "default" (thème intégré) + les fichiers JSON du répertoire themes
func ListThemes() ([]string, error) {
	themes := []string{"default"}

	dir, err := ThemeDir()
	if err != nil {
		return themes, nil // Retourner au moins "dark"
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return themes, nil // Répertoire n'existe pas encore
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if themeName, ok := strings.CutSuffix(name, ".json"); ok {
			// Éviter le doublon avec le thème intégré
			if themeName == "default" {
				continue
			}
			themes = append(themes, themeName)
		}
	}

	return themes, nil
}

// applyColor returns a lipgloss.Color from a hex string, or the fallback if empty.
func applyColor(hex string, fallback lipgloss.Color) lipgloss.Color {
	if hex == "" {
		return fallback
	}
	return lipgloss.Color(hex)
}

// ApplyTheme applique un thème en mettant à jour les variables de couleur globales
// puis en rafraîchissant tous les styles
func ApplyTheme(t *Theme) {
	// Mettre à jour les couleurs sémantiques globales
	ColorOK = lipgloss.Color(t.ColorOK)
	ColorError = lipgloss.Color(t.ColorError)
	ColorWarn = lipgloss.Color(t.ColorWarn)

	ColorPrimary = lipgloss.Color(t.ColorPrimary)
	ColorSecondary = lipgloss.Color(t.ColorSecondary)
	ColorBorder = lipgloss.Color(t.ColorBorder)
	ColorText = lipgloss.Color(t.ColorText)
	ColorDim = lipgloss.Color(t.ColorDim)
	ColorHighlight = lipgloss.Color(t.ColorHighlight)
	ColorWhite = lipgloss.Color(t.ColorWhite)
	ColorBlack = lipgloss.Color(t.ColorBlack)
	ColorBackground = applyColor(t.ColorBackground, ColorBackground)

	// Appliquer les couleurs par composant (fallback sur couleurs sémantiques)
	ColorTitleFg = applyColor(t.TitleFg, ColorPrimary)
	ColorAppTitleFg = applyColor(t.AppTitleFg, ColorPrimary)
	ColorSubTitleFg = applyColor(t.SubTitleFg, ColorSecondary)

	ColorButtonFg = applyColor(t.ButtonFg, ColorWhite)
	ColorButtonBg = applyColor(t.ButtonBg, ColorPrimary)
	ColorButtonInactiveFg = applyColor(t.ButtonInactiveFg, ColorWhite)
	ColorButtonInactiveBg = applyColor(t.ButtonInactiveBg, ColorDim)

	ColorTabActiveFg = applyColor(t.TabActiveFg, ColorBlack)
	ColorTabActiveBg = applyColor(t.TabActiveBg, ColorSecondary)
	ColorTabInactiveFg = applyColor(t.TabInactiveFg, ColorDim)
	ColorTabInactiveBg = applyColor(t.TabInactiveBg, ColorCmdLineBg)

	ColorTableHeaderFg = applyColor(t.TableHeaderFg, ColorSecondary)
	ColorTableSelectedFg = applyColor(t.TableSelectedFg, ColorBlack)
	ColorTableSelectedBg = applyColor(t.TableSelectedBg, ColorSecondary)

	ColorCmdLineFg = applyColor(t.CmdLineFg, ColorHighlight)
	ColorCmdLineBg = applyColor(t.CmdLineBg, ColorCmdLineBg)

	// Le fond d'un graphe est celui de la ligne de commande : c'est la surface
	// « un cran plus claire » que chaque thème définit déjà, donc un graphe se
	// détache du reste de sa boîte sans qu'aucun thème ait à déclarer une
	// couleur de plus.
	ColorChartBg = ColorCmdLineBg
	ColorCmdLineInactiveFg = applyColor(t.CmdLineInactiveFg, ColorDim)

	ColorViewportBorder = applyColor(t.ViewportBorder, ColorBorder)

	ColorHeaderKey = ColorSecondary

	// Syntax colors: aliases of the semantic palette, assigned here rather than
	// at declaration because the palette itself is only populated by this
	// function — a package-level `= ColorSecondary` would capture the zero
	// value and never follow a theme change. ColorChartBg above is the same
	// pattern.
	ColorSyntaxKey = ColorSecondary
	ColorSyntaxString = ColorOK
	ColorSyntaxNumber = ColorHighlight
	ColorSyntaxLiteral = ColorPrimary
	ColorSyntaxPunct = ColorDim
	ColorSyntaxTag = ColorSecondary
	ColorSyntaxAttr = ColorPrimary
	ColorSyntaxComment = ColorDim

	ColorSearchMatch = ColorHighlight
	ColorSearchMatchFg = ColorBlack

	// Severity colors (background + foreground pairs)
	ColorSeverityCritical = applyColor(t.SeverityCritical, ColorError)
	ColorSeverityCriticalFg = applyColor(t.SeverityCriticalFg, ColorBlack)
	ColorSeverityHigh = applyColor(t.SeverityHigh, ColorSeverityHigh)
	ColorSeverityHighFg = applyColor(t.SeverityHighFg, ColorBlack)
	ColorSeverityMedium = applyColor(t.SeverityMedium, ColorWarn)
	ColorSeverityMediumFg = applyColor(t.SeverityMediumFg, ColorBlack)
	ColorSeverityLow = applyColor(t.SeverityLow, ColorCmdLineBg)
	ColorSeverityLowFg = applyColor(t.SeverityLowFg, ColorText)
	ColorSeverityInfo = applyColor(t.SeverityInfo, ColorSeverityInfo)
	ColorSeverityInfoFg = applyColor(t.SeverityInfoFg, ColorText)

	// Footer message colours (Rule 128). Assigned after the severity block
	// because they alias it — the intent is that a footer error reads like a
	// CRITICAL finding and a warning like a MEDIUM one.
	ColorFooterInfo = ColorText
	ColorFooterWarn = ColorSeverityMedium
	ColorFooterError = ColorSeverityCritical

	// Rafraîchir les styles qui dépendent des couleurs
	RefreshStyles()
}

// CurrentThemeName stocke le nom du thème actuellement appliqué
var CurrentThemeName = "default"
