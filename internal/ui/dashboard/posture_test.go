package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func day(n int) time.Time {
	return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
}

// An empty cache and a never-read cache display the same zero if nothing
// distinguishes them, and one of the two would be a lie.
func TestAnUnreadPostureIsNotAnEmptyOne(t *testing.T) {
	m, _ := authenticatedModel(t)

	unread := lineStartingWith(renderHealthSection(m, 60, tierStandard), "Scanned")
	if !strings.HasSuffix(unread, "-") {
		t.Errorf("before the caches are read, Scanned shows %q, want %q", unread, "-")
	}

	m = feed(t, m, PostureMsg{Posture: posture{Read: true}})
	empty := lineStartingWith(renderHealthSection(m, 60, tierStandard), "Scanned")
	if !strings.Contains(empty, "0") {
		t.Errorf("an empty cache shows %q, want a measured zero", empty)
	}
	if oldest := lineStartingWith(renderHealthSection(m, 60, tierStandard), "Oldest"); !strings.Contains(oldest, "never") {
		t.Errorf("with no target scanned, Oldest shows %q", oldest)
	}
}

// The two families are counted separately — a CRITICAL in an image is fixed
// by changing tag, in a repository by changing code — and Total() only
// recombines them for the tiers too narrow for two trees.
func TestThePostureKeepsTheTwoFamiliesApart(t *testing.T) {
	var p posture
	p.Read = true
	p.Images.add(2, nil, day(3))
	p.Repositories.add(1, nil, day(10))

	if p.Images.Targets != 1 || p.Repositories.Targets != 1 {
		t.Errorf("targets = %d images / %d repositories, want one each",
			p.Images.Targets, p.Repositories.Targets)
	}
	if p.Images.Critical != 2 || p.Repositories.Critical != 1 {
		t.Errorf("critical = %d / %d, want 2 / 1", p.Images.Critical, p.Repositories.Critical)
	}

	total := p.Total()
	if total.Targets != 2 {
		t.Errorf("Total().Targets = %d, want 2", total.Targets)
	}
	if total.Critical != 3 {
		t.Errorf("Total().Critical = %d, want 3", total.Critical)
	}
	if got := time.Since(total.Oldest).Hours(); got < 240 {
		t.Errorf("Total().Oldest is %.0f hours old, want the older of the two (240)", got)
	}
}

// An empty family must not become the other one's oldest scan.
func TestAnEmptyFamilyDoesNotAgeTheTotal(t *testing.T) {
	var p posture
	p.Read = true
	p.Images.add(0, nil, day(4))

	if got := p.Total().Oldest; got.IsZero() {
		t.Error("an empty family zeroed the total's oldest scan")
	}
}

// An entry with no timestamp must not make the posture younger: the zero
// value of a time.Time predates everything, and would make an inventory read
// "never scanned" when it isn't.
func TestAnUndatedEntryDoesNotBecomeTheOldestScan(t *testing.T) {
	var side postureSide
	side.add(0, nil, day(2))
	side.add(1, nil, time.Time{})

	if side.Oldest.IsZero() {
		t.Error("an entry with no timestamp became the oldest scan")
	}
	if side.Targets != 2 {
		t.Errorf("Targets = %d — an undated entry is still a scanned target", side.Targets)
	}
}

// ── Path truncation ──────────────────────────────────────────────────────────

// Found in review: the truncation was cutting bytes, so on an accented
// path — `C:\Users\José\dépôts`, which a French Windows produces on every
// `Téléchargements` — the cut landed in the middle of a character and
// produced invalid UTF-8, which renders as a black diamond.
func TestTruncatingAPathNeverCutsARune(t *testing.T) {
	paths := []string{
		`C:\Users\José\dépôts\été\projet`,
		`C:\Users\anthoni\Téléchargements\devdesk`,
		"/home/anthoni/workspaces/devdesk",
	}

	for _, path := range paths {
		for width := 4; width <= 40; width++ {
			got := truncatePath(path, width)
			if !utf8.ValidString(got) {
				t.Errorf("truncatePath(%q, %d) = %q — the cut landed inside a rune", path, width, got)
			}
			if w := lipgloss.Width(got); w > width {
				t.Errorf("truncatePath(%q, %d) is %d cells wide", path, width, w)
			}
		}
	}
}

