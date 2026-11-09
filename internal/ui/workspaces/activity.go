package workspaces

import (
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// wsKinds are the operations this view can start, hence the only ones whose
// activity or remembered failure a row shows.
var wsKinds = []jobs.Kind{jobs.KindScan, jobs.KindSync, jobs.KindDelete}

// rowTargets returns the registry targets a row answers for: its own path and,
// for a directory that is not a git repository, the repositories under it (S
// on such a directory scans each of them).
func (m Model) rowTargets(e Entry) []string {
	targets := []string{e.Path}
	if !e.IsGitRepo {
		targets = append(targets, e.SubRepoPaths...)
	}
	return targets
}

// rowActivity resolves what one row says about its activity: the strongest
// state over its targets (running, then queued, then failed), described by the
// target that wins. Ties keep the first target.
func (m Model) rowActivity(e Entry) components.RowActivity {
	var best components.RowActivity
	bestRank := -1
	for _, target := range m.rowTargets(e) {
		a := components.DescribeRow(components.RowQuery{
			Runs: m.jobs, Failures: m.failures, Context: m.contextName,
			Target: jobs.TargetKey(target), Kinds: wsKinds,
		})
		if r := activityRank(a.State); r > bestRank {
			best, bestRank = a, r
		}
	}
	return best
}

// activityRank orders the states a row can show; Idle is the floor.
func activityRank(s indicator.State) int {
	switch s {
	case indicator.Running:
		return 3
	case indicator.Queued:
		return 2
	case indicator.Failed:
		return 1
	}
	return 0
}
