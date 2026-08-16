package dashboard

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func day(n int) time.Time {
	return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
}

// Un cache vide et un cache jamais lu affichent le même zéro si rien ne les
// distingue, et l'un des deux serait un mensonge.
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

// Les deux familles sont comptées séparément — une CRITICAL dans une image se
// règle en changeant de tag, dans un dépôt en changeant du code — et Total() ne
// les recombine que pour les paliers trop étroits pour deux arbres.
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

// Une famille vide ne doit pas devenir le scan le plus ancien de l'autre.
func TestAnEmptyFamilyDoesNotAgeTheTotal(t *testing.T) {
	var p posture
	p.Read = true
	p.Images.add(0, nil, day(4))

	if got := p.Total().Oldest; got.IsZero() {
		t.Error("an empty family zeroed the total's oldest scan")
	}
}

// Une entrée sans horodatage ne doit pas rajeunir la posture : le zéro d'un
// time.Time est antérieur à tout, et ferait lire « jamais scanné » à un
// inventaire qui l'est.
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

// Trouvé en revue : la troncature découpait des octets, donc sur un chemin
// accentué — `C:\Users\José\dépôts`, ce qu'un Windows français produit à chaque
// `Téléchargements` — la coupe tombait au milieu d'un caractère et rendait de
// l'UTF-8 invalide, qui s'affiche en losange noir.
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

// La queue est ce qui identifie un chemin : c'est elle qu'on garde.
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