// The tail is what identifies a path: that's what gets kept.
func TestTruncatingAPathKeepsItsTail(t *testing.T) {
	got := truncatePath(`C:\Users\anthoni\workspaces\devdesk`, 20)

	if !strings.HasSuffix(got, "devdesk") {
		t.Errorf("truncatePath = %q, want it to end on the leaf directory", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("truncatePath = %q, want the cut to be visible", got)
	}
}

// ── Certificate expiry ───────────────────────────────────────────────────────

func certWithDays(name string, days int) status.ComponentStatus {
	return status.ComponentStatus{Name: name, Type: status.TypeSSL, Status: status.StatusOK, SSLDaysLeft: &days}
}

func TestTheNearestExpiryIsTheOneShown(t *testing.T) {
	certs := []status.ComponentStatus{
		certWithDays("far", 300),
		certWithDays("soon", 9),
		certWithDays("middle", 45),
	}

	got := stripANSI(nearestExpiry(certs, false, true))
	if !strings.Contains(got, "9 days") || !strings.Contains(got, "soon") {
		t.Errorf("nearestExpiry = %q, want the 9-day certificate named", got)
	}
}

// Within the renew window the days-left figure matches the `to renew` node's
// own color — orange, not red: it's the same certificate and the same
// not-yet-urgent state, told once rather than in two colors on two lines.
func TestExpiryWithinTheRenewWindowIsWarningColored(t *testing.T) {
	certs := []status.ComponentStatus{certWithDays("soon", status.CertRenewWindowDays)}

	got := nearestExpiry(certs, false, false)
	want := theme.StatusWarningStyle.Render(fmt.Sprintf("%d days", status.CertRenewWindowDays))
	if got != want {
		t.Errorf("nearestExpiry = %q, want %q — warning orange, matching \"to renew\"", got, want)
	}
}

// Monitored certificates none of which could be read is a missing
// measurement — not a distant deadline.
func TestUnreadableCertificatesReadUnknownRatherThanFar(t *testing.T) {
	certs := []status.ComponentStatus{
		{Name: "unreachable", Type: status.TypeSSL, Status: status.StatusDown},
	}

	if got := stripANSI(nearestExpiry(certs, false, true)); got != "-" {
		t.Errorf("nearestExpiry = %q for a certificate that could not be read, want %q", got, "-")
	}
}

// A certificate already expired has no countdown left: the `expired` node
// says so, and "-2 days" would have required translating a negative number.
func TestAnExpiredCertificateIsNotTheNearestExpiry(t *testing.T) {
	certs := []status.ComponentStatus{
		certWithDays("gone", -2),
		certWithDays("soon", 9),
	}

	got := stripANSI(nearestExpiry(certs, false, true))
	if !strings.Contains(got, "9 days") {
		t.Errorf("nearestExpiry = %q, want the 9-day certificate", got)
	}
	if strings.Contains(got, "-") {
		t.Errorf("nearestExpiry = %q — a passed date is not a deadline", got)
	}
}

// And when nothing is running anymore, the line has no date to give.
func TestNothingAheadIsNotADeadline(t *testing.T) {
	certs := []status.ComponentStatus{certWithDays("gone", -2)}

	if got := stripANSI(nearestExpiry(certs, false, true)); got != "-" {
		t.Errorf("nearestExpiry = %q with every certificate expired, want %q", got, "-")
	}
}

func TestNoCertificateConfiguredIsNotAnExpiry(t *testing.T) {
	if got := stripANSI(nearestExpiry(nil, false, true)); !strings.Contains(got, "none configured") {
		t.Errorf("nearestExpiry = %q with no SSL monitor", got)
	}
}

// ── Alignment ────────────────────────────────────────────────────────────────

// valueColumn reports which cell a line's value starts on.
func valueColumn(t *testing.T, line, value string) int {
	t.Helper()
	i := strings.Index(line, value)
	if i < 0 {
		t.Fatalf("%q carries no %q", line, value)
	}
	return lipgloss.Width(line[:i])
}

// treeStemWidth is written rather than measured, because a constant cannot
// call lipgloss.Width. This is where it gets checked against the rendering.
func TestTheTreeStemIsAsWideAsItIsDeclared(t *testing.T) {
	for name, stem := range map[string]string{"branch": theme.IconTreeBranch, "end": theme.IconTreeEnd} {
		if got := lipgloss.Width(stem); got != treeStemWidth {
			t.Errorf("the %s stem is %d cells wide, treeStemWidth says %d", name, got, treeStemWidth)
		}
	}
}

// A top-level line that carries a value lines up with the nodes around it:
// otherwise the box displays two value columns for a single list of facts,
// and the eye can no longer scan one column.
func TestATopLevelRowLinesUpWithTheTreeAroundIt(t *testing.T) {
	const marker = "58 days"

	node := valueColumn(t, stripANSI(branch(false, "down", marker)), marker)
	top := valueColumn(t, stripANSI(rowAt(treeValueColumn, "Expiry", marker)), marker)

	if node != top {
		t.Errorf("a tree node puts its value on column %d and a top-level row on column %d", node, top)
	}
}

// The general safeguard: a label longer than the column does not break the
// alignment of just its own line, it opens a **second value column** in the
// box. This happened when adding `issues assigned`, fifteen cells against
// twelve, and nothing would have caught it.
//
// The check knows no label: on an aligned node, the cell before the value is
// always padding. A label that overflows puts one of its own characters
// there instead.
func TestEveryTreeNodeAlignsItsValue(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, PostureMsg{Posture: posture{Read: true}})

	healthLeft, healthRight := healthColumns(m)
	dockerLeft, dockerRight := dockerColumns(m, "4 total")

	// The columns are checked **before** being assembled: once joined, the
	// nodes of both trees share a line, and the right column starts where
	// the left one ends — not at a position known in advance.
	runs := []struct {
		name   string
		column int
		lines  []string
	}{
		{"Code", treeValueColumn, renderCodeSection(m, 60, tierWide)},
		{"Health left", narrowTreeValueColumn, healthLeft},
		{"Health right", narrowTreeValueColumn, healthRight},
		{"Docker left", narrowTreeValueColumn, dockerLeft},
		{"Docker right", narrowTreeValueColumn, dockerRight},
		{"Host", narrowTreeValueColumn, renderHostSection(m, 60, tierStandard)},
		{"Storage", narrowTreeValueColumn, renderStorageSection(m, 60, tierStandard)},
	}

	for _, run := range runs {
		for _, line := range run.lines {
			plain := stripANSI(line)
			if !strings.HasPrefix(plain, theme.IconTreeBranch) && !strings.HasPrefix(plain, theme.IconTreeEnd) {
				continue
			}
			// A node with no value has nothing to align.
			if lipgloss.Width(plain) <= run.column {
				continue
			}
			if gap := []rune(plain)[run.column-1]; gap != ' ' {
				t.Errorf("%s: %q — its label runs past column %d (%q there), so its value opens a second column",
					run.name, plain, run.column-1, string(gap))
			}
		}
	}
}

