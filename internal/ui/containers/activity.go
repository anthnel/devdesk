package containers

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// observedState maps what the engine reports for a container onto the shared
// activity vocabulary (Rule 125). A running container is Idle: the nominal
// state shows the container icon, not a glyph. An exit without a known code is
// Failed, so a possible crash is never shown as a clean stop. Engine synonyms
// (podman's configured, initialized, stopping...) and anything unknown fall
// back to Unknown.
func observedState(c docker.Container) indicator.State {
	switch c.State {
	case "running":
		return indicator.Idle
	case "restarting":
		return indicator.Restarting
	case "paused":
		return indicator.Suspended
	case "exited", "stopped":
		if c.Facts.HasExitCode && c.Facts.ExitCode == 0 {
			return indicator.Skipped
		}
		return indicator.Failed
	case "dead":
		return indicator.Failed
	}
	return indicator.Unknown
}

// restIcon is what the activity column shows for a container that is Idle.
func restIcon(docker.Container) string { return theme.IconContainer }

// restStyle colours restIcon with the container role (plain text by default).
func restStyle(docker.Container) lipgloss.Style {
	return theme.IconStyle(theme.IconRoleContainer)
}

// ActivityTables returns the read-only face of the view's table, keyed by the
// name the activity-column guard (internal/app) knows it by.
func (m Model) ActivityTables() map[string]datatable.ActivityTable {
	return map[string]datatable.ActivityTable{"containers": &m.containerTable}
}

// WantsBlink is the router's BlinkingView hook: true while a row action is in
// flight, so the router frames the view for the running dot's blink.
func (m Model) WantsBlink() bool {
	return len(m.containerTable.BusyLabels()) > 0 || m.status().Activity
}

// observedText is the footer sentence for a container at rest, without a
// glyph: the glyph is added by components.DescribeRow from observedState.
func observedText(c docker.Container) string {
	f := c.Facts
	ago := ""
	if f.HasAge {
		ago = " — " + agoPhrase(f.Age, "just now")
	}
	switch c.State {
	case "running":
		if f.HasAge {
			if f.Age < time.Minute {
				return "Running for less than a minute"
			}
			return "Running since " + theme.AgoFor(f.Age)
		}
		return "Running"
	case "restarting":
		if f.HasExitCode {
			return fmt.Sprintf("Restarting — last exit code %d", f.ExitCode)
		}
		return "Restarting"
	case "paused":
		return "Paused"
	case "created":
		return "Created, never started"
	case "exited", "stopped":
		switch {
		case !f.HasExitCode:
			return "Exited (exit code unknown)"
		case f.ExitCode == 0:
			return "Exited cleanly (code 0)" + ago
		}
		return fmt.Sprintf("Exited with code %d", f.ExitCode) + ago
	case "dead":
		return "Dead — the engine could not stop it cleanly"
	case "removing":
		return "Removing"
	}
	if c.State == "" {
		return ""
	}
	return strings.ToUpper(c.State[:1]) + c.State[1:]
}

// agoPhrase is theme.AgoFor, except that "now" (under a minute) reads badly
// inside a sentence, so it is replaced by the given wording.
func agoPhrase(d time.Duration, underMinute string) string {
	if d < time.Minute {
		return underMinute
	}
	return theme.AgoFor(d)
}

// selectedDetail is what the footer says about the selected container: the
// action running on it, else its observed state in words. Empty when nothing
// is selected.
func (m Model) selectedDetail() sharedcomponents.RowDetail {
	c := m.getSelectedContainer()
	if c == nil {
		return sharedcomponents.RowDetail{}
	}
	if label, ok := m.containerTable.BusyLabel(c.ID); ok {
		return sharedcomponents.RowDetail{
			Glyph:  indicator.Glyph(indicator.Running, true), // the footer glyph never blinks
			Lead:   strings.TrimSuffix(label, c.Name),
			Target: c.Name,
		}
	}
	return sharedcomponents.DescribeRow(sharedcomponents.RowQuery{
		Observed: observedState(*c), ObservedText: observedText(*c),
	}).Detail
}