// Des certificats surveillés dont aucun n'a pu être lu, c'est une absence de
// mesure — pas une échéance lointaine.
func TestUnreadableCertificatesReadUnknownRatherThanFar(t *testing.T) {
	certs := []status.ComponentStatus{
		{Name: "unreachable", Type: status.TypeSSL, Status: status.StatusDown},
	}

	if got := stripANSI(nearestExpiry(certs, false, true)); got != "-" {
		t.Errorf("nearestExpiry = %q for a certificate that could not be read, want %q", got, "-")
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

// treeStemWidth is written rather than measured, parce qu'une constante ne peut
// pas appeler lipgloss.Width. C'est ici qu'elle est confrontée au rendu.
func TestTheTreeStemIsAsWideAsItIsDeclared(t *testing.T) {
	for name, stem := range map[string]string{"branch": theme.IconTreeBranch, "end": theme.IconTreeEnd} {
		if got := lipgloss.Width(stem); got != treeStemWidth {
			t.Errorf("the %s stem is %d cells wide, treeStemWidth says %d", name, got, treeStemWidth)
		}
	}
}

// Une ligne de premier niveau qui porte une valeur s'aligne sur les nœuds qui
// l'entourent : sinon la boîte affiche deux colonnes de valeurs pour une seule
// liste de faits, et l'œil ne peut plus balayer une colonne.
func TestATopLevelRowLinesUpWithTheTreeAroundIt(t *testing.T) {
	const marker = "58 days"

	node := valueColumn(t, stripANSI(branch(false, "down", marker)), marker)
	top := valueColumn(t, stripANSI(rowAt(treeValueColumn, "Expiry", marker)), marker)

	if node != top {
		t.Errorf("a tree node puts its value on column %d and a top-level row on column %d", node, top)
	}
}

// Le garde-fou général : un libellé plus long que la colonne ne casse pas
// l'alignement de sa seule ligne, il ouvre une **seconde colonne de valeurs**
// dans la boîte. C'est arrivé en ajoutant `issues assigned`, quinze cellules
// contre douze, et rien ne l'aurait dit.
//
// La vérification ne connaît aucun libellé : sur un nœud aligné, la cellule qui
// précède la valeur est toujours du remplissage. Un libellé qui déborde y met un
// de ses propres caractères.
func TestEveryTreeNodeAlignsItsValue(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, PostureMsg{Posture: posture{Read: true}})

	healthLeft, healthRight := healthColumns(m)
	dockerLeft, dockerRight := dockerColumns(m, "4 total")

	// Les colonnes sont vérifiées **avant** leur assemblage : une fois collées,
	// les nœuds des deux arbres partagent une ligne, et la colonne de droite
	// commence là où la gauche finit — pas sur une position connue d'avance.
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
			// Un nœud sans valeur n'a rien à aligner.
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

// Les deux colonnes de Health portent chacune deux arbres, et les seconds
// doivent commencer sur la même ligne : l'échéance donne un nœud de plus aux
// certificats, donc sans rattrapage `Repositories` démarrerait une ligne
// au-dessus de `Images` et les deux arbres du bas se liraient en escalier.
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

// Une valeur qui remplit sa moitié ne doit pas toucher la colonne d'à côté :
// collées, le nombre et le coude du premier nœud se lisent ensemble.
func TestTwoColumnsKeepAGutter(t *testing.T) {
	out := sideBySide([]string{strings.Repeat("x", 100)}, []string{"R"}, 40)
	if len(out) != 1 {
		t.Fatalf("sideBySide returned %d lines for one row", len(out))
	}

	// Deux cellules est le minimum écrit ici plutôt que repris de la constante :
	// un test qui la relit passerait aussi bien avec zéro.
	got := stripANSI(out[0])
	if gap := strings.Index(got, "R") - strings.LastIndex(got, "x") - 1; gap < 2 {
		t.Errorf("a left column that fills its half leaves a %d-cell gutter: %q", gap, got)
	}
}

// Et la contrepartie : une colonne assemblée fait exactement la largeur de la
// boîte, sinon la boîte d'à côté se décale (Rule 116).
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

// L'échéance pend de Certs : c'est un fait sur les certificats, et flottant
// au-dessus des arbres elle ne disait pas de quoi elle parlait. Elle en est
// aussi le dernier nœud, donc le coude change de ligne.
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
	// Le nom du certificat est tombé avec la demi-boîte : :status possède la
	// liste nommée, et « 58 days  google.com » n'y tient pas.
	if strings.Contains(expiry, "google.com") {
		t.Errorf("the expiry node reads %q — a half-width column cannot hold the name", expiry)
	}
	if err := nodeUnder(certs, "Certs", "error"); strings.HasPrefix(err, theme.IconTreeEnd) {
		t.Errorf("the error node reads %q — it no longer ends the run", err)
	}

	// Les moniteurs n'ont pas d'échéance, donc pas de nœud.
	monitors, _ := healthColumns(m)
	if got := nodeUnder(monitors, "Monitors", "expiry"); got != "" {
		t.Errorf("the Monitors tree grew an expiry node: %q", got)
	}
}

// ── Monitor and certificate icons ────────────────────────────────────────────

// L'icône nomme l'état de la ligne, pas son compte : une coche sur « down »
// disait « tout va bien » à l'endroit même où l'on cherche combien sont tombés.
// Le vocabulaire est celui de :status — croix pour DOWN, alerte pour ERROR.
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

// Et le glyphe ne change pas quand le compte passe à un : seule la couleur le
// fait, ce que stripANSI efface — d'où la comparaison sur les deux états.
func TestAFailingNodeKeepsTheGlyphItHadAtZero(t *testing.T) {
	quiet := stripANSI(failureCount(0, theme.IconError, theme.StatusDownStyle))
	failing := stripANSI(failureCount(3, theme.IconError, theme.StatusDownStyle))

	if !strings.HasSuffix(quiet, theme.IconError) || !strings.HasSuffix(failing, theme.IconError) {
		t.Errorf("the glyph changed with the count: %q then %q", quiet, failing)
	}
	if !strings.HasPrefix(failing, "3") {
		t.Errorf("failureCount(3) = %q, want the count first and the icon on its right", failing)
	}
}

// ── Coverage ─────────────────────────────────────────────────────────────────

// Ce que la boîte compte désormais à la place des HIGH : les cibles sur
// lesquelles elle ne dit rien. C'est le seul de ses chiffres sur lequel on
// décide quelque chose — lancer un scan.
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

// Un inventaire pas encore chargé ne rend pas zéro : « rien à scanner » est
// exactement le contraire de « on ne sait pas encore ».
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

// Le cache garde l'entrée d'une image supprimée depuis, donc la différence peut
// passer sous zéro. Le plancher dit ce qu'il faut en retenir : plus rien à
// scanner — jamais un nombre négatif.
func TestACacheAheadOfTheInventoryReportsNothingLeft(t *testing.T) {
	if got := uncovered(2, 9); got != 0 {
		t.Errorf("uncovered(2, 9) = %d, want 0", got)
	}
}

// Une seule moitié manquante suffit à rendre le total non mesuré : la somme
// d'un nombre et d'une inconnue est une inconnue.
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

// runUnder returns the contiguous run of nodes a heading opens, stripped. Elle
// s'arrête au premier non-nœud : une boîte porte plusieurs arbres, les deux de
// posture portent les mêmes libellés, et balayer toute la boîte rendrait
// toujours le premier.
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

// Docker en place et rien à récupérer n'est pas Docker absent.
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

// La boîte détaille `docker system df` en entier. La vue en parsait les quatre
// colonnes et n'en lisait qu'une : le récupérable. Les trois autres étaient
// mesurées, rangées, et jetées.
func TestStorageBreaksDownWhatDockerHolds(t *testing.T) {
	lines := renderStorageSection(loadedOnly(t), 60, tierStandard)

	sizes := map[string]string{"images": "1.2GB", "containers": "300MB", "volumes": "50MB"}
	for label, want := range sizes {
		if got := nodeUnder(lines, "Docker", label); !strings.Contains(got, want) {
			t.Errorf("the %q node reads %q, want %q", label, got, want)
		}
	}
}

// Le volume dit ses trois chiffres, et l'occupation porte le pourcentage à côté
// des octets : c'est le pourcentage qui dit s'il faut agir, les octets de
// combien.
func TestStorageReportsTheVolume(t *testing.T) {
	lines := renderStorageSection(loadedOnly(t), 60, tierStandard)

	for label, want := range map[string]string{"capacity": "500.0 GB", "free": "210.0 GB", "used": "290.0 GB"} {
		if got := nodeUnder(lines, "Volume", label); !strings.Contains(got, want) {
			t.Errorf("the %q node reads %q, want %q", label, got, want)
		}
	}
	// Entre parenthèses : `290.0 GB  58%` se lit comme deux faits côte à côte,
	// `290.0 GB (58%)` comme une mesure et sa part.
	if got := nodeUnder(lines, "Volume", "used"); !strings.Contains(got, "(58%)") {
		t.Errorf("the used node reads %q, want the percentage bracketed beside the bytes", got)
	}
}

// Le cache de build est la quatrième ligne de `docker system df`, et celle qui
// répond le plus souvent à « où est passé le disque ». Elle entrait déjà dans la
// somme du récupérable ; sa taille propre était jetée.
func TestStorageNamesTheBuildCache(t *testing.T) {
	m := feed(t, loadedOnly(t), OCIStatsMsg{Stats: shared.OCIStats{
		Available: true, ImagesCount: 8, ImagesSize: "1.2GB", BuildCacheSize: "9.7GB",
	}})

	if got := nodeUnder(renderStorageSection(m, 60, tierStandard), "Docker", "build cache"); !strings.Contains(got, "9.7GB") {
		t.Errorf("the build cache node reads %q, want 9.7GB", got)
	}
}

// Le chemin des workspaces appartient à la boîte Code, qui le porte sous l'arbre
// qui en parle. Répété ici il occupait la première ligne pour ne rien ajouter.
func TestStorageDoesNotRepeatTheWorkspacesPath(t *testing.T) {
	for _, tr := range []tier{tierStandard, tierWide} {
		for _, line := range renderStorageSection(loadedOnly(t), 60, tr) {
			if strings.Contains(stripANSI(line), "workspaces") {
				t.Errorf("the Storage box repeats the path the Code box carries: %q", stripANSI(line))
			}
		}
	}
}
