package security

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/trust"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/ui/updatecol"
)

// The Remediation tab (§3.2, phase B): for each base image a repository's
// Dockerfiles build on, the newer tags it could move to, and what a scan of each
// finds — so a bump is proposed with its evidence rather than on trust.
//
// Nothing is measured until the user asks. Opening the tab reads the
// Dockerfiles and lists tags, which is cheap; `S` scans, which costs minutes.

const (
	// remediationCandidates caps the tags proposed per image. Each is a scan.
	remediationCandidates = 3
	// remediationFreshFor is how long a scan stands before `S` measures it again:
	// the vulnerability database moves daily, so a week-old count is a claim
	// about a different database.
	remediationFreshFor = 24 * time.Hour
)

// The reasons the tab's actions do not apply (Rule 130).
const (
	reasonNotRemediationTab = "Candidate scans belong to the Remediation tab"
	reasonFindingsOnly      = "That key works on findings — open another tab"
	reasonNoBaseImage       = "No base image to scan"
	reasonAllMeasured       = "Every image is already scanned — results are kept for 24 hours, except on a floating tag"
	reasonNoImageRow        = "No image selected"
	reasonPickACandidate    = "Space selects a candidate — move to a tag under the image"
	reasonScanFirst         = "Scan this candidate first (S) — a bump is proposed with its result, not without"
	reasonNothingChosen     = "Select a scanned candidate with space first"
	reasonNotEditable       = "This reference cannot be edited in place"
)

type remediationPhase int

const (
	// phaseIdle: the tab has not been opened for this result.
	phaseIdle remediationPhase = iota
	// phaseLoading: the Dockerfiles are being read and their tags listed.
	phaseLoading
	// phaseReady: entries are known, empty or not.
	phaseReady
)

// remediationState is everything the tab holds. It lives on the model beside
// findingsTable, and is reset whenever the result on screen changes.
type remediationState struct {
	table     datatable.Model[remediationRow]
	phase     remediationPhase
	target    string
	entries   []remediation.Entry
	truncated bool
	failed    bool
	results   map[string]cache.RemediationEntry
	scanning  map[string]bool
	// selected is the candidate chosen for each stage, by index into entries: at
	// most one per stage, and only a candidate that has been scanned.
	selected map[int]string
	// pending is what the confirmation on screen would write. It is held from
	// the moment the modal opens to the answer, so the write is exactly what was
	// shown — never recomputed from a table that may have moved.
	pending []preparedWrite
	// updates is what the registries said about each base image, and
	// localDigests the digests it is compared with — the reference's own pin,
	// or the image the engine holds under that name (§3.88).
	updates      imageupdate.Tracker
	localDigests map[string][]string
	// signatures is each image's signature verdict, and verifying the ones
	// still being asked (§3.82) — keyed by reference as written.
	signatures map[string]trust.Result
	verifying  map[string]bool
}

func newRemediationState() remediationState {
	return remediationState{
		table: datatable.New(datatable.Config[remediationRow]{
			Columns:    remediationColumns(),
			SortColumn: -1, // the order is the grouping: an image, then its candidates
		}),
		results:    map[string]cache.RemediationEntry{},
		scanning:   map[string]bool{},
		selected:   map[int]string{},
		signatures: map[string]trust.Result{},
		verifying:  map[string]bool{},
	}
}

// remediationRow is a line of the table: a base image as written, or one of the
// tags it could move to.
type remediationRow struct {
	// Entry indexes remediationState.entries; Ref is the reference without the
	// display indent, the one a selection and a write use.
	Entry int
	Ref   string
	// Selected: this candidate is the one chosen for its stage.
	Selected bool

	File  string
	Stage string
	// Image is the reference. A candidate is indented under the image it would
	// replace.
	Image   string
	Current bool
	// Note is why the image has no candidates, or what else the reader should
	// know about the row.
	Note string

	Scanned   bool
	Scanning  bool
	Counts    scan.SeverityCounts
	ScannedAt time.Time
	// Baseline is the current image's CRITICAL+HIGH, and HasBaseline whether it
	// is known: a delta needs both sides.
	Baseline    int
	HasBaseline bool
	// Update is whether a newer image exists for the base as written (§3.88).
	// Only a current row has one: a candidate is already the newer image.
	Update imageupdate.Status
	// Signature is what its signature check found (§3.82).
	Signature signatureState
}

