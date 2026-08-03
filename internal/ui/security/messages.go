package security

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
)

// ScanCompleteMsg is sent when scanning finishes
type ScanCompleteMsg struct {
	Result *scan.Result
	Error  error
	Gen    int // must match Model.scanGen; stale results (cancelled scans) are discarded
}

// DepsCheckedMsg is sent when dependency check completes
type DepsCheckedMsg struct {
	Deps scan.DependencyStatus
}

// SecretIgnoredMsg is sent when a secret is added to .gitleaksignore
type SecretIgnoredMsg struct {
	Finding scan.Finding
	Error   error
}

// SelectionRequestMsg is sent to the app router to request a selection from another view
type SelectionRequestMsg struct {
	Type    string // "directory" or "image"
	Message string // Context message to display in the selection view footer
}

// SelectionResultMsg is sent back to security view with the selected path/image
type SelectionResultMsg struct {
	Path string // Selected directory path or image name
}

// SelectionCancelledMsg is sent when user cancels the selection
type SelectionCancelledMsg struct{}

// StartScanMsg triggers the scan programmatically (e.g. for viewing cached results)
type StartScanMsg struct{}

// ScanProgressMsg carries a progress update from the scan goroutine to the TUI.
type ScanProgressMsg struct {
	Update scan.ProgressUpdate
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
