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

	// MergedCIConfig says the forge can resolve a repository's CI
	// configuration server-side and hand back the result — every `include` and
	// every component expanded, as the pipeline would actually run.
	//
	// GitLab does it through its lint endpoint. GitHub has no equivalent: a
	// workflow's `uses:` is resolved by the runner at execution time and there
	// is nothing to ask for, so the capability is declared false rather than
	// implemented as something adjacent (the InitialCommit precedent — refuse
	// what the platform cannot express).
	MergedCIConfig bool

	// RestrictsVisibilityByParent says a namespace or repository created under
	// a parent cannot be more open than that parent. GitLab enforces this
	// server-side for both a subgroup and a project — offering "public" under
	// a private group is a create call the API refuses — so VisibilitiesUnder
	// narrows the form before the round trip rather than after it.
	//
	// GitHub has nothing to narrow by: an organisation is not itself
	// public/private/internal the way a GitLab group is, so there is no parent
	// value to compare a new repository's visibility against.
	RestrictsVisibilityByParent bool
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

// VisibilitiesUnder is what a namespace or repository created beneath a parent
// whose own visibility is parentVisibility may take, most private first.
//
// An empty parentVisibility — nothing above this level, i.e. the root —
// imposes no restriction: the full Visibilities list is offered, same as
// today. A forge that does not restrict by parent (RestrictsVisibilityByParent
// false) answers the same regardless of parentVisibility, and so does one
// given a value it does not recognise — a create call the server refuses on
// its own terms is a better failure than a menu narrowed to nothing on a
// guess.
func (s Shape) VisibilitiesUnder(parentVisibility string) []string {
	if !s.RestrictsVisibilityByParent || parentVisibility == "" {
		return s.Visibilities
	}
	idx := slices.Index(s.Visibilities, parentVisibility)
	if idx < 0 {
		return s.Visibilities
	}
	return s.Visibilities[:idx+1]
}
