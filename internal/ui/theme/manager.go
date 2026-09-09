package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme represents a color theme for the application
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
	// TableLineSelected is the background a datatable's plain "normal"
	// selected row paints per cell (§3.72) — see ColorTableLineSelected.
	TableLineSelected string `json:"table_line_selected,omitempty"`

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

	// Icon colours (optional, fall back to the semantic palette)
	IconNamespace   string `json:"icon_namespace,omitempty"`
	IconRepository  string `json:"icon_repository,omitempty"`
	IconVisPublic   string `json:"icon_vis_public,omitempty"`
	IconVisInternal string `json:"icon_vis_internal,omitempty"`
	IconVisPrivate  string `json:"icon_vis_private,omitempty"`
	IconDirectory   string `json:"icon_directory,omitempty"`
	IconFile        string `json:"icon_file,omitempty"`
	IconImage       string `json:"icon_image,omitempty"`
}

// DefaultTheme returns the default theme (Catppuccin Mocha)
func DefaultTheme() *Theme {
	return &Theme{
		Name:             "default",
		ColorOK:          "#a6e3a1",
		ColorError:       "#f38ba8",
		ColorWarn:        "#fab387",
		ColorPrimary:     "#cba6f7",
		ColorSecondary:   "#b4befe",
		ColorBorder:      "#6c7086",
		ColorText:        "#cdd6f4",
		ColorDim:         "#585b70",
		ColorHighlight:   "#f9e2af",
		ColorWhite:       "#cdd6f4",
		ColorBlack:       "#1e1e2e",
		ColorBackground:  "#1e1e2e",
		TitleFg:          "#cba6f7",
		AppTitleFg:       "#cba6f7",
		SubTitleFg:       "#b4befe",
		ButtonFg:         "#1e1e2e",
		ButtonBg:         "#cba6f7",
		ButtonInactiveFg: "#cdd6f4",
		ButtonInactiveBg: "#585b70",
		TabActiveFg:      "#1e1e2e",
		TabActiveBg:      "#b4befe",
		TabInactiveFg:    "#585b70",
		TabInactiveBg:    "#313244",
		TableHeaderFg:    "#b4befe",
		TableSelectedFg:  "#1e1e2e",
		TableSelectedBg:  "#b4befe",
		// Same value the "normal" selection background used to get by
		// aliasing SeverityLow directly, before SeverityLow itself moved to
		// ColorDim's tone below — kept here so the selection's look does not
		// change with it.
		TableLineSelected:  "#313244",
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
		// Same tone as ColorDim — the :sec inventory's colour for a "0" count
		// (inventory_table.go, countColumn). A LOW finding reads as barely
		// more remarkable than nothing found.
		SeverityLow:    "#585b70",
		SeverityLowFg:  "#cdd6f4",
		SeverityInfo:   "#45475a",
		SeverityInfoFg: "#cdd6f4",
	}
}

// ThemeDir returns the themes directory (~/.devdesk/themes/)
func ThemeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".devdesk", "themes"), nil
}

// LoadTheme loads a theme from a JSON file.
// If name is empty, "dark" or "default", returns the built-in default theme
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

	// Ensure the name is set
	if t.Name == "" {
		t.Name = name
	}

	return &t, nil
}

