package agentusageview

import (
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// breakdownColumn is the token-mix bar: Cell is theme.GaugeFill(breakdownBarWidth)
// (one glyph, plain text, Rule 122) or a dim "-"; Spans paints the row's mix
// with the four theme.TokenStyle roles in bar order. The mix is computed once
// in agentRows, not per cell.
func breakdownColumn() datatable.Column[agentRow] {
	return datatable.Column[agentRow]{
		Title: "Breakdown", Sizing: datatable.SizingFixed, MinWidth: breakdownBarWidth, Optional: true, YieldsToContent: true,
		Cell: func(r agentRow) string {
			if !r.hasMix {
				return noValue
			}
			return theme.GaugeFill(breakdownBarWidth)
		},
		Spans: func(r agentRow) []datatable.Span {
			if !r.hasMix {
				return nil
			}
			roles := theme.TokenRoles()
			spans := make([]datatable.Span, 0, segCount)
			for i, w := range r.mix {
				spans = append(spans, datatable.Span{Width: w, Style: theme.TokenStyle(roles[i])})
			}
			return spans
		},
		Style: func(r agentRow) lipgloss.Style { return dimWhen(!r.hasMix) },
	}
}
