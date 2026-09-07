package help

import (
	"strings"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Section represents a section of the help content
type Section struct {
	Title string
	Body  string
}

// KeyBinding represents a documented keyboard shortcut
type KeyBinding struct {
	Key         string
	Description string
}

// Content represents a view's help content
type Content struct {
	Title       string
	Description string
	KeyBindings []KeyBinding
	Sections    []Section
}

// Provider is the interface views must implement to provide help
type Provider interface {
	GetHelpContent() Content
}

// Render produces the formatted help text for display in a viewport
func Render(c Content, width int) string {
	var b strings.Builder

	// Usable width (without the modal's padding/border)
	innerWidth := width - 6
	if innerWidth < 40 {
		innerWidth = 40
	}

	// Title
	b.WriteString(theme.TitleStyle.Width(innerWidth).Render(c.Title))
	b.WriteString("\n\n")

	// Description
	if c.Description != "" {
		b.WriteString(theme.BgWrap(wordWrap(c.Description, innerWidth), innerWidth))
		b.WriteString("\n")
	}

	// Keyboard shortcuts
	if len(c.KeyBindings) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Width(innerWidth).Render("Keyboard Shortcuts"))
		b.WriteString("\n\n")
		b.WriteString(renderKeyBindings(c.KeyBindings, innerWidth))
	}

	// Additional sections
	for _, section := range c.Sections {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Width(innerWidth).Render(section.Title))
		b.WriteString("\n\n")
		b.WriteString(theme.BgWrap(wordWrap(section.Body, innerWidth), innerWidth))
		b.WriteString("\n")
	}

	return b.String()
}

// renderKeyBindings renders the keyboard shortcuts aligned and padded to targetWidth
func renderKeyBindings(bindings []KeyBinding, targetWidth int) string {
	// Compute the max width of the keys
	maxKeyLen := 0
	for _, kb := range bindings {
		current := utf8.RuneCountInString(kb.Key)
		if current > maxKeyLen {
			maxKeyLen = current
		}
	}

	var b strings.Builder
	keyStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	descStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)

	for _, kb := range bindings {
		pad := strings.Repeat(" ", maxKeyLen-utf8.RuneCountInString(kb.Key)+2)
		line := theme.Bg("  ") + keyStyle.Render(kb.Key) + theme.Bg(pad) + descStyle.Render(kb.Description)
		line = theme.PadWithBg(line, targetWidth)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// wordWrap breaks the text at word boundaries to respect the width
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			result.WriteString("\n")
			continue
		}

		words := strings.Fields(paragraph)
		if len(words) == 0 {
			result.WriteString("\n")
			continue
		}

		lineLen := 0
		for i, word := range words {
			// Columns, not bytes: len() wraps accented text a rune early.
			wordLen := theme.StringWidth(word)
			if i > 0 && lineLen+1+wordLen > width {
				result.WriteString("\n")
				lineLen = 0
			} else if i > 0 {
				result.WriteString(" ")
				lineLen++
			}
			result.WriteString(word)
			lineLen += wordLen
		}
		result.WriteString("\n")
	}

	return result.String()
}
