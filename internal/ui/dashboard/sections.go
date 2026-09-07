package dashboard

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// section is one titled box of the dashboard. Sections are declared in a
// table rather than as that many render*Section methods: that's what makes
// the rule "a section declares its height and fills it" checkable for all
// of them at once (§3.19 phase 1).
type section struct {
	title string
	// render returns the box's content lines. It returns the *same count* of
	// them whatever the state of the data — that's the contract
	// TestEverySectionKeepsItsHeightWhateverItsState checks.
	render func(m Model, width int, t tier) []string
}

// labelWidth aligns every label column of every box on one width.
const labelWidth = 11

// overviewSections returns the four boxes of the Overview tab, in reading
// order: the two text boxes first, then the two that carry a chart.
//
// The grouping is by question asked, not by data source — hence two
// merges: workspaces joins GitLab (the explorer creates, workspaces
// reconciles: one subject seen from both ends) and the tools join Host
// (they are this machine's binaries, measured by the same probe).
func overviewSections(forgeType string) []section {
	return []section{
		{title: theme.ForgeIcon(forgeType) + " Code", render: renderCodeSection},
		{title: theme.IconSecurity + " Health", render: renderHealthSection},
		{title: theme.IconServer + " " + hostLabel(), render: renderHostSection},
		{title: theme.IconDocker + " Docker (VM)", render: renderDockerSection},
	}
}

// resourceSections returns the boxes of the Resources tab — inline in the
// third column at `wide`. They exist only because no view shows them: there
// is no :host, and :containers shows a container, not the machine.
func resourceSections() []section {
	return []section{
		{title: theme.IconArrowDown + " Network", render: renderNetworkSection},
		{title: theme.IconDirectory + " Storage", render: renderStorageSection},
	}
}

// hostLabel names the measurement point rather than the machine. gopsutil reads
// the OS dk runs on, while docker stats reads inside the Docker Desktop VM:
// both are true and they do not add up.
func hostLabel() string {
	switch runtime.GOOS {
	case "windows":
		return "Host (Windows)"
	case "darwin":
		return "Host (macOS)"
	default:
		return "Host (Linux)"
	}
}

func renderCodeSection(m Model, width int, t tier) []string {
	v := m.vocab()
	// The icon follows the value: the value column then starts at the same
	// place on every line, which an icon up front would shift by one cell
	// only on the lines that carry one.
	sessionLine := theme.DimStyle.Render("not connected") + theme.Bg("  ") + theme.DimStyle.Render(theme.IconError)
	if m.shared.IsAuthenticated {
		sessionLine = theme.PrimaryColorStyle.Bold(true).Render(m.shared.CurrentUser.Username) +
			theme.Bg("  ") + theme.StatusOKStyle.Render(theme.IconOK)
	}

	mrs, review, issues := unknownValue(), unknownValue(), unknownValue()
	if m.forgeStats != nil {
		mrs = maybeCountValue(m.forgeStats.AssignedChangeRequests)
		review = maybeCountValue(m.forgeStats.ReviewChangeRequests)
		issues = maybeCountValue(m.forgeStats.AssignedIssues)
	}

	workspaces := unknownValue()
	if !m.loadingWorkspaces {
		workspaces = countValue(m.workspaceCount)
	}

	if t != tierWide {
		return []string{
			sessionLine,
			row(v.ChangeRequestShort+"s", mrs+theme.Bg(" assigned  ")+review+theme.Bg(" to review")),
			row("Issues", issues+theme.Bg(" assigned")),
			theme.Bg(""),
			row("Workspaces", workspaces),
			theme.DimStyle.Render(truncatePath(m.config.App.WorkspacesDir, width-labelWidth)),
		}
	}

	// At `wide`, the box becomes two trees, and the split is §3.16's two
	// halves: the explorer creates what doesn't exist — the forge — and
	// workspaces reconciles what does — the disk. Each has its own question,
	// so each has its own root, and the forge's facts stop floating above a
	// tree they don't belong to.
	//
	// The counters become branches there: "0 assigned  0 to review" on one
	// line requires rereading the sentence to know which is which, two
	// nodes say it by lining them up.
	// The qualifier is in the label, not in the value: "issues 0 assigned"
	// and "MR assigned  0" said the same thing two different ways, and the
	// value column then didn't carry the same kind of thing everywhere. The
	// three counters now read in a column.
	pathWidth := theme.BoxContentWidth(width) - treeValueColumn
	return []string{
		theme.Bg(v.Name),
		branch(false, "user", sessionLine),
		branch(false, "host", theme.Bg(forgeHost(m.config.Forge.URL))),
		branch(false, "issues assigned", issues),
		branch(false, v.ChangeRequestShort+" assigned", mrs),
		branch(true, v.ChangeRequestShort+" to review", review),
		treeGap(),
		theme.Bg("Workspaces"),
		branch(false, "repositories", workspaces),
		branch(false, "path", theme.DimStyle.Render(truncatePath(m.config.App.WorkspacesDir, pathWidth))),
		branch(true, "disk", treeSize(m.wsSize)),
	}
}

// treeGap separates two trees stacked in one box. The last node's elbow
// says where one tree ends, but not that another begins: without this
// line, the two roots read as two more nodes.
func treeGap() string { return theme.Bg("") }

