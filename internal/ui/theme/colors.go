package theme

import "image/color"

// All color variables are populated by ApplyTheme(DefaultTheme()) in init().
// DefaultTheme() in manager.go is the single source of truth for hex values.

// Status colors
var (
	ColorOK    color.Color
	ColorError color.Color
	ColorWarn  color.Color
)

// UI colors (populated by ApplyTheme from DefaultTheme)
var (
	ColorPrimary    color.Color
	ColorSecondary  color.Color
	ColorBorder     color.Color
	ColorText       color.Color
	ColorDim        color.Color
	ColorHighlight  color.Color
	ColorWhite      color.Color
	ColorBlack      color.Color
	ColorBackground color.Color
)

// Component-specific colors (populated by ApplyTheme, fallback to semantic colors)
var (
	ColorTitleFg    color.Color
	ColorAppTitleFg color.Color
	ColorSubTitleFg color.Color

	ColorButtonFg         color.Color
	ColorButtonBg         color.Color
	ColorButtonInactiveFg color.Color
	ColorButtonInactiveBg color.Color

	ColorTabActiveFg   color.Color
	ColorTabActiveBg   color.Color
	ColorTabInactiveFg color.Color
	ColorTabInactiveBg color.Color

	ColorTableHeaderFg color.Color

	// ColorTableLineSelected is the background a datatable's plain "normal"
	// selected row paints per cell (§3.72) — DefaultTableStyles' s.Selected,
	// and render.go's cellStyle/runStyle selected branches (preserveColumnColors).
	// It used to alias ColorSeverityLow directly; a dedicated key lets a theme
	// tune the selection independently of the severity palette, which is what
	// SeverityLow now is — see its own doc below.
	ColorTableLineSelected color.Color

	ColorCmdLineFg         color.Color
	ColorCmdLineBg         color.Color
	ColorCmdLineInactiveFg color.Color

	ColorViewportBorder color.Color

	// ColorChartBg is the surface a chart is drawn on: one shade lighter than
	// the app background, so the frame reserved for the graph is visible.
	ColorChartBg color.Color

	ColorHeaderKey color.Color

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
	// Strong, emphasis and strikethrough are absent, and the absence is the
	// same decision: they are text *attributes*, so they are carried by
	// lipgloss's Bold, Italic and Strikethrough over the ordinary text colour.
	// A bold word is bold in every theme, which a hue is not.
	ColorSyntaxKey     color.Color
	ColorSyntaxString  color.Color
	ColorSyntaxNumber  color.Color
	ColorSyntaxLiteral color.Color
	ColorSyntaxKeyword color.Color
	ColorSyntaxPunct   color.Color
	ColorSyntaxTag     color.Color
	ColorSyntaxAttr    color.Color
	ColorSyntaxComment color.Color
	ColorSyntaxHeading color.Color

	// A search occurrence, in reverse video like a selected row: it is the most
	// urgent thing on the screen while a search is running, so it takes
	// precedence over the syntax class or the log level underneath it.
	ColorSearchMatch   color.Color
	ColorSearchMatchFg color.Color

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
	ColorFooterInfo  color.Color
	ColorFooterWarn  color.Color
	ColorFooterError color.Color

	// Activity colours: the state colours of the first-column activity
	// indicator (DEVDESK-137). Closed aliases assigned in ApplyTheme, no theme
	// file gains a key (D4): Running = ColorSecondary, Neutral = ColorText
	// (never ColorDim), Suspended = ColorFooterWarn, Failed = ColorFooterError.
	ColorActivityRunning   color.Color
	ColorActivityNeutral   color.Color
	ColorActivitySuspended color.Color
	ColorActivityFailed    color.Color

	// ColorUpdateAvailable is the arrow that says a newer image exists (§3.88).
	//
	// An alias assigned in ApplyTheme: no theme file gains a key. It is the
	// structural blue the headers use rather than a severity colour — an update
	// is news, not a finding, and a table already coloured by CVE counts must
	// not read one as another.
	ColorUpdateAvailable color.Color

	// ColorShortcutDisabled is the key of a header shortcut that exists in this
	// mode but does not apply right now (Rule 130).
	//
	// An alias assigned in ApplyTheme like the three above: no theme file gains
	// a key. It aliases ColorDim, which is also the description's colour — so a
	// disabled line reads as one uniform grey, and what separates it from an
	// available one is the key losing its hue and its weight. A theme wanting a
	// third grey declares it here without touching a single caller.
	ColorShortcutDisabled color.Color

	// ColorPaletteBorder is the border and prompt glyph of the command palette
	// overlay. A closed alias of ColorPrimary assigned in ApplyTheme: no theme
	// file gains a key, so a user theme cannot break the 3:1 border contrast
	// unseen.
	ColorPaletteBorder color.Color

	// Icon colours, one per role rather than one per glyph (iconcolors.go).
	//
	// A glyph is a value, not a name: a table keyed on U+F0849 says nothing about
	// what it is for, and the next reader cannot tell a wrong entry from a right
	// one. A role can be argued about, which is the whole point of a palette.
	//
	// They follow the severity block's shape rather than the syntax block's: a
	// theme file may override each of them, because these are the one part of the
	// palette a user is likely to have an opinion about — an icon is the first
	// thing seen on a row.
	ColorIconNamespace   color.Color
	ColorIconRepository  color.Color
	ColorIconVisPublic   color.Color
	ColorIconVisInternal color.Color
	ColorIconVisPrivate  color.Color
	ColorIconDirectory   color.Color
	ColorIconFile        color.Color
	ColorIconImage       color.Color
	ColorIconContainer   color.Color

	// Token-mix bar colours, one per role (tokencolors.go). Overridable from a
	// theme file; a theme without the keys falls back to the structural palette.
	ColorTokenInput      color.Color
	ColorTokenOutput     color.Color
	ColorTokenCacheWrite color.Color
	ColorTokenCacheRead  color.Color

	// Severity colors (background + foreground pairs)
	//
	// ColorSeverityLow's default now equals ColorDim's — the tone the :sec
	// inventory already uses for a "0" count (inventory_table.go, countColumn):
	// a LOW finding is, by the same reasoning that grades a count column,
	// barely worth a glance next to CRITICAL/HIGH/MEDIUM. A theme may still
	// separate the two; the default treats "low severity" and "nothing found"
	// as the same shade of unremarkable.
	ColorSeverityCritical   color.Color
	ColorSeverityCriticalFg color.Color
	ColorSeverityHigh       color.Color
	ColorSeverityHighFg     color.Color
	ColorSeverityMedium     color.Color
	ColorSeverityMediumFg   color.Color
	ColorSeverityLow        color.Color
	ColorSeverityLowFg      color.Color
	ColorSeverityInfo       color.Color
	ColorSeverityInfoFg     color.Color
)

// init populates all color variables from DefaultTheme.
// DefaultTheme() is the single source of truth for color hex values.
func init() {
	ApplyTheme(DefaultTheme())
}