// icon is the glyph column: the image itself, or a candidate's checkbox.
func (r remediationRow) icon() string {
	switch {
	case r.Current:
		return theme.IconDocker
	case r.Selected:
		return theme.IconChecked
	}
	return theme.IconCheckbox
}

// worst is the number the comparison is made on. MEDIUM and LOW are listed but
// do not decide a bump.
func (r remediationRow) worst() int { return r.Counts.Critical + r.Counts.High }

// delta is the change in CRITICAL+HIGH a candidate would bring, and false when
// it cannot be said — the row is the current image itself, or either side has
// not been scanned.
func (r remediationRow) delta() (int, bool) {
	if r.Current || !r.Scanned || !r.HasBaseline {
		return 0, false
	}
	return r.worst() - r.Baseline, true
}

// remediationRows lays entries out as the table shows them: each base image,
// then its candidates.
func remediationRows(entries []remediation.Entry, results map[string]cache.RemediationEntry,
	scanning map[string]bool, selected map[int]string, update func(ref string) imageupdate.Status) []remediationRow {
	var rows []remediationRow
	for i, e := range entries {
		current := remediationRowFor(e.File, e.StageLabel, imageLabel(e), true, e.Image, results, scanning)
		current.Entry = i
		if update != nil && e.Image != "" {
			current.Update = update(e.Image)
		}
		if len(e.Candidates) == 0 {
			current.Note = e.Reason
		}
		rows = append(rows, current)
		for _, ref := range e.Candidates {
			row := remediationRowFor("", "", "  "+ref, false, ref, results, scanning)
			row.Entry = i
			row.Selected = selected[i] == ref
			if current.Scanned {
				row.Baseline, row.HasBaseline = current.worst(), true
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// imageLabel is what the Image column shows for the current image. One that
// could not be resolved shows what was written, so the row still says which
// FROM it is about.
func imageLabel(e remediation.Entry) string {
	if e.Image != "" {
		return e.Image
	}
	return e.Stage.Written
}

func remediationRowFor(file, stage, label string, current bool, ref string,
	results map[string]cache.RemediationEntry, scanning map[string]bool) remediationRow {
	row := remediationRow{File: file, Stage: stage, Image: label, Current: current, Ref: ref}
	if ref == "" {
		return row
	}
	row.Scanning = scanning[ref]
	if entry, ok := results[ref]; ok {
		row.Scanned = true
		row.ScannedAt = entry.ScannedAt
		row.Counts = scan.SeverityCounts{Critical: entry.Critical, High: entry.High, Medium: entry.Medium, Low: entry.Low}
	}
	return row
}

// remediationColumns describes the table. Every cell is plain text and colour
// goes through Style (Rule 122).
func remediationColumns() []datatable.Column[remediationRow] {
	count := func(title, severity string, get func(scan.SeverityCounts) int) datatable.Column[remediationRow] {
		return datatable.Column[remediationRow]{
			Title: title, Sizing: datatable.SizingFixed, MinWidth: countColumnWidth,
			Cell: func(r remediationRow) string {
				if !r.Scanned {
					return "-"
				}
				return strconv.Itoa(get(r.Counts))
			},
			Style: func(r remediationRow) lipgloss.Style {
				if !r.Scanned || get(r.Counts) == 0 {
					return theme.DimStyle
				}
				return theme.SeverityTextStyle(severity)
			},
		}
	}
	return []datatable.Column[remediationRow]{
		{
			// The glyph has its own column (Rule 125): the image's kind on a
			// current row, and on a candidate the checkbox that says whether it
			// is the one chosen for the patch.
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(r remediationRow) string { return r.icon() },
			Style: func(remediationRow) lipgloss.Style { return theme.IconStyle(theme.IconRoleImage) },
		},
		{
			Title: "File", Sizing: datatable.SizingContent, MinWidth: 10, MaxWidth: 30, Optional: true, TruncateHead: true,
			Cell: func(r remediationRow) string { return r.File },
		},
		{
			Title: "Stage", Sizing: datatable.SizingContent, MinWidth: 5, MaxWidth: 14,
			Cell: func(r remediationRow) string { return r.Stage },
		},
		{
			Title: "Image", Sizing: datatable.SizingContent, MinWidth: 18, MaxWidth: 58, Flex: 1, TruncateHead: true,
			Cell: func(r remediationRow) string { return r.Image },
			Style: func(r remediationRow) lipgloss.Style {
				if r.Current {
					return lipgloss.NewStyle()
				}
				return theme.DimStyle
			},
		},
		updatecol.Column(true, func(r remediationRow) imageupdate.Status { return r.Update }),
		signatureColumn(),
		count("CRIT", "CRITICAL", func(c scan.SeverityCounts) int { return c.Critical }),
		count("HIGH", "HIGH", func(c scan.SeverityCounts) int { return c.High }),
		{
			// The comparison, in CRITICAL+HIGH: negative is the point of the tab.
			Title: "vs now", Sizing: datatable.SizingFixed, MinWidth: 8,
			Cell: func(r remediationRow) string {
				d, ok := r.delta()
				switch {
				case !ok:
					return ""
				case d == 0:
					return "="
				}
				return fmt.Sprintf("%+d", d)
			},
			Style: func(r remediationRow) lipgloss.Style {
				switch d, ok := r.delta(); {
				case !ok || d == 0:
					return theme.DimStyle
				case d < 0:
					return theme.StatusOKStyle
				}
				return theme.StatusErrorStyle
			},
		},
		{
			Title: "Scanned", Sizing: datatable.SizingFixed, MinWidth: 12,
			Cell: func(r remediationRow) string {
				switch {
				case r.Scanning:
					return "scanning..."
				case r.Scanned:
					return theme.TimeAgo(r.ScannedAt)
				}
				return "-"
			},
			Style: func(r remediationRow) lipgloss.Style {
				if r.Scanning || !r.Scanned {
					return theme.DimStyle
				}
				return lipgloss.NewStyle()
			},
		},
		{
			Title: "Note", Sizing: datatable.SizingContent, MinWidth: 10, MaxWidth: 60, Optional: true,
			Cell:  func(r remediationRow) string { return r.Note },
			Style: func(remediationRow) lipgloss.Style { return theme.DimStyle },
		},
	}
}

// ── Opening the tab ──────────────────────────────────────────────────────────

// resetRemediation forgets everything the tab knew, for a result that is not
// the one it was read for.
func (m *Model) resetRemediation() {
	m.remediation = newRemediationState()
	m.remediation.table.Resize(m.width, max(m.height, 5))
}

// enterRemediation is what switching to the tab does: read the Dockerfiles the
// first time. It returns the Cmd to run, if any.
//
// An image has no Dockerfile: the tab is on screen and says so rather than
// disappearing, so the set of tabs does not depend on what was scanned.
func (m *Model) enterRemediation() tea.Cmd {
	if m.result == nil || m.remediation.phase != phaseIdle {
		return nil
	}
	m.remediation.target = m.result.Target
	if m.result.TargetType != scan.TargetDirectory {
		m.remediation.phase = phaseReady
		return nil
	}
	// The tick is taken before the phase flips, or spinnerTickIfIdle would
	// answer "already running" about the chain this is trying to start.
	tick := m.spinnerTickIfIdle()
	m.remediation.phase = phaseLoading
	return tea.Batch(tick, discoverRemediationCmd(m.result.Target, remediation.ParseTrack(m.config.Scan.BaseImageTrack)))
}

func (m Model) handleRemediationDiscovered(msg RemediationDiscoveredMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.remediation.target || m.remediation.phase != phaseLoading {
		return m, nil // an answer about a result no longer on screen
	}
	m.remediation.phase = phaseReady
	if msg.Err != nil {
		m.remediation.failed = true
		return m, m.footer.Error("Failed to read the Dockerfiles — check logs")
	}
	m.remediation.entries = msg.Entries
	m.remediation.truncated = msg.Truncated
	m.remediation.results = msg.Results
	var refs []string
	for _, e := range msg.Entries {
		refs = append(refs, e.Image)
	}
	cmds := append(m.startSignatureChecks(),
		checkBaseImageUpdatesCmd(m.remediation.target, m.remediation.updates.Due(refs, time.Now())))
	m.refreshRemediation()
	return m, tea.Batch(cmds...)
}

func (m Model) handleBaseImageUpdatesChecked(msg BaseImageUpdatesCheckedMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.remediation.target {
		return m, nil
	}
	if m.remediation.localDigests == nil {
		m.remediation.localDigests = map[string][]string{}
	}
	for ref, d := range msg.Local {
		m.remediation.localDigests[ref] = d
	}
	m.remediation.updates.Store(msg.Facts)
	m.refreshRemediation()
	return m, nil
}

// baseImageUpdate is the Update cell of a base image.
func (m Model) baseImageUpdate(ref string) imageupdate.Status {
	return m.remediation.updates.Status(ref, m.remediation.localDigests[ref], imageupdate.NotLocal)
}

func (m Model) handleRemediationScanFinished(msg RemediationScanFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.Target != m.remediation.target {
		return m, nil
	}
	delete(m.remediation.scanning, msg.Ref)
	if msg.Err != nil {
		m.refreshRemediation()
		return m, m.footer.Error("Scan of " + msg.Ref + " failed — check logs")
	}
	m.remediation.results[msg.Ref] = msg.Entry
	m.refreshRemediation()
	if !m.remediationBusy() {
		return m, m.footer.Info("Candidate scans finished")
	}
	return m, nil
}

// refreshRemediation rebuilds the rows, keeping the cursor where it was.
func (m *Model) refreshRemediation() {
	cursor := m.remediation.table.Cursor()
	rows := remediationRows(
		m.remediation.entries, m.remediation.results, m.remediation.scanning, m.remediation.selected, m.baseImageUpdate)
	for i := range rows {
		res, known := m.remediation.signatures[rows[i].Ref]
		rows[i].Signature = signatureState{Result: res, Known: known, Verifying: m.remediation.verifying[rows[i].Ref]}
	}
	m.remediation.table.SetItems(rows)
	m.remediation.table.Remeasure()
	m.remediation.table.SetCursor(cursor)
}

// remediationBusy is true while anything is being read or scanned, which is
// what keeps the view's spinner going.
func (m Model) remediationBusy() bool {
	return m.remediation.phase == phaseLoading || len(m.remediation.scanning) > 0
}

// ── Scanning candidates ──────────────────────────────────────────────────────

// refsToScan are the images with no result, or one older than remediationFreshFor
// — each once, however many stages name it — and not already being scanned.
//
// An image on a floating tag is scanned whatever its result's age (§3.79): the
// cache is keyed by the reference as written, which does not change when the
// registry replaces what it points to, so a result an hour old may be about an
// image that is no longer there.
func (m Model) refsToScan(now time.Time) []string {
	seen := map[string]bool{}
	var refs []string
	add := func(ref string, floating bool) {
		if ref == "" || seen[ref] || m.remediation.scanning[ref] {
			return
		}
		seen[ref] = true
		if entry, ok := m.remediation.results[ref]; ok && !floating && now.Sub(entry.ScannedAt) < remediationFreshFor {
			return
		}
		refs = append(refs, ref)
	}
	for _, e := range m.remediation.entries {
		add(e.Image, e.Floating)
		for _, c := range e.Candidates {
			add(c, false)
		}
	}
	return refs
}

// hasBaseImage is whether the tab holds any image at all, as opposed to holding
// only rows that could not be resolved.
func (m Model) hasBaseImage() bool {
	for _, e := range m.remediation.entries {
		if e.Image != "" {
			return true
		}
	}
	return false
}

// canScanCandidates is the one computation behind the shortcut column and the
// handler (Rule 130). An operation in flight is not a cause of refusing here —
// it changes on every message — so it is the handler that says "already".
func (m Model) canScanCandidates() shortcut.Availability {
	switch {
	case m.activeTab != TabRemediation:
		return shortcut.Unavailable(reasonNotRemediationTab)
	case m.remediation.phase == phaseLoading:
		return shortcut.Availability{} // not known yet is not a no
	case !m.hasBaseImage():
		return shortcut.Unavailable(reasonNoBaseImage)
	case len(m.refsToScan(time.Now())) == 0 && len(m.remediation.scanning) == 0:
		return shortcut.Unavailable(reasonAllMeasured)
	}
	return shortcut.Availability{}
}

// scanCandidates measures every image that has not been, from its registry.
func (m Model) scanCandidates() (tea.Model, tea.Cmd) {
	if a := m.canScanCandidates(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	if m.remediation.phase == phaseLoading {
		return m, m.footer.Warn("Still reading the Dockerfiles")
	}
	refs := m.refsToScan(time.Now())
	if len(refs) == 0 {
		return m, m.footer.Warn("Scan already in progress")
	}
	tick := m.spinnerTickIfIdle()
	for _, ref := range refs {
		m.remediation.scanning[ref] = true
	}
	m.refreshRemediation()

	timeout := time.Duration(m.config.Scan.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	cmds := scanRemediationCmds(m.remediation.target, refs, m.scanOptions(), timeout)
	return m, tea.Batch(append(cmds, tick)...)
}

// ── Keys ─────────────────────────────────────────────────────────────────────

// handleRemediationKey answers the keys the tab owns. handled is false for
// everything the results state treats the same on every tab: leaving, tab,
// refresh.
//
// The keys that filter or open findings are refused here, with the reason,
// rather than reaching the findings table behind the tab: it is empty on this
// tab, but a search opened on it would take the keyboard from a table nobody
// can see.
func (m Model) handleRemediationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch key := msg.String(); key {
	case keymap.Scan:
		next, cmd := m.scanCandidates()
		return next, cmd, true
	case " ":
		next, cmd := m.toggleCandidate()
		return next, cmd, true
	case "enter":
		next, cmd := m.showRemediationDiff()
		return next, cmd, true
	case writeRemediationKey:
		next, cmd := m.prepareRemediationWrite()
		return next, cmd, true
	case "c", "h", "m", "l", "/", ".", keymap.Exclude, openPipelineKey:
		return m, m.footer.Warn(reasonFindingsOnly), true
	case "up", "down", "pgup", "pgdown", "home", "end":
		return m, m.remediation.table.Update(msg), true
	}
	return m, nil, false
}

// ── What the footer says ─────────────────────────────────────────────────────

// remediationStatus is the tab's line in the footer: what is happening, or why
// the table is empty (Rules 128 and 139 — the body stays a table).
func (m Model) remediationStatus() (sharedcomponents.Status, bool) {
	if m.state != StateResults || m.activeTab != TabRemediation {
		return sharedcomponents.Status{}, false
	}
	switch {
	case m.remediation.phase == phaseLoading:
		return sharedcomponents.Status{Text: "Reading Dockerfiles and listing tags...", Spinner: true}, true
	case len(m.remediation.scanning) > 0:
		return sharedcomponents.Status{
			Text:    fmt.Sprintf("Scanning %d image(s) from their registries...", len(m.remediation.scanning)),
			Spinner: true,
		}, true
	case m.remediation.phase != phaseReady || m.remediation.failed:
		return sharedcomponents.Status{}, false
	case m.result != nil && m.result.TargetType != scan.TargetDirectory:
		return sharedcomponents.Status{Text: "An image has no Dockerfile — open a repository"}, true
	case len(m.remediation.entries) == 0:
		return sharedcomponents.Status{Text: "No Dockerfile with a base image in this repository"}, true
	case m.remediation.truncated:
		return sharedcomponents.Status{Text: "Showing the first Dockerfiles only — the repository holds more"}, true
	}
	return sharedcomponents.Status{}, false
}

// remediationTabLabel counts the base images once they are known.
func (m Model) remediationTabLabel() string {
	if m.remediation.phase != phaseReady {
		return "Remediation"
	}
	n := 0
	for _, e := range m.remediation.entries {
		if e.Image != "" {
			n++
		}
	}
	return fmt.Sprintf("Remediation (%d)", n)
}
