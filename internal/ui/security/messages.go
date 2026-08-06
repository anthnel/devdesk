package security

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
)

// What went with the form (phase 3):
//
//   - ScanCompleteMsg, ScanProgressMsg and StartScanMsg drove the in-place
//     scanning screen. The inventory rescans in the background instead and
//     reports through InventoryScanFinishedMsg, so nothing waits on a whole
//     screen for one target.
//   - DepsCheckedMsg fed the Start button, the only thing left that read whether
//     a scanner was installed. The dashboard already reports that from
//     shared.State.Tools.
//   - SelectionRequestMsg / SelectionResultMsg / SelectionCancelledMsg were the
//     browser bridge: the form borrowed the workspaces or the images view to
//     pick a target. A target is a row of the inventory now.

// SecretIgnoredMsg is sent when a secret is added to .gitleaksignore
type SecretIgnoredMsg struct {
	Finding scan.Finding
	Error   error
}

// BackToOriginMsg is sent when the user presses Esc in StateResults to return to the originating view.
type BackToOriginMsg struct {
	Origin command.ViewType
}

// clearStatusMsg clears the footer status message (Rule 128).
type clearStatusMsg struct{}

// clearStatusCmd expires the footer status message after 3 seconds (Rule 128).
func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}