// treeSize renders what the workspaces occupy.
//
// **This is the tree's size, not the volume's fill level.** What these
// repositories cost is what you can act on — deleting one frees the space —
// whereas the volume mixes the workspaces in with the rest of the machine.
// The Host box keeps the free space, which is the other question.
//
// It comes at a cost: it's the only dashboard figure whose measurement
// walks the entire tree. See metrics.Size and Model.measuringSize.
func treeSize(size metrics.TreeSize) string {
	if !size.OK {
		return unknownValue()
	}
	out := theme.Bg(humanBytes(size.Bytes))
	if size.Partial {
		// A directory that denies access makes the total an underestimate,
		// and an underestimated total with no mention reads as a
		// measurement.
		out += theme.DimStyle.Render("  partial")
	}
	return out
}

// forgeHost strips the scheme off the configured forge URL: it's the host
// that identifies the instance, and `https://` is true of all of them.
func forgeHost(rawURL string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

// treeLabelWidth aligns the values of a tree's nodes, indent included. It
// is sized on the application's longest label — `issues assigned`, fifteen
// cells — because a label that overflows doesn't just break the alignment
// of its own line: it opens a second value column in the box.
// TestEveryTreeNodeAlignsItsValue checks it against every section.
const treeLabelWidth = 17

// treeStemWidth is what IconTreeBranch and IconTreeEnd occupy: two
// box-drawing characters and the space after them. It is written rather
// than measured because a constant cannot call lipgloss.Width, and
// TestATreeRowLinesUpWithItsBranches checks it against the actual
// rendering.
const treeStemWidth = 3

// treeValueColumn is where a tree node's value begins.
const treeValueColumn = treeStemWidth + treeLabelWidth - 1

// narrowTreeLabelWidth is the same column inside a **split** box, where each
// tree gets half the width.
//
// It is sized on `reclaimable`, eleven cells, the longest label of the
// three boxes concerned. The wide column would cost three more value cells
// there, out of the eleven a half-box leaves at the narrowest `wide` tier
// (180 columns) — enough to truncate `3 days ago`.
const (
	narrowTreeLabelWidth  = 14
	narrowTreeValueColumn = treeStemWidth + narrowTreeLabelWidth - 1
)

// branch renders one node of a tree. `last` picks the corner, so the run of
// nodes has a visible end — without it, two trees in a row read as one.
func branch(last bool, label, value string) string {
	return branchAt(treeLabelWidth, last, label, value)
}

// narrowBranch is a node of a tree that shares its box with another, side by
// side.
func narrowBranch(last bool, label, value string) string {
	return branchAt(narrowTreeLabelWidth, last, label, value)
}

func branchAt(column int, last bool, label, value string) string {
	stem := theme.IconTreeBranch
	if last {
		stem = theme.IconTreeEnd
	}
	padded := label
	if n := column - len(label) - 2; n > 0 {
		padded = label + strings.Repeat(" ", n)
	}
	return theme.DimStyle.Render(stem) + theme.Bg(padded+" ") + value
}

// sideBySide lays two runs of lines out as two columns of one box.
//
// The cut is in the middle, and each half is **truncated** rather than let
// to overflow: a line too long on the left would shift the whole right
// column, and a line too long on the right would overflow the box, which
// RenderTitledBox would cut anyway — but after breaking the alignment (Rule
// 116).
//
// The assembly is done line by line with PadWithBg and never with
// lipgloss.JoinHorizontal, which inserts bare spaces that let the
// terminal's native background show through (Rule 115).
func sideBySide(left, right []string, width int) []string {
	half := max(width/2, 1)
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		var l, r string
		if i < len(left) {
			// The gutter is subtracted from the left column, not added to the
			// right: without it, a value that fills its half touches the
			// elbow of the first node next to it and the two read as one.
			l = theme.Truncate(left[i], half-columnGutter)
		}
		if i < len(right) {
			r = theme.Truncate(right[i], width-half)
		}
		out[i] = theme.PadWithBg(l, half) + r
	}
	return out
}

// columnGutter is the blank kept between two columns of one box.
const columnGutter = 2

// padRuns lengthens a run with blank lines. The padding is **empty of
// content**, not padded to width: sideBySide lays down the whole line's
// background (Rule 115).
func padRuns(run []string, height int) []string {
	out := make([]string, 0, max(height, len(run)))
	out = append(out, run...)
	for len(out) < height {
		out = append(out, theme.Bg(""))
	}
	return out
}

func renderHealthSection(m Model, width int, t tier) []string {
	if t == tierWide {
		left, right := healthColumns(m)
		return sideBySide(left, right, theme.BoxContentWidth(width))
	}

	monitors, certs := unknownValue(), unknownValue()
	if !m.loadingServices {
		monitors = statusSummary(m.filterComponents(false))
		certs = certSummary(m.filterComponents(true))
	}
	expiry := nearestExpiry(m.filterComponents(true), m.loadingServices, true)

	// Six lines, not seven: the blank line that used to separate monitoring
	// from security paid for the certificate expiry. A seventh line here
	// would overflow the whole overview.
	total := m.posture.Total()
	scanned, critical, oldest := unknownValue(), unknownValue(), unknownValue()
	if m.posture.Read {
		scanned = countValue(total.Targets) + theme.Bg(" targets")
		critical = severityCount(total.Critical) + theme.Bg("  ") +
			unscannedValue(m.unscannedTotal()) + theme.DimStyle.Render(" unscanned")
		oldest = scanAge(total)
	}

	return []string{
		row("Monitors", monitors),
		row("Certs", certs),
		row("Expiry", expiry),
		row("Scanned", scanned),
		row("Critical", critical),
		row("Oldest", oldest),
	}
}

