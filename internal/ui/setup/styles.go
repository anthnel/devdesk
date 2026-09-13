package setup

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The wizard runs as its own standalone program, before any context — and so
// before any theme — is loaded, and it is meant to sit on whatever terminal
// background the user already has rather than impose one of its own. Every
// shared theme style bakes in .Background(theme.ColorBackground) (Rule 115
// requires that everywhere else), so this file holds local variants with
// that one property dropped. The colors themselves still come from the
// theme package — only the forced background is opted out of, deliberately,
// and only here.
var (
	titleStyle = lipgloss.NewStyle().Foreground(theme.ColorTitleFg).Bold(true)
	focusStyle = lipgloss.NewStyle().Foreground(theme.ColorHighlight).Bold(true)
	dimStyle   = lipgloss.NewStyle().Foreground(theme.ColorDim)
	valueStyle = lipgloss.NewStyle().Foreground(theme.ColorText)
	helpStyle  = lipgloss.NewStyle().Foreground(theme.ColorDim).Italic(true)
	errorStyle = lipgloss.NewStyle().Foreground(theme.ColorFooterError).Bold(true)
	warnStyle  = lipgloss.NewStyle().Foreground(theme.ColorFooterWarn).Bold(true)
	infoStyle  = lipgloss.NewStyle().Foreground(theme.ColorFooterInfo)

	spinnerStyle = lipgloss.NewStyle().Foreground(theme.ColorSecondary)
)

// footerLevelStyle mirrors components.Level.Style() without the background.
func footerLevelStyle(l components.Level) lipgloss.Style {
	switch l {
	case components.LevelError:
		return errorStyle
	case components.LevelWarning:
		return warnStyle
	default:
		return infoStyle
	}
}

// styleTextInput mirrors theme.StyleTextInput, minus the background.
func styleTextInput(ti *textinput.Model) {
	ti.Prompt = ""
	ti.TextStyle = lipgloss.NewStyle().Foreground(theme.ColorText)
	ti.PromptStyle = lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(theme.ColorDim)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	ti.Cursor.TextStyle = lipgloss.NewStyle().Foreground(theme.ColorText)
}
