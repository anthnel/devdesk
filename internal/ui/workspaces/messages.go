package workspaces

import (
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/git"
)

// EntriesLoadedMsg is sent when entries are loaded.
//
// Path is the directory the listing was read from, in the model's own terms —
// "" for the workspaces root. It is what lets Update tell a listing that
// belongs here from one the user has already navigated away from: a load is a
// Cmd, so nothing stops a second navigation from overtaking the first.
type EntriesLoadedMsg struct {
	Path    string
	Entries []Entry
}

// LoadErrorMsg est envoyé en cas d'erreur. Path carries the same stamp, for the
// same reason: an error about a directory nobody is looking at any more must
// not be reported over the one on screen.
type LoadErrorMsg struct {
	Path  string
	Error error
}

// WorkspaceCreatedMsg est envoyé quand un workspace est créé
type WorkspaceCreatedMsg struct {
	Path  string
	Error error
}

// EntryDeletedMsg is sent when an entry is deleted
type EntryDeletedMsg struct {
	Path  string
	Error error
}

// EntryRenamedMsg is sent when an entry is renamed
type EntryRenamedMsg struct {
	OldPath string
	NewPath string
	Error   error
}

// IDEOpenedMsg est envoyé quand l'IDE est ouvert
type IDEOpenedMsg struct {
	Error error
}

// BrowserOpenedMsg is sent when the default browser has been launched
type BrowserOpenedMsg struct {
	Error error
}

// ScanRequestMsg is sent when user requests a security scan on a directory
type ScanRequestMsg struct {
	TargetPath string
}

// DirectorySelectedMsg is sent when a directory is selected in selection mode
type DirectorySelectedMsg struct {
	Path string
}

// SelectionCancelledMsg is sent when user cancels directory selection
type SelectionCancelledMsg struct{}

// WorkspaceScanStartingMsg is sent when a workspace scan begins for a repo path
type WorkspaceScanStartingMsg struct {
	RepoPath string
}

// WorkspaceScanCompleteMsg is sent by the security view when a workspace scan finishes
type WorkspaceScanCompleteMsg struct {
	RepoPath string
	Critical int
	High     int
	Medium   int
	Low      int
	// Sensitive is the secret verdict, nil when no stage looked — le même
	// tri-état que porte l'entrée de cache, transporté tel quel plutôt
	// qu'aplati en chemin.
	Sensitive *bool
	ScannedAt time.Time
	Error     error
}

// WorkspaceSyncStartingMsg is sent when a repository's sync begins
type WorkspaceSyncStartingMsg struct {
	RepoPath string
}

// WorkspaceSyncCompleteMsg is sent when a repository's sync ends, whatever it did.
type WorkspaceSyncCompleteMsg struct {
	RepoPath string
	Outcome  git.SyncOutcome
	Behind   int
	Reason   string
	// Status carries the repository re-read after the sync. Only its git fields
	// are meaningful: it exists so the Git Status column stops describing the
	// state before the fetch, which is the whole point of syncing (D35).
	Status Entry
	Error  error
}

// clearSyncSummaryMsg is sent after a delay to drop a finished sync's summary.
type clearSyncSummaryMsg struct{}

// ScanDetailsRequestMsg is sent when the user wants to view scan details for a repo
type ScanDetailsRequestMsg struct {
	RepoPath string
}

// ScanCacheLoadedMsg is sent when the scan cache has been loaded from disk
type ScanCacheLoadedMsg struct {
	Cache map[string]cache.WorkspaceScanEntry
}

// TerminalExitMsg is sent when the interactive terminal process exits (in-place fallback)
type TerminalExitMsg struct{}

// TerminalOpenedMsg is sent after a non-blocking terminal launch attempt
type TerminalOpenedMsg struct {
	Error error
}
