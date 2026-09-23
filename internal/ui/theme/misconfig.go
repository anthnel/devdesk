package theme

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// MisconfigState is what a scan can say about misconfigurations, and like
// SecretsState it has three values rather than two: "nothing found" and "nobody
// looked" are not the same claim, and the Misconfiguration category can be cut
// by the option, by a missing tool, or fail.
//
// It is a **count** rather than the verdict glyph secrets get, and the
// difference is what the number means. One secret is already an alarm, so a
// glyph says everything there is to say; a misconfiguration is not — almost any
// repository carrying a Dockerfile has some — so a glyph would render "found"
// on one finding and on two hundred alike.
//
// The rendering lives here because three views display it — workspaces,
// oci/images and the security inventory — and there is one iconography. The
// state is what decides the colour, never the rendered string.
type MisconfigState int

const (
	// MisconfigNever is a target no misconfiguration stage has read.
	MisconfigNever MisconfigState = iota
	// MisconfigClean is a target a stage read and found nothing in.
	MisconfigClean
	// MisconfigFound is a target with at least one misconfiguration.
	MisconfigFound
)

// MisconfigVerdict maps a cached summary onto the three states.
//
// `has` is whether the cache holds a summary at all — the pointer's nil-ness,
// unwrapped by the caller so this package keeps taking plain values (see
// scan.MisconfigSummary). `scanned` is the target's own state: one that was
// never scanned has no verdict, whatever an entry might carry.
func MisconfigVerdict(has, scanned bool, count int) MisconfigState {
	if !scanned || !has {
		return MisconfigNever
	}
	if count > 0 {
		return MisconfigFound
	}
	return MisconfigClean
}

// MisconfigCell is what the cell prints. Plain text, no ANSI sequence: it is
// MisconfigStyle that colours it, and the reverse would violate Rule 122.
//
// The "?" suffix is the same glyph the CI column uses, for the same meaning:
// this did not conclude. `partial` is a count that does not cover the whole
// target — Helm charts and Kustomize overlays no stage could render, or, on a
// directory row, sub-repositories nobody has scanned. It matters most on "0?",
// where a bare "0" would report a repository of charts as clean when nothing in
// it was ever read.
func MisconfigCell(state MisconfigState, count int, partial bool) string {
	if state == MisconfigNever {
		return "-"
	}
	cell := strconv.Itoa(count)
	if partial {
		cell += "?"
	}
	return cell
}

// MisconfigStyle colours the count by the worst severity among the findings.
//
// Only a count that found something is coloured, exactly as the four severity
// columns are: a clean target showing a coloured zero reads as a problem, which
// is the opposite of what the colour is for. A partial count with nothing in it
// stays dim too — the "?" is what carries that, and colouring it would claim a
// severity for findings nobody has.
func MisconfigStyle(state MisconfigState, worst string) lipgloss.Style {
	if state != MisconfigFound {
		return DimStyle
	}
	return SeverityTextStyle(worst)
}
