package components

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// RowQuery names one row: the registry snapshot, the current context, the
// row's target as the registry knows it, the kinds the view can rerun (nil =
// all), and what the view observes on its own (container, agent).
type RowQuery struct {
	Runs     []jobs.Run     // any context; filtered here
	Failures []jobs.Failure // any context; filtered here
	Context  string
	Target   string
	Kinds    []jobs.Kind
	// Observed is the view's own state (Idle if none); ObservedText is its
	// sentence without glyph, shown when Observed wins or on an idle row.
	Observed     indicator.State
	ObservedText string
	// Extra are further registry identities the same row answers for; their
	// jobs and failures are merged with the main target's. Nil = none.
	Extra []RowTarget
}

// RowTarget is one more registry identity a row answers for, with the kinds
// the view can start again on it (nil = all). The query's Context applies.
//
// An Extra must not share the main Target with kinds that overlap it: a job or
// failure registered on that target would be counted by both, and a failure
// would show twice.
type RowTarget struct {
	Target string
	Kinds  []jobs.Kind
}

// RowActivity is what the first column and the footer say about one row.
type RowActivity struct {
	State  indicator.State
	Detail RowDetail
}

// RowState resolves the row's state without building any text. It is what the
// activity column calls; DescribeRow agrees with it by construction.
func RowState(q RowQuery) indicator.State { return resolveRow(q).state }

// DescribeRow resolves the row and builds its footer sentence.
func DescribeRow(q RowQuery) RowActivity {
	f := resolveRow(q)
	a := RowActivity{State: f.state}
	glyph := indicator.Glyph(f.state, true) // the footer glyph never blinks
	switch {
	case len(f.active) > 0:
		it := f.active[0]
		a.Detail = RowDetail{Glyph: glyph, Target: targetDisplay(it.Item.Name())}
		a.Detail.Lead = activeLead(f.state, it.Kind)
		a.Detail.Tail = maskedSuffix(f.failures)
	case len(f.failures) > 0:
		fl := f.failures[0]
		a.Detail = RowDetail{Glyph: glyph, Target: targetDisplay(failureName(fl)),
			Tail: failedTail(fl.Kind, fl.Detail) + maskedSuffix(f.failures[1:])}
	default:
		// No job and no failure: the view's own sentence. Idle shows it with no
		// glyph (D8); any other observed state shows it with the glyph.
		a.Detail = RowDetail{Lead: oneLine(q.ObservedText)}
		if f.state != indicator.Idle && a.Detail.Lead != "" {
			a.Detail.Glyph = glyph
		}
	}
	return a
}

// FailureEvent is the 3-second Error a view posts when an operation fails:
// "Failed to <kind> <target> — <cause>".
func FailureEvent(kind jobs.Kind, target, cause string) string {
	return "Failed to " + string(kind) + " " + targetDisplay(target) + causeSuffix(kind, cause)
}

// rowFacts is the single computation behind RowState and DescribeRow.
type rowFacts struct {
	state    indicator.State
	active   []jobs.Active  // running first, then queued
	failures []jobs.Failure // newest first; the winner when state is Failed
}

func resolveRow(q RowQuery) rowFacts {
	f := rowFacts{
		active:   jobs.ActiveOn(q.Runs, q.Context, q.Target, q.Kinds...),
		failures: jobs.FailuresOn(q.Failures, q.Context, q.Target, q.Kinds...),
	}
	for _, x := range q.Extra {
		f.active = append(f.active, jobs.ActiveOn(q.Runs, q.Context, x.Target, x.Kinds...)...)
		f.failures = append(f.failures, jobs.FailuresOn(q.Failures, q.Context, x.Target, x.Kinds...)...)
	}
	if len(q.Extra) > 0 {
		// Keep "running first, then queued" across the merged targets.
		slices.SortStableFunc(f.active, func(a, b jobs.Active) int {
			return cmp.Compare(activeRank(a), activeRank(b))
		})
	}
	slices.SortStableFunc(f.failures, func(a, b jobs.Failure) int { return b.At.Compare(a.At) })
	act := indicator.Activity{Failed: len(f.failures) > 0, Observed: q.Observed}
	for _, a := range f.active {
		act.Running = act.Running || a.Item.State == jobs.ItemRunning
		act.Queued = act.Queued || a.Item.State == jobs.ItemQueued
	}
	f.state = indicator.Resolve(act)
	return f
}

// activeRank orders running before queued.
func activeRank(a jobs.Active) int {
	if a.Item.State == jobs.ItemRunning {
		return 0
	}
	return 1
}

// failureName is the target as the launcher wants it printed.
func failureName(f jobs.Failure) string {
	if f.Display != "" {
		return f.Display
	}
	return f.Target
}

// targetDisplay folds the home directory of an absolute path; an image
// reference or a forge path is kept verbatim.
func targetDisplay(s string) string {
	s = oneLine(s)
	if isPathTarget(s) {
		return theme.FoldHome(s)
	}
	return s
}

// causeSuffix is " — <cause>", or "" when there is no cause. The cause is put
// on one line and loses a leading "<kind> failed" (the sentence says it) with
// the separator that follows it.
func causeSuffix(kind jobs.Kind, detail string) string {
	cause := oneLine(detail)
	if p := string(kind) + " failed"; len(cause) >= len(p) && strings.EqualFold(cause[:len(p)], p) {
		cause = cause[len(p):]
		if cause == "" || cause[0] == ' ' || cause[0] == ':' || cause[0] == '-' || strings.HasPrefix(cause, "—") {
			cause = strings.TrimSpace(trimSeparator(strings.TrimSpace(cause)))
		} else {
			cause = p + cause
		}
	}
	if cause == "" {
		return ""
	}
	return " — " + cause
}

// trimSeparator removes one leading "—", "--", "-" or ":".
func trimSeparator(s string) string {
	for _, sep := range []string{"—", "--", "-", ":"} {
		if rest, ok := strings.CutPrefix(s, sep); ok {
			return rest
		}
	}
	return s
}

// maxTextRunes bounds free text before it is processed.
const maxTextRunes = 512

// oneLine is the single normalization of free text (causes, targets, observed
// sentences): it keeps the first maxTextRunes runes, drops invalid UTF-8 and Unicode format
// characters (bidi overrides, zero-width), turns control characters into
// spaces and collapses whitespace runs, so the text never spans more than one
// footer line.
func oneLine(s string) string {
	if n := 0; len(s) > maxTextRunes {
		for i := range s {
			if n == maxTextRunes {
				s = s[:i]
				break
			}
			n++
		}
	}
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r):
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// maskedSuffix lists the failure kinds a winner hides, in jobs.Kinds() order.
func maskedSuffix(masked []jobs.Failure) string {
	var names []string
	for _, k := range jobs.Kinds() {
		if slices.ContainsFunc(masked, func(f jobs.Failure) bool { return f.Kind == k }) {
			names = append(names, string(k))
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return " · earlier failure: " + names[0] + " (see :jobs)"
	}
	return " · earlier failures: " + strings.Join(names, ", ") + " (see :jobs)"
}
