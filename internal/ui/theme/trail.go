package theme

import "github.com/charmbracelet/x/ansi"

// TrailEllipsis marks the head of a breadcrumb trail that was cut to fit its
// line (DEVDESK-769). It is the one-cell U+2026, not Ellipsis ("..."), which
// would cost three cells in a pill that has none to spare.
const TrailEllipsis = "…"

const (
	// trailPillPad is what a pill adds around its label: one space on each side.
	trailPillPad = 2
	// trailGap is the cell between two pills.
	trailGap = 1
	// trailHeadCost is the cost of the TrailEllipsis pill followed by its gap.
	trailHeadCost = 1 + trailPillPad + trailGap
	// trailMinCutLabel is the fewest label cells worth showing beside the head
	// pill: a lone TrailEllipsis label says nothing the head pill does not.
	trailMinCutLabel = 2
)

// pillsWidth is the width RenderTabs draws for labels: width+2 per pill and one
// cell between two pills. It measures the plain text, before any style is
// applied (Rule 122).
func pillsWidth(labels []string) int {
	if len(labels) == 0 {
		return 0
	}
	cost := trailGap * (len(labels) - 1)
	for _, l := range labels {
		cost += ansi.StringWidth(l) + trailPillPad
	}
	return cost
}

// FitTrail returns the labels of a breadcrumb trail that fit in room cells.
//
// The current segment is always the last label. A trail that fits is returned
// as is (the same slice). Otherwise the head is replaced by one TrailEllipsis
// pill and as many of the last labels as fit are kept. When even the current
// segment does not fit beside that pill, it is cut at its end with a trailing
// TrailEllipsis. Below trailMinCutLabel cells of label the head pill is
// dropped, and under 3 cells no pill fits at all (nil). The input is never
// modified.
func FitTrail(labels []string, room int) []string {
	n := len(labels)
	if n == 0 || pillsWidth(labels) <= room {
		return labels
	}
	// Drop the head: the TrailEllipsis pill plus the k last labels.
	for k := n - 1; k >= 1; k-- {
		if trailHeadCost+pillsWidth(labels[n-k:]) <= room {
			return append([]string{TrailEllipsis}, labels[n-k:]...)
		}
	}
	last := labels[n-1]
	// Cut the current segment beside the head pill, only when a head was
	// really dropped and the label keeps enough cells to be worth showing.
	if cut := room - trailHeadCost - trailPillPad; n > 1 && cut >= trailMinCutLabel {
		return []string{TrailEllipsis, ansi.Truncate(last, cut, TrailEllipsis)}
	}
	if cut := room - trailPillPad; cut >= 1 {
		return []string{ansi.Truncate(last, cut, TrailEllipsis)}
	}
	return nil
}

// RenderTrail renders a breadcrumb trail on one line of exactly width cells,
// the current (last) segment highlighted. The line starts with the one-cell
// margin of Rule 123; a trail too long for it is fitted by FitTrail.
func RenderTrail(tabs []TabItem, width int) string {
	if width <= 0 {
		return ""
	}
	labels := make([]string, len(tabs))
	for i, t := range tabs {
		labels[i] = t.Label
	}
	fitted := FitTrail(labels, width-1)
	items := make([]TabItem, len(fitted))
	for i, l := range fitted {
		items[i] = TabItem{Label: l}
	}
	return PadWithBg(Bg(" ")+RenderTabs(items, len(items)-1), width)
}
