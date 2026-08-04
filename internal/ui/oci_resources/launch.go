package ociresources

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
)

// handleImageExposedPorts opens the launch form once exposed ports are fetched
func (m Model) handleImageExposedPorts(msg ImageExposedPortsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] exposed ports %s: %v", msg.ImageName, msg.Err)
	}
	m.launchForm = NewLaunchForm(msg.ImageName, msg.Ports, m.networkTable.Items(), m.width-2)
	return m, loadLaunchOptionsCmd(msg.ImageName)
}

// handleLaunchOptionsCacheLoaded applies cached options to the launch form if it is still open.
func (m Model) handleLaunchOptionsCacheLoaded(msg LaunchOptionsCacheLoadedMsg) (tea.Model, tea.Cmd) {
	if m.launchForm == nil || msg.Entry == nil || m.launchForm.image != msg.ImageName {
		return m, nil
	}
	m.launchForm.ApplyCached(*msg.Entry)
	// Trigger entrypoint verification if a cached entrypoint is present
	if msg.Entry.Entrypoint != "" {
		m.launchForm.verify = verifyChecking
		m.launchForm.verifySeq++
		return m, verifyEntrypointCmd(m.launchForm.verifySeq, m.launchForm.image, msg.Entry.Entrypoint)
	}
	return m, nil
}

// handleContainerLaunchComplete processes container launch results
func (m Model) handleContainerLaunchComplete(msg ContainerLaunchCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] launch container: %v", msg.Err)
		m.errorMsg = "Failed to launch container — check logs"
		m.lastLaunchOpts = nil
		m.lastLaunchImage = ""
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	if m.lastLaunchOpts != nil {
		saveCmd := saveLaunchOptionsCmd(m.lastLaunchImage, *m.lastLaunchOpts)
		m.lastLaunchOpts = nil
		m.lastLaunchImage = ""
		return m, saveCmd
	}
	return m, nil
}

// handleClipboardCopy processes the result of copying the docker command to clipboard (Rule 128).
// clearInfoMsgCmd clears both errorMsg and infoMsg after 3 seconds — correct for both branches.
func (m Model) handleClipboardCopy(msg ClipboardCopyMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] clipboard copy: %v", msg.Err)
		m.errorMsg = "Failed to copy command to clipboard"
	} else {
		m.infoMsg = "Docker command copied to clipboard"
	}
	return m, clearInfoMsgCmd()
}

// handleLaunchFormSubmit dispatches the container launch.
// handleLaunchFormSubmitWithCache snapshots form state into the pending save
// fields before delegating to handleLaunchFormSubmit.
func (m Model) handleLaunchFormSubmitWithCache(msg LaunchFormSubmitMsg) (tea.Model, tea.Cmd) {
	if m.launchForm != nil {
		entry := m.launchForm.ToCacheEntry()
		m.lastLaunchOpts = &entry
		m.lastLaunchImage = m.launchForm.image
	}
	m.launchForm = nil
	return m.handleLaunchFormSubmit(msg)
}

// Interactive (-it) containers require a real TTY, so they are handed off via
// tea.ExecProcess instead of the background launchContainerCmd.
func (m Model) handleLaunchFormSubmit(msg LaunchFormSubmitMsg) (tea.Model, tea.Cmd) {
	opts := msg.Opts
	if opts.Interactive && opts.TTY {
		cmd, err := docker.BuildLaunchCmd(opts)
		if err != nil {
			log.Printf("ERROR [oci_resources] build launch cmd: %v", err)
			m.errorMsg = "Failed to launch container — check logs"
			return m, clearInfoMsgCmd()
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				log.Printf("ERROR [oci_resources] interactive container exit: %v", err)
			}
			return ContainerLaunchCompleteMsg{Err: err}
		})
	}
	return m, launchContainerCmd(opts)
}

// handleResourceFormSubmit handles resource form submission (network or volume creation)
func (m Model) handleResourceFormSubmit(msg ResourceFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.resourceForm = nil
	if msg.Kind == resourceFormNetwork {
		return m, createNetworkCmd(msg.Name, msg.Driver)
	}
	return m, createVolumeCmd(msg.Name, msg.Driver)
}
