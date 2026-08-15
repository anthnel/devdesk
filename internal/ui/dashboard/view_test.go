package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── Whole view ───────────────────────────────────────────────────────────────

// plain returns a rendering with its escape sequences removed, so an assertion
// compares what is on screen rather than how it is coloured.
func plain(s string) string {
	out := make([]string, 0)
	for _, line := range strings.Split(s, "\n") {
		out = append(out, stripANSI(line))
	}
	return strings.Join(out, "\n")
}

func TestViewRendersEveryBox(t *testing.T) {
	out := plain(loadedOnly(t).View())

	for _, title := range []string{"Code", "Health", "Host", "Docker (VM)"} {
		if !strings.Contains(out, title) {
			t.Errorf("View() is missing the %q box:\n%s", title, out)
		}
	}
}

// Chaque boîte est encadrée, et le cadre extérieur a disparu : la vue déclare
// Frameless(), donc le routeur ne dessine ni bordure ni coins autour d'elle.
func TestTheViewDeclaresItselfFrameless(t *testing.T) {
	if !loadedOnly(t).Frameless() {
		t.Error("the dashboard draws its own boxes and must not be framed a second time")
	}
}

// The columns are stitched line by line rather than with JoinHorizontal
// (Rule 115), so every row must come out the same width — a short row would
// show the terminal's own background.
func TestViewRowsAreUniformWidth(t *testing.T) {
	m, _ := loadedModel(t)

	lines := strings.Split(m.View(), "\n")
	if len(lines) < 5 {
		t.Fatalf("View() produced %d lines, want a full dashboard", len(lines))
	}
	width := lipgloss.Width(lines[0])
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("line %d is %d cells wide, want %d — the columns are not padded to a common width", i, got, width)
		}
	}
}

func TestViewSurvivesANarrowTerminal(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, testutil.Resize(20, 10))

	if out := m.View(); out == "" {
		t.Error("View() returned nothing on a narrow terminal")
	}
}

// ── The boxes ────────────────────────────────────────────────────────────────

func TestTheCodeBoxShowsTheSessionAndCounts(t *testing.T) {
	out := plain(loadedOnly(t).View())

	for _, want := range []string{"anthoni", "3 assigned", "2 to review", "5 assigned", "Workspaces 6"} {
		if !strings.Contains(out, want) {
			t.Errorf("the Code box is missing %q:\n%s", want, out)
		}
	}
}

// À `wide`, la boîte devient deux arbres, et la séparation est celle de §3.16 :
// la forge d'un côté, le disque de l'autre. Aucun fait ne flotte plus au-dessus
// d'un arbre auquel il n'appartient pas.
func TestTheCodeBoxIsTwoTreesAtWide(t *testing.T) {
	lines := renderCodeSection(loadedOnly(t), 60, tierWide)

	if got := stripANSI(lines[0]); got != "GitLab" {
		t.Errorf("the box opens on %q, want a GitLab root", got)
	}

	trees := map[string][]string{
		"GitLab":     {"user", "host", "issues assigned", "MR assigned", "MR to review"},
		"Workspaces": {"repositories", "path", "disk"},
	}
	for heading, labels := range trees {
		run := runUnder(lines, heading)
		if len(run) != len(labels) {
			t.Errorf("%s has %d nodes, want %d: %v", heading, len(run), len(labels), run)
			continue
		}
		for i, label := range labels {
			if nodeUnder(lines, heading, label) == "" {
				t.Errorf("%s has no %q node: %v", heading, label, run)
			}
			// Tous les enfants au même niveau : un seul coude, et c'est le
			// dernier. Sans lui, deux arbres qui se suivent se lisent comme un.
			isLast := i == len(labels)-1
			if got := strings.HasPrefix(run[i], theme.IconTreeEnd); got != isLast {
				t.Errorf("%s's %q node ends the run: %v, want %v", heading, label, got, isLast)
			}
		}
	}
}

