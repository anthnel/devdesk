package templates

import "github.com/anthnel/devdesk/internal/template"

// Messages are named [Component][Action]Msg (Rule 109).

// CatalogLoadedMsg carries the opened catalog back into Update (Rule 110).
type CatalogLoadedMsg struct {
	Store *template.Store
	Err   error
}

// DiscoveredMsg carries the templates the configured registry listed.
type DiscoveredMsg struct {
	Entries []template.Entry
	Err     error
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
