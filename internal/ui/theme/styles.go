package theme

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// Layout styles
var (
	HeaderStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Bold(false)

	HeaderKeyStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorHeaderKey).
			Bold(true)

	HeaderValueStyle = lipgloss.NewStyle().
				Background(ColorBackground).
				Foreground(ColorPrimary).
				Bold(true)

	CommandLineStyle = lipgloss.NewStyle().
				Background(ColorCmdLineBg).
				Foreground(ColorCmdLineFg).
				Padding(0, 1, 0, 1)

	CommandLineInactiveStyle = lipgloss.NewStyle().
					Background(ColorBackground).
					Foreground(ColorCmdLineInactiveFg).
					Padding(0, 1, 0, 1)

	ShortcutKeyStyle = lipgloss.NewStyle().
				Background(ColorBackground).
				Foreground(ColorSecondary).
				Bold(true)

	// ShortcutKeyDisabledStyle : la touche d'un raccourci affiché mais sans
	// objet ici (Rule 130). Elle perd la couleur *et* la graisse — l'une des
	// deux seule laisserait un gris gras, qui se lit comme une emphase.
	ShortcutKeyDisabledStyle = lipgloss.NewStyle().
					Background(ColorBackground).
					Foreground(ColorShortcutDisabled).
					Bold(false)

	ShortcutDescriptionStyle = lipgloss.NewStyle().
					Background(ColorBackground).
					Foreground(ColorDim).
					Bold(false)

	TitleStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorTitleFg).
			Bold(true)

	// View subtitle style
	SubTitleStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorSubTitleFg).
			Bold(true)
)

// Status styles
var (
	StatusOKStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorOK).
			Bold(true)

	StatusDownStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorError).
			Bold(true)

	StatusErrorStyle = lipgloss.NewStyle().
				Background(ColorBackground).
				Foreground(ColorError).
				Bold(true)

	StatusWarningStyle = lipgloss.NewStyle().
				Background(ColorBackground).
				Foreground(ColorWarn).
				Bold(true)
)

// Footer message styles, one per level (Rule 128).
//
// Info is not bold, the other two are: the hierarchy runs through weight as
// well as hue, and a neutral notice rendered bold would read as an alert again
// — which is what the yellow ColorHighlight line did before.
//
// Nothing outside components.FooterMessage should use these. They exist as
// exported styles so a theme change refreshes them like every other style, not
// so a view can render its own footer line.
var (
	FooterInfoStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorFooterInfo).
			Bold(false)

	FooterWarnStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorFooterWarn).
			Bold(true)

	FooterErrorStyle = lipgloss.NewStyle().
				Background(ColorBackground).
				Foreground(ColorFooterError).
				Bold(true)
)

// Component styles
var (
	KeyStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorHighlight).
			Bold(true)

	PausedStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorError).
			Bold(true)

	DimStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorDim).
			Bold(false)

	HelpStyle = lipgloss.NewStyle().
			Background(ColorBackground).
			Foreground(ColorDim).
			Italic(true)
)

var (
	PrimaryColorStyle = lipgloss.NewStyle().
				Background(ColorBackground).
				Foreground(ColorPrimary)

	// AppBackgroundStyle applique le fond global de l'application
	AppBackgroundStyle = lipgloss.NewStyle().
				Background(ColorBackground)
)

// Background helper functions — use these everywhere to avoid naked spaces (Rule 115)

// Bg renders plain text with the app background color
func Bg(s string) string {
	return AppBackgroundStyle.Render(s)
}

// PadWithBg pads content to targetWidth using the app background color
func PadWithBg(content string, targetWidth int) string {
	currentWidth := lipgloss.Width(content)
	if currentWidth >= targetWidth {
		return content
	}
	return content + AppBackgroundStyle.Render(strings.Repeat(" ", targetWidth-currentWidth))
}

// BgLine renders text with background and pads to targetWidth
func BgLine(s string, targetWidth int) string {
	styled := Bg(s)
	return PadWithBg(styled, targetWidth)
}

// BgWrap applies background to each line of a multi-line string and pads to targetWidth
func BgWrap(s string, targetWidth int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = BgLine(line, targetWidth)
		}
	}
	return strings.Join(lines, "\n")
}

// EmptyLineBg returns an empty line filled with background to targetWidth
func EmptyLineBg(targetWidth int) string {
	return AppBackgroundStyle.Render(strings.Repeat(" ", targetWidth))
}

