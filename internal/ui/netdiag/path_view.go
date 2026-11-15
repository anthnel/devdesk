package netdiag

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// pathBarLabel prefixes the target field in the bar.
const pathBarLabel = "Target › "

// pathColumns describes the Path column. Plain text in Cell, colour in Style
// (Rule 122). A silent hop is dim, never red: it is not a failure.
func pathColumns() []datatable.Column[pathStep] {
	hop := datatable.Column[pathStep]{
		Title: "Hop", Sizing: datatable.SizingFixed, MinWidth: 3,
		Cell: pathStep.hopCell,
	}
	step := datatable.Column[pathStep]{
		Title: "Step", Sizing: datatable.SizingFixed, MinWidth: 11,
		Cell:  pathStep.stepCell,
		Style: pathStepStyle,
	}
	address := datatable.Column[pathStep]{
		Title: "Address", Sizing: datatable.SizingContent, MinWidth: 15, Flex: 1,
		Cell:  pathStep.addressCell,
		Style: pathAddressStyle,
	}
	rtt := datatable.Column[pathStep]{
		Title: "RTT", Sizing: datatable.SizingFixed, MinWidth: 9,
		Cell:  pathStep.rttCell,
		Style: pathRTTStyle,
	}
	return []datatable.Column[pathStep]{hop, step, address, rtt}
}

func pathStepStyle(s pathStep) lipgloss.Style {
	if s.Kind == stepNoResponse {
		return theme.DimStyle
	}
	if s.Kind != stepHop {
		return lipgloss.Style{}
	}
	switch s.Hop.Kind {
	case nettrace.HopSilent, nettrace.HopUnresolved:
		return theme.DimStyle
	case nettrace.HopError:
		return theme.SeverityTextStyle("MEDIUM")
	}
	return lipgloss.Style{}
}

func pathAddressStyle(s pathStep) lipgloss.Style {
	switch s.Kind {
	case stepTrace, stepEngine:
		return theme.DimStyle
	case stepNetwork:
		if s.Container.dimNetwork() {
			return theme.DimStyle
		}
	case stepHop:
		if !s.Hop.Addr.IsValid() {
			return theme.DimStyle
		}
	case stepInterface, stepGateway:
		if c := s.addressCell(); c == routeUndetermined || c == gatewayDirect || c == gatewayNone {
			return theme.DimStyle
		}
	}
	return lipgloss.Style{}
}

func pathRTTStyle(s pathStep) lipgloss.Style {
	if s.Kind == stepNoResponse || (s.Kind == stepHop && s.Hop.RTT <= 0) {
		return theme.DimStyle
	}
	return lipgloss.Style{}
}

// view renders the table; the body is never replaced (Rule 139). The open
// selector is drawn over it.
func (pm *PathModel) view() string {
	if pm.pickerOpen() {
		return pm.viewWithPicker(pm.table.View())
	}
	return pm.table.View()
}

// barView is the target field in its frame, two lines in every state.
func (pm *PathModel) barView(width int) string {
	return components.BarFrame(width, theme.KeyStyle.Render(pathBarLabel)+pm.input.View())
}

// statusLine is derived on every frame (Rule 128).
func (pm *PathModel) statusLine() components.Status {
	if pm.errText != "" {
		return components.Status{Text: pm.errText, Level: components.LevelError}
	}
	if pm.pickerOpen() {
		return pm.pickerStatus()
	}
	if pm.state != pathRunning {
		if a := pm.launchable(); !a.Enabled() {
			// The platform cannot trace: say so up front, derived every frame.
			return components.Status{Text: a.Reason, Level: components.LevelWarning}
		}
		return components.Status{}
	}
	host := pm.target.Host
	n := pm.hopCount()
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
	if !pm.source.isHost() {
		out = append(out, shortcut.HeaderInfo{Key: "Source", Value: pm.source.Name, Style: theme.HeaderValueStyle})
	}
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
	if pm.pickerOpen() { // a mode: the list is replaced
		return shortcut.Shortcuts{
			{Key: "enter", Description: "Trace from it"},
			{Key: "esc", Description: "Close"},
		}
	}
	off := !pm.launchable().Enabled()
	source := shortcut.Shortcut{Key: pathSourceKey, Description: "Trace from…", Disabled: !pm.sourceChoosable().Enabled()}
	switch pm.state {
	case pathRunning:
		return shortcut.Shortcuts{{Key: "esc", Description: "Cancel the trace"}}
	case pathDone:
		return shortcut.Shortcuts{
			{Key: "tab", Description: "Switch tab"},
			{Key: "enter", Description: "Trace again", Disabled: off},
			source,
			{Key: "esc", Description: "Edit the target"},
			{Key: "?", Description: "Help"},
		}
	}
	return shortcut.Shortcuts{
		{Key: "tab", Description: "Switch tab"},
		{Key: "enter", Description: "Trace the path", Disabled: off},
		source,
	}
}
