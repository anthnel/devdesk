package filebrowser

import "github.com/anthnel/devdesk/internal/fsbrowse"

// ── Picker contract ─────────────────────────────────────────────────────────
//
// Any view may borrow this one to have a path chosen (§3.95). The borrower sends
// PickRequestMsg; the router lends a picker, and delivers PathPickedMsg or
// PickCancelledMsg back to the borrower. One pair for every borrower rather than
// a translation per borrower (compare internal/app/selection.go): the answer is
// always a path, and Tag is how a form with several path fields knows which one
// it is for.

// PickKind is what the picker lets the user choose.
type PickKind int

const (
	// PickDir picks a directory. The browsed directory itself is offered as
	// the first row ("."), so an empty directory can be picked too.
	PickDir PickKind = iota
	// PickFile picks a regular file.
	PickFile
)

// PickRequestMsg asks the router to open a picker. Start is the value the field
// currently holds — the picker opens there, or at its nearest existing ancestor.
type PickRequestMsg struct {
	Kind   PickKind
	Start  string
	Prompt string
	Tag    string
}

// PathPickedMsg carries the chosen absolute path back to the borrower.
type PathPickedMsg struct {
	Tag  string
	Path string
}

// PickCancelledMsg says the picker was closed without a choice.
type PickCancelledMsg struct {
	Tag string
}

// ── Internal messages ───────────────────────────────────────────────────────

// EntriesLoadedMsg carries a listing of Dir back into Update (Rule 110).
type EntriesLoadedMsg struct {
	Dir     string
	Entries []fsbrowse.Entry
}

// LoadErrorMsg says Dir could not be listed.
type LoadErrorMsg struct {
	Dir string
	Err error
}

// EntryCreatedMsg reports N.
type EntryCreatedMsg struct {
	Path string
	Err  error
}

// DeletePreparedMsg carries what the confirmation needs to say about an entry:
// how much a directory holds is read off the disk, so it is a Cmd's answer.
type DeletePreparedMsg struct {
	Entry fsbrowse.Entry
	Count int
}

// EntryDeletedMsg reports D. It is a jobs.Reporter (jobs.go).
type EntryDeletedMsg struct {
	Path string
	Err  error
}

// PathCopiedMsg reports Y.
type PathCopiedMsg struct {
	Path string
	Err  error
}
