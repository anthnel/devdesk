package templates

import (
	"time"

	"github.com/anthnel/devdesk/internal/template"
)

// Messages are named [Component][Action]Msg (Rule 109).

// CatalogLoadedMsg carries the opened catalog back into Update (Rule 110).
type CatalogLoadedMsg struct {
	Store *template.Store
	Err   error
}

// SavedMsg reports a Put. Entry is what was written, normalized.
type SavedMsg struct {
	Entry template.Entry
	Err   error
}

// DeletedMsg reports a Delete.
type DeletedMsg struct {
	Slug string
	Err  error
}

// FormSubmitMsg is sent when the entry form is confirmed.
type FormSubmitMsg struct {
	Entry template.Entry
}

// FormCancelMsg is sent when the entry form is left with esc.
type FormCancelMsg struct{}

// TemplateSelectedMsg is the answer to a view that borrowed this one to pick a
// template. Slug is the catalog identifier — what the caller keeps — and Name is
// what it is called, for showing.
type TemplateSelectedMsg struct {
	Slug string
	Name string
}

// SelectionCancelledMsg is sent when the picker is left with esc.
type SelectionCancelledMsg struct{}

// SyncedLoadedMsg carries when each template's cached copy was read, by slug. A
// template with no copy of its current source is absent.
type SyncedLoadedMsg struct {
	At map[string]time.Time
}
