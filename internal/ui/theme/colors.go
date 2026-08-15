package theme

import "github.com/charmbracelet/lipgloss"

// All color variables are populated by ApplyTheme(DefaultTheme()) in init().
// DefaultTheme() in manager.go is the single source of truth for hex values.

// Status colors
var (
	ColorOK    lipgloss.Color
	ColorError lipgloss.Color
	ColorWarn  lipgloss.Color
)

// UI colors (populated by ApplyTheme from DefaultTheme)
var (
	ColorPrimary    lipgloss.Color
	ColorSecondary  lipgloss.Color
	ColorBorder     lipgloss.Color
	ColorText       lipgloss.Color
	ColorDim        lipgloss.Color
	ColorHighlight  lipgloss.Color
	ColorWhite      lipgloss.Color
	ColorBlack      lipgloss.Color
	ColorBackground lipgloss.Color
)

// Component-specific colors (populated by ApplyTheme, fallback to semantic colors)
var (
	ColorTitleFg    lipgloss.Color
	ColorAppTitleFg lipgloss.Color
	ColorSubTitleFg lipgloss.Color

	ColorButtonFg         lipgloss.Color
	ColorButtonBg         lipgloss.Color
	ColorButtonInactiveFg lipgloss.Color
	ColorButtonInactiveBg lipgloss.Color

	ColorTabActiveFg   lipgloss.Color
	ColorTabActiveBg   lipgloss.Color
	ColorTabInactiveFg lipgloss.Color
	ColorTabInactiveBg lipgloss.Color

	ColorTableHeaderFg   lipgloss.Color
	ColorTableSelectedFg lipgloss.Color
	ColorTableSelectedBg lipgloss.Color

	ColorCmdLineFg         lipgloss.Color
	ColorCmdLineBg         lipgloss.Color
	ColorCmdLineInactiveFg lipgloss.Color

	ColorViewportBorder lipgloss.Color

	// ColorChartBg is the surface a chart is drawn on: un cran plus clair que
	// le fond de l'application, pour que le cadre réservé au graphe se voie.
	ColorChartBg lipgloss.Color

	ColorHeaderKey lipgloss.Color

	// Severity colors (background + foreground pairs)
	ColorSeverityCritical   lipgloss.Color
	ColorSeverityCriticalFg lipgloss.Color
	ColorSeverityHigh       lipgloss.Color
	ColorSeverityHighFg     lipgloss.Color
	ColorSeverityMedium     lipgloss.Color
	ColorSeverityMediumFg   lipgloss.Color
	ColorSeverityLow        lipgloss.Color
	ColorSeverityLowFg      lipgloss.Color
	ColorSeverityInfo       lipgloss.Color
	ColorSeverityInfoFg     lipgloss.Color
)

// init populates all color variables from DefaultTheme.
// DefaultTheme() is the single source of truth for color hex values.
func init() {
	ApplyTheme(DefaultTheme())
}