// ListThemes returns the list of available themes.
// Always includes "default" (built-in theme) + the JSON files in the themes directory
func ListThemes() ([]string, error) {
	themes := []string{"default"}

	dir, err := ThemeDir()
	if err != nil {
		return themes, nil // Return at least "dark"
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return themes, nil // Directory doesn't exist yet
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if themeName, ok := strings.CutSuffix(name, ".json"); ok {
			// Avoid duplicating the built-in theme
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

// ApplyTheme applies a theme by updating the global color variables
// then refreshing all styles
func ApplyTheme(t *Theme) {
	// Update the global semantic colors
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

	// Apply per-component colors (falling back to semantic colors)
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

	// Falls back to ColorCmdLineBg, not to ColorSeverityLow: assigned after
	// ColorCmdLineBg above rather than beside the other Table* colors, since
	// a fallback read before ColorCmdLineBg's own assignment this pass would
	// see last theme's value instead of this one's — the same ordering
	// ColorChartBg below depends on. Any theme file predating this key omits
	// it, so this fallback is what most installs actually render with.
	ColorTableLineSelected = applyColor(t.TableLineSelected, ColorCmdLineBg)

	// A chart's background is that of the command line: it's the surface
	// "one shade lighter" that every theme already defines, so a chart
	// stands out from the rest of its box without any theme having to
	// declare one more color.
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

	// A keyword shares the literal's colour today, and that is deliberate
	// rather than an oversight: `if` and `true` are both words of the language,
	// so the pairing is defensible until a theme decides otherwise — at which
	// point it has a name to decide about. It is the argument already made for
	// ColorFooterError against ColorError.
	ColorSyntaxKeyword = ColorPrimary

	// A heading takes the key colour, and its weight comes from the style
	// rather than from here.
	ColorSyntaxHeading = ColorSecondary

	ColorSearchMatch = ColorHighlight
	ColorSearchMatchFg = ColorBlack

	// Severity colors (background + foreground pairs)
	ColorSeverityCritical = applyColor(t.SeverityCritical, ColorError)
	ColorSeverityCriticalFg = applyColor(t.SeverityCriticalFg, ColorBlack)
	ColorSeverityHigh = applyColor(t.SeverityHigh, ColorSeverityHigh)
	ColorSeverityHighFg = applyColor(t.SeverityHighFg, ColorBlack)
	ColorSeverityMedium = applyColor(t.SeverityMedium, ColorWarn)
	ColorSeverityMediumFg = applyColor(t.SeverityMediumFg, ColorBlack)
	// Falls back to ColorDim rather than ColorCmdLineBg: a theme predating
	// this recolouring that still sets severity_low explicitly keeps its own
	// choice, but one that leaves it unset gets the new intent — "low
	// severity" reading as unremarkable as a "0" count — rather than the old
	// near-background tone by coincidence.
	ColorSeverityLow = applyColor(t.SeverityLow, ColorDim)
	ColorSeverityLowFg = applyColor(t.SeverityLowFg, ColorText)
	ColorSeverityInfo = applyColor(t.SeverityInfo, ColorSeverityInfo)
	ColorSeverityInfoFg = applyColor(t.SeverityInfoFg, ColorText)

	// Footer message colours (Rule 128). Assigned after the severity block
	// because they alias it — the intent is that a footer error reads like a
	// CRITICAL finding and a warning like a MEDIUM one.
	ColorFooterInfo = ColorText
	ColorFooterWarn = ColorSeverityMedium
	ColorFooterError = ColorSeverityCritical

	// Icon colours (iconcolors.go). Assigned here like every other alias, and
	// through applyColor so a theme file can name its own.
	//
	// The defaults carry an argument each. A namespace takes the structural
	// colour the headers and tabs already use, and a repository the leaf colour
	// beside it. Public is green because it is the state worth spotting without
	// reading; internal is the warning hue because it is restricted without being
	// closed; and private is **dim** because it is the majority — a colour every
	// row carries informs of nothing (Rule 122), which is the same argument the
	// severity counters make for their zeroes.
	ColorIconNamespace = applyColor(t.IconNamespace, ColorSecondary)
	ColorIconRepository = applyColor(t.IconRepository, ColorPrimary)
	ColorIconVisPublic = applyColor(t.IconVisPublic, ColorOK)
	ColorIconVisInternal = applyColor(t.IconVisInternal, ColorWarn)
	ColorIconVisPrivate = applyColor(t.IconVisPrivate, ColorDim)

	// The workspaces and :sec listings. A directory takes the namespace's
	// colour and the argument is the same shape: both are the thing that
	// *holds* repositories, on a forge and on a disk. They are two roles rather
	// than one alias so a theme can separate them; the default says they are
	// the same idea.
	ColorIconDirectory = applyColor(t.IconDirectory, ColorSecondary)

	// A loose file in a workspaces listing is the row nothing applies to — W, S
	// and F are all greyed for it (Rule 130, availability.go). Dim is what the
	// shortcut column already says about that row, said once more.
	ColorIconFile = applyColor(t.IconFile, ColorDim)

	// An image takes the highlight rather than a third purple: Primary and
	// Secondary are a mauve and a lavender one notch apart, and the :sec
	// inventory's first column has exactly two values — the one place where the
	// two would sit on adjacent rows with nothing else to separate them.
	ColorIconImage = applyColor(t.IconImage, ColorHighlight)

	// A disabled shortcut's key (Rule 130), same kind of alias.
	ColorShortcutDisabled = ColorDim

	// Refresh the styles that depend on the colors
	RefreshStyles()
}

// CurrentThemeName stores the name of the currently applied theme
var CurrentThemeName = "default"
