package ociresources

import (
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// ociKinds are the operations whose activity or remembered failure an Images
// row shows: a scan or a pull, whoever started it (D5).
var ociKinds = []jobs.Kind{jobs.KindScan, jobs.KindPull}

// rowActivity resolves what one Images row says about its activity, read from
// the router snapshot against the context it carries. The identity is the raw
// reference, never a canonical form. A removal in flight is this view's own
// observation: it is reported as Running with its label.
func (m Model) rowActivity(r imageRow) components.RowActivity {
	q := components.RowQuery{
		Runs: m.jobs, Failures: m.failures, Context: m.contextName,
		Target: jobs.TargetKey(r.RawName), Kinds: ociKinds,
	}
	if label, ok := m.imageTable.BusyLabel(r.Image.ID); ok {
		q.Observed = indicator.Running
		q.ObservedText = label
	}
	return components.DescribeRow(q)
}

// scanRunning reports whether a scan of the image is running now in the
// current context. A queued scan is not running.
func (m Model) scanRunning(name string) bool { return m.runningOn(name, jobs.KindScan) }

// pullRunning reports whether a pull of the image is running now in the
// current context. A queued pull is not running.
func (m Model) pullRunning(name string) bool { return m.runningOn(name, jobs.KindPull) }

func (m Model) runningOn(name string, kind jobs.Kind) bool {
	for _, a := range jobs.ActiveOn(m.jobs, m.contextName, name, kind) {
		if a.Item.State == jobs.ItemRunning {
			return true
		}
	}
	return false
}

// ActivityTables returns the read-only face of the view's table, keyed by the
// name the activity-column guard (internal/app) knows it by. Tests only.
func (m Model) ActivityTables() map[string]datatable.ActivityTable {
	return map[string]datatable.ActivityTable{"oci/images": &m.imageTable}
}