// Both Health columns each carry two trees, and the second ones must start
// on the same line: the expiry gives the certificates one more node, so
// without a catch-up `Repositories` would start a line above `Images` and
// the two bottom trees would read like a staircase.
func TestBothHealthColumnsStartTheirSecondTreeTogether(t *testing.T) {
	m := healthModel(t, []status.ComponentStatus{
		{Name: "web", Type: status.TypeHTTPS, Status: status.StatusOK},
		certWithDays("cert", 58),
	})
	left, right := healthColumns(m)

	at := func(lines []string, heading string) int {
		for i, line := range lines {
			if stripANSI(line) == heading {
				return i
			}
		}
		t.Fatalf("no %q heading in %v", heading, lines)
		return -1
	}

	if repos, images := at(left, "Repositories"), at(right, "Images"); repos != images {
		t.Errorf("Repositories opens on line %d and Images on line %d", repos, images)
	}
}

// A value that fills its half must not touch the column next to it: joined
// together, the number and the first node's elbow read as one.
func TestTwoColumnsKeepAGutter(t *testing.T) {
	out := sideBySide([]string{strings.Repeat("x", 100)}, []string{"R"}, 40)
	if len(out) != 1 {
		t.Fatalf("sideBySide returned %d lines for one row", len(out))
	}

	// Two cells is the minimum written here rather than taken from the
	// constant: a test that reads it back would pass just as well with zero.
	got := stripANSI(out[0])
	if gap := strings.Index(got, "R") - strings.LastIndex(got, "x") - 1; gap < 2 {
		t.Errorf("a left column that fills its half leaves a %d-cell gutter: %q", gap, got)
	}
}

