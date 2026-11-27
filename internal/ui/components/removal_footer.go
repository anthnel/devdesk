package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
)

// Removal sets the footer to the outcome of a batch removal (OC-22) and returns its
// expiry timer. Everything removed or already gone is Info; a refusal the engine
// explained (running, in use) is a Warning, nothing failed; a refusal DevDesk could
// not explain is an Error (Rule 128).
func (f *FooterMessage) Removal(report docker.RemovalReport) tea.Cmd {
	text := report.Summary()
	switch {
	case report.CountRefused(docker.ReasonOther) > 0:
		return f.Error(text)
	case report.Count(docker.RemovalRefused) > 0:
		return f.Warn(text)
	default:
		return f.Info(text)
	}
}

// removalPreviewItems is how many items a removal confirmation lists before "and N more".
const removalPreviewItems = 8

// RemovalPreview is the text of a confirmation that removes a frozen list (OC-22):
// it says what will go, `what` being a plural noun ("stopped containers"), and that
// nothing else will. It never says "all".
func RemovalPreview(what string, items []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Remove these %d %s?\n\n", len(items), what)
	for i, item := range items {
		if i == removalPreviewItems {
			fmt.Fprintf(&b, "and %d more\n", len(items)-i)
			break
		}
		fmt.Fprintf(&b, "%s\n", item)
	}
	b.WriteString("\nOnly the items listed here are removed.")
	return b.String()
}
