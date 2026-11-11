package jobs

// ProgressState is the summarized state of the registry, in the order of
// precedence of what a reader should be told.
type ProgressState int

const (
	// ProgressIdle means nothing runs and no failure is unread.
	ProgressIdle ProgressState = iota
	// ProgressIndeterminate means something runs but no fair percentage exists.
	ProgressIndeterminate
	// ProgressPercent means every running run is measured: Percent is valid.
	ProgressPercent
	// ProgressWarning means a cancelled run has not settled yet.
	ProgressWarning
	// ProgressError means a failure has not been read yet.
	ProgressError
)

// Progress is the whole registry summarized for one reader. It holds only
// comparable fields so callers can detect a change with ==.
type Progress struct {
	State ProgressState
	// Percent is 0..100. It is meaningful when Measured, and 100 for a
	// ProgressWarning that has no measure. It is not forced to 100 in
	// ProgressError: it keeps the measure (if any) of the runs still going, and
	// a presentation that wants a full bar draws it itself.
	Percent int
	// Measured reports that every unsettled run has a known size, so Percent is
	// the weighted completion of all of them.
	Measured bool
	// Running counts the unsettled runs, Stopping those among them that were
	// asked to stop, Unread the failures above the read watermark.
	Running  int
	Stopping int
	Unread   int
}

// measurable reports whether a run has a known size: it is sealed and holds at
// least two targets. A one-target run has no halfway to report.
func measurable(r Run) bool { return !r.Open() && r.Total() >= 2 }

// Summarize reduces the runs and failures of every context to one Progress.
// Failures with a Seq above readThrough are unread. It is pure: no clock, no
// registry, no filtering by context.
func Summarize(runs []Run, failures []Failure, readThrough uint64) Progress {
	var p Progress
	var done, total int
	allMeasured := true
	for _, run := range runs {
		if run.Finished() {
			continue
		}
		p.Running++
		if run.Cancelled() {
			p.Stopping++
		}
		if measurable(run) {
			done += run.Done()
			total += run.Total()
		} else {
			allMeasured = false
		}
	}
	for _, f := range failures {
		if f.Seq > readThrough {
			p.Unread++
		}
	}

	if p.Running > 0 && allMeasured {
		p.Measured = true
		p.Percent = 100 * done / total
	}

	switch {
	case p.Unread > 0:
		p.State = ProgressError
	case p.Stopping > 0:
		p.State = ProgressWarning
		if !p.Measured {
			p.Percent = 100
		}
	case p.Running == 0:
		p.State = ProgressIdle
	case p.Measured:
		p.State = ProgressPercent
	default:
		p.State = ProgressIndeterminate
	}
	return p
}

// LatestFailureSeq returns the highest Seq among failures, 0 for none. It is
// the watermark a reader stores once it has shown every failure.
func LatestFailureSeq(failures []Failure) uint64 {
	var latest uint64
	for _, f := range failures {
		latest = max(latest, f.Seq)
	}
	return latest
}

// FailuresIn returns a copy of the failures of contextName, in input order.
func FailuresIn(failures []Failure, contextName string) []Failure {
	var out []Failure
	for _, f := range failures {
		if f.Context == contextName {
			out = append(out, f)
		}
	}
	return out
}
