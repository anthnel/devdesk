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

// RefusalKind is what `D` was asked to delete when it refuses because containers use it.
type RefusalKind int

const (
	RefusalImage RefusalKind = iota
	RefusalVolume
	RefusalNetwork
)

const (
	// inUseNames is how many container names the refusal lists before "and N more".
	inUseNames = 3
	// inUseNameWidth is the most cells one name, or the item itself, takes, so a single
	// long name (an anonymous volume is 64 characters) cannot push the other names and
	// what to do off the footer.
	inUseNameWidth = 40
)

// InUseRefusal is the footer line of `D` on an item containers use (OC-25), for the
// caller to show at level Warning. Names keep the order given (the caller sorted
// them), go through Printable, are cut to 40 cells with an ellipsis, and beyond three
// become "and N more". An image adds what to do, the other kinds do not.
//
// With no user there is nothing to refuse, and a sentence that says "used by" nobody
// would be false: it returns "" and the caller shows nothing (and so does not start the
// deletion's refusal). Showing it is the caller's mistake, not a message.
func InUseRefusal(kind RefusalKind, item string, users []string) string {
	if len(users) == 0 {
		return ""
	}
	shown := users[:min(len(users), inUseNames)]
	names := make([]string, 0, len(shown))
	for _, u := range shown {
		names = append(names, ansi.Truncate(Printable(u), inUseNameWidth, "…"))
	}
	list := strings.Join(names, ", ")
	if more := len(users) - len(shown); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	item = ansi.Truncate(Printable(item), inUseNameWidth, "…")
	switch kind {
	case RefusalVolume:
		return fmt.Sprintf("Cannot delete volume %s: used by %s", item, list)
	case RefusalNetwork:
		return fmt.Sprintf("Cannot delete network %s: used by %s", item, list)
	default:
		first := "them"
		if len(users) == 1 {
			first = "it"
		}
		return fmt.Sprintf("Cannot delete %s: used by %s — delete %s first", item, list, first)
	}
}
