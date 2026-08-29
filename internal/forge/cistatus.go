package forge

// The vocabulary a backend must report in `Repository.CIStatus`.
//
// **It is GitLab's**, and the choice is historical rather than principled:
// GitLab was the only backend when the field was added, so `lastPipelineStatus`
// returned the API's own word and the explorer's CI column switched on it. By
// the time GitHub arrived the words were already load-bearing in the view.
//
// Keeping it was still the right call. GitLab's list is the larger of the two —
// eleven values against GitHub's nine, and it separates "queued" from "not
// started" in a way GitHub does not — so translating GitHub onto it loses less
// than the reverse would. And a third backend translating onto a list that
// belongs to one of the other two is no worse than translating onto a list
// invented here.
//
// What was **not** right is that the promise was only ever a doc comment. The
// GitHub backend returned GitHub's own words for six years' worth of values
// nothing in the application had a case for, and the CI column printed them
// raw: a failed build read `failu…` in orange, which is the colour of a warning
// (§1.1 D66).
const (
	CIStatusCreated            = "created"
	CIStatusWaitingForResource = "waiting_for_resource"
	CIStatusPreparing          = "preparing"
	CIStatusPending            = "pending"
	CIStatusRunning            = "running"
	CIStatusSuccess            = "success"
	CIStatusFailed             = "failed"
	CIStatusCanceled           = "canceled"
	CIStatusSkipped            = "skipped"
	CIStatusManual             = "manual"
	CIStatusScheduled          = "scheduled"
)

// CIStatuses is every value a backend may report, and therefore every value a
// view must be able to render.
//
// It exists to be walked by a test rather than read by production code: the
// defect this file answers was a promise nobody could check, and a list a test
// can iterate is the difference between a promise and a contract. A value
// missing from a view's switch now fails a test instead of appearing on screen
// truncated to six cells.
//
// The empty string is deliberately absent. It is not a status — it means the
// decoration was not asked for, there is no pipeline, or the backend has no
// equivalent — and a view renders it as an empty cell, which is a different
// question from rendering a status.
func CIStatuses() []string {
	return []string{
		CIStatusCreated,
		CIStatusWaitingForResource,
		CIStatusPreparing,
		CIStatusPending,
		CIStatusRunning,
		CIStatusSuccess,
		CIStatusFailed,
		CIStatusCanceled,
		CIStatusSkipped,
		CIStatusManual,
		CIStatusScheduled,
	}
}
