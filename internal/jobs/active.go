package jobs

import "slices"

// Active is one item of a run that is running or queued on a target.
type Active struct {
	Kind  Kind
	RunID JobID
	Item  Item
}

// ActiveOn returns the running and queued items on target in contextName:
// running ones first, then queued ones, each group in run order. The target is
// normalized with TargetKey on both sides. With no kinds, every kind is kept.
// Items are copied by value, without their cancel hook. No match returns nil.
func ActiveOn(runs []Run, contextName, target string, kinds ...Kind) []Active {
	target = TargetKey(target)
	var running, queued []Active
	for _, r := range runs {
		if r.Context != contextName || (len(kinds) > 0 && !slices.Contains(kinds, r.Kind)) {
			continue
		}
		for _, it := range r.Items {
			if TargetKey(it.Target) != target {
				continue
			}
			it.cancel = nil
			a := Active{Kind: r.Kind, RunID: r.ID, Item: it}
			switch it.State {
			case ItemRunning:
				running = append(running, a)
			case ItemQueued:
				queued = append(queued, a)
			}
		}
	}
	return append(running, queued...)
}
