package templates

import (
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// syncingText is the Synced cell while a sync of the row runs. Plain text:
// the colour goes through the column's Style (Rule 122).
const syncingText = "syncing"

// rowQuery says what a row answers for: a sync on its slug and a scan on its
// scan directory. Only those two operations are shown on a template's row.
func (m Model) rowQuery(e template.Entry) sharedcomponents.RowQuery {
	q := sharedcomponents.RowQuery{
		Runs: m.jobs, Failures: m.failures, Context: m.contextName,
		Target: e.Slug, Kinds: []jobs.Kind{jobs.KindSync},
	}
	if dir, err := template.ScanDir(e.Slug); err == nil {
		q.Extra = []sharedcomponents.RowTarget{{Target: dir, Kinds: []jobs.Kind{jobs.KindScan}}}
	}
	return q
}

// rowActivity resolves what one row says about its activity.
func (m Model) rowActivity(e template.Entry) sharedcomponents.RowActivity {
	return sharedcomponents.DescribeRow(m.rowQuery(e))
}

// syncing reports whether a sync of the template is running (not merely queued).
func (m Model) syncing(slug string) bool {
	for _, a := range jobs.ActiveOn(m.jobs, m.contextName, slug, jobs.KindSync) {
		if a.Item.State == jobs.ItemRunning {
			return true
		}
	}
	return false
}

// ActivityTables returns the read-only face of the view's table, keyed by the
// name the activity-column guard (internal/app) knows it by. Tests only.
func (m Model) ActivityTables() map[string]datatable.ActivityTable {
	return map[string]datatable.ActivityTable{"templates": &m.table}
}
