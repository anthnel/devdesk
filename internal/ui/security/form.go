package security

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
)

// totalFields returns the number of form fields
func (m Model) totalFields() int {
	return 13 // 0-6 (left) + 7-11 (right, with ignoreEOL at 9) + 12 (start button)
}

// isServerMode returns true when a Trivy server URL is configured.
func (m Model) isServerMode() bool {
	return strings.TrimSpace(m.trivyServerInput.Value()) != ""
}

// isServerIncompatibleField returns true for scan options that are not supported
// by the Trivy client-server protocol (misconfig=4, license=5, sbom=6).
func (m Model) isServerIncompatibleField(fieldIdx int) bool {
	return fieldIdx == 4 || fieldIdx == 5 || fieldIdx == 6
}

// isFieldSkipped returns true when a field should be bypassed during Tab navigation.
func (m Model) isFieldSkipped(fieldIdx int) bool {
	return m.isServerMode() && m.isServerIncompatibleField(fieldIdx)
}

// nextField returns the next focusable field index, skipping disabled fields.
func (m Model) nextField() int {
	total := m.totalFields()
	next := (m.focusedField + 1) % total
	for m.isFieldSkipped(next) {
		next = (next + 1) % total
	}
	return next
}

// prevField returns the previous focusable field index, skipping disabled fields.
func (m Model) prevField() int {
	total := m.totalFields()
	prev := (m.focusedField - 1 + total) % total
	for m.isFieldSkipped(prev) {
		prev = (prev - 1 + total) % total
	}
	return prev
}

// applyServerModeConstraints auto-disables options incompatible with Trivy server mode
// and persists the updated config. No-op when not in server mode.
func (m *Model) applyServerModeConstraints() {
	if !m.isServerMode() {
		return
	}
	m.enableMisconfig = false
	m.enableLicense = false
	m.generateSBOM = false
	m.saveOptionsToConfig()
}

// isTextInputField returns true if the field index is a text input
func (m Model) isTextInputField(idx int) bool {
	return idx == 1 || idx == 7 || idx == 10
}

// blurAllTextInputs removes focus from all text inputs
func (m *Model) blurAllTextInputs() {
	m.targetInput.Blur()
	m.trivyServerInput.Blur()
	m.gitleaksConfigInput.Blur()
}

// saveOptionsToConfig syncs all form options to config and persists to disk.
func (m Model) saveOptionsToConfig() {
	m.config.Scan.EnableVuln = m.enableVuln
	m.config.Scan.EnableSecret = m.enableSecret
	m.config.Scan.EnableMisconfig = m.enableMisconfig
	m.config.Scan.EnableLicense = m.enableLicense
	m.config.Scan.GenerateSBOM = m.generateSBOM
	m.config.Scan.IgnoreUnfixed = m.ignoreUnfixed
	m.config.Scan.IgnoreEOL = m.ignoreEOL
	m.config.Scan.GitleaksHistory = m.gitleaksHistory
	m.config.Scan.TrivyServer = m.trivyServerInput.Value()
	m.config.Scan.GitleaksConfig = m.gitleaksConfigInput.Value()
	_ = config.Save(m.config)
}

// focusTextField focuses the text input for the given field index
func (m *Model) focusTextField(idx int) {
	switch idx {
	case 1:
		m.targetInput.Focus()
	case 7:
		m.trivyServerInput.Focus()
	case 10:
		m.gitleaksConfigInput.Focus()
	}
}

