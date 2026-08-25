package shortcut

import (
	"strings"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
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
	// Disabled : l'action existe dans ce mode, mais elle ne s'applique pas à
	// l'état courant — la ligne sélectionnée n'est pas la bonne, ou l'outil
	// qu'elle réclame est absent de la machine.
	//
	// L'entrée reste affichée, à sa place, la touche en gris. La masquer ferait
	// bouger toutes les autres à chaque déplacement du curseur, ce qui est
	// précisément ce que cette colonne ne doit pas faire : on la lit du coin de
	// l'œil, et une liste qui se réorganise sous le regard ne se lit plus.
	//
	// Un changement de *mode* reste un changement de liste : un formulaire n'a
	// pas les mêmes touches qu'une table, et les griser afficherait la réunion
	// de tous les modes.
	Disabled bool
}

// Availability dit pourquoi une action ne s'applique pas, ou porte une raison
// vide quand elle s'applique.
//
// Un seul champ, donc le booléen et le motif ne peuvent pas diverger — c'est le
// point : la vue calcule la disponibilité une fois, le header la lit pour
// griser et le handler la lit pour refuser. Deux calculs pour une question sont
// ce que scan.Categorize et Result.SecretVerdict ont eu chacun à défaire.
//
// Le motif ne va jamais dans le header : il n'y a pas la place, et une colonne
// de raisons se lirait moins bien qu'un gris. Il va au footer quand
// l'utilisateur appuie quand même (Rule 128, Warn).
type Availability struct{ Reason string }

// Enabled reports whether the action applies right now.
func (a Availability) Enabled() bool { return a.Reason == "" }

// Unavailable builds a refused state from the reason to show the user.
func Unavailable(reason string) Availability { return Availability{Reason: reason} }

// maxLenKey mesure la plus longue touche, désactivées comprises.
//
// L'alignement ne doit pas dépendre de ce qui est disponible : sinon la colonne
// se décale au moment même où l'on cherche à ce qu'elle ne bouge pas.
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
		// Seule la touche change : la description est déjà en ColorDim, donc le
		// discriminant est la touche, qui perd sa couleur et sa graisse.
		keyStyle := theme.ShortcutKeyStyle
		if shortcut.Disabled {
			keyStyle = theme.ShortcutKeyDisabledStyle
		}
		result = append(result,
			keyStyle.Render(key)+pad+
				theme.ShortcutDescriptionStyle.Render(shortcut.Description))
	}
	return result
}
