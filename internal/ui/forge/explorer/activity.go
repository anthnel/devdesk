package explorer

import (
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// rowKinds are the operations a tree row can show: a delete on any row, a
// create only on a placeholder (a created node is a real node afterwards).
func rowKinds(node *TreeNode) []jobs.Kind {
	if node.Creating {
		return []jobs.Kind{jobs.KindDelete, jobs.KindCreate}
	}
	return []jobs.Kind{jobs.KindDelete}
}

// rowActivity resolves what one tree row says about its activity, read from
// the router snapshot against the context it carries.
func (m Model) rowActivity(node *TreeNode) components.RowActivity {
	return components.DescribeRow(components.RowQuery{
		Runs: m.jobs, Failures: m.failures, Context: m.contextName,
		Target: node.FullPath, Kinds: rowKinds(node),
	})
}
