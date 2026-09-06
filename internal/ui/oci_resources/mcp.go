package ociresources

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
)

// What an agent asks this view to do (§3.61). See workspaces/mcp.go for why the
// request is a message to the view rather than work the router builds itself.

// ImageScanRequestedMsg asks for a scan of the named images, or of every
// unscanned one when none are named.
type ImageScanRequestedMsg struct {
	// Images are references as images_list reports them. Empty means every
	// image that has never been scanned, which is what `A` does.
	Images     []string
	Invocation string
}

// ImagePullRequestedMsg asks for one image to be pulled.
type ImagePullRequestedMsg struct {
	Image      string
	Invocation string
}

// handleImageScanRequested is `S` and `A` reached from an agent.
//
// Like the workspaces one it does not purge: the purging variant is behind a
// modal with a checkbox, and a tool call has no modal (§3.26).
func (m Model) handleImageScanRequested(msg ImageScanRequestedMsg) (tea.Model, tea.Cmd) {
	if cmd, waiting := m.deferUntilListed(msg); waiting {
		return m, cmd
	}

	local := make(map[string]string, len(m.images))
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		local[img.Name()] = img.ScanTarget()
	}

	var queue []imageScanJob
	if len(msg.Images) == 0 {
		for _, img := range m.images {
			if img.Repository == "<none>" {
				continue
			}
			name := img.Name()
			if _, scanned := m.scanCache[name]; !scanned && !m.scanningImage(name) {
				queue = append(queue, imageScanJob{Name: name, Target: img.ScanTarget()})
			}
		}
		if len(queue) == 0 {
			return m, jobs.Refuse(msg.Invocation, "Every image on this machine has been scanned already")
		}
	} else {
		for _, name := range msg.Images {
			target, ok := local[name]
			if !ok {
				return m, jobs.Refuse(msg.Invocation, "No image "+name+" on this machine — images_list is what is here")
			}
			if !m.scanningImage(name) {
				queue = append(queue, imageScanJob{Name: name, Target: target})
			}
		}
		if len(queue) == 0 {
			return m, jobs.Refuse(msg.Invocation, reasonImageScanned)
		}
	}

	return m, jobs.WithInvocation(msg.Invocation, jobs.Start(
		m.scanRun(jobNames(queue)),
		batchScanCmd(queue, scan.OptionsFromConfig(m.config)),
	))
}

// handleImagePullRequested is `G` reached from an agent.
//
// It reuses the pull the registry browser already emits, so a pull started by
// an agent is the same run, the same spinner in the Images table and the same
// entry in `:jobs` as one started by a key (§3.60).
func (m Model) handleImagePullRequested(msg ImagePullRequestedMsg) (tea.Model, tea.Cmd) {
	if msg.Image == "" {
		return m, jobs.Refuse(msg.Invocation, "No image named")
	}
	// A pull names an image that is on a registry, not one this view lists, so
	// it does not wait for the listing the way a scan does.
	if m.pullingImage(msg.Image) {
		return m, jobs.Refuse(msg.Invocation, "This image is already being pulled")
	}

	return m, jobs.WithInvocation(msg.Invocation,
		jobs.Start(m.pullRun(msg.Image), pullOneImageCmd(msg.Image)))
}

// deferUntilListed holds a request that arrived before the daemon had answered.
//
// The router builds this view on demand, and building it is not filling it:
// `m.images` is empty until fetchImages returns, so a scan request served
// against it would refuse with "No image ... on this machine" — a statement
// about the daemon that this view is in no position to make yet. See
// workspaces/mcp.go for the same reasoning in full.
func (m *Model) deferUntilListed(msg tea.Msg) (tea.Cmd, bool) {
	if m.listed {
		return nil, false
	}
	m.pendingRequests = append(m.pendingRequests, msg)
	if len(m.pendingRequests) > 1 {
		return nil, true
	}
	return fetchImages(), true
}

// drainPendingRequests re-emits what waited for the listing, as messages, so a
// request that waited takes the same path as one that did not.
func (m *Model) drainPendingRequests() tea.Cmd {
	if len(m.pendingRequests) == 0 {
		return nil
	}
	queued := m.pendingRequests
	m.pendingRequests = nil

	cmds := make([]tea.Cmd, 0, len(queued))
	for _, msg := range queued {
		cmds = append(cmds, func() tea.Msg { return msg })
	}
	return tea.Batch(cmds...)
}

// refusePendingRequests answers everything that waited for a listing that never
// came. An agent would otherwise hang until its own timeout, with the reason in
// a log it cannot see.
func (m *Model) refusePendingRequests(reason string) tea.Cmd {
	if len(m.pendingRequests) == 0 {
		return nil
	}
	queued := m.pendingRequests
	m.pendingRequests = nil

	cmds := make([]tea.Cmd, 0, len(queued))
	for _, msg := range queued {
		if req, ok := msg.(ImageScanRequestedMsg); ok {
			cmds = append(cmds, jobs.Refuse(req.Invocation, reason))
		}
	}
	return tea.Batch(cmds...)
}
