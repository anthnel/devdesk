package netdiag

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/nettrace"
)

// lastAnswerSuffix ends the address of the last hop that answered, when a
// silent end follows it.
const lastAnswerSuffix = " · last to answer"

// pathTail is the silent end of a path: TTLs First..Last never answered.
// Anchor is the TTL of the last router that did answer; 0 when none did.
type pathTail struct{ First, Last, Anchor int }

// silentTail finds the silent end of an unfinished trace. It applies only to a
// run that stopped on the hop limit or the deadline; the tail is the longest
// final run of silent or unresolved hops. res is not modified.
func silentTail(res *nettrace.Result) (pathTail, bool) {
	if res == nil || (res.Outcome != nettrace.OutcomeMaxHops && res.Outcome != nettrace.OutcomeDeadline) {
		return pathTail{}, false
	}
	start := len(res.Hops)
	for start > 0 && isSilentKind(res.Hops[start-1].Kind) {
		start--
	}
	if start == len(res.Hops) {
		return pathTail{}, false
	}
	tail := pathTail{First: res.Hops[start].TTL, Last: res.Hops[len(res.Hops)-1].TTL}
	for _, h := range res.Hops[:start] {
		// Invariant: a hop that already answered as the target or as
		// unreachable contradicts "no response beyond", so the rows stay as is.
		if h.Kind == nettrace.HopTarget || h.Kind == nettrace.HopUnreachable {
			return pathTail{}, false
		}
		if h.Kind == nettrace.HopRouter && h.Addr.IsValid() && h.TTL > tail.Anchor {
			tail.Anchor = h.TTL
		}
	}
	return tail, true
}

func isSilentKind(k nettrace.HopKind) bool {
	return k == nettrace.HopSilent || k == nettrace.HopUnresolved
}

// withNoResponse folds the silent end of the path into one row and marks the
// last hop that answered. Other outcomes, and a nil result, return an
// unchanged copy. Neither input is modified.
func withNoResponse(steps []pathStep, res *nettrace.Result) []pathStep {
	tail, ok := silentTail(res)
	if !ok {
		return append([]pathStep(nil), steps...)
	}
	out := make([]pathStep, 0, len(steps))
	grouped := false
	for _, s := range steps {
		if s.Kind != stepHop {
			out = append(out, s)
			continue
		}
		switch ttl := s.Hop.TTL; {
		case ttl >= tail.First && ttl <= tail.Last:
			if !grouped {
				out = append(out, pathStep{Kind: stepNoResponse, Tail: tail})
				grouped = true
			}
		default:
			s.LastAnswer = tail.Anchor > 0 && ttl == tail.Anchor
			out = append(out, s)
		}
	}
	return out
}

// hopCell is the Hop cell of the grouped row: "12+", or the TTL
// alone when a single hop is silent.
func (t pathTail) hopCell() string {
	if t.First == t.Last {
		return fmt.Sprint(t.Last)
	}
	return fmt.Sprintf("%d+", t.First)
}

// addressCell says what was observed, never why.
func (t pathTail) addressCell() string {
	span := fmt.Sprintf("hops %d to %d", t.First, t.Last)
	if t.First == t.Last {
		span = fmt.Sprintf("hop %d", t.Last)
	}
	if t.Anchor == 0 {
		return "no response from any hop (" + span + ")"
	}
	return fmt.Sprintf("no response beyond hop %d (%s)", t.Anchor, span)
}
