package help

import (
	"strings"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Section représente une section du contenu d'aide
type Section struct {
	Title string
	Body  string
}

// KeyBinding représente un raccourci clavier documenté
type KeyBinding struct {
	Key         string
	Description string
}

// Content représente le contenu d'aide d'une vue
type Content struct {
	Title       string
	Description string
	KeyBindings []KeyBinding
	Sections    []Section
}

// Provider est l'interface que les vues doivent implémenter pour fournir de l'aide
type Provider interface {
	GetHelpContent() Content
}

// Render produit le texte formaté de l'aide pour affichage dans un viewport
func Render(c Content, width int) string {
	var b strings.Builder

	// Largeur utile (sans padding/bordure de la modale)
	innerWidth := width - 6
	if innerWidth < 40 {
		innerWidth = 40
	}

	// Titre
	b.WriteString(theme.TitleStyle.Width(innerWidth).Render(c.Title))
	b.WriteString("\n\n")

	// Description
	if c.Description != "" {
		b.WriteString(theme.BgWrap(wordWrap(c.Description, innerWidth), innerWidth))
		b.WriteString("\n")
	}

	// Raccourcis clavier
	if len(c.KeyBindings) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Width(innerWidth).Render("Keyboard Shortcuts"))
		b.WriteString("\n\n")
		b.WriteString(renderKeyBindings(c.KeyBindings, innerWidth))
	}

	// Sections supplémentaires
	for _, section := range c.Sections {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Width(innerWidth).Render(section.Title))
		b.WriteString("\n\n")
		b.WriteString(theme.BgWrap(wordWrap(section.Body, innerWidth), innerWidth))
		b.WriteString("\n")
	}

	return b.String()
}

// renderKeyBindings affiche les raccourcis clavier alignés et paddés à targetWidth
func renderKeyBindings(bindings []KeyBinding, targetWidth int) string {
	// Calculer la largeur max des clés
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

// wordWrap coupe le texte aux limites de mots pour respecter la largeur
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
			wordLen := len(word)
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