// Le chiffre qui pend du chemin est ce que l'arborescence occupe, **pas** le
// remplissage du volume : c'est celui sur lequel on peut agir. La boîte Host
// garde la place libre, qui est l'autre question.
func TestTheWorkspacesTreeReportsWhatItOccupies(t *testing.T) {
	m := loadedOnly(t) // a 12 GiB tree on a volume that is 58 % full
	got := nodeUnder(renderCodeSection(m, 60, tierWide), "Workspaces", "disk")

	if !strings.Contains(got, "12.0 GB") {
		t.Errorf("the disk node reads %q, want the size of the tree", got)
	}
	for _, volume := range []string{"58", "free", "290"} {
		if strings.Contains(got, volume) {
			t.Errorf("the disk node reads %q — %q describes the volume, not the tree", got, volume)
		}
	}
}

// Un dossier refusé fait sous-estimer le total, et un total sous-estimé sans
// mention se lit comme une mesure.
func TestAPartialWalkSaysSo(t *testing.T) {
	m := feed(t, loadedOnly(t), WorkspaceSizeMsg{Size: metrics.TreeSize{Bytes: 3 << 30, Partial: true, OK: true}})

	if got := nodeUnder(renderCodeSection(m, 60, tierWide), "Workspaces", "disk"); !strings.Contains(got, "partial") {
		t.Errorf("the disk node reads %q after a walk that could not read everything", got)
	}
}

// Une arborescence pas encore parcourue rend `-`, pas un zéro : zéro octet
// serait une mesure, et le squelette de la boîte ne bouge pas.
func TestAnUnmeasuredTreeLeavesTheDiskNodeUnknown(t *testing.T) {
	m, _ := authenticatedModel(t)
	if got := nodeUnder(renderCodeSection(m, 60, tierWide), "Workspaces", "disk"); !strings.HasSuffix(got, "-") {
		t.Errorf("the disk node reads %q before the tree was walked, want %q", got, "-")
	}
}

// Le parcours est le seul appel du dashboard qui peut durer plus longtemps que
// l'intervalle qui le déclenche. Un second tour ne doit donc pas en lancer un
// deuxième — et doit le relancer une fois le premier revenu.
func TestOnlyOneWorkspaceWalkRunsAtATime(t *testing.T) {
	m, _ := newTestModel(t) // New() raises the flag, Init() issues the walk

	if !m.measuringSize {
		t.Fatal("the flag is down before the first walk answered")
	}
	next, _ := m.alsoMeasuringSize(nil)
	if issued, ok := next.(Model); !ok || !issued.measuringSize {
		t.Error("a slow round changed the flag while a walk was still running")
	}

	m = feed(t, m, WorkspaceSizeMsg{Size: metrics.TreeSize{OK: true}})
	if m.measuringSize {
		t.Error("the flag stayed up after the walk answered — no walk would ever run again")
	}
	after, cmd := m.alsoMeasuringSize(nil)
	if cmd == nil {
		t.Error("the next slow round issued no walk")
	}
	if issued, ok := after.(Model); !ok || !issued.measuringSize {
		t.Error("the flag was not raised for the walk just issued")
	}
}

// Signing out changes the values, never the labels: le squelette reste, et
// c'est ce qui distingue "pas mesuré" de "cassé".
func TestSignedOutKeepsTheCodeBoxLabels(t *testing.T) {
	m, _ := newTestModel(t)

	out := plain(m.View())
	for _, label := range []string{"Merge req.", "Issues", "Workspaces"} {
		if !strings.Contains(out, label) {
			t.Errorf("signed out, the Code box dropped the %q label:\n%s", label, out)
		}
	}
	if !strings.Contains(out, "not connected") {
		t.Error("the Code box does not report the missing session")
	}
}

func TestTheHealthBoxCountsMonitorsAndCertificates(t *testing.T) {
	lines := renderHealthSection(loadedOnly(t), 40, tierStandard)

	// Fixtures: one OK, one down and one warning among the services; one OK and
	// one down among the certificates.
	if !containsLine(lines, "Monitors") || !containsLine(lines, "Certs") {
		t.Errorf("the Health box lost a label: %q", lines)
	}
	monitors := stripANSI(lines[0])
	if !strings.Contains(monitors, "1") {
		t.Errorf("the monitors line shows no count: %q", monitors)
	}
}