// healthColumns splits the box in two, and the split is by **subject rather
// than by kind**: supervision on the left with the repositories it watches over,
// certificates on the right with the images. The four trees used to fit in one
// column at nineteen lines, which made Health the tallest box in its row and
// cropped the other three boxes' curves by that much.
//
// They are rendered separately from their assembly: that's what lets them
// be checked one by one, since once joined the nodes of both trees share a
// line.
func healthColumns(m Model) (left, right []string) {
	unscannedImages, imagesMeasured := m.unscannedImages()
	unscannedRepos, reposMeasured := m.unscannedRepositories()

	monitors := append([]string{theme.Bg("Monitors")},
		statusBranches(m.filterComponents(false), m.loadingServices)...)

	// The expiry hangs from Certs rather than floating above: it's a fact
	// about the certificates, and it only makes sense there. The
	// certificate's name dropped along with the split into two columns —
	// the half-width box doesn't hold it — and it's :status that owns the
	// named list.
	//
	// This extra node is also what shifts the two columns: without the
	// catch-up below, `Repositories` would start a line above `Images` and
	// the two bottom trees would read like a staircase.
	certs := m.filterComponents(true)
	certTree := append([]string{theme.Bg("Certs")},
		certBranches(certs, m.loadingServices, nearestExpiry(certs, m.loadingServices, false))...)

	top := max(len(monitors), len(certTree))
	left = append(padRuns(monitors, top), treeGap(), theme.Bg("Repositories"))
	left = append(left, postureBranches(m.posture, m.posture.Repositories, unscannedRepos, reposMeasured)...)

	right = append(padRuns(certTree, top), treeGap(), theme.Bg("Images"))
	right = append(right, postureBranches(m.posture, m.posture.Images, unscannedImages, imagesMeasured)...)

	return left, right
}

// statusBranches renders one node per monitor state. All three states are
// always there, including at zero: a missing branch reads as a state that
// isn't monitored, which is the opposite of what a zero means.
//
// Certificates no longer go through here — see certBranches, and
// status.CertState for what tells them apart.
func statusBranches(components []status.ComponentStatus, loading bool) []string {
	nodes := func(up, down, errValue string) []string {
		return []string{
			narrowBranch(false, "up", up),
			narrowBranch(false, "down", down),
			narrowBranch(true, "error", errValue),
		}
	}

	switch {
	case loading:
		return nodes(unknownValue(), unknownValue(), unknownValue())
	case len(components) == 0:
		return nothingConfigured(len(nodes("", "", "")))
	default:
		ok, down, errCount := countStatuses(components)
		return nodes(
			countValue(ok)+theme.Bg("  ")+theme.StatusOKStyle.Render(theme.IconOK),
			alertCount(down, theme.IconError, theme.StatusDownStyle),
			alertCount(errCount, theme.IconWarning, theme.StatusErrorStyle),
		)
	}
}

// certBranches renders one node per certificate state, then the nearest
// expiry.
//
// **The vocabulary is not the monitors', and that was the bug.** A
// certificate rendered under `up` / `down` / `error` put an expired
// certificate, one expiring next week, and one that couldn't be read —
// SSLChecker gives `ERROR` to all three — into the same bucket, while `up`
// named "reachable" which means "valid". The four states of
// status.CertState tell them apart, and the one line that calls for action —
// `to renew` — stops being drowned in the other two.
//
// The expiry stays its own node: `to renew` says how many, it says when,
// and two figures on one line require remembering which is which (that's
// what postureBranches already settled).
func certBranches(certs []status.ComponentStatus, loading bool, expiry string) []string {
	nodes := func(valid, toRenew, expired, errValue string) []string {
		return []string{
			narrowBranch(false, "valid", valid),
			narrowBranch(false, "to renew", toRenew),
			narrowBranch(false, "expired", expired),
			narrowBranch(false, "error", errValue),
			narrowBranch(true, "expiry", expiry),
		}
	}

	switch {
	case loading:
		return nodes(unknownValue(), unknownValue(), unknownValue(), unknownValue())
	case len(certs) == 0:
		return nothingConfigured(len(nodes("", "", "", "")))
	default:
		valid, toRenew, expired, errored := status.CertCounts(certs)
		return nodes(
			countValue(valid)+theme.Bg("  ")+certGlyph(status.CertValid),
			certAlert(toRenew, status.CertToRenew),
			certAlert(expired, status.CertExpired),
			certAlert(errored, status.CertError),
		)
	}
}

// certGlyph and certAlert read one state's glyph and colour from the theme
// rather than naming them here: `:status` renders the same column, and two
// lookup tables would eventually diverge on the one that matters — the one
// that tells `expired` apart from `error`.
func certGlyph(state status.CertState) string {
	return theme.CertStateStyle(string(state)).Render(theme.CertStateIcon(string(state)))
}

func certAlert(n int, state status.CertState) string {
	return alertCount(n, theme.CertStateIcon(string(state)), theme.CertStateStyle(string(state)))
}

// nothingConfigured says a tree watches nothing, and keeps the height it
// would have had — a box that shrinks shifts its entire row.
func nothingConfigured(height int) []string {
	out := []string{narrowBranch(true, "configured", theme.DimStyle.Render("none"))}
	for range height - 1 {
		out = append(out, theme.Bg(""))
	}
	return out
}

