package imageupdate

import "time"

// Tracker is what a view holds between checks: the facts it has, and the
// references a check is out for. It is mutated from Update only (Rule 110);
// the check itself runs in a Cmd, through Check.
type Tracker struct {
	facts  map[string]Facts
	asking map[string]bool
}

// Due returns the references worth asking about — never asked, or whose answer
// has gone stale — that no check is already out for, and marks them as asked.
// A view calls it whenever its list is reloaded; most reloads return nothing.
func (t *Tracker) Due(refs []string, now time.Time) []string {
	if t.asking == nil {
		t.asking = map[string]bool{}
	}
	var due []string
	for _, r := range refs {
		if r == "" || t.asking[r] {
			continue
		}
		if f, ok := t.facts[r]; ok && fresh(f, now) {
			continue
		}
		t.asking[r] = true
		due = append(due, r)
	}
	return due
}

// Store records a check's answer.
func (t *Tracker) Store(facts map[string]Facts) {
	if t.facts == nil {
		t.facts = map[string]Facts{}
	}
	for r, f := range facts {
		t.facts[r] = f
		delete(t.asking, r)
	}
}

// Status is the verdict for a reference, against the local image's digests;
// noLocal is what having none means to the caller (see Evaluate).
func (t Tracker) Status(ref string, local []string, noLocal Kind) Status {
	return Evaluate(t.facts[ref], local, noLocal)
}
