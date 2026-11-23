package security

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/indicator"
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
		return m.detailsShortcuts()
	case StateGroup:
		return m.groupShortcuts()
	}
	return nil
}

// detailsShortcuts is the detail's column: the same six keys for every finding,
// each greyed when it has nothing to act on (Rule 130). Every availability is
// computed from the finding on screen alone, so the header reads no file. The
// descriptions are at most ten cells: the column is 19 cells wide at 80 columns
// and `<ctrl+o>` takes nine of them.
func (m Model) detailsShortcuts() shortcut.Shortcuts {
	// A finding with no published reference is the same screen as one with three,
	// so W is greyed rather than dropped. keymap.Web is what handleDetailsState
	// binds: a key advertised under another name would do nothing.
	f := m.selectedFinding
	return []shortcut.Shortcut{
		{Key: "esc", Description: "Back"},
		{Key: keymap.Web, Description: "Open ref", Disabled: f == nil || len(f.References) == 0},
		{Key: "enter", Description: "Open file", Disabled: !m.canOpenFindingFile().Enabled()},
		{Key: writeRemediationKey, Description: "Apply fix", Disabled: !m.canFixMisconfig().Enabled()},
		{Key: keymap.Copy, Description: "Copy cmd", Disabled: !m.canCopyFixCommand().Enabled()},
		{Key: keymap.Exclude, Description: "Exclude", Disabled: !m.canExclude().Enabled()},
	}
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
	if m.isGrouped() {
		enter = shortcut.Shortcut{Key: "enter", Description: "Open group", Disabled: !m.canOpenGroup().Enabled()}
	}
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
		// Third, so that it is among the seven entries that show at 80 columns.
		{Key: groupToggleKey, Description: m.groupToggleLabel(), Disabled: !m.canGroup().Enabled()},
		{Key: "space", Description: "Choose candidate", Disabled: !m.canSelectCandidate().Enabled()},
		writeShortcut,
		{Key: keymap.Scan, Description: "Scan candidates", Disabled: !m.canScanCandidates().Enabled()},
		{Key: "c", Description: "Toggle CRITICAL", Disabled: notFindings},
		{Key: "h", Description: "Toggle HIGH", Disabled: notFindings},
		{Key: "m", Description: "Toggle MEDIUM", Disabled: notFindings},
		{Key: "l", Description: "Toggle LOW", Disabled: notFindings},
		{Key: pssFilterKey, Description: "Toggle Pod Security filter", Disabled: !m.canTogglePSS().Enabled()},
		{Key: "/", Description: "Search", Disabled: notFindings},
		{Key: ".", Description: "Sort", Disabled: notFindings},
		{Key: keymap.Exclude, Description: "Exclude", Disabled: !m.canExclude().Enabled()},
		{Key: keymap.Copy, Description: "Copy cmd", Disabled: !m.canCopyGroupCommand().Enabled()},
		{Key: openPipelineKey, Description: "Open resolved pipeline", Disabled: !m.canOpenPipeline().Enabled()},
		{Key: "ctrl+r", Description: "New scan"},
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
		if g, ok := m.currentGroup(); ok && m.state == StateGroup {
			return base + " " + annotation + " " + theme.IconChevronRight + " " + groupTitle(g)
		}
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
	if mode, ok := m.groupModeHeader(); ok {
		info = append(info, mode)
	}
	if pss, ok := m.pssHeader(); ok {
		info = append(info, pss)
	}
	if value, style, ok := m.headerFixable(); ok {
		info = append(info, shortcut.HeaderInfo{Key: "Fixable", Value: value, Style: style})
	}
	if value, ok := m.headerUnrendered(); ok {
		info = append(info, shortcut.HeaderInfo{Key: "Not rendered", Value: value, Style: theme.HeaderValueStyle})
	}
	if sig, ok := m.signatureHeader(); ok {
		info = append(info, sig)
	}
	return info
}

// groupModeHeader names the grouped display in the info column, whole where the
// column has room for it. The shortcut column says the way back; this says where
// the user is.
func (m Model) groupModeHeader() (shortcut.HeaderInfo, bool) {
	if m.state != StateResults || !m.isGrouped() {
		return shortcut.HeaderInfo{}, false
	}
	return shortcut.HeaderInfo{Key: "View", Value: labelGroupByFix, Style: theme.HeaderValueStyle}, true
}