// OverlayBoxStyle returns the standard style for overlay modals (help, context list, theme list)
func OverlayBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorViewportBorder).
		BorderBackground(ColorBackground).
		Padding(1, 2).
		Background(ColorBackground)
}

// StyleTextInput applies the app background to a bubbles textinput
func StyleTextInput(ti *textinput.Model) {
	bgStyle := lipgloss.NewStyle().Background(ColorBackground)
	ti.Prompt = "" // Label and separator are handled externally (Rule 120)
	ti.TextStyle = bgStyle.Foreground(ColorText)
	ti.PromptStyle = bgStyle.Foreground(ColorHighlight)
	ti.PlaceholderStyle = bgStyle.Foreground(ColorDim)
	ti.Cursor.Style = bgStyle.Foreground(ColorHighlight)
	ti.Cursor.TextStyle = bgStyle.Foreground(ColorText)
}

// SpinnerStyle returns the standard spinner style (secondary color on theme background)
func SpinnerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(ColorSecondary).
		Background(ColorBackground)
}

// SpinnerMessage renders a spinner + text with consistent styling (spinner in secondary, text in dim)
func SpinnerMessage(spinnerView, text string) string {
	return SpinnerStyle().Render(spinnerView) + Bg(" ") + DimStyle.Render(text)
}

// Helper functions for dynamic styles

// ResponseTimeStyle retourne le style selon le temps de réponse
func ResponseTimeStyle(ms int64) lipgloss.Style {
	base := lipgloss.NewStyle().Background(ColorBackground).Bold(false)
	if ms < 100 {
		return base.Foreground(ColorOK).SetString(IconOK)
	} else if ms < 500 {
		return base.Foreground(ColorWarn).SetString(IconWarning)
	}
	return base.Foreground(ColorError).SetString(IconError)
}

// StatusStyle retourne le style selon le type de status
func StatusStyle(status string) lipgloss.Style {
	switch status {
	case "OK":
		return StatusOKStyle
	case "DOWN":
		return StatusDownStyle
	case "ERROR":
		return StatusErrorStyle
	case "WARNING":
		return StatusWarningStyle
	default:
		return lipgloss.NewStyle().Background(ColorBackground)
	}
}

// DefaultTableStyles retourne les styles par défaut pour toutes les tables de l'application
func DefaultTableStyles() table.Styles {
	s := table.DefaultStyles()

	s.Header = s.Header.
		Background(ColorBackground).
		Foreground(ColorTableHeaderFg).
		Bold(true)

	s.Selected = s.Selected.
		Foreground(ColorTableSelectedFg).
		Background(ColorTableSelectedBg).
		Bold(true)

	return s
}

// TableStylesForState returns table styles with selection color based on row state.
// Use "error" for exited/dead containers, "normal" for default selection.
func TableStylesForState(state string) table.Styles {
	s := DefaultTableStyles()
	switch state {
	case "error":
		s.Selected = s.Selected.
			Background(ColorError).
			Foreground(ColorBlack)
	case "busy":
		// A row an action is running on. It outranks "error" rather than the
		// other way round: `exited` is precisely what is about to stop being
		// true, so the transition is the newer fact.
		//
		// ColorHighlight is not an arbitrary pick — the containers view already
		// paints docker's own transitional states (`created`, `restarting`) with
		// it, and a transition DevDesk started is the same kind of thing.
		s.Selected = s.Selected.
			Background(ColorHighlight).
			Foreground(ColorBlack)
	default:
		// normal — uses DefaultTableStyles values (ColorSecondary bg, ColorBlack fg)
	}
	return s
}

// SeverityTextStyle returns the text style for a CVE severity, drawn from the
// same palette TableStylesForSeverity uses for the selected row. Callers pass
// the severity uppercased, as the scanners report it.
//
// Composing this by hand is what let the security details view render CRITICAL
// and HIGH identically (D11 in the backlog): ColorError + Bold happens to equal
// StatusErrorStyle, so the two collapsed.
func SeverityTextStyle(severity string) lipgloss.Style {
	base := lipgloss.NewStyle().Background(ColorBackground)
	switch strings.ToUpper(severity) {
	case "CRITICAL":
		return base.Foreground(ColorSeverityCritical).Bold(true)
	case "HIGH":
		return base.Foreground(ColorSeverityHigh).Bold(true)
	case "MEDIUM":
		return base.Foreground(ColorSeverityMedium)
	case "LOW":
		return base.Foreground(ColorSeverityLow)
	default:
		return base.Foreground(ColorSeverityInfo)
	}
}