// And the counterpart: an assembled column is exactly the box's width,
// otherwise the box next to it shifts (Rule 116).
func TestASplitBoxFillsItsWidthExactly(t *testing.T) {
	m, _ := loadedModel(t)

	for _, width := range []int{54, 60, 80} {
		for name, lines := range map[string][]string{
			"Health":  renderHealthSection(m, width, tierWide),
			"Docker":  renderDockerSection(m, width, tierWide),
			"Storage": renderStorageSection(m, width, tierWide),
		} {
			for i, line := range lines {
				if got := lipgloss.Width(line); got > theme.BoxContentWidth(width) {
					t.Errorf("%s line %d is %d cells in a box holding %d", name, i, got, theme.BoxContentWidth(width))
				}
			}
		}
	}
}

// The expiry hangs from Certs: it's a fact about the certificates, and
// floating above the trees it didn't say what it was about. It's also its
// last node, so the elbow moves to a different line.
func TestTheExpiryHangsFromTheCertificates(t *testing.T) {
	m := healthModel(t, []status.ComponentStatus{
		{Name: "web", Type: status.TypeHTTPS, Status: status.StatusOK},
		certWithDays("google.com", 58),
	})
	_, certs := healthColumns(m)

	expiry := nodeUnder(certs, "Certs", "expiry")
	if !strings.Contains(expiry, "58 days") {
		t.Errorf("the expiry node reads %q", expiry)
	}
	if !strings.HasPrefix(expiry, theme.IconTreeEnd) {
		t.Errorf("the expiry node reads %q, want it to close the Certs run", expiry)
	}
	// The certificate's name dropped along with the half-width box: :status
	// owns the named list, and "58 days  google.com" doesn't fit here.
	if strings.Contains(expiry, "google.com") {
		t.Errorf("the expiry node reads %q — a half-width column cannot hold the name", expiry)
	}
	if err := nodeUnder(certs, "Certs", "error"); strings.HasPrefix(err, theme.IconTreeEnd) {
		t.Errorf("the error node reads %q — it no longer ends the run", err)
	}

	// Monitors have no expiry, hence no node.
	monitors, _ := healthColumns(m)
	if got := nodeUnder(monitors, "Monitors", "expiry"); got != "" {
		t.Errorf("the Monitors tree grew an expiry node: %q", got)
	}
}

// ── Certificate states ───────────────────────────────────────────────────────

// The monitors' vocabulary does not say what a certificate is. `up` named
// "reachable" which means "valid", and `error` received both the expired one
// and the one expiring next week.
func TestTheCertificateTreeSpeaksOfCertificates(t *testing.T) {
	m := healthModel(t, []status.ComponentStatus{
		{Name: "web", Type: status.TypeHTTPS, Status: status.StatusOK},
		certWithDays("google.com", 58),
	})
	_, certs := healthColumns(m)

	for _, label := range []string{"valid", "to renew", "expired", "error", "expiry"} {
		if nodeUnder(certs, "Certs", label) == "" {
			t.Errorf("the Certs tree has no %q node", label)
		}
	}
	for _, label := range []string{"up", "down"} {
		if got := nodeUnder(certs, "Certs", label); got != "" {
			t.Errorf("the Certs tree kept the monitor node %q: %q", label, got)
		}
	}

	// Monitors keep their own, and don't borrow the other way around.
	monitors, _ := healthColumns(m)
	for _, label := range []string{"up", "down", "error"} {
		if nodeUnder(monitors, "Monitors", label) == "" {
			t.Errorf("the Monitors tree lost its %q node", label)
		}
	}
	for _, label := range []string{"valid", "to renew", "expired"} {
		if got := nodeUnder(monitors, "Monitors", label); got != "" {
			t.Errorf("the Monitors tree grew the certificate node %q: %q", label, got)
		}
	}
}

