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

// handleWorkspaceScanResultLoaded opens the security view on the loaded result.
// When the result file is gone the view opens on its form with the repository
// filled in, so the user can rescan rather than face a dead end.
func (a *App) handleWorkspaceScanResultLoaded(msg WorkspaceScanResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		log.Printf("Workspace scan result not on disk for %s (fallback to input): %v", msg.RepoPath, msg.Err)
		return a, a.openSecurityView(
			security.NewWithTargetReturnToWorkspaces(a.config, msg.RepoPath),
			command.ViewWorkspaces,
		)
	}
	return a, a.openSecurityView(
		security.NewWithPreloadedResult(a.config, msg.Result),
		command.ViewWorkspaces,
	)
}

// handleImageScanResultLoaded opens the security view on the loaded result.
// When the result file is gone — an image scanned before results were persisted
// — the scan is started again rather than reported as empty.
func (a *App) handleImageScanResultLoaded(msg ImageScanResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		log.Printf("Image scan result not on disk for %s (fallback to scan): %v", msg.ImageName, msg.Err)
		cmd := a.openSecurityView(
			security.NewWithImageTarget(a.config, msg.ImageName, false),
			command.ViewOCIResources,
		)
		return a, tea.Batch(cmd, func() tea.Msg { return security.StartScanMsg{} })
	}
	return a, a.openSecurityView(
		security.NewWithPreloadedResult(a.config, msg.Result),
		command.ViewOCIResources,
	)
}

// openSecurityView installs a freshly built security view, records where esc
// should return to, and lays it out.
func (a *App) openSecurityView(view security.Model, origin command.ViewType) tea.Cmd {
	view.OriginView = origin
	a.views[command.ViewSecurity] = view
	a.currentView = command.ViewSecurity
	return tea.Batch(view.Init(), a.requestResize())
}

// routeToSecurityView forwards a message to the security view even when it is
// not on screen, for the same reason as routeToOCIImagesView below: a scan
// started there has to finish there.
func (a *App) routeToSecurityView(msg tea.Msg) (tea.Model, tea.Cmd) {
	view, ok := a.views[command.ViewSecurity]
	if !ok {
		return a, nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[command.ViewSecurity] = updatedView
	return a, cmd
}

// routeToOCIImagesView forwards a message to the OCI view even when it is not
// on screen, so a scan started there keeps progressing while the user is
// elsewhere.
func (a *App) routeToOCIImagesView(msg tea.Msg) (tea.Model, tea.Cmd) {
	view, ok := a.views[command.ViewOCIResources]
	if !ok {
		return a, nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[command.ViewOCIResources] = updatedView
	return a, cmd
}

// handleLaunchScan switches to the OCI view and hands it the scan the security
// view delegated back.
func (a *App) handleLaunchScan(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, exists := a.views[command.ViewOCIResources]; !exists {
		a.createView(command.ViewOCIResources)
	}
	a.currentView = command.ViewOCIResources

	view, ok := a.views[command.ViewOCIResources]
	if !ok {
		return a, nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[command.ViewOCIResources] = updatedView
	return a, tea.Batch(cmd, a.requestResize())
}
