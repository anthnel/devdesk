package security

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func (m Model) GetShortcuts() shortcut.Shortcuts {
	switch m.state {
	case StateInventory:
		return m.inventoryShortcuts()
	case StateResults:
		return m.resultsShortcuts()
	case StateDetails:
		shortcuts := []shortcut.Shortcut{
			{Key: "esc/⌫", Description: "Back"},
		}
		if f := m.selectedFinding; f != nil {
			if len(f.References) > 0 {
				shortcuts = append(shortcuts, shortcut.Shortcut{Key: "o", Description: "Open ref"})
			}
		}
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+p", Description: "Command"})
		return shortcuts
	}
	return nil
}

// resultsShortcuts advertises the findings table's own keys.
//
// The four severity toggles were bound and unadvertised — a user had to open
// the help to learn the view filtered at all — and `.` was offered on three
// tabs out of four, from back when it cycled the severity floor. It is the sort
// now (Rule 111), so it applies wherever there is a table.
//
// While the search has the keyboard, everything else is a character: listing
// keys that no longer do what they say is worse than listing nothing, so only
// the two that end the search are shown. Same shape as the ports view.
func (m Model) resultsShortcuts() shortcut.Shortcuts {
	if m.findingsTable.InEditMode() {
		return shortcut.Shortcuts{{Key: "enter/esc", Description: "Confirm / Cancel search"}}
	}
	shortcuts := []shortcut.Shortcut{
		{Key: "tab", Description: "Switch tab"},
		{Key: "enter", Description: "Details"},
		{Key: "c", Description: "Toggle CRITICAL"},
		{Key: "h", Description: "Toggle HIGH"},
		{Key: "m", Description: "Toggle MEDIUM"},
		{Key: "l", Description: "Toggle LOW"},
		{Key: "/", Description: "Search"},
		{Key: ".", Description: "Sort"},
	}
	// Rule 130: .gitleaksignore only accepts a Gitleaks fingerprint, so X means
	// nothing on a secret Trivy found — and the Secrets tab now holds both.
	if selected, ok := m.findingsTable.Selected(); m.activeTab == TabSecrets && ok &&
		selected.Source == scan.SourceGitleaks {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: keymap.Exclude, Description: "Exclude"})
	}
	return append(shortcuts,
		shortcut.Shortcut{Key: "ctrl+r", Description: "New scan"},
		shortcut.Shortcut{Key: "ctrl+p", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
}

// inventoryShortcuts advertises only what the selected row can actually do
// (Rule 130). An empty inventory offers none of the per-row actions, and a row
// with no stored counts cannot be opened.
func (m Model) inventoryShortcuts() shortcut.Shortcuts {
	shortcuts := []shortcut.Shortcut{}
	if target, ok := m.inventory.Selected(); ok {
		if target.Scanned {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "enter", Description: "Open findings"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "S", Description: "Rescan"},
			shortcut.Shortcut{Key: "A", Description: "Rescan all"},
			shortcut.Shortcut{Key: "/", Description: "Filter"},
		)
	}
	return append(shortcuts,
		shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh"},
		shortcut.Shortcut{Key: "ctrl+p", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
}

func (m Model) GetTitle() string {
	base := theme.IconSecurity + " Security Scanner"
	if m.state == StateInventory {
		// The context is named because the caches are scoped to one: two
		// contexts hold different inventories, and their rows look identical.
		return base + " " + theme.IconChevronRight + " Inventory · " + config.CurrentContextName()
	}
	// The label, not the key: the title is the one place the target is read
	// rather than used, and a registry prefix is what pushes the image's own
	// name off the line.
	if m.targetLabel != "" {
		annotation := lipgloss.NewStyle().
			Foreground(theme.ColorSecondary).
			Background(theme.ColorBackground).
			Render("(" + m.targetLabel + ")")
		return base + " " + annotation
	}
	return base
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header: the context, and one
// count.
//
// It used to carry seven fields in the results state, which is exactly the
// number buildInfoLines renders — an eighth would have been dropped in silence.
// Most of them had stopped earning their line:
//
//   - Trivy and Gitleaks versions answered "can I scan?", which the dashboard
//     already answers from shared.State.Tools, and which an inventory of past
//     scans is not about.
//   - Filter showed the severity filter, permanently reading ALL, and meaning
//     nothing on the Secrets tab or in the details.
//   - Secrets and Licenses duplicated the tab bar, which renders the same two
//     numbers a line below.
//
// The context is what replaced them, and it is the one thing that was missing:
// the scan caches are scoped to a context, so the same inventory rows mean
// different things in two of them and look identical.
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	info := []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
	if key, value, ok := m.headerCount(); ok {
		info = append(info, shortcut.HeaderInfo{Key: key, Value: value, Style: theme.HeaderValueStyle})
	}
	return info
}

// headerCount is the one number the header shows, named for whatever the state
// is actually a list of. The form and the scanning screen are lists of nothing,
// and report no count rather than a zero.
func (m Model) headerCount() (key, value string, ok bool) {
	switch m.state {
	case StateInventory:
		return "Targets", strconv.Itoa(len(m.inventory.Items())), true
	case StateResults, StateDetails:
		if m.result == nil {
			return "", "", false
		}
		return "Findings", strconv.Itoa(m.result.TotalFindings()), true
	}
	return "", "", false
}

// GetHelpContent retourne le contenu d'aide de la vue Security
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Security Scanner",
		Description: "This view opens on an inventory of everything scanned in the current configuration context — images and repositories, with what each scan found and when. Opening a row shows its findings; rescanning re-runs Trivy and Gitleaks with the options set in the configuration view.",
		KeyBindings: []help.KeyBinding{
			{Key: "enter", Description: "Open the stored findings for the selected target (inventory)"},
			{Key: "S", Description: "Rescan the selected target, overwriting its cached result (inventory)"},
			{Key: "A", Description: "Rescan every target. The confirmation carries a checkbox to purge the cached counts first (inventory)"},
			{Key: "ctrl+r", Description: "Reload the inventory from the scan caches"},
			{Key: "/", Description: "Search — the inventory by target name, the findings by ID, title, package or file"},
			{Key: ".", Description: "Cycle the sort column — the findings table opens on the order the scanner reported, and the cycle leads back to it"},
			{Key: "enter", Description: "Open the details of the selected finding (results)"},
			{Key: "X", Description: "Exclude a secret — add it to .gitleaksignore (Secrets tab, Gitleaks findings only)"},
			{Key: "o", Description: "Open first reference URL in the default browser (detail view)"},
			{Key: "tab / shift+tab", Description: "Switch tabs in results (CVE, Secrets, Licenses, Misconfig)"},
			{Key: "c / h / m / l", Description: "Filter by severity — cumulative, so c and h together show CRITICAL and HIGH"},
			{Key: "ctrl+r", Description: "Back to the inventory (results)"},
			{Key: "esc", Description: "Back"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Inventory",
				Body:  "The table lists every image and repository scanned in the current context, sorted by CRITICAL findings. Counts come from the scan caches; the Scanned column shows how long ago each result was produced.\nScans launched from the OCI resources and workspaces views write to the same caches and appear here.\nA rescan reads its options from the configuration view (:cfg), scan tab — there is nothing to set here.\nRescanning all (A) purges the cached results first, when its checkbox is ticked, so a target shows '-' until its scan returns.",
			},
			{
				Title: "Scan Types",
				Body:  "Vulnerability Scan: detects CVEs in dependencies and packages (Trivy).\nSecret Scan: runs both scanners, and the Secrets tab shows their findings together. Gitleaks reads a repository's working tree and git history; Trivy reads the target's content, which is what gives an image a secret scan at all — Gitleaks cannot scan one. The Source column says which tool found each finding.\nMisconfig Scan: detects IaC misconfigurations in Dockerfiles, Terraform, K8s manifests (Trivy).\nLicense Scan: analyzes dependency licenses (Trivy).",
			},
			{
				Title: "Targets",
				Body:  "A target is a Docker image or a repository under the configured workspaces directory. Images are scanned from the OCI resources view (:oci) and repositories from the workspaces view (:w); both write to the caches this inventory reads, so anything scanned anywhere appears here and can be rescanned from here.",
			},
			{
				Title: "Scan Options",
				Body:  "Every option lives in the configuration view (:cfg), scan tab: which scanners run, whether to use a Trivy server, custom binaries or images, whether to ignore unfixed or end-of-life findings, a custom .gitleaks.toml, and whether Gitleaks reads the full git history. A rescan started here reads them at the moment it runs.",
			},
			{
				Title: "Results",
				Body:  "Results are displayed by tab (CVE, Secrets, Licenses, Misconfig). Every finding belongs to exactly one tab, and the count on each label is the same number the scan recorded. Severity is filtered with c, h, m and l — they are cumulative, so c and h together ask for CRITICAL or HIGH, which a threshold could not express. The active ones are shown in the bar under the table, beside the search field. '.' cycles the sort column and '/' searches. Press Enter to view finding details. For secrets, X adds a finding to .gitleaksignore; it is offered for Gitleaks findings only, since that file is matched on a Gitleaks fingerprint a Trivy secret does not have.\nEsc returns to the inventory, or to the list the results were opened from.",
			},
			{
				Title: "Command Logging",
				Body:  "Executed scanner commands are logged to the application log file (~/.devdesk/devdesk.log).",
			},
			{
				Title: "Prerequisites",
				Body:  "Trivy and Gitleaks must be installed (local binary or Docker image). Whether each one is available is shown on the dashboard; a scan that could run no scanner at all reports that rather than returning an empty result.",
			},
		},
	}
}