// The bug, counted: three certificates that `up` / `down` / `error` put
// into two buckets are now split across three.
func TestAnExpiredCertificateIsNotCountedAsAReadFailure(t *testing.T) {
	m := healthModel(t, []status.ComponentStatus{
		certWithDays("far", 300),
		certWithDays("soon", 9),
		certWithDays("gone", -2),
		{Name: "unreachable", Type: status.TypeSSL, Status: status.StatusDown},
	})
	_, certs := healthColumns(m)

	for _, c := range []struct{ label, want string }{
		{"valid", "1"},
		{"to renew", "1"},
		{"expired", "1"},
		{"error", "1"},
	} {
		got := stripANSI(nodeUnder(certs, "Certs", c.label))
		if !strings.Contains(got, c.want) {
			t.Errorf("the %q node reads %q, want a count of %s", c.label, got, c.want)
		}
	}
}

// Each state carries its own glyph, including at zero: without that,
// `expired` and `error` carried the same alert and became one line read as
// two.
func TestEachCertificateStateCarriesItsOwnGlyph(t *testing.T) {
	m := healthModel(t, []status.ComponentStatus{certWithDays("google.com", 58)})
	_, certs := healthColumns(m)

	for _, c := range []struct{ label, want string }{
		{"valid", theme.IconOK},
		{"to renew", theme.IconHourglass},
		{"expired", theme.IconError},
		{"error", theme.IconWarning},
	} {
		line := nodeUnder(certs, "Certs", c.label)
		if !strings.Contains(line, c.want) {
			t.Errorf("the %q node reads %q, want it to carry its own icon", c.label, line)
		}
	}
}

// ── Monitor and certificate icons ────────────────────────────────────────────

// The icon names the line's state, not its count: a check mark on "down"
// said "everything is fine" right where one is looking for how many are
// down. The vocabulary is :status's own — a cross for DOWN, an alert for
// ERROR.
func TestTheDownAndErrorNodesKeepTheirOwnIconAtZero(t *testing.T) {
	m := healthModel(t, []status.ComponentStatus{
		{Name: "web", Type: status.TypeHTTPS, Status: status.StatusOK},
	})
	monitors, _ := healthColumns(m)

	cases := []struct{ label, want string }{
		{"down", theme.IconError},
		{"error", theme.IconWarning},
	}
	for _, c := range cases {
		line := nodeUnder(monitors, "Monitors", c.label)
		if strings.Contains(line, theme.IconOK) {
			t.Errorf("the %q node reads %q — a check mark says the opposite of what the row counts", c.label, line)
		}
		if !strings.Contains(line, c.want) {
			t.Errorf("the %q node reads %q, want it to carry its own icon", c.label, line)
		}
	}
}

// And the glyph does not change when the count goes to one: only the color
// does, which stripANSI erases — hence the comparison across both states.
func TestAFailingNodeKeepsTheGlyphItHadAtZero(t *testing.T) {
	quiet := stripANSI(alertCount(0, theme.IconError, theme.StatusDownStyle))
	failing := stripANSI(alertCount(3, theme.IconError, theme.StatusDownStyle))

	if !strings.HasSuffix(quiet, theme.IconError) || !strings.HasSuffix(failing, theme.IconError) {
		t.Errorf("the glyph changed with the count: %q then %q", quiet, failing)
	}
	if !strings.HasPrefix(failing, "3") {
		t.Errorf("alertCount(3) = %q, want the count first and the icon on its right", failing)
	}
}

