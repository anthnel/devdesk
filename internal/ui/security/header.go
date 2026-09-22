package security

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/remediation"
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
		// A finding with no published reference is the same screen as one with
		// three, so `o` is greyed rather than dropped (Rule 130).
		f := m.selectedFinding
		return []shortcut.Shortcut{
			{Key: "esc", Description: "Back"},
			// keymap.Web, which is what handleDetailsState binds. It read
			// "o" and nothing answered it: the key advertised did nothing and
			// the key that worked was never shown — Rule 130's hazard seen
			// from the other side.
			{Key: keymap.Web, Description: "Open ref", Disabled: f == nil || len(f.References) == 0},
			{Key: "ctrl+p", Description: "Command"},
		}
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
	// X is greyed rather than dropped (Rule 130): a tab is not a different
	// screen, and the entry used to appear and disappear as the cursor crossed
	// a Trivy secret in a list holding both tools' findings.
	// The Remediation tab has a table of its own: what filters and sorts the
	// findings does not reach it, so those keys are greyed there and S — which
	// means nothing on the four tabs of findings — is the one that lights up.
	notFindings := m.activeTab == TabRemediation
	// Enter opens the selected finding, or on the Remediation tab the diff of the
	// chosen bases: one key, the same place in the column, a different verb.
	enter := shortcut.Shortcut{Key: "enter", Description: "Details", Disabled: !m.canOpenFinding().Enabled()}
	if notFindings {
		enter = shortcut.Shortcut{Key: "enter", Description: "Show diff", Disabled: !m.canPreviewRemediation().Enabled()}
	}
	// ctrl+o is one key with one verb — write the file — and the object it
	// writes is the tab's. The label changes with it so the column never
	// offers "Write Dockerfile" on a tab holding a Kubernetes manifest.
	writeShortcut := shortcut.Shortcut{Key: writeRemediationKey, Description: "Write Dockerfile", Disabled: !m.canWriteRemediation().Enabled()}
	if m.activeTab == TabMisconfig {
		writeShortcut = shortcut.Shortcut{Key: writeRemediationKey, Description: "Apply built-in fix", Disabled: !m.canFixMisconfig().Enabled()}
	}
	return []shortcut.Shortcut{
		{Key: "tab", Description: "Switch tab"},
		enter,
		{Key: "space", Description: "Choose candidate", Disabled: !m.canSelectCandidate().Enabled()},
		writeShortcut,
		{Key: keymap.Scan, Description: "Scan candidates", Disabled: !m.canScanCandidates().Enabled()},
		{Key: "c", Description: "Toggle CRITICAL", Disabled: notFindings},
		{Key: "h", Description: "Toggle HIGH", Disabled: notFindings},
		{Key: "m", Description: "Toggle MEDIUM", Disabled: notFindings},
		{Key: "l", Description: "Toggle LOW", Disabled: notFindings},
		{Key: "/", Description: "Search", Disabled: notFindings},
		{Key: ".", Description: "Sort", Disabled: notFindings},
		{Key: keymap.Exclude, Description: "Exclude", Disabled: !m.canExclude().Enabled()},
		{Key: openPipelineKey, Description: "Open resolved pipeline", Disabled: !m.canOpenPipeline().Enabled()},
		{Key: "ctrl+r", Description: "New scan"},
		{Key: "ctrl+p", Description: "Command"},
		{Key: "?", Description: "Help"},
	}
}

// inventoryShortcuts advertises only what the selected row can actually do
// (Rule 130). An empty inventory offers none of the per-row actions, and a row
// with no stored counts cannot be opened.
// inventoryShortcuts is the landing state's column.
//
// An empty inventory is the same screen as a full one — the table is there,
// with no rows — so the row actions are greyed rather than dropped (Rule 130).
// The first scan of a context used to make four entries appear at once.
func (m Model) inventoryShortcuts() shortcut.Shortcuts {
	_, hasRow := m.inventory.Selected()
	return []shortcut.Shortcut{
		{Key: "enter", Description: "Open findings", Disabled: !m.canOpenTarget().Enabled()},
		{Key: keymap.Scan, Description: "Rescan", Disabled: !m.canRescanSelected().Enabled()},
		{Key: keymap.ScanAll, Description: "Rescan all", Disabled: !m.canRescanAll().Enabled()},
		{Key: "/", Description: "Filter", Disabled: !hasRow},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "ctrl+p", Description: "Command"},
		{Key: "?", Description: "Help"},
	}
}

