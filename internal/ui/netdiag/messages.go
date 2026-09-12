package netdiag

import (
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/netcheck"
)

// OpenRequestMsg asks the router to open netdiag on a specific target,
// prefilled from wherever the request came from (today: a monitor selected in
// status).
type OpenRequestMsg struct {
	Target netcheck.Target
	// AutoRun starts the pipeline immediately. It is true when the producer
	// already knows the exact port (an http/https/ssl monitor); false lands
	// on the prefilled form instead, so the user can confirm or adjust a
	// guessed port (icmp/dns, which have none of their own) before running.
	AutoRun bool
}

// BackToOriginMsg asks the router to return to the view a run was opened
// from. It mirrors security.BackToOriginMsg and uiviewer.BackToOriginMsg, for
// the same reason: the view knows where it came from, the router knows how
// to get there.
type BackToOriginMsg struct {
	Origin command.ViewType
}