// alertCount renders one non-nominal state's tally. The glyph is the
// state's own — a cross for DOWN, an alert for ERROR, an hourglass for a
// coming renewal, :status's vocabulary — including at zero: a check mark on
// the "down" line said "everything is fine" right where one is looking for
// how many are down, and it's the line's state an icon names, not its
// count.
//
// It's the color that carries the count: dimmed at zero, because a red
// cross on "0 down" teaches the reader the color instead of alerting them.
// The icon stays to the right of the number, as everywhere else.
func alertCount(n int, glyph string, style lipgloss.Style) string {
	if n == 0 {
		return countValue(0) + theme.Bg("  ") + theme.DimStyle.Render(glyph)
	}
	return severityCount(n) + theme.Bg("  ") + style.Render(glyph)
}

// postureBranches renders one family's tally, one figure per node.
//
// Each figure gets its own branch rather than sharing a line: two numbers
// side by side require remembering which is which, and that's exactly the
// figure read in a hurry.
//
// `unscanned` took HIGH's place because it's actionable: it names the
// targets the whole box says nothing about, and the answer is to run a
// scan. One more HIGH changed no decision the CRITICAL above hadn't already
// made.
func postureBranches(p posture, side postureSide, unscanned int, measured bool) []string {
	if !p.Read {
		return []string{
			narrowBranch(false, "scanned", unknownValue()),
			narrowBranch(false, "critical", unknownValue()),
			narrowBranch(false, "secrets", unknownValue()),
			narrowBranch(false, "unscanned", unknownValue()),
			narrowBranch(true, "oldest", unknownValue()),
		}
	}
	return []string{
		narrowBranch(false, "scanned", countValue(side.Targets)+theme.Bg(" targets")),
		narrowBranch(false, "critical", severityCount(side.Critical)),
		narrowBranch(false, "secrets", secretsValue(side)),
		narrowBranch(false, "unscanned", unscannedValue(unscanned, measured)),
		narrowBranch(true, "oldest", scanAge(side)),
	}
}

// secretsValue renders how many targets carry a secret.
//
// These are targets and not secrets: two repositories are two decisions,
// forty leaks in the same one are only one, and it's the inventory (:sec)
// that breaks it down.
//
// `-` when no verdict at all is known, which is not a theoretical
// precaution: `scan.enable_secret` turned off, a missing tool, or entries
// written before the image scan had a secrets step — in all three cases a
// `0` would say "no target carries any" for targets nobody has looked at. A
// verdict known on only part of the set is enough to display the count: it's
// then a floor, and a nonzero floor is actionable.
func secretsValue(side postureSide) string {
	if side.SecretsKnown == 0 {
		return unknownValue()
	}
	return severityCount(side.Secrets)
}

// unscannedValue renders a coverage gap. It does not take severityCount's
// red: a never-scanned target has no CRITICAL, it has an unknown, and the
// two are not fixed the same way.
func unscannedValue(n int, measured bool) string {
	if !measured {
		return unknownValue()
	}
	return countValue(n)
}

// severityCount colours a finding count only when there is one to find. A
// zero in red teaches the reader the color instead of alerting them.
func severityCount(n int) string {
	if n == 0 {
		return theme.DimStyle.Render("0")
	}
	return theme.StatusErrorStyle.Render(fmt.Sprintf("%d", n))
}

// scanAge renders how stale the oldest scan is. A CRITICAL count three
// weeks old is counting code that no longer exists.
// `never` rather than "nothing scanned": under a node called `oldest` the
// long sentence repeats the label, and a half-width box at the narrowest
// `wide` tier only holds eleven value cells.
func scanAge(side postureSide) string {
	if side.Targets == 0 {
		return theme.DimStyle.Render("never")
	}
	if side.Oldest.IsZero() {
		return unknownValue()
	}
	return theme.Bg(theme.TimeAgo(side.Oldest))
}

// nearestExpiry renders the deadline that comes first — the only one of
// the N dates that calls for a decision.
//
// **It only looks at what is still running.** A certificate already expired
// has no countdown left, and rendering it as "-2 days" required the reader
// to translate a negative number into a fact the `expired` node already
// states. Nothing ahead, then, reads `-`: that's what the line has always
// meant when it has no date to give.
//
// `named` is false in a shared column: a half-width box does not hold "58
// days  registry.example.com", and it's the number of days that's
// actionable. The named list belongs to :status, which already owns it.
func nearestExpiry(certs []status.ComponentStatus, loading, named bool) string {
	if loading {
		return unknownValue()
	}
	if len(certs) == 0 {
		return theme.DimStyle.Render("none configured")
	}

	var soonest *status.ComponentStatus
	for i, c := range certs {
		switch status.CertStateOf(c) {
		case status.CertValid, status.CertToRenew:
		default:
			// Expired or unreadable: both have their node, and neither has an
			// expiry still ahead.
			continue
		}
		if soonest == nil || *c.SSLDaysLeft < *soonest.SSLDaysLeft {
			soonest = &certs[i]
		}
	}
	if soonest == nil {
		return unknownValue()
	}

	days := *soonest.SSLDaysLeft
	value := theme.Bg(fmt.Sprintf("%d days", days))
	if days <= status.CertRenewWindowDays {
		value = theme.StatusErrorStyle.Render(fmt.Sprintf("%d days", days))
	}
	if !named {
		return value
	}
	return value + theme.DimStyle.Render("  "+soonest.Name)
}

// chartsPerBox is what a chart-bearing box holds — CPU and RAM, RX and TX.
// The three chart-bearing boxes share the same row, so the free height is
// split between the two curves of just one of them.
const chartsPerBox = 2