// La boîte Docker (VM) répond à « combien il y en a », la boîte Storage à
// « combien de place ». Les deux chiffres étaient dans les deux boîtes, sous
// deux formes différentes.
func TestTheDockerBoxCountsWithoutSizing(t *testing.T) {
	out := plain(loadedOnly(t).View())

	for _, want := range []string{"4 total", "2 running"} {
		if !strings.Contains(out, want) {
			t.Errorf("the Docker box is missing %q:\n%s", want, out)
		}
	}

	lines := renderDockerSection(loadedOnly(t), 60, tierWide)
	for _, size := range []string{"1.2GB", "50MB", "300MB"} {
		if containsLine(lines, size) {
			t.Errorf("the Docker box carries %q — the sizes belong to Storage", size)
		}
	}
}

func TestAbsentDockerReadsNotAvailableRatherThanZero(t *testing.T) {
	lines := renderDockerSection(withNoDocker(t), 40, tierStandard)

	if !containsLine(lines, "n/a") {
		t.Errorf("with no Docker the box shows no n/a: %q", lines)
	}
	for _, line := range lines {
		if strings.Contains(stripANSI(line), "0 total") {
			t.Error("an absent Docker rendered as zero containers, which is a measurement it never made")
		}
	}
}

// Sections settle independently: one slow fetch must not hold the others back,
// and the ones still waiting keep showing `-`.
func TestSectionsSettleIndependently(t *testing.T) {
	m, _ := authenticatedModel(t)

	m = feed(t, m, DockerStatsMsg{Stats: shared.DockerStats{Available: true, Running: 2}})

	out := plain(m.View())
	if !strings.Contains(out, "2 running") {
		t.Errorf("the Docker box did not render once its own result arrived:\n%s", out)
	}
	if !strings.Contains(out, "CPU        -") {
		t.Errorf("the sections still waiting stopped showing their placeholder:\n%s", out)
	}
}

func TestMonitorsReportAnEmptyConfiguration(t *testing.T) {
	m, _ := authenticatedModel(t)
	m = feed(t, m, StatusCheckMsg{Result: status.MonitorResult{Components: nil}})

	if out := plain(m.View()); !strings.Contains(out, "none configured") {
		t.Errorf("the Health box does not report an empty configuration:\n%s", out)
	}
}

// ── Metrics ──────────────────────────────────────────────────────────────────

func TestTheHostBoxShowsTheSample(t *testing.T) {
	out := plain(loadedOnly(t).View())

	for _, want := range []string{"CPU        9 %", "RAM        92 %"} {
		if !strings.Contains(out, want) {
			t.Errorf("the Host box is missing %q:\n%s", want, out)
		}
	}
}

// Un échantillon sans débit affiche `-`, pas `0` : le premier relevé d'un
// compteur cumulatif n'a rien à soustraire, et zéro serait une mesure.
func TestThroughputWithoutARateReadsUnknownRatherThanZero(t *testing.T) {
	m, _ := loadedModel(t) // its sample carries no rate
	lines := renderNetworkSection(m, 40, tierStandard)

	// Les lignes sont repérées par leur libellé, pas par leur position : un
	// graphe s'intercale entre RX et TX selon le palier.
	for _, label := range []string{"RX", "TX"} {
		got := lineStartingWith(lines, label)
		if got == "" {
			t.Fatalf("the Network box has no %q row: %q", label, lines)
		}
		if strings.Contains(got, "0 B/s") {
			t.Errorf("a sample with no rate rendered as zero throughput: %q", got)
		}
		if !strings.HasSuffix(got, "-") {
			t.Errorf("a sample with no rate should read %q, got %q", "-", got)
		}
	}

	m = feed(t, m, HostSampleMsg{Sample: metrics.HostSample{OK: true, HasRate: true, NetRXPerSec: 2048, NetTXPerSec: 1024}})
	if got := lineStartingWith(renderNetworkSection(m, 40, tierStandard), "RX"); !strings.Contains(got, "2.0 KB/s") {
		t.Errorf("a rate did not reach the Network box: %q", got)
	}
}

// lineStartingWith returns the first line whose visible text starts with the
// label, or "" when there is none.
func lineStartingWith(lines []string, label string) string {
	for _, line := range lines {
		if plain := stripANSI(line); strings.HasPrefix(plain, label) {
			return plain
		}
	}
	return ""
}

