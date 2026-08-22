package forge

import "slices"

// Shape is what a backend can express, as opposed to what it is called.
//
// The distinction is the one §3.6 turns on: "Group" against "Organization" is a
// *word*, and words live in the vocabulary. Whether namespaces nest, whether
// `internal` visibility exists, whether a delete can be made permanent — those
// change what the application can promise, and no wording helps with them.
//
// A shape is **declared by the backend**, never sniffed from a URL. That is the
// registry `provider` field's rule (§3.8): a thing is treated as what it says
// it is, so registration order and URL spelling decide nothing.
type Shape struct {
	// Name identifies the backend for a config discriminator and a log line —
	// "gitlab", "github". It is not a display name; that is the vocabulary's.
	Name string

	// MaxNamespaceDepth is how deep namespaces may nest, counting the root as
	// 1. GitHub is 1: an organisation holds repositories and never another
	// organisation. GitLab nests without a documented limit.
	//
	// Zero or less means unbounded. Ask CanNestUnder rather than comparing,
	// so that the sentinel is read in one place instead of at every call site
	// — a `depth < s.MaxNamespaceDepth` written against an unbounded forge is
	// false for every depth, which is the sentinel's whole hazard.
	MaxNamespaceDepth int

	// Visibilities are the values a namespace or repository may take, most
	// private first. GitHub.com has no `internal`, so a form offering three
	// values there offers one that will be refused.
	Visibilities []string

	// PermanentDelete says a deletion can bypass the forge's grace period.
	// GitLab schedules and then permanently removes; GitHub deletes at once and
	// has no equivalent, so the confirmation's "permanent" checkbox is
	// meaningless there and must be out of reach rather than ignored.
	PermanentDelete bool
}

// CanNestUnder reports whether a namespace may be created under a parent that
// is itself at parentDepth, counting a root namespace as depth 1.
//
// A parentDepth of 0 means "at the root", which every forge allows.
func (s Shape) CanNestUnder(parentDepth int) bool {
	if parentDepth <= 0 {
		return true
	}
	if s.MaxNamespaceDepth <= 0 {
		return true
	}
	return parentDepth < s.MaxNamespaceDepth
}

// AllowsVisibility reports whether v is one this forge accepts. An empty
// Visibilities list allows nothing, which is what an unconfigured backend
// should look like — silently accepting anything is how a form ends up
// offering a value the forge refuses.
func (s Shape) AllowsVisibility(v string) bool {
	return slices.Contains(s.Visibilities, v)
}

// DefaultVisibility is the most private value the forge offers, which is the
// only defensible default: a repository created more open than intended cannot
// be made private again in the eyes of whoever already fetched it.
//
// Empty when the backend declares no visibility at all.
func (s Shape) DefaultVisibility() string {
	if len(s.Visibilities) == 0 {
		return ""
	}
	return s.Visibilities[0]
}