// The count and the icon must carry the same color — the bug this pins was a
// certificate "to renew" reading a red count next to its own orange
// hourglass, one state told in two colors on the same line.
func TestAlertCountAndItsIconShareOneColor(t *testing.T) {
	got := alertCount(1, theme.IconHourglass, theme.StatusWarningStyle)
	want := theme.StatusWarningStyle.Render("1") + theme.Bg("  ") + theme.StatusWarningStyle.Render(theme.IconHourglass)
	if got != want {
		t.Errorf("alertCount = %q, want %q — number and icon must share one style", got, want)
	}
}

// ── Coverage ─────────────────────────────────────────────────────────────────

// A coverage gap is an attention signal, like a certificate to renew — not a
// finding (severityCount's red) and not a neutral tally (countValue's
// purple, which `scanned` keeps).
func TestUnscannedValueIsWarningColoredRatherThanNeutral(t *testing.T) {
	got := unscannedValue(4, true)
	want := theme.StatusWarningStyle.Render("4")
	if got != want {
		t.Errorf("unscannedValue(4) = %q, want %q", got, want)
	}
}

// What the box now counts instead of HIGH: the targets it says nothing
// about. It's the only one of its figures that's actionable — run a scan.
func TestTheHealthBoxCountsWhatHasNeverBeenScanned(t *testing.T) {
	m, _ := loadedModel(t) // eight images, six workspaces
	var p posture
	p.Read = true
	p.Images.add(1, nil, day(2))
	p.Repositories.add(0, nil, day(5))
	m = feed(t, m, PostureMsg{Posture: p})

	repos, images := healthColumns(m)

	unscannedImages := nodeUnder(images, "Images", "unscanned")
	if !strings.Contains(unscannedImages, "7") {
		t.Errorf("with 8 images and 1 scanned, the node reads %q, want 7", unscannedImages)
	}
	unscannedRepos := nodeUnder(repos, "Repositories", "unscanned")
	if !strings.Contains(unscannedRepos, "5") {
		t.Errorf("with 6 workspaces and 1 scanned, the node reads %q, want 5", unscannedRepos)
	}

	for _, line := range renderHealthSection(m, 60, tierWide) {
		if strings.Contains(stripANSI(line), "high") {
			t.Errorf("the box still carries a HIGH tally: %q", stripANSI(line))
		}
	}
}

// An inventory not yet loaded does not yield zero: "nothing to scan" is
// exactly the opposite of "we don't know yet".
func TestAnUnreadInventoryLeavesTheCoverageUnknown(t *testing.T) {
	m, _ := authenticatedModel(t) // nothing loaded yet
	m = feed(t, m, PostureMsg{Posture: posture{Read: true}})

	if _, measured := m.unscannedImages(); measured {
		t.Error("the image coverage claims to be measured before docker answered")
	}
	if _, measured := m.unscannedRepositories(); measured {
		t.Error("the repository coverage claims to be measured before the workspaces were counted")
	}
	_, images := healthColumns(m)
	if got := nodeUnder(images, "Images", "unscanned"); !strings.HasSuffix(got, "-") {
		t.Errorf("the unscanned node reads %q, want %q", got, "-")
	}
}

// The cache keeps the entry of an image deleted since, so the difference
// can go below zero. The floor says what should be taken away from it:
// nothing left to scan — never a negative number.
func TestACacheAheadOfTheInventoryReportsNothingLeft(t *testing.T) {
	if got := uncovered(2, 9); got != 0 {
		t.Errorf("uncovered(2, 9) = %d, want 0", got)
	}
}

// A single missing half is enough to make the total unmeasured: the sum of
// a number and an unknown is an unknown.
func TestAHalfMeasuredTotalIsNotMeasured(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, PostureMsg{Posture: posture{Read: true}})
	m.loadingWorkspaces = true

	if _, measured := m.unscannedTotal(); measured {
		t.Error("the total claims to be measured while the workspace count is still loading")
	}
}