// Le modèle garde l'historique, pas le graphe : ntcharts.Resize rééchelonne son
// propre ring buffer, donc un changement de palier tronquerait l'historique au
// moment précis où la fenêtre s'agrandit pour en montrer plus.
func TestTheModelKeepsTheSampleHistoryBounded(t *testing.T) {
	m, _ := loadedModel(t)
	before := len(m.samples)

	for i := range maxSamples + 10 {
		m = feed(t, m, HostSampleMsg{Sample: metrics.HostSample{OK: true, CPUPercent: float64(i)}})
	}

	if len(m.samples) != maxSamples {
		t.Errorf("the history holds %d samples, want it bounded at %d", len(m.samples), maxSamples)
	}
	if before == 0 && len(m.samples) == 0 {
		t.Error("no sample was ever recorded")
	}
	// The newest sample must survive the trim, not the oldest.
	if last := m.samples[len(m.samples)-1]; last.CPUPercent != float64(maxSamples+9) {
		t.Errorf("the history kept the wrong end: last sample is %.0f", last.CPUPercent)
	}
}

// Chaque horloge se réarme elle-même, sinon elle s'arrête au premier tick — et
// une horloge morte laisse des valeurs figées qui ressemblent à des valeurs.
func TestEachClockRearmsItself(t *testing.T) {
	m, _ := loadedModel(t)

	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"the host clock", HostTickMsg(time.Now())},
		{"the docker clock", DockerTickMsg(time.Now())},
		{"the slow clock", RefreshTickMsg(time.Now())},
	} {
		updated, cmd := m.Update(tc.msg)
		if cmd == nil {
			t.Errorf("%s produced no command — it stops after one tick", tc.name)
		}
		m = updated.(Model)
	}
}

// Docker absent et Docker pas encore lu ne sont pas la même chose.
func TestTheDockerAggregateTellsUnreadFromUnavailable(t *testing.T) {
	m, _ := authenticatedModel(t)

	if got := stripANSI(dockerPercent(m, func(a docker.Aggregate) float64 { return a.CPUPercent })); got != "-" {
		t.Errorf("before any sample the aggregate reads %q, want %q", got, "-")
	}

	m = feed(t, m, DockerMetricsMsg{Aggregate: docker.Aggregate{Available: false}})
	if got := stripANSI(dockerPercent(m, func(a docker.Aggregate) float64 { return a.CPUPercent })); got != "n/a" {
		t.Errorf("with Docker absent the aggregate reads %q, want %q", got, "n/a")
	}
}

func TestFreeSpaceReachesTheStorageBox(t *testing.T) {
	lines := renderStorageSection(loadedOnly(t), 40, tierStandard)

	if !containsLine(lines, "210.0 GB") {
		t.Errorf("the Storage box does not show the free space: %q", lines)
	}
}

// ── Tabs ─────────────────────────────────────────────────────────────────────

// Rule 135: Tab switches tabs, and nothing else does.
func TestTabSwitchesBetweenOverviewAndResources(t *testing.T) {
	m, _ := loadedModel(t)

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != tabResources {
		t.Fatalf("tab left the view on %v, want the Resources tab", m.activeTab)
	}
	if out := plain(m.View()); !strings.Contains(out, "Network") || !strings.Contains(out, "Storage") {
		t.Errorf("the Resources tab does not show its boxes:\n%s", out)
	}

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != tabOverview {
		t.Errorf("tab did not cycle back to the Overview, landed on %v", m.activeTab)
	}
}

// La boîte Host compte les outils, et c'est tout ce que le dashboard en dit :
// la liste nommée a quitté l'onglet Resources, sa place est dans la vue de
// configuration — c'est un état de la machine, pas une ressource à surveiller.
func TestTheToolsAreCountedAndNotListed(t *testing.T) {
	m, _ := loadedModel(t)

	overview := plain(m.View())
	if !strings.Contains(overview, "of 5 available") {
		t.Errorf("the Host box does not count the tools:\n%s", overview)
	}

	m.activeTab = tabResources
	for _, tab := range []string{overview, plain(m.View())} {
		if strings.Contains(tab, "Gitleaks") {
			t.Error("a tool is named on the dashboard — the list belongs to the configuration view")
		}
	}
}

