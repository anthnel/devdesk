// Package sigcol is the Sig column (§3.82): an image's signature verdict as a
// glyph, the same in every table that shows one — the Remediation tab and the
// OCI view's Images tab.
package sigcol

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/trust"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Title is the column's header.
const Title = "Sig"

// State is what one row's cell shows.
type State struct {
	Result trust.Result
	// Known is a verdict that came back; Verifying one still being asked.
	Known     bool
	Verifying bool
	// LocalBuild is an image with no registry digest — built or loaded here —
	// so there is no published signature to ask about.
	LocalBuild bool
}

// Cell is the glyph, plain text (Rule 122).
func Cell(s State) string {
	switch {
	case s.Verifying:
		return theme.IconHourglass
	case s.LocalBuild:
		return theme.IconHammer
	case !s.Known:
		return "-"
	}
	switch s.Result.Decision {
	case trust.Block:
		return theme.IconError
	case trust.Warn:
		if s.Result.Verdict == trust.Failed {
			return theme.IconHelpCircle
		}
		return theme.IconWarning
	}
	if s.Result.Verdict == trust.Verified {
		return theme.IconOK
	}
	return "-"
}

// Style colours by the decision. Green on Verified is Rule 122's fourth
// declared exception: a rare, established guarantee whose failure is red —
// unlike the Update column's "up to date", the resting state, which stays dim.
func Style(s State) lipgloss.Style {
	if !s.Known || s.Verifying {
		return theme.DimStyle
	}
	switch s.Result.Decision {
	case trust.Block:
		return theme.StatusErrorStyle
	case trust.Warn:
		return theme.StatusWarningStyle
	}
	if s.Result.Verdict == trust.Verified {
		return theme.StatusOKStyle
	}
	return theme.DimStyle
}

// Column is the Sig column: a glyph, optional — the first to give way on a
// narrow terminal — with no sort and no search (Rule 125: nothing to type).
func Column[T any](get func(T) State) datatable.Column[T] {
	return datatable.Column[T]{
		Title: Title, Sizing: datatable.SizingFixed, MinWidth: len(Title), Optional: true,
		Cell:  func(r T) string { return Cell(get(r)) },
		Style: func(r T) lipgloss.Style { return Style(get(r)) },
	}
}
