package security

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// The Repro line of a finding detail and what Y copies (DEVDESK-752). The Repro
// is the command a scan wrote for a finding of its own, run from the scanned
// directory with the tool on this machine. DevDesk shows and copies it; it never
// runs it.

// reasonToolInContainer is why Y is greyed on a finding the scan reproduced from
// a container: what ran there is not the tool installed on this machine.
const reasonToolInContainer = "The scan ran this tool in a container, not the one on this machine"

// reproNoneInContainer is the Repro line of a finding whose tool ran in a
// container.
const reproNoneInContainer = "none: the tool ran in a container, not on this machine"

// reproCommand is the Repro line of f, trimmed, when it is one line of plain
// words. A Repro read back from a cache is text a scanner build wrote: it is
// checked again here, as runCommand checks a FixCommand, and trimmed of spaces. A finding the scan
// reproduced from a container has none, whatever its cache says.
func reproCommand(f scan.Finding) (string, bool) {
	// Only blanks are trimmed: a trailing line break or carriage return is not
	// padding, it is text PlainWords refuses.
	cmd := strings.Trim(f.Repro, " ")
	if cmd == "" || f.RanFrom == scan.ToolSourceContainer || !scan.PlainWords(cmd) {
		return "", false
	}
	return cmd, true
}

// copyCommand is what Y copies and whether it can: the command under Run when
// there is one, else the Repro line, else the reason there is nothing to copy.
func copyCommand(f scan.Finding) (string, shortcut.Availability) {
	if cmd, ok := runCommand(f); ok {
		return cmd, shortcut.Availability{}
	}
	if cmd, ok := reproCommand(f); ok {
		return cmd, shortcut.Availability{}
	}
	if f.RanFrom == scan.ToolSourceContainer {
		return "", shortcut.Unavailable(reasonToolInContainer)
	}
	return "", shortcut.Unavailable(reasonNoCommand)
}

// detailsRepro is the Repro line of the Fix block, or nothing. It is cut when it
// is wider than the column: the copy key puts the whole of it on the clipboard,
// and the line names no key (Rule 134).
func detailsRepro(f scan.Finding, t detailsText) []string {
	const label = "Repro: "
	if cmd, ok := reproCommand(f); ok {
		return []string{t.heading(label) + t.text.Render(oneLine("$ "+cmd, t.width-ansi.StringWidth(label)))}
	}
	if f.RanFrom == scan.ToolSourceContainer {
		return t.field(label, reproNoneInContainer, t.text)
	}
	return nil
}
