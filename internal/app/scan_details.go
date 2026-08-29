package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// Rule 126: opening a scanned item reads the cache; it never triggers a scan.
// The full result lives in its own file, so the metadata entry can outlive it —
// every load here has a fallback for that.

// handleWorkspaceScanDetails loads a cached workspace scan result from disk.
func (a *App) handleWorkspaceScanDetails(msg workspaces.ScanDetailsRequestMsg) (tea.Model, tea.Cmd) {
	repoPath := msg.RepoPath
	return a, func() tea.Msg {
		result, err := cache.LoadWorkspaceScanResult(repoPath)
		return WorkspaceScanResultLoadedMsg{Result: result, RepoPath: repoPath, Err: err}
	}
}

// handleScanDetailsRequest loads a cached image scan result from disk.
func (a *App) handleScanDetailsRequest(msg ociresources.ScanDetailsRequestMsg) (tea.Model, tea.Cmd) {
	log.Printf("Scan details requested for image: %s", msg.ImageName)
	imageName := msg.ImageName
	return a, func() tea.Msg {
		result, err := cache.LoadImageScanResult(imageName)
		return ImageScanResultLoadedMsg{Result: result, ImageName: imageName, Err: err}
	}
}

// A missing result file is not a dead end, and it is not the security view's
// problem either: the list the user pressed enter in is where the target lives
// and where the scan that would replace it runs. So the fallback stays put and
// asks that list to rescan, rather than opening a security view on nothing.
//
// It used to open the scan form with the target filled in. With the form gone
// there is nothing to fill in — every option comes from the configuration view
// — so what is left to carry is the target's name.

// handleWorkspaceScanResultLoaded opens the security view on the loaded result,
// or asks the workspaces list to rescan the repository.
func (a *App) handleWorkspaceScanResultLoaded(msg WorkspaceScanResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		log.Printf("Workspace scan result not on disk for %s (rescanning): %v", msg.RepoPath, msg.Err)
		return a.rescanInOrigin(command.ViewWorkspaces, workspaces.ScanRequestMsg{TargetPath: msg.RepoPath})
	}
	return a, a.openSecurityView(
		security.NewWithPreloadedResult(a.config, a.sharedState.Secrets.Storage, msg.Result),
		command.ViewWorkspaces,
	)
}

// handleImageScanResultLoaded opens the security view on the loaded result, or
// asks the images list to rescan the image.
func (a *App) handleImageScanResultLoaded(msg ImageScanResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		log.Printf("Image scan result not on disk for %s (rescanning): %v", msg.ImageName, msg.Err)
		return a.rescanInOrigin(command.ViewOCIResources, ociresources.ScanRequestMsg{ImageName: msg.ImageName})
	}
	return a, a.openSecurityView(
		security.NewWithPreloadedResult(a.config, a.sharedState.Secrets.Storage, msg.Result),
		command.ViewOCIResources,
	)
}

// rescanInOrigin hands a scan request to the list the target belongs to, and
// stays there. The view is created if it is not open — the request can arrive
// from a cached row whose list was dropped on a context switch.
func (a *App) rescanInOrigin(view command.ViewType, request tea.Msg) (tea.Model, tea.Cmd) {
	a.createView(view)
	held, ok := a.views[view]
	if !ok {
		return a, nil
	}
	a.currentView = view
	updatedView, cmd := held.Update(request)
	a.views[view] = updatedView
	return a, tea.Batch(cmd, a.requestResize())
}

// openSecurityView installs a freshly built security view, records where esc
// should return to, and lays it out.
func (a *App) openSecurityView(view security.Model, origin command.ViewType) tea.Cmd {
	view.OriginView = origin
	a.views[command.ViewSecurity] = view
	a.currentView = command.ViewSecurity
	return tea.Batch(view.Init(), a.requestResize())
}

// handleLaunchScan is gone with the form. It carried the options the form had
// collected back to the view that would run the scan; the options come from the
// configuration view now, so there is nothing to carry — rescanInOrigin names a
// target and that is all.
