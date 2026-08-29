package theme

import "github.com/charmbracelet/lipgloss"

// IconRole is what a glyph *means*, which is the only thing a palette can have
// an opinion about.
//
// The alternative was a table keyed on the glyph itself. It was rejected for a
// reason worth writing down: a codepoint is a value, not a name, so
// `U+F0849 -> ColorSecondary` cannot be read in review — nothing on the line
// says whether the entry is right, and a wrong one is indistinguishable from a
// deliberate one. A role is a word, so it can be argued with.
//
// It is the shape ForgeIcon, SeverityTextStyle and CIScoreStyle already have:
// the caller names a meaning, the theme answers with a colour.
type IconRole string

const (
	// The explorer's two node kinds. They are named for the model rather than
	// for either forge's wording — a GitLab group and a GitHub organisation are
	// one role, which is exactly what forge.Vocabulary exists to say.
	IconRoleNamespace  IconRole = "namespace"
	IconRoleRepository IconRole = "repository"

	// The three visibilities. GitHub has no internal, so the role simply never
	// resolves there; a role with no rows is cheaper than a second table.
	IconRoleVisPublic   IconRole = "vis-public"
	IconRoleVisInternal IconRole = "vis-internal"
	IconRoleVisPrivate  IconRole = "vis-private"
)

// IconColor is the colour a role is painted in.
//
// An unrecognised role falls back to the ordinary text colour rather than to
// the zero Color: a zero lipgloss.Color is the empty string, which renders as
// "no colour set" and inherits whatever the terminal last emitted — the exact
// bleed Rule 122 is about. A role nobody declared should look like plain text,
// not like the row above it.
func IconColor(role IconRole) lipgloss.Color {
	switch role {
	case IconRoleNamespace:
		return ColorIconNamespace
	case IconRoleRepository:
		return ColorIconRepository
	case IconRoleVisPublic:
		return ColorIconVisPublic
	case IconRoleVisInternal:
		return ColorIconVisInternal
	case IconRoleVisPrivate:
		return ColorIconVisPrivate
	}
	return ColorText
}

// IconStyle is what a datatable column's Style function returns for an icon
// cell. Foreground only: datatable gives every unselected cell the application
// background itself, so naming one here would be a second answer to a question
// already settled (Rule 122).
func IconStyle(role IconRole) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(IconColor(role))
}