// Bounds: below 3 lines braille has no room to show its vertical
// resolution, and beyond 12 a percentage chart tells nothing more for the
// height it takes.
const (
	minBrailleHeight = 3
	maxChartHeight   = 12
)

// chartHeight is how many lines a chart gets.
//
// At `standard` it's 1: the grid barely fits there, so a chart takes the
// place of a blank line rather than adding to it.
//
// At `wide` it is **measured**, not derived from constants: View() first
// renders the grid with no curve at all, observes what's left, and passes
// the result back through `chartLines`. Constants describing the text
// boxes' height were right the day they were written and wrong the moment
// a box gained a line — which happened to Health the very same day.
func (m Model) chartHeight(t tier) int {
	if t != tierWide {
		return 1
	}
	if m.chartLines > 0 {
		return m.chartLines
	}
	return 0
}

func renderHostSection(m Model, width int, t tier) []string {
	// The load average is displayed nowhere: on Windows it returns
	// {0,0,0} with err=nil, hence a value indistinguishable from real data,
	// and a zero reads as "idle".
	cpu, ram := unknownValue(), unknownValue()
	if m.host.OK {
		// The percentage carries what makes it readable: 40% on four cores
		// and 40% on thirty-two don't describe the same machine, and 92% of
		// memory doesn't say whether two gigabytes are left or two
		// hundred.
		cpu = percentValue(m.host.CPUPercent) + coreSuffix(m.host.Cores)
		ram = percentValue(m.host.MemPercent) +
			theme.DimStyle.Render("  "+humanBytes(m.host.MemUsed)+" of "+humanBytes(m.host.MemTotal))
	}

	// CPU then its curve, RAM then its own: the three chart-bearing boxes
	// follow the same order, so their curves land on the same lines from
	// one column to another. That's what makes them comparable at a
	// glance, and an inserted detail line undid it.
	lines := []string{row("CPU", cpu)}
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.CPUPercent }, 100)...)
	lines = append(lines, row("RAM", ram))
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.MemPercent }, 100)...)

	// There is **no** Disk line here: free space was Storage's first fact,
	// word for word. This box measures what the CPU and memory do *right
	// now*; the disk doesn't move by the second and belongs to the box
	// that breaks it down.
	return append(lines, toolsBlock(m)...)
}

// toolsBlock says whether this machine can do the work, and names what it
// cannot.
//
// A counter — "4 of 5 available" — raises the question it doesn't answer:
// which one is missing, and hence what to install. The full list, on the
// other hand, costs five lines to say "yes" five times on a properly
// equipped machine. Hence the two forms: one line when everything is there,
// one node per missing tool otherwise. It's the only dashboard block whose
// height follows its data, and it can afford to — an installed tool doesn't
// get uninstalled between two refreshes, whereas a count changes every
// round.
//
// The missing ones are read against knownTools and not against what was
// detected: a tool absent from detection is simply absent, and a
// denominator that shrinks along with it would render "everything is
// there" for a machine that lost a probe.
func toolsBlock(m Model) []string {
	if m.loadingTools {
		return []string{row("Tools", unknownValue())}
	}

	missing := missingTools(m.tools)
	if len(missing) == 0 {
		return []string{row("Tools", theme.Bg("all available  ")+theme.StatusOKStyle.Render(theme.IconOK))}
	}

	// The names keep their declared case where the other nodes are
	// lowercase: `running` and `images` are words, `Gitleaks` is what needs
	// to be typed to install it.
	lines := []string{theme.Bg("Missing tools")}
	for i, name := range missing {
		lines = append(lines, narrowBranch(i == len(missing)-1, name,
			theme.StatusDownStyle.Render(theme.IconError)))
	}
	return lines
}

