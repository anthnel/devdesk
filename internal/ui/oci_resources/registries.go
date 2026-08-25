package ociresources

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// getSelectedRegistry returns a pointer to the selected registry item, or nil —
// including inside a group, where the rows are cached members rather than
// config entries and nothing on them is editable.
func (m Model) getSelectedRegistry() *config.RegistryItem {
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return nil
	}
	return &m.registries[idx]
}

// enterSelectedGroup drills into the selected group (Rule 111: → goes down a
// level). A group with nothing discovered has no level to enter.
func (m Model) enterSelectedGroup() (tea.Model, tea.Cmd) {
	if in := m.registryDrillIn(); !in.Enabled() {
		return m, m.footer.Warn(in.Reason)
	}
	reg := m.getSelectedRegistry()
	if len(m.groupCache[reg.Slug].Members) == 0 {
		return m, m.footer.Warn("No members discovered yet — press ctrl+r to look")
	}
	m.registryGroupSlug = reg.Slug
	m.updateRegistryTable()
	m.registryTable.GotoTop()
	return m, nil
}

// leaveGroup goes back up to the registry list (Rule 111: ← and esc).
func (m Model) leaveGroup() (tea.Model, tea.Cmd) {
	if m.registryGroupSlug == "" {
		return m, nil
	}
	m.registryGroupSlug = ""
	m.updateRegistryTable()
	m.registryTable.GotoTop()
	return m, nil
}

// getSelectedRegistryIndex returns the index of the selected registry, or -1 —
// including inside a group, where the rows are cached members and carry no
// config index of their own.
func (m Model) getSelectedRegistryIndex() int {
	row, ok := m.registryTable.Selected()
	if !ok || row.config < 0 || row.config >= len(m.registries) {
		return -1
	}
	return row.config
}

// editSelectedRegistry opens a registry edit form for the selected entry
func (m Model) editSelectedRegistry() (tea.Model, tea.Cmd) {
	if act := m.registryEntryActions(); !act.Enabled() {
		return m, m.footer.Warn(act.Reason)
	}
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return m, m.footer.Warn(reasonNoRegistry)
	}
	m.registryForm = NewRegistryEditForm(idx, m.registries[idx], m.registries, m.width-2)
	return m, nil
}

// loginSelectedRegistry triggers a docker login for the selected registry
// loginSelectedRegistry opens the edit form so the user can enter a password.
// Passwords are never stored in config, so login always requires re-entry.
// toggleSelectedRegistryAuth logs in or out depending on where the row stands.
//
// The row already answers "am I logged in" — it is the Logged column, and the
// cell the spinner takes while the operation runs. Two keys separated by shift
// alone made the user answer a question the screen had already answered, and
// pressing the wrong one silently did the opposite of what was wanted.
func (m Model) toggleSelectedRegistryAuth() (tea.Model, tea.Cmd) {
	if act := m.registryEntryActions(); !act.Enabled() {
		return m, m.footer.Warn(act.Reason)
	}
	reg := m.getSelectedRegistry()
	if m.registryLoginStatus[reg.URL] {
		return m.logoutSelectedRegistry()
	}
	return m.loginSelectedRegistry()
}

func (m Model) loginSelectedRegistry() (tea.Model, tea.Cmd) {
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return m, nil
	}
	reg := m.registries[idx]
	if !config.UsesCredentials(reg.AuthMode) {
		return m, m.footer.Warn("This registry is marked anonymous")
	}
	m.registryForm = NewRegistryEditForm(idx, reg, m.registries, m.width-2)
	return m, nil
}

// refreshRegistries reloads the registry list and login status, and re-runs
// discovery for the selected group.
//
// Discovery is explicit rather than automatic (§3.8, decision 3): it costs a
// network round trip against a repository manager, and running it on every
// browser open is what made the browser wait. The Members column is what says
// how old the answer it replaces was.
func (m Model) refreshRegistries() (tea.Model, tea.Cmd) {
	m.registries = m.config.Registry.Registries

	cmds := []tea.Cmd{m.registryLoginStatusCmd()}
	if cmd := m.refreshSelectedGroupCmd(); cmd != nil {
		cmds = append(cmds, cmd, m.spinner.Tick)
	}
	m.updateRegistryTable()
	return m, tea.Batch(cmds...)
}

