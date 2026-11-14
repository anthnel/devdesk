package netdiag

import (
	"fmt"
	"strconv"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// pathBarLabel prefixes the target field in the bar.
const pathBarLabel = "Target › "

// pathColumns describes the hops table. Plain text in Cell, colour in Style
// (Rule 122). A silent hop is dim, never red: it is not a failure.
func pathColumns() []datatable.Column[nettrace.Hop] {
	hop := datatable.Column[nettrace.Hop]{
		Title: "Hop", Sizing: datatable.SizingFixed, MinWidth: 3,
		Cell: func(h nettrace.Hop) string { return strconv.Itoa(h.TTL) },
	}
	answer := datatable.Column[nettrace.Hop]{
		Title: "Answer", Sizing: datatable.SizingFixed, MinWidth: 11,
		Cell:  func(h nettrace.Hop) string { return h.Kind.String() },
		Style: pathAnswerStyle,
	}
	address := datatable.Column[nettrace.Hop]{
		Title: "Address", Sizing: datatable.SizingContent, MinWidth: 15, Flex: 1,
		Cell: func(h nettrace.Hop) string {
			if !h.Addr.IsValid() {
				return "-"
			}
			return h.Addr.String()
		},
		Style: func(h nettrace.Hop) lipgloss.Style {
			if !h.Addr.IsValid() {
				return theme.DimStyle
			}
			return lipgloss.Style{}
		},
	}
	rtt := datatable.Column[nettrace.Hop]{
		Title: "RTT", Sizing: datatable.SizingFixed, MinWidth: 9,
		Cell: func(h nettrace.Hop) string {
			if h.RTT <= 0 {
				return "-"
			}
			return fmt.Sprintf("%.1f ms", float64(h.RTT)/float64(time.Millisecond))
		},
		Style: func(h nettrace.Hop) lipgloss.Style {
			if h.RTT <= 0 {
				return theme.DimStyle
			}
			return lipgloss.Style{}
		},
	}
	return []datatable.Column[nettrace.Hop]{hop, answer, address, rtt}
}

func pathAnswerStyle(h nettrace.Hop) lipgloss.Style {
	switch h.Kind {
	case nettrace.HopSilent, nettrace.HopUnresolved:
		return theme.DimStyle
	case nettrace.HopError:
		return theme.SeverityTextStyle("MEDIUM")
	}
	return lipgloss.Style{}
}

// view renders the table; the body is never replaced (Rule 139).
func (pm *PathModel) view() string { return pm.table.View() }

// barView is the target field in its frame, two lines in every state.
func (pm *PathModel) barView(width int) string {
	return components.BarFrame(width, theme.KeyStyle.Render(pathBarLabel)+pm.input.View())
}

// statusLine is derived on every frame (Rule 128).
func (pm *PathModel) statusLine() components.Status {
	if pm.errText != "" {
		return components.Status{Text: pm.errText, Level: components.LevelError}
	}
	if pm.state != pathRunning {
		return components.Status{}
	}
	host := pm.target.Host
	n := len(pm.table.Items())
	if !pm.addr.IsValid() && n == 0 {
		return components.Status{Text: "Resolving " + host + "...", Activity: true}
	}
	where := host
	if pm.addr.IsValid() && pm.addr.String() != host {
		where += " (" + pm.addr.String() + ")"
	}
	noun := "hops"
	if n == 1 {
		noun = "hop"
	}
	return components.Status{
		Text:     fmt.Sprintf("Tracing %s — %d %s answered", where, n, noun),
		Activity: true,
	}
}

// headerInfo lists the target and, once a run ended, how it ended.
func (pm *PathModel) headerInfo() []shortcut.HeaderInfo {
	var out []shortcut.HeaderInfo
	if pm.target.Host != "" {
		value := pm.target.String()
		if pm.addr.IsValid() && pm.addr.String() != pm.target.Host {
			value += " (" + pm.addr.String() + ")"
		}
		out = append(out, shortcut.HeaderInfo{Key: "Target", Value: value, Style: theme.HeaderValueStyle})
	}
	if pm.result != nil {
		out = append(out, shortcut.HeaderInfo{Key: "Path", Value: pathOutcomeLabel(*pm.result), Style: theme.HeaderValueStyle})
	}
	return out
}

// shortcuts: the running mode replaces the list, the other states grey out
// (Rule 130).
func (pm *PathModel) shortcuts() shortcut.Shortcuts {
	off := !pm.launchable().Enabled()
	switch pm.state {
	case pathRunning:
		return shortcut.Shortcuts{{Key: "esc", Description: "Cancel the trace"}}
	case pathDone:
		return shortcut.Shortcuts{
			{Key: "tab", Description: "Switch tab"},
			{Key: "enter", Description: "Trace again", Disabled: off},
			{Key: "esc", Description: "Edit the target"},
			{Key: "?", Description: "Help"},
		}
	}
	return shortcut.Shortcuts{
		{Key: "tab", Description: "Switch tab"},
		{Key: "enter", Description: "Trace the path", Disabled: off},
	}
}