// The footer carries the age of the data. Une valeur périmée ressemble
// exactement à une valeur fraîche une fois les libellés figés — l'âge est le
// prix des placeholders, pas un ornement.
func TestTheFooterCarriesTheAgeOfTheData(t *testing.T) {
	m, _ := loadedModel(t)

	if got := plain(m.RenderFooter(120)); !strings.Contains(got, "Updated") {
		t.Errorf("the footer does not say when the data was measured:\n%s", got)
	}
}

func TestTheFooterCarriesTheTabBar(t *testing.T) {
	got := plain(loadedOnly(t).RenderFooter(120))

	if !strings.Contains(got, "Overview") || !strings.Contains(got, "Resources") {
		t.Errorf("the footer does not carry the tab bar:\n%s", got)
	}
}

// Un seul onglet n'est pas un choix : la barre coûterait une ligne pour dire
// « vous êtes ici », ce que l'écran dit déjà.
func TestOneTabMeansNoTabBar(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 240, Height: 45}) // tierWide

	if got := plain(m.RenderFooter(240)); strings.Contains(got, "Overview") {
		t.Errorf("a single tab still drew a tab bar:\n%s", got)
	}
	if got := m.GetFooterHeight(); got != 2 {
		t.Errorf("GetFooterHeight() = %d without a tab bar, want 2", got)
	}
}

// La hauteur promise et la hauteur rendue doivent coïncider à chaque palier :
// le routeur budgète le viewport sur la promesse.
func TestTheFooterHeightMatchesAtEveryPalier(t *testing.T) {
	m, _ := loadedModel(t)

	for _, tc := range tierCases {
		at := feed(t, m, tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		want := at.GetFooterHeight()
		if got := strings.Count(at.RenderFooter(tc.width), "\n") + 1; got != want {
			t.Errorf("on %s, RenderFooter emitted %d lines and GetFooterHeight promised %d",
				tc.name, got, want)
		}
	}
}

// ── Header, footer and help ──────────────────────────────────────────────────

// Rule 130: the browser shortcuts only work with a session, so they must not be
// advertised without one.
func TestShortcutsFollowAuthentication(t *testing.T) {
	m, _ := newTestModel(t)

	signedOut := m.GetShortcuts()
	if hasShortcut(signedOut, "m") || hasShortcut(signedOut, "i") {
		t.Error("the browser shortcuts are advertised without a session")
	}
	if !hasShortcut(signedOut, "ctrl+r") {
		t.Error("refresh is not advertised")
	}

	authenticated, _ := authenticatedModel(t)
	signedIn := authenticated.GetShortcuts()
	if !hasShortcut(signedIn, "m") || !hasShortcut(signedIn, "i") {
		t.Error("the browser shortcuts are missing for a signed-in user")
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	m, _ := authenticatedModel(t)

	for _, s := range m.GetShortcuts() {
		if s.Description == "" {
			t.Errorf("shortcut %q has no description", s.Key)
			continue
		}
		if first := s.Description[0]; first < 'A' || first > 'Z' {
			t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
		}
	}
}

func hasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	for _, s := range shortcuts {
		if s.Key == key {
			return true
		}
	}
	return false
}

func TestGetTitleAndIcon(t *testing.T) {
	m, _ := loadedModel(t)

	if !strings.Contains(m.GetTitle(), "Dashboard") {
		t.Errorf("GetTitle() = %q, want it to name the view", m.GetTitle())
	}
	if m.GetIcon() != "" {
		t.Errorf("GetIcon() = %q, want empty — the title carries the icon", m.GetIcon())
	}
}

func TestGetHeaderInfoCarriesTheContext(t *testing.T) {
	m, _ := loadedModel(t)

	info := m.GetHeaderInfo("work")
	if len(info) != 1 || info[0].Key != "Context" || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v, want the active context", info)
	}
}

func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	m, _ := loadedModel(t)

	want := m.GetFooterHeight()
	got := strings.Count(m.RenderFooter(120), "\n") + 1
	if got != want {
		t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() promised %d", got, want)
	}
}

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := loadedOnly(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 {
		t.Error("the help content lists no key bindings")
	}
}

func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m, _ := authenticatedModel(t)
	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		documented[kb.Key] = true
	}

	for _, s := range m.GetShortcuts() {
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

// loadedOnly drops the shared-state return for the tests that do not need it.
func loadedOnly(t *testing.T) Model {
	t.Helper()
	m, _ := loadedModel(t)
	return m
}
