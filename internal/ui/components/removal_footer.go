package components

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
)

// Removal sets the footer to the outcome of a batch removal (OC-22) and returns its
// expiry timer. Everything removed or already gone is Info; a refusal the engine
// explained (running, in use) is a Warning, nothing failed; a refusal DevDesk could
// not explain is an Error (Rule 128).
func (f *FooterMessage) Removal(report docker.RemovalReport) tea.Cmd {
	text := report.Summary()
	var cmd tea.Cmd
	switch {
	case report.CountRefused(docker.ReasonOther) > 0:
		cmd = f.Error(text)
	case report.Count(docker.RemovalRefused) > 0:
		cmd = f.Warn(text)
	default:
		cmd = f.Info(text)
	}
	f.removal = true
	return cmd
}

// removalPreviewItems is how many items a removal confirmation lists before "and N more".
const removalPreviewItems = 8

// Printable makes text that came from the engine safe to draw: escape sequences are
// stripped, and the control, format and line-separator characters that remain become
// '?', so a container named with a terminal sequence or a bidirectional mark shows
// what it is and draws nothing else.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return '?'
		}
		return r
	}, ansi.Strip(s))
}

// RemovalPreview is the text of a confirmation that removes a frozen list (OC-22):
// it says what will go, `what` being a plural noun ("stopped containers"), and that
// the list is the whole of it, the items it does not show included. It never says
// "all". Names go through Printable.
func RemovalPreview(what string, items []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Remove these %d %s?\n\n", len(items), what)
	shown := min(len(items), removalPreviewItems)
	for _, item := range items[:shown] {
		fmt.Fprintf(&b, "%s\n", Printable(item))
	}
	if hidden := len(items) - shown; hidden > 0 {
		fmt.Fprintf(&b, "and %d more (not shown)\n\nThese %d will be removed, including the %d not shown.", hidden, len(items), hidden)
	} else {
		b.WriteString("\nExactly the items listed above will be removed.")
	}
	return b.String()
}
