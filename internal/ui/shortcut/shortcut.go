package shortcut

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// HeaderInfo represents a key-value pair displayed in the header
type HeaderInfo struct {
	Key   string
	Value string
	Style lipgloss.Style // optional, to color the value
}

type Shortcuts []Shortcut

type Shortcut struct {
	Key         string
	Description string
}

func (s Shortcuts) maxLenKey() int {
	max := 0
	for _, shortcut := range s {
		current := utf8.RuneCountInString(shortcut.Key)
		if current > max {
			max = current
		}
	}
	return max
}

func (s Shortcuts) ToStrings() []string {
	maxKeyLen := s.maxLenKey() + 2 // ajoute l'espace des chevrons
	var result []string
	for _, shortcut := range s {
		key := "<" + shortcut.Key + ">"
		padLen := (maxKeyLen + 1) - utf8.RuneCountInString(key)
		pad := theme.AppBackgroundStyle.Render(strings.Repeat(" ", padLen))
		result = append(result,
			theme.ShortcutKeyStyle.Render(key)+pad+
				theme.ShortcutDescriptionStyle.Render(shortcut.Description))
	}
	return result
}
