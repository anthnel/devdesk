package security

import (
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// secKinds are the operations whose activity or remembered failure an
// inventory row shows: a scan only (D5), whoever started it.
var secKinds = []jobs.Kind{jobs.KindScan}

// rowActivity resolves what one inventory row says about its activity, read
// from the router snapshot against the context it carries.
func (m Model) rowActivity(t scanTarget) components.RowActivity {
	return components.DescribeRow(components.RowQuery{
		Runs: m.jobs, Failures: m.failures, Context: m.contextName,
		Target: jobs.TargetKey(t.Name), Kinds: secKinds,
	})
}

// scanRunning reports whether a scan of the target is running now in the
// current context. A queued scan is not running.
func (m Model) scanRunning(name string) bool {
	for _, a := range jobs.ActiveOn(m.jobs, m.contextName, name, jobs.KindScan) {
		if a.Item.State == jobs.ItemRunning {
			return true
		}
	}
	return false
}