// missingTools returns the known tools this machine does not have, in the order
// knownTools declares them.
func missingTools(tools []shared.ToolInfo) []string {
	available := make(map[string]bool, len(tools))
	for _, t := range tools {
		available[t.Name] = t.Available
	}

	var missing []string
	for _, name := range knownTools {
		if !available[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// coreSuffix names how many cores the percentage is spread over.
func coreSuffix(cores int) string {
	if cores <= 0 {
		return ""
	}
	unit := " cores"
	if cores == 1 {
		unit = " core"
	}
	return theme.DimStyle.Render(fmt.Sprintf("  %d%s", cores, unit))
}

// chartOf renders one host series at the tier's chart height. Percentages
// are drawn on a fixed 0-to-100 scale: without it, an idle machine renders
// a chart as tall as a saturated machine, because the scale would follow
// the observed maximum.
func chartOf(m Model, width int, t tier, pick func(metrics.HostSample) float64, maxValue float64) []string {
	return chartBlock(m, series(m.samples, pick), width, t, maxValue)
}

// chartBlock is a chart followed by one empty line. The chart's background
// is lighter than the box's, so without this line the area touches the
// value that follows it and the two read as a single block.
func chartBlock(m Model, values []float64, width int, t tier, maxValue float64) []string {
	lines := renderChart(values, theme.BoxContentWidth(width), m.chartHeight(t), maxValue)
	return append(lines, theme.Bg(""))
}

func renderDockerSection(m Model, width int, t tier) []string {
	containers := unavailableValue()
	if m.loadingDocker {
		containers = unknownValue()
	} else if m.dockerStats != nil && m.dockerStats.Available {
		d := m.dockerStats
		containers = countValue(d.Running+d.Stopped+d.Paused) + theme.Bg(" total")
		if t != tierWide {
			// Without the tree, the total alone doesn't say how many are
			// running.
			containers += theme.Bg("  ") + countValue(d.Running) + theme.Bg(" running")
		}
	}

	// The **counts** only: the sizes are in the Storage box, which answers
	// "how much space" where this one answers "how many are there". They
	// used to be in both, in two different forms.

	// CPU and RAM first, in the same order as Host and Network: the three
	// chart-bearing boxes then read on the same lines, and the curves
	// compare without hunting for which is which. The counts follow.
	//
	// `docker stats` runs on its own clock (≈ 2 s per call, measured). Both
	// shares are scaled to what the daemon holds, so they run from 0 to 100
	// like Host's — that's what the layout promises by putting them on the
	// same lines. The suffix says against what: `docker info` counts the
	// VM's cores on Windows and macOS, not the machine's, so it's not
	// necessarily the Host box's figure.
	lines := []string{row("CPU", dockerPercent(m,
		func(a docker.Aggregate) float64 { return a.CPUPercent }, coreSuffix(m.dockerAgg.Cores)))}
	lines = append(lines, chartBlock(m, m.dockerSamples, width, t, 100)...)
	lines = append(lines, row("RAM", dockerPercent(m,
		func(a docker.Aggregate) float64 { return a.MemPercent }, "")))
	lines = append(lines, chartBlock(m, m.dockerMemSamples, width, t, 100)...)

	if t != tierWide {
		// Without the second column, the two trees stack: the containers
		// then shrink down to their heading line, which already carries the
		// running count, and the inventory keeps its root — it's the root
		// that says the following three figures are all about the same
		// thing.
		return append(append(lines, rowAt(narrowTreeValueColumn, "Containers", containers)),
			resourceTree(m)...)
	}

	// Under the curves, two columns: the container tree on the left, the
	// inventory on the right. They each come to four lines, so the box
	// fills up without either half waiting on the other.
	left, right := dockerColumns(m, containers)
	return append(lines, sideBySide(left, right, theme.BoxContentWidth(width))...)
}

// dockerColumns builds the two runs the Docker box ends on. One node per
// state: "9 total  2 running" leaves the reader to subtract to know how
// many are asleep, and says nothing about paused containers.
func dockerColumns(m Model, containers string) (left, right []string) {
	left = []string{
		rowAt(narrowTreeValueColumn, "Containers", containers),
		narrowBranch(false, "running", dockerState(m, func(d shared.DockerStats) int { return d.Running })),
		narrowBranch(false, "stopped", dockerState(m, func(d shared.DockerStats) int { return d.Stopped })),
		narrowBranch(true, "paused", dockerState(m, func(d shared.DockerStats) int { return d.Paused })),
	}
	return left, resourceTree(m)
}

// resourceTree lists what the daemon holds besides its containers. The
// three counts hang from one root rather than floating side by side: they
// are objects of the same daemon, and aligning them under one word says
// which, where three top-level lines used to read as three subjects.
//
// Networks is included for the same reason images and volumes are: it's a
// resource :oci manages that the dashboard wasn't counting — the only one
// of the three `docker system df` ignores, for lack of bytes to report.
func resourceTree(m Model) []string {
	return []string{
		theme.Bg("Resources"),
		narrowBranch(false, "images", ociCount(m, func(s shared.OCIStats) int { return s.ImagesCount })),
		narrowBranch(false, "volumes", ociCount(m, func(s shared.OCIStats) int { return s.VolumesCount })),
		narrowBranch(true, "networks", ociCount(m, func(s shared.OCIStats) int { return s.NetworksCount })),
	}
}

// ociCount renders one inventory count, telling "not read yet" from "Docker is
// not there" the way every other value does.
func ociCount(m Model, pick func(shared.OCIStats) int) string {
	switch {
	case m.loadingOCI:
		return unknownValue()
	case m.ociStats == nil || !m.ociStats.Available:
		return unavailableValue()
	default:
		return countValue(pick(*m.ociStats))
	}
}

// dockerState renders one container-state count, telling "not read yet" from
// "Docker is not there" the way every other value does.
func dockerState(m Model, pick func(shared.DockerStats) int) string {
	switch {
	case m.loadingDocker:
		return unknownValue()
	case m.dockerStats == nil || !m.dockerStats.Available:
		return unavailableValue()
	default:
		return countValue(pick(*m.dockerStats))
	}
}

// dockerPercent renders one figure of the container aggregate, telling apart
// "not sampled yet" from "Docker is not there".
// dockerPercent renders one share of the daemon, and appends `suffix` only when
// there is a number for it to qualify.
//
// The suffix is a parameter rather than something the caller sticks on
// afterwards because the decision is the same one: three of the four branches
// below produce a word, not a value, and `-  16 cores` would read as a
// measurement of nothing.
func dockerPercent(m Model, pick func(docker.Aggregate) float64, suffix string) string {
	switch {
	case !m.dockerRead:
		return unknownValue()
	case !m.dockerAgg.Available:
		return unavailableValue()
	case m.dockerAgg.Running == 0:
		return theme.DimStyle.Render("no running container")
	default:
		return percentValue(pick(m.dockerAgg)) + suffix
	}
}

func renderNetworkSection(m Model, width int, t tier) []string {
	// net.IOCounters is cumulative: the first sample has nothing to
	// subtract from, and a reset interface makes the counter go backwards.
	// In both cases there is no throughput — `-`, not `0`.
	rx, tx := unknownValue(), unknownValue()
	if m.host.HasRate {
		rx = theme.Bg(humanBytes(uint64(m.host.NetRXPerSec))) + theme.DimStyle.Render("/s")
		tx = theme.Bg(humanBytes(uint64(m.host.NetTXPerSec))) + theme.DimStyle.Render("/s")
	}

	samples := unknownValue()
	if n := len(m.samples); n > 0 {
		samples = countValue(n) + theme.DimStyle.Render(" samples")
	}

	// Throughput has no known ceiling: the scale stays automatic, against a
	// fixed 0-to-100 scale for a percentage.
	lines := []string{row("RX", rx)}
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.NetRXPerSec }, 0)...)
	lines = append(lines, row("TX", tx))
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.NetTXPerSec }, 0)...)
	return append(lines, row("History", samples))
}