// TableStylesForSeverity returns table styles with selection color based on CVE severity.
func TableStylesForSeverity(severity string) table.Styles {
	s := DefaultTableStyles()
	switch severity {
	case "CRITICAL":
		s.Selected = s.Selected.
			Background(ColorSeverityCritical).
			Foreground(ColorSeverityCriticalFg)
	case "HIGH":
		s.Selected = s.Selected.
			Background(ColorSeverityHigh).
			Foreground(ColorSeverityHighFg)
	case "MEDIUM":
		s.Selected = s.Selected.
			Background(ColorSeverityMedium).
			Foreground(ColorSeverityMediumFg)
	case "LOW":
		s.Selected = s.Selected.
			Background(ColorSeverityLow).
			Foreground(ColorSeverityLowFg)
	case "UNKNOWN", "INFO":
		s.Selected = s.Selected.
			Background(ColorSeverityInfo).
			Foreground(ColorSeverityInfoFg)
	default:
		// normal — uses DefaultTableStyles values
	}
	return s
}

// BlurredTableStyles retourne les styles pour une table qui n'a pas le focus
func BlurredTableStyles() table.Styles {
	s := table.DefaultStyles()

	s.Header = s.Header.
		Background(ColorBackground).
		Foreground(ColorTableHeaderFg).
		Bold(true)

	s.Selected = s.Selected.
		Background(ColorBackground).
		Foreground(ColorText).
		Bold(false)

	return s
}

// Button styles

// ButtonStyle retourne un style de bouton selon l'état (focus ou non)
func ButtonStyle(focused bool, variant string) lipgloss.Style {
	style := lipgloss.NewStyle().
		Padding(0, 2)

	if focused {
		switch variant {
		case "primary":
			return style.Bold(true).Background(ColorButtonBg).Foreground(ColorButtonFg)
		case "danger":
			return style.Bold(true).Background(ColorError).Foreground(ColorButtonFg)
		case "success":
			return style.Bold(true).Background(ColorOK).Foreground(ColorBlack)
		default:
			return style.Bold(true).Background(ColorButtonBg).Foreground(ColorButtonFg)
		}
	}

	switch variant {
	case "primary":
		return style.Background(ColorButtonInactiveBg).Foreground(ColorButtonInactiveFg)
	case "danger":
		return style.Background(ColorButtonInactiveBg).Foreground(ColorError)
	case "success":
		return style.Background(ColorButtonInactiveBg).Foreground(ColorOK)
	case "secondary":
		return style.Background(ColorButtonInactiveBg).Foreground(ColorButtonInactiveFg)
	default:
		return style.Background(ColorButtonInactiveBg).Foreground(ColorPrimary)
	}
}

// RenderButton retourne le rendu d'un bouton avec le texte donné
func RenderButton(text string, focused bool, variant string) string {
	return ButtonStyle(focused, variant).Render(text)
}

// RenderCheckbox retourne le rendu d'une checkbox
func RenderCheckbox(checked bool, label string, focused bool) string {
	box := IconCheckbox
	if checked {
		box = IconChecked
	}
	text := box + " " + label
	if focused {
		return lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorHighlight).Bold(true).Render(IconCircleSmall + " " + text)
	}
	return lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorText).Render("  " + text)
}

// CheckState is the state of a checkbox that stands for a set: every member
// selected, none, or some.
type CheckState int

const (
	CheckNone CheckState = iota
	CheckAll
	CheckSome
)

// RenderCheckboxTri renders a checkbox covering a set of items. `Some` is a
// distinct mark rather than an empty box: a group whose members are half
// selected is not the same statement as one with none, and rendering them alike
// is how a user unchecks something they did not mean to.
func RenderCheckboxTri(state CheckState, label string, focused bool) string {
	box := IconCheckbox
	switch state {
	case CheckAll:
		box = IconChecked
	case CheckSome:
		box = IconCheckboxIndeterminate
	case CheckNone:
	}
	text := box + " " + label
	if focused {
		return lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorHighlight).Bold(true).Render(IconCircleSmall + " " + text)
	}
	return lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorText).Render("  " + text)
}