func (m Model) GetTitle() string {
	base := theme.IconSecurity + " Security Scanner"
	if m.state == StateInventory {
		return base + " " + theme.IconChevronRight + " Inventory"
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

// GetHeaderInfo returns the key-value info for the header: the context, one
// count, and in the results how much of it is fixable.
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
// Fixable came back on its own merits: it is the one figure that says what can
// be done, which none of the removed ones did.
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
	if value, style, ok := m.headerFixable(); ok {
		info = append(info, shortcut.HeaderInfo{Key: "Fixable", Value: value, Style: style})
	}
	return info
}

// headerFixable is how many of the vulnerabilities have a fixed version, split
// by what has to move: "12 (8 base, 4 deps)". It is the one number that says
// what can be done about the findings, which the count above does not. Like the
// count, it is absent where the state is a list of nothing, and it is present
// on both sides of the results/details step so the header does not change
// under the reader (Rule 130).
func (m Model) headerFixable() (string, lipgloss.Style, bool) {
	if (m.state != StateResults && m.state != StateDetails) || m.result == nil {
		return "", lipgloss.Style{}, false
	}
	s := remediation.Summarize(m.result.Findings)
	if s.Fixable() == 0 {
		return "0", theme.DimStyle, true
	}
	var parts []string
	if s.BaseImage > 0 {
		parts = append(parts, strconv.Itoa(s.BaseImage)+" base")
	}
	if s.Dependencies > 0 {
		parts = append(parts, strconv.Itoa(s.Dependencies)+" deps")
	}
	if s.Unclassified > 0 {
		parts = append(parts, strconv.Itoa(s.Unclassified)+" unclassified")
	}
	return strconv.Itoa(s.Fixable()) + " (" + strings.Join(parts, ", ") + ")", theme.HeaderValueStyle, true
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

// GetHelpContent returns the help content for the Security view
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Security Scanner",
		Description: "This view opens on an inventory of everything scanned in the current configuration context — images and repositories, with what each scan found and when. Opening a row shows its findings; rescanning re-runs Trivy and Gitleaks with the options set in the configuration view.",
		KeyBindings: []help.KeyBinding{
			{Key: "enter", Description: "Open the stored findings for the selected target (inventory)"},
			{Key: keymap.Scan, Description: "Rescan the selected target, overwriting its cached result (inventory)"},
			{Key: keymap.ScanAll, Description: "Rescan every target. The confirmation carries a checkbox to purge the cached counts first (inventory)"},
			{Key: "ctrl+r", Description: "Reload the inventory from the scan caches"},
			{Key: "/", Description: "Search — the inventory by target name, the findings by ID, title, package or file"},
			{Key: ".", Description: "Cycle the sort column — the findings table opens on the order the scanner reported, and the cycle leads back to it"},
			{Key: "enter", Description: "Open the details of the selected finding (results)"},
			{Key: "space", Description: "Choose the candidate under the cursor for its stage — only a scanned one (Remediation tab)"},
			{Key: "enter", Description: "Show what the chosen bases would change, as a diff (Remediation tab)"},
			{Key: writeRemediationKey, Description: "Write the chosen bases into the Dockerfiles, after a confirmation that defaults to No (Remediation tab)"},
			{Key: writeRemediationKey, Description: "Apply the built-in fix for the selected rule, after a confirmation showing the diff and defaulting to No. Only a few rules have one; the rest are handed to an agent through the MCP server (Misconfigurations tab)"},
			{Key: keymap.Scan, Description: "Measure the base images and their candidates by scanning them from their registries (Remediation tab)"},
			{Key: keymap.Exclude, Description: "Exclude a secret — add it to .gitleaksignore (Secrets tab, Gitleaks findings only)"},
			{Key: keymap.Web, Description: "Open first reference URL in the default browser (detail view)"},
			{Key: openPipelineKey, Description: "Open the pipeline the forge resolves for this repository — every include and component expanded (CI tab)"},
			{Key: "tab / shift+tab", Description: "Switch tabs in results (CVE, Secrets, Licenses, Misconfig, CI, Remediation)"},
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
				Body:  "Vulnerability Scan: detects CVEs in dependencies and packages (Trivy).\nSecret Scan: runs both scanners, and the Secrets tab shows their findings together. Gitleaks reads a repository's working tree and git history; Trivy reads the target's content, which is what gives an image a secret scan at all — Gitleaks cannot scan one. The Source column says which tool found each finding.\nMisconfig Scan: detects IaC misconfigurations in Dockerfiles, Terraform, Kubernetes manifests and Helm charts (Trivy). On the Misconfigurations tab, the Source column shows the dialect each finding comes from.\nK8s schema: validates a repository's Kubernetes manifests against the API schema of the configured Kubernetes version (kubeconform) — a wrong type, an unknown field, an apiVersion that release no longer serves. Its findings are on the Misconfigurations tab, with 'schema' as their source. Helm charts and Kustomize overlays are not validated raw: a template is not YAML until it is rendered, and the log names those left unvalidated.\nLicense Scan: analyzes dependency licenses (Trivy).\nCI Score: grades a repository's pipeline configuration (plumber), for this context's forge only — a repository hosted elsewhere is not graded rather than graded without credentials. The grade is on the CI tab, above the issues; a run that could not collect everything says so instead of showing a letter.",
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
				Body:  "Results are displayed by tab (CVE, Secrets, Licenses, Misconfig). Every finding belongs to exactly one tab, and the count on each label is the same number the scan recorded. Severity is filtered with c, h, m and l — they are cumulative, so c and h together ask for CRITICAL or HIGH, which a threshold could not express. The active ones are shown in the bar under the table, beside the search field. '.' cycles the sort column and '/' searches. The Fixable figure in the header counts the vulnerabilities that have a fixed version, split into base image packages (cleared by upgrading the image's packages or moving to a newer base image) and application dependencies (cleared only by bumping the dependency itself); a result scanned before this was recorded shows its findings as unclassified until the next scan. Press Enter to view finding details, which name the package kind and the command that moves it. For secrets, X adds a finding to .gitleaksignore; it is offered for Gitleaks findings only, since that file is matched on a Gitleaks fingerprint a Trivy secret does not have.\nEsc returns to the inventory, or to the list the results were opened from.",
			},
			{
				Title: "Remediation",
				Body:  "For a repository, this tab reads the Dockerfiles under it and lists each base image — every stage, since a vulnerability in a build stage can reach the image that ships — with the newer tags it could move to. Candidates keep the image's variant (alpine stays alpine, slim stays slim) and its precision (3.18 is offered 3.21, not 3.21.1); by default they stay on the same major version, and the scan tab of the configuration view (Base image bumps) lets them take the next one.\nNothing is measured until you press S: each image is scanned straight from its registry, without being pulled, and the counts are kept for 24 hours. The vs now column is the change in CRITICAL plus HIGH against the image as written; negative is better. A candidate is a proposal with its evidence — the scan says the CVEs are gone, not that the application still runs on the new base.\nAn image scan has no Dockerfile, so the tab is empty for one.\nChoosing: space picks the candidate under the cursor for its stage — only one that has been scanned, since a bump is proposed with its evidence — and Enter shows what the choices would change. Ctrl+O writes them, and only when you ask: it lists each file and line, and says what git will be able to undo (a clean tracked file can be reverted with git checkout; one with uncommitted changes cannot without losing them; an untracked file or one outside a repository cannot at all). The answer defaults to No. Only the image reference changes — comments, line endings and the rest of the file are untouched — and DevDesk makes no commit, branch or push. A file that changed since the diff was computed is refused, not overwritten. A reference pinned by digest loses its pin, which the modal says. A stage that reads its image from an ARG changes that ARG's default, and a --build-arg on the command line can still override it.",
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