// knownTools names the tools DevDesk detects, in the order detectTools builds
// them. It's **this list** that decides what's missing, never the detected
// one: a tool detection no longer returns is simply absent, and counting it
// out of the denominator would make it disappear instead of flagging it.
//
// It must stay in sync with detectTools (model.go). The names are constants
// because the two lists have already diverged once (§3.47): two literals
// for a single name can only drift apart; a constant cannot.
const (
	toolDocker   = "Docker"
	toolTrivy    = "Trivy"
	toolGitleaks = "Gitleaks"
	toolPlumber  = "Plumber"
	toolGit      = "Git"
)

var knownTools = []string{toolDocker, toolTrivy, toolGitleaks, toolPlumber, toolGit}

// renderStorageSection answers one question — **where does the space go**
// — in two trees: the volume the workspaces live on, and what Docker holds
// on it.
//
// The workspaces path is no longer here: the Code box already carries it,
// under the tree that talks about it. Repeated here it took up the first
// line to add nothing.
//
// The Docker sizes come from `system df`, of which the view used to only
// read the reclaimable figure — the other three columns were parsed and
// thrown away. The Docker (VM) box keeps the counts, this one takes the
// bytes: it used to be in both, in two forms.
func renderStorageSection(m Model, _ int, _ tier) []string {
	volume := []string{
		theme.Bg("Volume"),
		narrowBranch(false, "capacity", diskField(m, func(d metrics.DiskUsage) string { return humanBytes(d.Total) })),
		narrowBranch(false, "used", diskUsedField(m)),
		narrowBranch(true, "free", diskField(m, func(d metrics.DiskUsage) string { return humanBytes(d.Free) })),
	}

	dockerTree := []string{
		theme.Bg("Docker"),
		narrowBranch(false, "images", ociSize(m, func(s shared.OCIStats) string { return s.ImagesSize })),
		narrowBranch(false, "containers", ociSize(m, func(s shared.OCIStats) string { return s.ContainersSize })),
		narrowBranch(false, "volumes", ociSize(m, func(s shared.OCIStats) string { return s.VolumesSize })),
		narrowBranch(false, "build cache", ociSize(m, func(s shared.OCIStats) string { return s.BuildCacheSize })),
		narrowBranch(true, "reclaimable", reclaimable(m)),
	}

	// The two trees **stack**, at every tier: they come to ten lines
	// together, which is the height of their row neighbors, and putting
	// them side by side would leave the bottom half of the box empty.
	return append(append(volume, treeGap()), dockerTree...)
}

// diskField renders one figure of the volume, `-` until it has been read.
func diskField(m Model, pick func(metrics.DiskUsage) string) string {
	if !m.wsDisk.OK {
		return unknownValue()
	}
	return theme.Bg(pick(m.wsDisk))
}

// diskUsedField carries the percentage next to the bytes: it's the
// percentage that says whether to act, and the bytes say how much. The
// parentheses say which of the two is the measurement: `290 GB  86 %`
// reads as two facts side by side, `290 GB (86 %)` as one.
func diskUsedField(m Model) string {
	if !m.wsDisk.OK {
		return unknownValue()
	}
	return theme.Bg(humanBytes(m.wsDisk.Used)) +
		theme.DimStyle.Render(fmt.Sprintf("  (%.0f%%)", m.wsDisk.UsedPercent))
}

// ociSize renders one line of `docker system df`, telling "not read yet" from
// "Docker is not there" the way every other value does.
func ociSize(m Model, pick func(shared.OCIStats) string) string {
	switch {
	case m.loadingOCI:
		return unknownValue()
	case m.ociStats == nil || !m.ociStats.Available:
		return unavailableValue()
	case pick(*m.ociStats) == "":
		return unknownValue()
	default:
		return theme.Bg(pick(*m.ociStats))
	}
}

// reclaimable is the one figure of the box that names an action. Docker
// present with nothing to reclaim is not Docker absent.
func reclaimable(m Model) string {
	switch {
	case m.loadingOCI:
		return unknownValue()
	case m.ociStats == nil || !m.ociStats.Available:
		return unavailableValue()
	case m.ociStats.Reclaimable == "":
		return theme.DimStyle.Render("nothing")
	default:
		return theme.Bg(m.ociStats.Reclaimable)
	}
}

