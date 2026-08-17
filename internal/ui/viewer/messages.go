package viewer

import (
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/viewer"
)

// OpenRequestMsg asks the router to open a document. It is what every producer
// emits — workspaces for a file, containers for an inspect or a log — and the
// only thing they have to agree on.
//
// It carries the source rather than the content: the read happens in the
// viewer's own Init, so one place decides what is too large or not text, and one
// place reports it. A producer that loaded the bytes itself would each have to
// answer that question again, and answer it the same way.
type OpenRequestMsg struct {
	Source viewer.Source
}

// DocumentLoadedMsg carries the result of a Load back into Update (Rule 110).
type DocumentLoadedMsg struct {
	Name    string
	Kind    viewer.Kind
	Content []byte
	Err     error
}

// BackToOriginMsg asks the router to return to the view the document was opened
// from. It mirrors security.BackToOriginMsg, and for the same reason: the view
// knows where it came from, the router knows how to get there.
type BackToOriginMsg struct {
	Origin command.ViewType
}

// PagerExitMsg comes back when the system pager or the follow process ends.
type PagerExitMsg struct {
	Err error
}