// headerUnrendered is how many Helm charts and Kustomize overlays the schema
// stage could not look at, for want of helm or kustomize (§3.80). Without it a
// repository of charts would read as schema-clean when nothing was checked.
// Absent when there are none — it is not a count of findings, and a "0" on
// every result would be noise about a stage most repositories do not need.
func (m Model) headerUnrendered() (string, bool) {
	if !m.showsResult() || m.result == nil || len(m.result.K8sUnrendered) == 0 {
		return "", false
	}
	return strconv.Itoa(len(m.result.K8sUnrendered)) + " (install helm / kustomize)", true
}

// headerFixable is how many of the vulnerabilities have a fixed version, split
// by what has to move: "12 (8 base, 4 deps)". It is the one number that says
// what can be done about the findings, which the count above does not. Like the
// count, it is absent where the state is a list of nothing, and it is present
// on both sides of the results/details step so the header does not change
// under the reader (Rule 130).
func (m Model) headerFixable() (string, lipgloss.Style, bool) {
	if !m.showsResult() || m.result == nil {
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

// showsResult reports whether the state is about the result on screen: the
// results, the findings of one group, or one finding.
func (m Model) showsResult() bool {
	return m.state == StateResults || m.state == StateDetails || m.state == StateGroup
}

// headerCount is the one number the header shows, named for whatever the state
// is actually a list of. The form and the scanning screen are lists of nothing,
// and report no count rather than a zero.
func (m Model) headerCount() (key, value string, ok bool) {
	switch m.state {
	case StateInventory:
		return "Targets", strconv.Itoa(len(m.inventory.Items())), true
	case StateResults, StateDetails, StateGroup:
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
			{Key: "enter", Description: "Open the details of the selected finding (results). In the grouped list, open the findings of the selected group, then enter again for one finding's details"},
			{Key: groupToggleKey, Description: "Group the tab by fix, or back to one row per finding: on the CVE tab one row per package and installed version, with the move that clears its CVEs; on the Misconfigurations tab one row per file and rule, with the state of the built-in fix (CVE and Misconfigurations tabs)"},
			{Key: "space", Description: "Choose the candidate under the cursor for its stage — only a scanned one (Remediation tab)"},
			{Key: "enter", Description: "Show what the chosen bases would change, as a diff (Remediation tab)"},
			{Key: writeRemediationKey, Description: "Write the chosen bases into the Dockerfiles, after a confirmation that defaults to No (Remediation tab)"},
			{Key: writeRemediationKey, Description: "Apply the built-in fix for the selected rule, after a confirmation showing the diff and defaulting to No. Only a few rules have one — some Dockerfile rules, privileged and allowPrivilegeEscalation on a plain Kubernetes manifest, an apiVersion whose replacement is a rename, and .git added to an existing .dockerignore; the rest are handed to an agent through the MCP server (Misconfigurations tab, and the detail of a finding)"},
			{Key: "enter", Description: "Open the finding's file in the viewer, which has O for the IDE and esc to come back to the detail (detail)"},
			{Key: keymap.Copy, Description: "Copy the command under Run to the clipboard, whole. DevDesk never runs it (detail)"},
			{Key: keymap.Copy, Description: "Copy the command of the selected group to the clipboard, whole, when the group has one. DevDesk never runs it (grouped list and group)"},
			{Key: keymap.Scan, Description: "Measure the base images and their candidates by scanning them from their registries (Remediation tab)"},
			{Key: keymap.Exclude, Description: "Exclude a secret — add it to .gitleaksignore (Secrets tab and the detail of a Gitleaks finding)"},
			{Key: keymap.Web, Description: "Open first reference URL in the default browser (detail view)"},
			{Key: openPipelineKey, Description: "Open the pipeline the forge resolves for this repository — every include and component expanded (CI tab)"},
			{Key: "tab / shift+tab", Description: "Switch tabs in results (CVE, Secrets, Licenses, Misconfig, CI, Remediation)"},
			{Key: "c / h / m / l", Description: "Filter by severity — cumulative, so c and h together show CRITICAL and HIGH"},
			{Key: pssFilterKey, Description: "Toggle the Pod Security filter, which hides the Kubernetes findings the profile in scan.pss_profile does not require (Misconfigurations tab)"},
			{Key: "ctrl+r", Description: "Back to the inventory (results)"},
			{Key: "esc", Description: "Back"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			indicator.Legend(),
			{
				Title: "Inventory",
				Body:  "The table lists every image and repository scanned in the current context, sorted by CRITICAL findings. Counts come from the scan caches; the Scanned column shows how long ago each result was produced, or 'scanning' while a scan of that row runs.\nThe first column is the activity indicator: it shows a scan running or queued, and a red " + indicator.Glyph(indicator.Failed, true) + " stays on a row whose scan failed until the next attempt on that row. The footer names the state of the selected row and the progress of a batch.\nScans launched from the OCI resources and workspaces views write to the same caches and appear here.\nA rescan reads its options from the configuration view (:cfg), scan tab — there is nothing to set here.\nRescanning all (A) purges the cached results first, when its checkbox is ticked, so a target shows '-' until its scan returns.\nWith the Misconfiguration category on, a CFG column counts what the misconfiguration stages found, coloured by the worst severity among them. A dash there is a target no such stage read — not a zero — and a '?' after the count says the count is partial: a Helm chart or Kustomize overlay nothing could render. '0?' is the one to stop on, since nothing was found because nothing was read.",
			},
			{
				Title: "Scan Types",
				Body:  "Vulnerability Scan: detects CVEs in dependencies and packages (Trivy).\nSecret Scan: runs both scanners, and the Secrets tab shows their findings together. Gitleaks reads a repository's working tree and git history; Trivy reads the target's content, which is what gives an image a secret scan at all — Gitleaks cannot scan one. The Source column says which tool found each finding.\nMisconfig Scan: detects IaC misconfigurations in Dockerfiles, Terraform, Kubernetes manifests and Helm charts (Trivy). On the Misconfigurations tab, the Source column shows the dialect each finding comes from.\nK8s schema: validates a repository's Kubernetes manifests against the API schema of the configured Kubernetes version (kubeconform) — a wrong type, an unknown field, an apiVersion that release no longer serves. Its findings are on the Misconfigurations tab, with 'schema' as their source. Helm charts and Kustomize overlays are never validated raw — a template is not YAML until it is rendered. When helm is available, each chart is linted ('helm lint') and rendered, then validated; when kustomize is available, each overlay is built, then validated. A finding from a rendered resource points at its template or its kustomization, with no line. Without either tool, the results header counts them as Not rendered.\nBuild context: DevDesk's own check, run with the Misconfiguration category and needing no tool. A Dockerfile at the repository's root whose final image copies the whole context (COPY . or ADD .) is reported when that context holds a .git directory, or files such as .env, *.pem, *.key, id_rsa, credentials.json or .npmrc, that no .dockerignore leaves out — the git history, and a secret committed then deleted with it, would be readable from the image's layers. Only what is certain is reported: a .dockerignore pattern DevDesk cannot read, or one that may bring part of .git back, means silence. A Dockerfile below the root is not checked, since its build context is not written anywhere; the log says so. Its findings have 'context' as their source.\nBase image signatures: DevDesk's own check too, run with the Misconfiguration category unless Verify image signatures is unticked (scan tab of the configuration view). Every FROM of every Dockerfile — a build stage included, since a compromised builder writes the binary that ships — is checked against your rules (~/.devdesk/trust.yaml) and the built-in ones, with cosign. A signature by an unexpected identity is CRITICAL (DEVDESK-SIG-001); no accepted signature where a rule requires one is HIGH (DEVDESK-SIG-002). Only a proven violation is a finding: an image no rule covers says nothing, and a check that could not run under one of your rules is reported as an error of the scan. Its findings have 'signature' as their source, and the Remediation tab lists candidates whose signature holds.\nLicense Scan: analyzes dependency licenses (Trivy).\nCI Score: grades a repository's pipeline configuration (plumber), for this context's forge only — a repository hosted elsewhere is not graded rather than graded without credentials. The grade is on the CI tab, above the issues; a run that could not collect everything says so instead of showing a letter.",
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
				Body:  "Results are displayed by tab (CVE, Secrets, Licenses, Misconfig). Every finding belongs to exactly one tab, and the count on each label is the same number the scan recorded. Severity is filtered with c, h, m and l — they are cumulative, so c and h together ask for CRITICAL or HIGH, which a threshold could not express. The active ones are shown in the bar under the table, beside the search field. '.' cycles the sort column and '/' searches. The Fixable figure in the header counts the vulnerabilities that have a fixed version, split into base image packages (cleared by upgrading the image's packages or moving to a newer base image) and application dependencies (cleared only by bumping the dependency itself); a result scanned before this was recorded shows its findings as unclassified until the next scan. On the Misconfigurations tab a File column shows where each finding is, as a path relative to the scanned directory (a path under the scanner container's /scan mount is shortened to its relative form, and any other absolute path under your home directory starts with ~); it is the first column dropped on a narrow terminal, and a long path loses its start, not its file name. Press Enter to view finding details (see Finding details), which name the package kind and the command that moves it. For secrets, X adds a finding to .gitleaksignore; it is offered for Gitleaks findings only, since that file is matched on a Gitleaks fingerprint a Trivy secret does not have.\nEsc returns to the inventory, or to the list the results were opened from.",
			},
			{
				Title: "Finding details",
				Body: "The detail of a finding puts what to do before why: its severity, ID and title (always one line, cut with an ellipsis), then the Fix block and what the fix would write, then Why, Output and Refs. It is one column of at most 110 cells. A section with nothing to say is left out.\n" +
					"Fix is always there and says the status in words, one of six. 'Built-in fix available': DevDesk can write the fix for this rule. 'Built-in fix unavailable: <reason>': the rule has a built-in fix and it declines this instance, for the reason given. 'Manual': the finding tells how to fix it by hand, which includes a vulnerability with a fixed version. 'No fix published': a vulnerability with no fixed version. 'Cannot fix here': a misconfiguration of an image, whose files are inside it and cannot be written. 'No guidance available': nothing tells how to fix it. For a misconfiguration the status is the same answer the shortcut for writing a fix gives.\n" +
					"Under Fix, with no blank line: Where is the file and line, then the package, the CI job, its script line and the matched text when the finding has them. Do is the numbered steps (the ones DevDesk writes for the findings it builds itself, such as a chart or overlay that does not render, a schema or helm lint message, or a secret, come first), then the resolution the scanner gave, word for word. Run is the command, on one line, cut with an ellipsis when it is wider than the column; DevDesk shows it and never runs it. Repro, for a finding DevDesk built from a tool that ran on this machine, is the one command that reproduces it, to run from the scanned directory, without the arguments you set for the tool; when the tool ran in a container the line says there is none, and no line is shown when no plain command says it (a path with a space, a finding in rendered output). A sentence such as 'Update foo to 1.2', for an ecosystem DevDesk has no command for, is a step under Do, never a command. So is a command that is not plain words (a line break, a control character, a shell metacharacter such as ; | & $ or a quote): its text is under Do, cleaned, and Y cannot copy it. Effect is what the fix does, or 'Fixed in <version>', and Warning is what the built-in fix may break.\n" +
					"Change is what the built-in fix would write, shown before you ask for it: the file it edits, then its diff (+ for an added line, - for a removed one, @@ for where a hunk starts), at most 8 rows with '… N more lines' after them (the Ctrl+O confirmation shows every row), then Git: what git will and will not be able to undo. It is computed when the detail opens, and again after a re-scan; the footer says 'Preparing the change preview...' meanwhile. A vulnerability with a fixed version shows the move ('libfoo 0.8.0 -> 0.17.0') and never a diff, since DevDesk does not write lockfiles. Where DevDesk cannot write the fix (a finding it has no rule for, an image, a rule that declines this instance) it says 'none: DevDesk cannot write this fix'. A file written after the scan ended shows no diff, only 'stale preview', and Ctrl+O is greyed with the same reason until a re-scan: the lines the scan reported may no longer be the file's. A preview that could not be computed says so, and Ctrl+O stays available, since it computes the fix again. Rows longer than the column are cut with an ellipsis; the confirmation has them whole.\n" +
					"Why holds the Description, the Pod Security profile, the Source and the Type. Output is what the scanner said about this instance, which tells two findings of one rule apart: at most 8 lines of at most 110 cells, then '… N more lines'; the rest is in the log. An Output that only repeats the title or the description is not shown. Refs lists the references.\n" +
					"The detail has six keys, the same for every finding, each greyed with its reason in the footer when it has nothing to act on. Esc goes back. W opens the first reference. Enter opens the finding's file in the viewer (O there opens the IDE, esc comes back to the detail); whether the file exists is for the viewer to say, and a path outside the scanned directory, a file reached through a link that leaves it, or a target that is an image, is refused. Ctrl+O applies the built-in fix of this finding, with the same preview, the same confirmation that defaults to No, the same write and the same re-scan as the Misconfigurations tab; while the re-scan runs the footer says so, and when it ends the detail shows the finding as the re-scan read it, or closes if the finding is gone, with the verdict in the footer. Y copies the command under Run, or else the Repro line, to the clipboard, whole even when the screen cuts it; DevDesk never runs a command. X adds a Gitleaks secret to .gitleaksignore, after a confirmation. Each acts on the finding on screen, not on the row behind it.\n" +
					"Paths in the title, Where and Output are shown relative to the scanned directory (an absolute path under your home directory starts with ~); the finding itself is not changed.\n" +
					"When the detail is taller than the viewport, the footer says which lines are on screen: 'Lines a-b of N'. A long detail scrolls with the arrows, page keys, Home and End.",
			},
			{
				Title: "Group by fix",
				Body: "On the CVE tab, g replaces the table of findings with one row per cause: a package at one installed version, the move that clears its CVEs (1.1.1k -> 1.1.1w), how many CVEs that is, and the fix status. The worst severity comes first, then the group with the most CVEs. A group is listed when at least one of its CVEs passes the severity filters and the search, and its count is the whole group, since one move clears all of it. g again brings the findings table back, with its filters, sort and search as they were, on the first finding of the group you left.\n" +
					"Fixable in the header still counts findings with a fixed version; a group with no fixed version says 'No fix published' and is not counted in it. When several candidate versions cannot be ordered, the row lists them and none is preferred. Nothing is marked as done: the list is built from the last scan, and a re-scan after a fix is the only thing that shrinks a row or removes it.\n" +
					"On the Misconfigurations tab, g gives one row per file and per fix: the worst severity, the file (an image's findings name none and say '(image)'), the rule, how many findings that is, and the state of the built-in fix in the words of the finding detail. Two states of one rule in one file are two rows, and so are two instances the built-in fix declines for different reasons. The count is a number of findings, not a promise: Ctrl+O fixes one finding at a time, from its detail. The Pod Security filter (p), the severity filters and the search apply as on the flat list, and Fixable in the header is not changed by it.\n" +
					"Enter opens the group's findings, the filtered-out ones included, and enter there opens a finding's detail, the same detail as from the table; on the Misconfigurations tab Ctrl+O there applies the built-in fix with the same confirmation, and when the re-scan comes back the group is read again, so its count drops or the row goes. Esc goes back one level at a time. Y copies the group's command (for example apk upgrade openssl, or the command every finding of a misconfiguration group agrees on) to the clipboard, whole; DevDesk never runs it. Y is greyed for a group with no fix published or no plain command. S, space and ctrl+o act on one finding and are greyed in the list: open the group first. The tab bar keeps its six tabs; the mode is held per tab for as long as the view lives, and the header says View: Group by fix while it is on.",
			},
			{
				Title: "Pod Security",
				Body: "On the Misconfigurations tab, a Kubernetes finding that a Pod Security Standards profile requires starts its title with that profile: baseline or restricted. The label is there under every setting. restricted includes baseline, so a restricted finding is one the baseline profile does not ask for.\n" +
					"The profile comes from a table of Trivy's rules kept in DevDesk, checked against Trivy's own output. A rule the table lacks but whose reference carries a #baseline or #restricted anchor is labelled with it and marked '(from Trivy, no fix)': the profile is Trivy's, and DevDesk offers no fix for it. A rule that links the standard without an anchor, or not at all, has no label. KSV-0104 is baseline when the profile is explicitly Unconfined and restricted when none is set, read from the manifest at scan time; a result scanned before that, or a file changed since, counts as baseline.\n" +
					"scan.pss_profile (scan tab of the configuration view) is all, baseline or restricted, and all by default. Under baseline or restricted, p hides what that profile does not require, and the header says the profile, the state of the filter and how many findings it hides. The filter is off whenever the tab opens, and p is greyed, with the reason, under all or elsewhere than the Misconfigurations tab. A value outside the three is read as all, with a warning on entering the tab.\n" +
					"When the filter leaves no Pod Security finding, the header says 'no violation detected by Trivy for <profile>': that is what one scanner found in these manifests, not proof of compliance.",
			},
			{
				Title: "Remediation",
				Body: "For a repository, this tab reads the Dockerfiles under it and lists each base image — every stage, since a vulnerability in a build stage can reach the image that ships — with the newer tags it could move to. Candidates keep the image's variant (alpine stays alpine, slim stays slim) and its precision (3.18 is offered 3.21, not 3.21.1); by default they stay on the same major version, and the scan tab of the configuration view (Base image bumps) lets them take the next one.\nNothing is measured until you press S: each image is scanned straight from its registry, without being pulled, and the counts are kept for 24 hours. The vs now column is the change in CRITICAL plus HIGH against the image as written; negative is better. A candidate is a proposal with its evidence — the scan says the CVEs are gone, not that the application still runs on the new base.\nThe Update column puts an arrow on a base image whose registry has something newer: a later patch tag, or new content behind the same tag (the arrow alone — a new build) — compared with the digest the Dockerfile pins, or with the image the engine holds under that name; one the engine does not hold only gets the patch side, and shows a cloud (not local). The other states, each an icon: a check mark (up to date), an hourglass (no answer yet), a question mark in a circle (the registry did not answer), a pin (a digest with no tag, which cannot move). The registries are asked in the background when the tab opens, and each answer is kept 6 hours.\nA floating tag — latest, an untagged reference, main, a codename, or a vendor tag rebuilt in place such as Docker Hardened Images' — has no version to move to, so it lists no candidates and says so. Its count is of what the tag pointed to when it was scanned, and the registry may have replaced that since: S scans it again every time, whatever its age, rather than trusting the 24 hours. Nothing is written for it — pinning it by digest would stop it following upstream fixes.\nAn image scan has no Dockerfile, so the tab is empty for one.\nChoosing: space picks the candidate under the cursor for its stage — only one that has been scanned, since a bump is proposed with its evidence — and Enter shows what the choices would change. Ctrl+O writes them, and only when you ask: it lists each file and line, and says what git will be able to undo (a clean tracked file can be reverted with git checkout; one with uncommitted changes cannot without losing them; an untracked file or one outside a repository cannot at all). The answer defaults to No. Only the image reference changes — comments, line endings and the rest of the file are untouched — and DevDesk makes no commit, branch or push. A file that changed since the diff was computed is refused, not overwritten. A reference pinned by digest loses its pin, which the modal says. A stage that reads its image from an ARG changes that ARG's default, and a --build-arg on the command line can still override it.\n" +
					"The Sig column is each image's signature (§3.82), checked in the background when the tab opens: the base image against your rules (~/.devdesk/trust.yaml) and the built-in ones, a candidate also against whoever signed that base. A check mark is a proven signature, a dash no rule to check against, an hourglass a check still running. A red cross is refused — an unexpected identity, or no signature where a rule asks for one: the candidate stays listed, since a tag republished by someone else is worth seeing, but space will not choose it and says which rule refused it. An orange sign warns — unsigned where nothing required it, or a check that could not run: it can be chosen, and the Ctrl+O confirmation repeats the warning. A verdict that arrives after you chose releases the choice. Turn the check off per context by unticking Verify image signatures (scan tab of the configuration view); the header then says Signatures: off.",
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