// Value states (§3.19). Three states, not two: `-` is not `0`, and an
// unavailable source keeps its labels instead of replacing them.
//
// These are functions, not `var`s, for two reasons, the second of which
// affected the whole application:
//
//   - a package-level `Render()` runs at init, hence before `ApplyTheme` has
//     loaded the context's theme: both strings stayed frozen on the default
//     theme's colors. It's the same trap already documented on
//     `ColorChartBg` in `ApplyTheme`.
//   - more importantly, this first render triggers the `sync.Once` by which
//     lipgloss memorizes the terminal's color profile, **permanently**. It
//     was therefore computed during package init, i.e. before the line in
//     `main()` that sets `COLORTERM=truecolor` when WSL hasn't propagated
//     it. The whole TUI then fell back to ANSI256, where each theme's
//     background is quantized onto the 256 palette: `#1e1e2e` (default,
//     mocha) becomes black 232 and `#24273a` (macchiato) becomes navy blue
//     17. The background wasn't "respecting the theme" because it never
//     received its exact color.
//
// Rendering on demand is enough to fix both: the first render then happens
// in `View()`, long after `main()`.
func unknownValue() string     { return theme.DimStyle.Render("-") }
func unavailableValue() string { return theme.DimStyle.Render("n/a") }

// row renders one "label  value" line, aligned on labelWidth. A label as
// long as the column still keeps its space: without it, "Docker root" and
// its value would touch and read as a single word.
func row(label, value string) string {
	return rowAt(labelWidth, label, value)
}

// rowAt is the same line on a chosen value column — what a tree-bearing
// box needs: its nodes line up their values on treeValueColumn, and a
// top-level line that kept labelWidth would open a second value column in
// the same box.
func rowAt(column int, label, value string) string {
	width := max(column, len(label)+1)
	return theme.Bg(label+strings.Repeat(" ", width-len(label))) + value
}

// countValue renders a measured count: a zero informs no one, so it stays dim
// and the colour is spent on what is worth spotting (Rule 122's discipline).
// maybeCountValue renders a counter that may not have been read (D52).
//
// The five come from five independent requests, and the one that fails leaves
// nil. Rendering it as `0` — which is what an int forced — says "you have no
// merge requests assigned" for a token whose scope does not cover them. `-` is
// what the section already shows before the counters arrive, and it means the
// same thing here: nobody knows.
func maybeCountValue(n *int) string {
	if n == nil {
		return unknownValue()
	}
	return countValue(*n)
}

func countValue(n int) string {
	if n > 0 {
		return theme.PrimaryColorStyle.Bold(true).Render(fmt.Sprintf("%d", n))
	}
	return theme.DimStyle.Render("0")
}

// percentValue renders a measured percentage. It is never colored by
// threshold: a threshold is a setting, it would live in the configuration
// view, and a color invented here would say "watch out" without anyone
// having asked for it.
func percentValue(pct float64) string {
	return theme.Bg(fmt.Sprintf("%.0f", pct)) + theme.DimStyle.Render(" %")
}

// humanBytes renders a byte count in the largest unit that keeps it readable.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// statusSummary renders "✓ N  ✗ N" for a set of components, hiding neither
// half: a missing failure count reads as zero failures.
func statusSummary(components []status.ComponentStatus) string {
	if len(components) == 0 {
		return theme.DimStyle.Render("none configured")
	}
	ok, down, errCount := countStatuses(components)
	out := theme.Bg(fmt.Sprintf("%d ", ok)) + theme.StatusOKStyle.Render(theme.IconOK)
	out += tally(down, theme.IconError, theme.StatusDownStyle)
	out += tally(errCount, theme.IconWarning, theme.StatusErrorStyle)
	return out
}

// certSummary is the same line for certificates, on their own four states
// (status.CertState). It only has room for glyphs — the words are in the
// `wide`-tier tree, which is the only half-box wide enough for them — but
// all four get a distinct one, which is exactly what was missing: expired
// and to-renew used to share "error"'s alert.
//
// The hourglass is the only orange one: it's the only state that calls for
// action and still leaves time to take it.
func certSummary(certs []status.ComponentStatus) string {
	if len(certs) == 0 {
		return theme.DimStyle.Render("none configured")
	}
	valid, toRenew, expired, errored := status.CertCounts(certs)
	out := theme.Bg(fmt.Sprintf("%d ", valid)) + certGlyph(status.CertValid)
	for _, c := range []struct {
		n     int
		state status.CertState
	}{
		{toRenew, status.CertToRenew},
		{expired, status.CertExpired},
		{errored, status.CertError},
	} {
		out += tally(c.n, theme.CertStateIcon(string(c.state)), theme.CertStateStyle(string(c.state)))
	}
	return out
}

// tally appends one non-nominal state to a summary line, and nothing at all
// when it is empty. The summary fits on one line shared with its label: a
// "0" per state would fill it with what didn't happen, whereas the tree —
// which has one line per state — keeps them all.
func tally(n int, glyph string, style lipgloss.Style) string {
	if n == 0 {
		return ""
	}
	return theme.Bg(fmt.Sprintf("  %d ", n)) + style.Render(glyph)
}

// truncatePath keeps a path's tail, which is the half that identifies it.
//
// It counts in **cells and runes**, not bytes: `len(path)` measures
// bytes, so on `C:\Users\José\dépôts` the cut landed in the middle of an
// accented character and produced invalid UTF-8 (`…\xa9\projet`). A French
// Windows produces one on every `Téléchargements`.
func truncatePath(path string, width int) string {
	if width < 4 || lipgloss.Width(path) <= width {
		return path
	}

	runes := []rune(path)
	kept := countKept(runes, width-1) // the ellipsis occupies one cell
	return "…" + string(runes[len(runes)-kept:])
}

// countKept returns how many trailing runes fit in `room` cells.
func countKept(runes []rune, room int) int {
	used, n := 0, 0
	for i := len(runes) - 1; i >= 0; i-- {
		w := lipgloss.Width(string(runes[i]))
		if used+w > room {
			break
		}
		used += w
		n++
	}
	return n
}
