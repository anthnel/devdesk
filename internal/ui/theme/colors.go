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

	// Syntax colors, for the viewer's highlighted text and tree.
	//
	// Every one of them is an alias of a colour the theme already declares, so
	// no theme file gains a key and all six get a coherent palette for free. A
	// theme that wants to separate them later declares its own, exactly as the
	// severity colours already allow.
	//
	// Log levels are deliberately absent: StatusErrorStyle, StatusWarningStyle
	// and DimStyle already mean error, warn and debug, and a third name for one
	// colour is how a palette stops being one.
	ColorSyntaxKey     lipgloss.Color
	ColorSyntaxString  lipgloss.Color
	ColorSyntaxNumber  lipgloss.Color
	ColorSyntaxLiteral lipgloss.Color
	ColorSyntaxPunct   lipgloss.Color
	ColorSyntaxTag     lipgloss.Color
	ColorSyntaxAttr    lipgloss.Color
	ColorSyntaxComment lipgloss.Color

	// Footer message colors, one per level (Rule 128).
	//
	// Aliases assigned in ApplyTheme, like the syntax colours above: no theme
	// file gains a key, and the three follow whatever palette is applied.
	//
	// They point at the **severity** colours rather than at ColorError/ColorWarn.
	// The default theme makes each pair identical, so the choice is invisible
	// today; it stops being invisible in a theme that separates them, and the
	// intent is that a footer error reads like a CRITICAL finding and a warning
	// like a MEDIUM one — same alphabet everywhere in the application.
	//
	// Info is deliberately ColorText, the application's ordinary text colour:
	// ColorHighlight carried it before and is a yellow one notch from the
	// warning's orange, which made the two levels indistinguishable.
	ColorFooterInfo  lipgloss.Color
	ColorFooterWarn  lipgloss.Color
	ColorFooterError lipgloss.Color

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
