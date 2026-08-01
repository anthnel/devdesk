package theme

import (
	"fmt"
	"time"
)

// TimeAgo formats a time.Time as a compact human-readable relative string.
// Returns "" for zero values; callers that need a different zero sentinel (e.g. "-")
// should check t.IsZero() before calling.
//
// Format (max 11 chars, safe for compact table columns):
//
//	< 1 min   → "now"
//	< 1 hour  → "59 min ago"
//	< 24 h    → "23 hr ago"
//	< 30 days → "1 day ago" / "30 days ago"
//	< 12 mo   → "11 mo ago"
//	else      → "99 yr ago"
func TimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case d < 12*30*24*time.Hour:
		return fmt.Sprintf("%d mo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%d yr ago", int(d.Hours()/24/365))
	}
}
