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
	// Disabled: the action exists in this mode, but does not apply to the
	// current state — the selected row is not the right one, or the tool it
	// requires is absent from the machine.
	//
	// The entry stays displayed, in its place, the key grayed out. Hiding it
	// would move every other entry each time the cursor moves, which is
	// exactly what this column must not do: it is read from the corner of
	// the eye, and a list that reorganizes under the gaze can no longer be
	// read.
	//
	// A change of *mode* remains a change of list: a form does not have the
	// same keys as a table, and graying them out would show the union of
	// every mode.
	Disabled bool
}

// Availability says why an action does not apply, or carries an empty
// reason when it does.
//
// A single field, so the boolean and the reason cannot diverge — that is
// the point: the view computes availability once, the header reads it to
// gray out and the handler reads it to refuse. Two computations for one
// question are what scan.Categorize and Result.SecretVerdict each had to
// undo.
//
// The reason never goes into the header: there is no room for it, and a
// column of reasons would read worse than a gray-out. It goes to the footer
// when the user presses the key anyway (Rule 128, Warn).
type Availability struct{ Reason string }

// Enabled reports whether the action applies right now.
func (a Availability) Enabled() bool { return a.Reason == "" }

// Unavailable builds a refused state from the reason to show the user.
func Unavailable(reason string) Availability { return Availability{Reason: reason} }

// maxLenKey measures the longest key, disabled ones included.
//
// The alignment must not depend on what is available: otherwise the column
// shifts at the very moment it is meant to hold still.
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
	maxKeyLen := s.maxLenKey() + 2 // adds room for the angle brackets
	var result []string
	for _, shortcut := range s {
		key := "<" + shortcut.Key + ">"
		padLen := (maxKeyLen + 1) - utf8.RuneCountInString(key)
		pad := theme.AppBackgroundStyle.Render(strings.Repeat(" ", padLen))
		// Only the key changes: the description is already in ColorDim, so the
		// discriminant is the key, which loses its color and its weight.
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