// refreshSelectedGroupCmd starts a discovery for the selected row when it is a
// group that is not already being refreshed. Returns nil otherwise, which is
// the ordinary case: a plain registry has no members to discover.
func (m *Model) refreshSelectedGroupCmd() tea.Cmd {
	reg := m.getSelectedRegistry()
	if reg == nil || reg.Kind != config.KindGroup || m.refreshingGroups[reg.Slug] {
		return nil
	}
	m.refreshingGroups[reg.Slug] = true
	return detectRegistryGroupCmd(*reg, "")
}

// deleteSelectedRegistry shows a confirm modal for registry removal
func (m Model) deleteSelectedRegistry() (tea.Model, tea.Cmd) {
	if act := m.registryEntryActions(); !act.Enabled() {
		return m, m.footer.Warn(act.Reason)
	}
	reg := m.getSelectedRegistry()
	m.pendingAction = "delete-registry"
	m.confirmModal = sharedcomponents.NewConfirmModal(
		"Remove Registry", fmt.Sprintf("Remove registry '%s'?", registryRef(reg.URL, reg.RepoPrefix)))
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
		return m, m.footer.Error("Failed to save registry — check logs")
	}
	m.footer.Clear()
	if msg.Password != "" && config.UsesCredentials(msg.Item.AuthMode) {
		m.registryTable.MarkBusy(msg.Item.URL, "Logging in to "+browserAlias(msg.Item))
		return m, tea.Batch(registryLoginCmd(msg.Item.URL, msg.Item.Username, msg.Password), m.busyTick())
	}
	return m, nil
}

// handleRegistryLoginComplete processes the result of a docker login operation.
// registryLoginStatusCmd builds a command that checks login status for all configured registries.
func (m Model) registryLoginStatusCmd() tea.Cmd {
	urls := make([]string, 0, len(m.registries))
	for _, reg := range m.registries {
		if config.UsesCredentials(reg.AuthMode) {
			urls = append(urls, reg.URL)
		}
	}
	return checkRegistryLoginStatusCmd(urls)
}

func (m Model) handleRegistryLoginComplete(msg RegistryLoginCompleteMsg) (tea.Model, tea.Cmd) {
	m.registryTable.ClearBusy(msg.RegistryURL)
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] login %s: %v", msg.RegistryURL, msg.Err)
		m.registryLoginStatus[msg.RegistryURL] = false
		m.updateRegistryTable()
		return m, m.footer.Error("Login failed — check logs")
	}
	log.Printf("INFO [oci_resources] login successful: %s", msg.RegistryURL)
	m.footer.Clear()
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
	if m.registryTable.IsBusy(reg.URL) {
		return m, m.footer.Warn(busyMessage)
	}
	m.registryTable.MarkBusy(reg.URL, "Logging out of "+browserAlias(*reg))
	return m, tea.Batch(registryLogoutCmd(reg.URL), m.busyTick())
}

func (m Model) handleRegistryLogoutComplete(msg RegistryLogoutCompleteMsg) (tea.Model, tea.Cmd) {
	m.registryTable.ClearBusy(msg.RegistryURL)
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] logout %s: %v", msg.RegistryURL, msg.Err)
		return m, m.footer.Error("Logout failed — check logs")
	}
	log.Printf("INFO [oci_resources] logout successful: %s", msg.RegistryURL)
	m.footer.Clear()
	// Do NOT call registryLoginStatusCmd here: docker logout docker.io may not remove
	// the https://index.docker.io/v1/ key from config.json, causing the file-check to
	// override this correct false status back to true.
	m.registryLoginStatus[msg.RegistryURL] = false
	m.updateRegistryTable()
	return m, nil
}