// healthModel loads a model and replaces its monitors.
func healthModel(t *testing.T, components []status.ComponentStatus) Model {
	t.Helper()
	m, _ := loadedModel(t)
	return feed(t, m, StatusCheckMsg{Result: status.MonitorResult{Components: components, Timestamp: time.Now()}})
}

// runUnder returns the contiguous run of nodes a heading opens, stripped. It
// stops at the first non-node: a box carries several trees, both of
// posture's trees carry the same labels, and scanning the whole box would
// always return the first one.
func runUnder(lines []string, heading string) []string {
	for i, line := range lines {
		if stripANSI(line) != heading {
			continue
		}
		var run []string
		for _, node := range lines[i+1:] {
			plain := stripANSI(node)
			if !strings.HasPrefix(plain, theme.IconTreeBranch) && !strings.HasPrefix(plain, theme.IconTreeEnd) {
				break
			}
			run = append(run, plain)
		}
		return run
	}
	return nil
}

// nodeUnder returns one named node of the tree a heading opens.
func nodeUnder(lines []string, heading, label string) string {
	for _, node := range runUnder(lines, heading) {
		if strings.HasPrefix(node, theme.IconTreeBranch+label) || strings.HasPrefix(node, theme.IconTreeEnd+label) {
			return node
		}
	}
	return ""
}

// ── Storage ──────────────────────────────────────────────────────────────────

// Docker present with nothing to reclaim is not Docker absent.
func TestNothingToReclaimIsNotAnAbsentDocker(t *testing.T) {
	m, _ := loadedModel(t) // its OCI stats carry no reclaimable figure

	got := nodeUnder(renderStorageSection(m, 60, tierStandard), "Docker", "reclaimable")
	if !strings.Contains(got, "nothing") {
		t.Errorf("with Docker present and nothing to reclaim, the node reads %q", got)
	}

	absent := nodeUnder(renderStorageSection(withNoDocker(t), 60, tierStandard), "Docker", "reclaimable")
	if !strings.Contains(absent, "n/a") {
		t.Errorf("with Docker absent, the node reads %q, want n/a", absent)
	}
}

// The box breaks down `docker system df` in full. The view used to parse
// its four columns and only read one: the reclaimable one. The other three
// were measured, stored, and thrown away.
func TestStorageBreaksDownWhatDockerHolds(t *testing.T) {
	lines := renderStorageSection(loadedOnly(t), 60, tierStandard)

	sizes := map[string]string{"images": "1.2GB", "containers": "300MB", "volumes": "50MB"}
	for label, want := range sizes {
		if got := nodeUnder(lines, "Docker", label); !strings.Contains(got, want) {
			t.Errorf("the %q node reads %q, want %q", label, got, want)
		}
	}
}

// The volume gives its three figures, and usage carries the percentage
// beside the bytes: the percentage says whether to act, the bytes say how
// much.
func TestStorageReportsTheVolume(t *testing.T) {
	lines := renderStorageSection(loadedOnly(t), 60, tierStandard)

	for label, want := range map[string]string{"capacity": "500.0 GB", "free": "210.0 GB", "used": "290.0 GB"} {
		if got := nodeUnder(lines, "Volume", label); !strings.Contains(got, want) {
			t.Errorf("the %q node reads %q, want %q", label, got, want)
		}
	}
	// In parentheses: `290.0 GB  58%` reads as two facts side by side,
	// `290.0 GB (58%)` as one measurement and its share.
	if got := nodeUnder(lines, "Volume", "used"); !strings.Contains(got, "(58%)") {
		t.Errorf("the used node reads %q, want the percentage bracketed beside the bytes", got)
	}
}

// The build cache is `docker system df`'s fourth line, and the one that
// most often answers "where did the disk go". It already fed into the
// reclaimable total; its own size was thrown away.
func TestStorageNamesTheBuildCache(t *testing.T) {
	m := feed(t, loadedOnly(t), OCIStatsMsg{Stats: shared.OCIStats{
		Available: true, ImagesCount: 8, ImagesSize: "1.2GB", BuildCacheSize: "9.7GB",
	}})

	if got := nodeUnder(renderStorageSection(m, 60, tierStandard), "Docker", "build cache"); !strings.Contains(got, "9.7GB") {
		t.Errorf("the build cache node reads %q, want 9.7GB", got)
	}
}