// RenderCheckboxDisabled renders a locked (non-interactive) checkbox in a dimmed style.
// Use this for options that are unavailable due to the current configuration (e.g. incompatible with Trivy server mode).
func RenderCheckboxDisabled(label string) string {
	text := IconCheckbox + " " + label + " 󰌾"
	return lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorDim).Render("  " + text)
}

// Il n'y a plus de RenderRadioButton. Ses deux seuls appelants étaient le choix
// de destination du token dans la vue d'authentification, et ce choix a disparu
// avec le passage au gestionnaire de secrets de l'hôte (§3.9). Pour un ensemble
// fermé de valeurs, le contrôle est le champ à cycle ←→ (Rule 132) ;
// RenderCheckbox reste pour les booléens indépendants.

// Tab styles — exported so views can use them directly (Rule 123)
var (
	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTabActiveFg).
			Background(ColorTabActiveBg)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(ColorTabInactiveFg).
				Background(ColorTabInactiveBg)
)

// TabItem représente un onglet avec son label
type TabItem struct {
	Label string
}

// RenderTabs génère une barre de tabs uniforme
func RenderTabs(tabs []TabItem, activeIdx int) string {
	var result string
	for i, t := range tabs {
		label := " " + t.Label + " "
		if i > 0 {
			result += Bg(" ")
		}
		if i == activeIdx {
			result += ActiveTabStyle.Render(label)
		} else {
			result += InactiveTabStyle.Render(label)
		}
	}

	return result
}

// RenderBorderTitle construit une ligne de bordure supérieure avec un titre incrusté.
// Format: ┌─ Title ───────────────────┐
func RenderBorderTitle(title string, width int) string {
	border := lipgloss.NormalBorder()
	borderStyle := lipgloss.NewStyle().Foreground(ColorViewportBorder).Background(ColorBackground)
	titleStyle := lipgloss.NewStyle().Foreground(ColorTitleFg).Background(ColorBackground).Bold(true)

	left := borderStyle.Render(border.TopLeft + border.Top + " ")
	renderedTitle := titleStyle.Render(title)
	right := borderStyle.Render(" ")

	// "┌─ " = 3 chars, title, " " = 1 char, "┐" = 1 char
	usedWidth := 3 + lipgloss.Width(renderedTitle) + 1
	remainingDashes := width - usedWidth - 1
	if remainingDashes < 0 {
		remainingDashes = 0
	}

	fill := borderStyle.Render(strings.Repeat(border.Top, remainingDashes))
	corner := borderStyle.Render(border.TopRight)

	return left + renderedTitle + right + fill + corner
}

// RefreshStyles reconstruit tous les styles globaux à partir des couleurs actuelles.
// Doit être appelé après chaque changement de thème via ApplyTheme.
func RefreshStyles() {
	HeaderStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorText).
		Bold(false)

	HeaderKeyStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorHeaderKey).
		Bold(true)

	HeaderValueStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorPrimary).
		Bold(true)

	CommandLineStyle = lipgloss.NewStyle().
		Background(ColorCmdLineBg).
		Foreground(ColorCmdLineFg).
		Padding(0, 0, 0, 1)

	CommandLineInactiveStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorCmdLineInactiveFg)

	ShortcutKeyStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorSecondary).
		Bold(true)

	ShortcutKeyDisabledStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorShortcutDisabled).
		Bold(false)

	ShortcutDescriptionStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorDim).
		Bold(false)

	TitleStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorTitleFg).
		Bold(true)

	SubTitleStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorSubTitleFg).
		Bold(true)

	StatusOKStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorOK).
		Bold(true)

	StatusDownStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorError).
		Bold(true)

	StatusErrorStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorError).
		Bold(true)

	StatusWarningStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorWarn).
		Bold(true)

	FooterInfoStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorFooterInfo).
		Bold(false)

	FooterWarnStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorFooterWarn).
		Bold(true)

	FooterErrorStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorFooterError).
		Bold(true)

	KeyStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorHighlight).
		Bold(true)

	PausedStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorError).
		Bold(true)

	DimStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorDim).
		Bold(false)

	HelpStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorDim).
		Italic(true)

	PrimaryColorStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorPrimary)

	AppBackgroundStyle = lipgloss.NewStyle().
		Background(ColorBackground).
		Foreground(ColorText)

	ActiveTabStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTabActiveFg).
		Background(ColorTabActiveBg)

	InactiveTabStyle = lipgloss.NewStyle().
		Foreground(ColorTabInactiveFg).
		Background(ColorTabInactiveBg)
}