// handleInputState processes input in form state
func (m Model) handleInputState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle text input fields (1=target, 7=trivy server, 10=gitleaks config)
	if m.isTextInputField(m.focusedField) {
		switch msg.String() {
		case "down":
			switch m.focusedField {
			case 7:
				m.applyServerModeConstraints()
			case 10:
				m.saveOptionsToConfig()
			}
			m.blurAllTextInputs()
			m.focusedField = m.nextField()
			if m.isTextInputField(m.focusedField) {
				m.focusTextField(m.focusedField)
			}
			return m, nil
		case "up":
			switch m.focusedField {
			case 7:
				m.applyServerModeConstraints()
			case 10:
				m.saveOptionsToConfig()
			}
			m.blurAllTextInputs()
			m.focusedField = m.prevField()
			if m.isTextInputField(m.focusedField) {
				m.focusTextField(m.focusedField)
			}
			return m, nil
		case "b":
			if m.focusedField == 1 {
				if m.targetType == "directory" {
					return m.openFileBrowser()
				}
				if m.targetType == "image" {
					return m.openImageBrowser()
				}
			}
			fallthrough
		default:
			var cmd tea.Cmd
			switch m.focusedField {
			case 1:
				m.targetInput, cmd = m.targetInput.Update(msg)
				m.targetPath = m.targetInput.Value()
			case 7:
				m.trivyServerInput, cmd = m.trivyServerInput.Update(msg)
			case 10:
				m.gitleaksConfigInput, cmd = m.gitleaksConfigInput.Update(msg)
			}
			return m, cmd
		}
	}

	// Handle non-textinput fields
	switch msg.String() {
	case "down":
		m.blurAllTextInputs()
		m.focusedField = m.nextField()
		if m.isTextInputField(m.focusedField) {
			m.focusTextField(m.focusedField)
		}
		return m, nil
	case "up":
		m.blurAllTextInputs()
		m.focusedField = m.prevField()
		if m.isTextInputField(m.focusedField) {
			m.focusTextField(m.focusedField)
		}
		return m, nil
	case "left":
		return m.handleFormLeft()
	case "right":
		return m.handleFormRight()
	case "enter":
		// Enter always starts the scan in this view (Rule 135 — unique action)
		return m.startScan()
	case " ":
		return m.handleFormSpace()
	case "ctrl+s":
		return m.startScan()
	}
	return m, nil
}

// handleFormLeft handles left arrow key for cycle fields (Rule 132)
func (m Model) handleFormLeft() (tea.Model, tea.Cmd) {
	if m.focusedField == 0 {
		return m.cycleTargetType("left"), nil
	}
	return m, nil
}

// handleFormRight handles right arrow key for cycle fields (Rule 132)
func (m Model) handleFormRight() (tea.Model, tea.Cmd) {
	if m.focusedField == 0 {
		return m.cycleTargetType("right"), nil
	}
	return m, nil
}

// cycleTargetType toggles the target type and resets the target input
func (m Model) cycleTargetType(_ string) Model {
	switch m.targetType {
	case "directory":
		m.targetType = "image"
		m.targetInput.Placeholder = "image:tag"
	case "image":
		m.targetType = "directory"
		m.targetInput.Placeholder = "/path/to/scan"
	}
	m.targetPath = ""
	m.targetInput.SetValue("")
	return m
}

// handleFormSpace handles space key in form (same as enter for toggles)
func (m Model) handleFormSpace() (tea.Model, tea.Cmd) {
	// Server-incompatible fields cannot be toggled when a Trivy server is set
	if m.isServerMode() && m.isServerIncompatibleField(m.focusedField) {
		return m, nil
	}
	switch m.focusedField {
	case 2:
		m.enableVuln = !m.enableVuln
		m.saveOptionsToConfig()
	case 3:
		m.enableSecret = !m.enableSecret
		m.saveOptionsToConfig()
	case 4:
		m.enableMisconfig = !m.enableMisconfig
		m.saveOptionsToConfig()
	case 5:
		m.enableLicense = !m.enableLicense
		m.saveOptionsToConfig()
	case 6:
		m.generateSBOM = !m.generateSBOM
		m.saveOptionsToConfig()
	case 8:
		m.ignoreUnfixed = !m.ignoreUnfixed
		m.saveOptionsToConfig()
	case 9:
		m.ignoreEOL = !m.ignoreEOL
		m.saveOptionsToConfig()
	case 11:
		m.gitleaksHistory = !m.gitleaksHistory
		m.saveOptionsToConfig()
	}
	return m, nil
}

// openFileBrowser sends a selection request to browse directories via the workspaces view
func (m Model) openFileBrowser() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg {
		return SelectionRequestMsg{Type: "directory", Message: "Select a directory to scan"}
	}
}

// openImageBrowser sends a selection request to browse images via the OCI images view
func (m Model) openImageBrowser() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg {
		return SelectionRequestMsg{Type: "image", Message: "Select an image to scan"}
	}
}