// The workspaces path belongs to the Code box, which carries it under the
// tree that talks about it. Repeated here it took up the first line to add
// nothing.
func TestStorageDoesNotRepeatTheWorkspacesPath(t *testing.T) {
	for _, tr := range []tier{tierStandard, tierWide} {
		for _, line := range renderStorageSection(loadedOnly(t), 60, tr) {
			if strings.Contains(stripANSI(line), "workspaces") {
				t.Errorf("the Storage box repeats the path the Code box carries: %q", stripANSI(line))
			}
		}
	}
}

// A repository deleted after its scan leaves its entry in the cache, and
// nothing removes it. `ws` doesn't list it — it lists the disk — and `:sec`
// discards it on read; the dashboard was counting it. Its CRITICALs were
// therefore only visible on the one screen from which you cannot go look at
// them.
func TestThePostureDropsARepositoryThatIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	here := filepath.Join(home, "still-there")
	if err := os.MkdirAll(here, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(home, "deleted-since")

	c, err := cache.NewWorkspaceScanCache("default")
	if err != nil {
		t.Fatal(err)
	}
	for path, critical := range map[string]int{here: 1, gone: 3} {
		if err := c.Set(path, cache.WorkspaceScanEntry{
			RepoPath: path, Critical: critical, ScannedAt: day(1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	p := readPosture("default", nil, false)

	if p.Repositories.Targets != 1 {
		t.Errorf("Targets = %d, want 1 — the deleted repository is still counted",
			p.Repositories.Targets)
	}
	if p.Repositories.Critical != 1 {
		t.Errorf("Critical = %d, want 1 — the dashboard is reporting %d CRITICAL that no view can show",
			p.Repositories.Critical, p.Repositories.Critical-1)
	}
}

// The absence must be *certain*. An unreadable path — a slow share, a
// missing permission — is not a deletion, and discarding it would make
// perfectly present repositories vanish from the count.
func TestThePostureKeepsARepositoryItCannotStat(t *testing.T) {
	if cache.RepositoryGone(filepath.Join(t.TempDir(), "no-such-dir", "child")) != true {
		t.Error("a definite absence was not reported as one")
	}
	dir := t.TempDir()
	if cache.RepositoryGone(dir) {
		t.Error("a directory that exists was reported gone")
	}
}

// The "images" half of the same bug: a `docker rmi` after a scan left its
// CRITICALs in the Images tree, and `:sec` already no longer showed them.
func TestThePostureDropsAnImageThatIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	c, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatal(err)
	}
	for name, critical := range map[string]int{"nginx:1.27": 1, "removed:latest": 3} {
		if err := c.Set(name, cache.ImageScanEntry{Critical: critical, ScannedAt: day(1)}); err != nil {
			t.Fatal(err)
		}
	}

	p := readPosture("default", map[string]struct{}{"nginx:1.27": {}}, true)

	if p.Images.Targets != 1 || p.Images.Critical != 1 {
		t.Errorf("Images = %d targets / %d critical, want 1 / 1 — an image the daemon no longer lists is still counted",
			p.Images.Targets, p.Images.Critical)
	}
}

// The guard everything rests on. "Docker is stopped" and "the image was
// deleted" are the same silence, and reading the first as the second would
// make the Images box display `0 CRITICAL` — the one wrong answer nobody
// would go check.
func TestADaemonThatCannotBeReachedKeepsEveryImageInThePosture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	c, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("nginx:1.27", cache.ImageScanEntry{Critical: 4, ScannedAt: day(1)}); err != nil {
		t.Fatal(err)
	}

	p := readPosture("default", nil, false)

	if p.Images.Targets != 1 || p.Images.Critical != 4 {
		t.Errorf("Images = %d targets / %d critical, want 1 / 4 — a stopped daemon read as a deletion",
			p.Images.Targets, p.Images.Critical)
	}
}
