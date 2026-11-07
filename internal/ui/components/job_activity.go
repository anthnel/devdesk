package components

import (
	"fmt"
	"strings"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// RunIndicator maps a run's state onto the activity column. It never returns
// indicator.Idle: a :jobs row always has a state.
func RunIndicator(s jobs.RunState) indicator.State {
	switch s {
	case jobs.RunRunning:
		return indicator.Running
	case jobs.RunDone:
		return indicator.Done
	case jobs.RunFailed:
		return indicator.Failed
	case jobs.RunCancelled:
		return indicator.Cancelled
	}
	return indicator.Queued
}

// ItemIndicator maps one target's state onto the activity column. It never
// returns indicator.Idle.
func ItemIndicator(s jobs.ItemState) indicator.State {
	switch s {
	case jobs.ItemRunning:
		return indicator.Running
	case jobs.ItemDone:
		return indicator.Done
	case jobs.ItemSkipped:
		return indicator.Skipped
	case jobs.ItemFailed:
		return indicator.Failed
	}
	return indicator.Queued
}

// DescribeRun is the footer sentence of a :jobs run row. Its State is
// RunIndicator(r.State()) and its sentences agree with DescribeRow.
func DescribeRun(r jobs.Run) RowActivity {
	state := RunIndicator(r.State())
	d := RowDetail{Glyph: indicator.Glyph(state, true), Target: targetDisplay(r.Label)}
	switch state {
	case indicator.Failed:
		d.Tail = failedTail(r.Kind, runCause(r))
	case indicator.Cancelled:
		d.Lead, d.Tail = kindTitle(r.Kind)+" of ", " was cancelled"
	case indicator.Done:
		d.Lead, d.Tail = kindTitle(r.Kind)+" of ", " finished"+agoSuffix(r)
	default:
		d.Lead, d.Tail = activeLead(state, r.Kind), ""
	}
	return RowActivity{State: state, Detail: d}
}

// DescribeItem is the footer sentence of one target of a run of kind k.
func DescribeItem(k jobs.Kind, it jobs.Item) RowActivity {
	state := ItemIndicator(it.State)
	d := RowDetail{Glyph: indicator.Glyph(state, true), Target: targetDisplay(it.Name())}
	switch state {
	case indicator.Failed:
		d.Tail = failedTail(k, it.Detail)
	case indicator.Skipped:
		d.Lead, d.Tail = kindTitle(k)+" of ", " skipped"+causeSuffix(k, it.Detail)
	case indicator.Done:
		d.Lead, d.Tail = kindTitle(k)+" of ", " finished"
	default:
		d.Lead = activeLead(state, k)
	}
	return RowActivity{State: state, Detail: d}
}

// activeLead is the lead-in of a running or queued row, the same words
// DescribeRow uses.
func activeLead(state indicator.State, k jobs.Kind) string {
	if state == indicator.Running {
		return k.Verb() + " "
	}
	return "Queued: " + string(k) + " "
}

// failedTail is ": last <kind> failed" followed by the cause, if any.
func failedTail(k jobs.Kind, cause string) string {
	return ": last " + string(k) + " failed" + causeSuffix(k, cause)
}

// runCause is the failed item's detail when exactly one target failed, else a
// count of the failed targets.
func runCause(r jobs.Run) string {
	var failed []jobs.Item
	for _, it := range r.Items {
		if it.State == jobs.ItemFailed {
			failed = append(failed, it)
		}
	}
	if len(failed) == 1 {
		return failed[0].Detail
	}
	return fmt.Sprintf("%d of %d targets failed", len(failed), len(r.Items))
}

// agoSuffix is " <time ago>" for a run with an end time, else "".
func agoSuffix(r jobs.Run) string {
	if ago := theme.TimeAgo(r.EndedAt); ago != "" {
		return " " + ago
	}
	return ""
}

// kindTitle is the kind with its first letter capitalized ("Scan").
func kindTitle(k jobs.Kind) string {
	s := string(k)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
