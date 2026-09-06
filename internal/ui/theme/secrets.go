package theme

import "github.com/charmbracelet/lipgloss"

// SecretsState is what a scan can say about secrets, and it has three values
// rather than two. "Nothing found" and "nobody looked" look a lot alike and
// don't mean the same thing at all: a secrets step can be cut by the option,
// by a missing tool, or have failed, and an image scan didn't even have one
// before §3.11. A boolean would render the icon green in all of these cases.
//
// The rendering lives here because three views display it — workspaces,
// oci/images and the security inventory — and there is only one iconography.
// The workspaces view used to decide its color by comparing the
// already-rendered icon string, which a rename of the icon would have broken
// silently.
type SecretsState int

const (
	SecretsUnknown SecretsState = iota
	SecretsClean
	SecretsFound
)

// SecretsVerdict maps a cached verdict onto the three states.
//
// `scanned` is the target's own state: a target that was never scanned has
// no verdict, whatever the cache entry might carry — there is no entry.
func SecretsVerdict(sensitive *bool, scanned bool) SecretsState {
	if !scanned || sensitive == nil {
		return SecretsUnknown
	}
	if *sensitive {
		return SecretsFound
	}
	return SecretsClean
}

// SecretsIcon is what the cell prints. Plain text, no ANSI sequence: it's
// SecretsStyle that colors it, and the reverse would violate Rule 122.
func SecretsIcon(state SecretsState) string {
	switch state {
	case SecretsFound:
		return IconWorkspaceUntrusted
	case SecretsClean:
		return IconWorkspaceTrusted
	default:
		return IconWorkspaceUnknown
	}
}

// SecretsStyle colours the verdict. This is the only column of these tables
// that reports a finding rather than a count, and a repo carrying a secret
// is what deserves to be seen before the counters.
func SecretsStyle(state SecretsState) lipgloss.Style {
	switch state {
	case SecretsFound:
		return StatusErrorStyle
	case SecretsClean:
		return StatusOKStyle
	default:
		return DimStyle
	}
}
