package status

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// columnName is the column the monitors table opens sorted by.
const columnName = 0

// monitorColumns describes the Monitors tab. Status carries an icon and orders
// by nothing anyone would recognise, so it does not sort.
func monitorColumns() []datatable.Column[status.ComponentStatus] {
	return []datatable.Column[status.ComponentStatus]{
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: 16, Flex: 1,
			Cell:   func(c status.ComponentStatus) string { return c.Name },
			Less:   func(a, b status.ComponentStatus) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) },
			Search: func(c status.ComponentStatus) string { return c.Name },
		},
		{
			Title: "Target", Sizing: datatable.SizingContent, TruncateHead: true, MinWidth: 24, Flex: 2,
			Cell:   func(c status.ComponentStatus) string { return c.Target },
			Less:   func(a, b status.ComponentStatus) bool { return strings.ToLower(a.Target) < strings.ToLower(b.Target) },
			Search: func(c status.ComponentStatus) string { return c.Target },
		},
		{
			Title: "Status", Sizing: datatable.SizingFixed, MinWidth: 10,
			Cell:  monitorStatusCell,
			Style: componentStatusStyle,
		},
		{
			Title: "Type", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 10,
			Cell: monitorTypeCell,
			Less: func(a, b status.ComponentStatus) bool {
				return strings.ToLower(string(a.Type)) < strings.ToLower(string(b.Type))
			},
			Search: func(c status.ComponentStatus) string { return string(c.Type) },
		},
		{
			Title: "Response", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 12,
			Cell: func(c status.ComponentStatus) string {
				if c.ResponseTime <= 0 {
					return "-"
				}
				return fmt.Sprintf("%dms", c.ResponseTime.Milliseconds())
			},
			Less: func(a, b status.ComponentStatus) bool { return a.ResponseTime < b.ResponseTime },
		},
	}
}

// monitorStatusCell renders the status as an icon. Plain text — Rule 122.
func monitorStatusCell(c status.ComponentStatus) string {
	switch c.Status {
	case "OK":
		return theme.IconOK
	case "DOWN":
		return theme.IconError
	case "ERROR":
		return theme.IconWarning
	default:
		return string(c.Status)
	}
}

// componentStatusStyle colours a status cell. Both tables use it: the icon is
// the same vocabulary on a monitor and on a certificate, so it has to read the
// same colour in both, and theme.StatusStyle is where that mapping already
// lived — the Status cells rendered through it until Rule 122 sent them back to
// plain text (the commented-out Render calls in view.go are what was left).
func componentStatusStyle(c status.ComponentStatus) lipgloss.Style {
	return theme.StatusStyle(string(c.Status))
}

// certStatusStyle is the same for the Certificates tab, on the certificate
// vocabulary its icon now speaks (status.CertState). It cannot be
// componentStatusStyle: `to renew` has no StatusType to give
// theme.StatusStyle, and an expired certificate would there take on the
// color of one that couldn't be read — the color would stop separating what
// the icon has just separated.
func certStatusStyle(c status.ComponentStatus) lipgloss.Style {
	return theme.CertStateStyle(string(status.CertStateOf(c)))
}

// monitorTypeCell names the check kind, or says so when the config did not.
func monitorTypeCell(c status.ComponentStatus) string {
	if c.Type == "" {
		return "unknown"
	}
	return string(c.Type)
}

// sslColumns describes the Certificates tab. Nothing sorts: the tab has never
// offered `.`, and an expiry table read in config order is what the user wrote.
//
// **The order groups columns by nature.** The three that follow their
// content — Name, Host, Issuer — are on the left and share the surplus; the
// three fixed-width ones are packed on the right, where a glyph and two
// dates take up exactly what they declare. `Issuer` used to be at the tail,
// after two dates, so the only extensible column of the right half was
// pulling the row toward a side nothing required of it.
//
// Intended consequence on degradation: `drop` removes the rightmost
// Optional column first, which is now `Expires` rather than `Issuer`. Both
// state the same deadline, one absolute and the other relative, and
// `Days Left` stays — so it's the redundant one that leaves first.
func sslColumns() []datatable.Column[status.ComponentStatus] {
	return []datatable.Column[status.ComponentStatus]{
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: 16, Flex: 1,
			Cell:   func(c status.ComponentStatus) string { return c.Name },
			Search: func(c status.ComponentStatus) string { return c.Name },
		},
		{
			Title: "Host", Sizing: datatable.SizingContent, TruncateHead: true, MinWidth: 22, Flex: 2,
			Cell:   func(c status.ComponentStatus) string { return c.Target },
			Search: func(c status.ComponentStatus) string { return c.Target },
		},
		{
			Title: "Issuer", Sizing: datatable.SizingContent, Optional: true, MinWidth: 20, Flex: 1,
			Cell: func(c status.ComponentStatus) string {
				if c.SSLIssuer == "" {
					return "-"
				}
				return c.SSLIssuer
			},
		},
		{Title: "Status", Sizing: datatable.SizingFixed, MinWidth: 12, Cell: formatSSLStatus, Style: certStatusStyle},
		{
			Title: "Days Left", Sizing: datatable.SizingFixed, MinWidth: 11,
			Cell: func(c status.ComponentStatus) string {
				if c.SSLDaysLeft == nil {
					return "-"
				}
				return fmt.Sprintf("%d", *c.SSLDaysLeft)
			},
		},
		{
			Title: "Expires", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 18,
			Cell: func(c status.ComponentStatus) string {
				if c.SSLExpires == nil {
					return "-"
				}
				return c.SSLExpires.Format("2006-01-02 15:04")
			},
		},
	}
}
