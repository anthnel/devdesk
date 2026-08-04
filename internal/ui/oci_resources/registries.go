package ociresources

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// getSelectedRegistry returns a pointer to the selected registry item or nil
func (m Model) getSelectedRegistry() *config.RegistryItem {
	cursor := m.registryTable.Cursor()
	if cursor < 0 || cursor >= len(m.registries) {
		return nil
	}
	return &m.registries[cursor]
}

// getSelectedRegistryIndex returns the index of the selected registry or -1
func (m Model) getSelectedRegistryIndex() int {
	cursor := m.registryTable.Cursor()
	if cursor < 0 || cursor >= len(m.registries) {
		return -1
	}
	return cursor
}

// editSelectedRegistry opens a registry edit form for the selected entry
func (m Model) editSelectedRegistry() (tea.Model, tea.Cmd) {
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return m, nil
	}
	m.registryForm = NewRegistryEditForm(idx, m.registries[idx], m.registries, m.width-2)
	return m, nil
}

// loginSelectedRegistry triggers a docker login for the selected registry
// loginSelectedRegistry opens the edit form so the user can enter a password.
// Passwords are never stored in config, so login always requires re-entry.
func (m Model) loginSelectedRegistry() (tea.Model, tea.Cmd) {
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return m, nil
	}
	reg := m.registries[idx]
	if !reg.AuthEnabled {
		m.errorMsg = "Auth not enabled for this registry"
		return m, clearInfoMsgCmd()
	}
	m.registryForm = NewRegistryEditForm(idx, reg, m.registries, m.width-2)
	return m, nil
}

// deleteSelectedRegistry shows a confirm modal for registry removal
func (m Model) deleteSelectedRegistry() (tea.Model, tea.Cmd) {
	reg := m.getSelectedRegistry()
	if reg == nil {
		return m, nil
	}
	m.pendingAction = "delete-registry"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Registry", fmt.Sprintf("Remove registry '%s'?", reg.URL))
	return m, nil
}

// handleRegistryFormSubmit saves a new or edited registry to the config and optionally logs in.
func (m Model) handleRegistryFormSubmit(msg RegistryFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.registryForm = nil
	if msg.Index < 0 {
		m.config.Registry.Registries = append(m.config.Registry.Registries, msg.Item)
	} else if msg.Index < len(m.config.Registry.Registries) {
		m.config.Registry.Registries[msg.Index] = msg.Item
	}
	m.registries = m.config.Registry.Registries
	m.updateRegistryTable()
	if err := config.Save(m.config); err != nil {
		log.Printf("ERROR [oci_resources] save registry config: %v", err)
		m.errorMsg = "Failed to save registry — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	if msg.Password != "" && msg.Item.AuthEnabled {
		return m, registryLoginCmd(msg.Item.URL, msg.Item.Username, msg.Password)
	}
	return m, nil
}

// handleRegistryLoginComplete processes the result of a docker login operation.
// registryLoginStatusCmd builds a command that checks login status for all configured registries.
func (m Model) registryLoginStatusCmd() tea.Cmd {
	urls := make([]string, 0, len(m.registries))
	for _, reg := range m.registries {
		if reg.AuthEnabled {
			urls = append(urls, reg.URL)
		}
	}
	return checkRegistryLoginStatusCmd(urls)
}

func (m Model) handleRegistryLoginComplete(msg RegistryLoginCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] login %s: %v", msg.RegistryURL, msg.Err)
		m.errorMsg = "Login failed — check logs"
		m.registryLoginStatus[msg.RegistryURL] = false
		m.updateRegistryTable()
		return m, clearInfoMsgCmd()
	}
	log.Printf("INFO [oci_resources] login successful: %s", msg.RegistryURL)
	m.errorMsg = ""
	// Optimistic update + re-check from disk to confirm credential helper cases
	m.registryLoginStatus[msg.RegistryURL] = true
	m.updateRegistryTable()
	return m, m.registryLoginStatusCmd()
}

// logoutSelectedRegistry triggers a docker logout for the selected registry
func (m Model) logoutSelectedRegistry() (tea.Model, tea.Cmd) {
	reg := m.getSelectedRegistry()
	if reg == nil {
		return m, nil
	}
	return m, registryLogoutCmd(reg.URL)
}

func (m Model) handleRegistryLogoutComplete(msg RegistryLogoutCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] logout %s: %v", msg.RegistryURL, msg.Err)
		m.errorMsg = "Logout failed — check logs"
		return m, clearInfoMsgCmd()
	}
	log.Printf("INFO [oci_resources] logout successful: %s", msg.RegistryURL)
	m.errorMsg = ""
	// Do NOT call registryLoginStatusCmd here: docker logout docker.io may not remove
	// the https://index.docker.io/v1/ key from config.json, causing the file-check to
	// override this correct false status back to true.
	m.registryLoginStatus[msg.RegistryURL] = false
	m.updateRegistryTable()
	return m, nil
}
