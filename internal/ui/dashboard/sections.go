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

// section is one titled box of the dashboard. Les sections sont déclarées dans
// une table plutôt qu'en autant de méthodes render*Section : c'est ce qui rend
// la règle « une section déclare sa hauteur et la remplit » vérifiable pour
// toutes d'un coup (§3.19 phase 1).
type section struct {
	title string
	// render returns the box's content lines. Il en retourne le *même nombre*
	// quel que soit l'état des données — c'est le contrat que
	// TestEverySectionKeepsItsHeightWhateverItsState vérifie.
	render func(m Model, width int, t tier) []string
}

// labelWidth aligns every label column of every box on one width.
const labelWidth = 11

// overviewSections returns the four boxes of the Overview tab, in reading
// order: the two text boxes first, then the two that carry a chart.
//
// Le regroupement est par question posée, pas par source de données — d'où
// deux fusions : workspaces rejoint GitLab (l'explorer crée, workspaces
// réconcilie : un seul sujet vu des deux bouts) et les outils rejoignent Host
// (ce sont les binaires de cette machine, mesurés par la même sonde).
func overviewSections() []section {
	return []section{
		{title: theme.IconGitlab + " Code", render: renderCodeSection},
		{title: theme.IconSecurity + " Health", render: renderHealthSection},
		{title: theme.IconServer + " " + hostLabel(), render: renderHostSection},
		{title: theme.IconDocker + " Docker (VM)", render: renderDockerSection},
	}
}

// resourceSections returns the boxes of the Resources tab — inline in the third
// column at `wide`. Elles n'existent que parce qu'aucune vue ne les montre :
// il n'y a pas de :host, et :containers montre un conteneur, pas la machine.
func resourceSections() []section {
	return []section{
		{title: theme.IconArrowDown + " Network", render: renderNetworkSection},
		{title: theme.IconDirectory + " Storage", render: renderStorageSection},
	}
}

// hostLabel names the measurement point rather than the machine. gopsutil reads
// the OS dk runs on, while docker stats reads inside the Docker Desktop VM:
// les deux sont vrais et ne s'additionnent pas.
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
	// L'icône suit la valeur : la colonne des valeurs commence alors au même
	// endroit sur toutes les lignes, ce qu'une icône en tête décale d'un cran
	// sur les seules lignes qui en portent une.
	sessionLine := theme.DimStyle.Render("not connected") + theme.Bg("  ") + theme.DimStyle.Render(theme.IconError)
	if m.shared.IsAuthenticated && m.shared.CurrentUser != nil {
		sessionLine = theme.PrimaryColorStyle.Bold(true).Render(m.shared.CurrentUser.Username) +
			theme.Bg("  ") + theme.StatusOKStyle.Render(theme.IconOK)
	}

	mrs, review, issues := unknownValue(), unknownValue(), unknownValue()
	if m.gitlabStats != nil {
		mrs = countValue(m.gitlabStats.AssignedMRs)
		review = countValue(m.gitlabStats.ReviewMRs)
		issues = countValue(m.gitlabStats.AssignedIssues)
	}

	workspaces := unknownValue()
	if !m.loadingWorkspaces {
		workspaces = countValue(m.workspaceCount)
	}

	if t != tierWide {
		return []string{
			sessionLine,
			row("Merge req.", mrs+theme.Bg(" assigned  ")+review+theme.Bg(" to review")),
			row("Issues", issues+theme.Bg(" assigned")),
			theme.Bg(""),
			row("Workspaces", workspaces),
			theme.DimStyle.Render(truncatePath(m.config.App.WorkspacesDir, width-labelWidth)),
		}
	}

	// À `wide`, la boîte devient deux arbres, et la séparation est celle des
	// deux moitiés de §3.16 : l'explorer crée ce qui n'existe pas — la forge —
	// et workspaces réconcilie ce qui existe — le disque. Chacune a sa question,
	// donc chacune a sa racine, et les faits de la forge cessent de flotter
	// au-dessus d'un arbre auquel ils n'appartiennent pas.
	//
	// Les compteurs y deviennent des branches : « 0 assigned  0 to review » sur
	// une ligne demande de relire la phrase pour savoir lequel est lequel, deux
	// nœuds le disent en les alignant.
	// Le qualificatif est dans le libellé, pas dans la valeur : « issues 0
	// assigned » et « MR assigned  0 » disaient la même chose de deux façons,
	// et la colonne des valeurs ne portait alors pas partout la même sorte de
	// chose. Les trois compteurs se lisent maintenant en colonne.
	pathWidth := theme.BoxContentWidth(width) - treeValueColumn
	return []string{
		theme.Bg("GitLab"),
		branch(false, "user", sessionLine),
		branch(false, "host", theme.Bg(forgeHost(m.config.GitLab.URL))),
		branch(false, "issues assigned", issues),
		branch(false, "MR assigned", mrs),
		branch(true, "MR to review", review),
		treeGap(),
		theme.Bg("Workspaces"),
		branch(false, "repositories", workspaces),
		branch(false, "path", theme.DimStyle.Render(truncatePath(m.config.App.WorkspacesDir, pathWidth))),
		branch(true, "disk", treeSize(m.wsSize)),
	}
}

// treeGap separates two trees stacked in one box. Le coude du dernier nœud dit
// où un arbre finit, mais pas qu'un autre commence : sans cette ligne les deux
// racines se lisent comme deux nœuds de plus.
func treeGap() string { return theme.Bg("") }

// treeSize renders what the workspaces occupy.
//
// **C'est la taille de l'arborescence, pas le remplissage du volume.** Ce que
// ces dépôts coûtent est ce sur quoi on peut agir — en supprimer un rend la
// place — là où le volume mélange les workspaces à tout le reste de la machine.
// La boîte Host garde la place libre, qui est l'autre question.
//
// Elle se paie : c'est le seul chiffre du dashboard dont la mesure parcourt
// l'arborescence entière. Voir metrics.Size et Model.measuringSize.
func treeSize(size metrics.TreeSize) string {
	if !size.OK {
		return unknownValue()
	}
	out := theme.Bg(humanBytes(size.Bytes))
	if size.Partial {
		// Un dossier refusé fait sous-estimer le total, et un total
		// sous-estimé sans mention se lit comme une mesure.
		out += theme.DimStyle.Render("  partial")
	}
	return out
}

// forgeHost strips the scheme off the configured forge URL: c'est l'hôte qui
// identifie l'instance, et `https://` le dit de toutes.
func forgeHost(rawURL string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

// treeLabelWidth aligns the values of a tree's nodes, indent included. Il est
// dimensionné sur le plus long libellé de l'application — `issues assigned`,
// quinze cellules — parce qu'un libellé qui déborde ne casse pas l'alignement de
// sa seule ligne : il ouvre une seconde colonne de valeurs dans la boîte.
// TestEveryTreeNodeAlignsItsValue le vérifie sur toutes les sections.
const treeLabelWidth = 17

// treeStemWidth is what IconTreeBranch and IconTreeEnd occupy: two box-drawing
// characters and the space after them. Il est écrit plutôt que mesuré parce
// qu'une constante ne peut pas appeler lipgloss.Width, et
// TestATreeRowLinesUpWithItsBranches le vérifie contre le rendu réel.
const treeStemWidth = 3

// treeValueColumn is where a tree node's value begins.
const treeValueColumn = treeStemWidth + treeLabelWidth - 1

// narrowTreeLabelWidth is the same column inside a **split** box, where each
// tree gets half the width.
//
// Elle est dimensionnée sur `reclaimable`, onze cellules, le plus long libellé
// des trois boîtes concernées. La colonne large y coûterait trois cellules de
// valeur de plus, sur les onze que laisse une demi-boîte au palier `wide` le
// plus étroit (180 colonnes) — de quoi tronquer `3 days ago`.
const (
	narrowTreeLabelWidth  = 14
	narrowTreeValueColumn = treeStemWidth + narrowTreeLabelWidth - 1
)

// branch renders one node of a tree. `last` picks the corner, so the run of
// nodes has a visible end — sans lui, deux arbres qui se suivent se lisent
// comme un seul.
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
// La coupe est au milieu, et chaque moitié est **tronquée** plutôt que laissée
// déborder : une ligne trop longue à gauche décalerait toute la colonne de
// droite, et une ligne trop longue à droite déborderait de la boîte, ce que
// RenderTitledBox couperait de toute façon — mais après avoir cassé
// l'alignement (Rule 116).
//
// L'assemblage est fait ligne à ligne avec PadWithBg et jamais par
// lipgloss.JoinHorizontal, qui insère des espaces nus laissant passer le fond
// natif du terminal (Rule 115).
func sideBySide(left, right []string, width int) []string {
	half := max(width/2, 1)
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		var l, r string
		if i < len(left) {
			// La gouttière est retirée de la colonne de gauche, pas ajoutée à
			// droite : sans elle, une valeur qui remplit sa moitié touche le
			// coude du premier nœud d'à côté et les deux se lisent ensemble.
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

// padRuns lengthens a run with blank lines. Le padding est **vide de contenu**,
// pas paddé en largeur : sideBySide pose le fond de la ligne entière (Rule 115).
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
		certs = statusSummary(m.filterComponents(true))
	}
	expiry := nearestExpiry(m.filterComponents(true), m.loadingServices, true)

	// Six lignes, et pas sept : la ligne vide qui séparait la supervision de la
	// sécurité a payé l'échéance de certificat. Une septième ligne ici ferait
	// déborder l'overview entier.
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
// certificates on the right with the images. Les quatre arbres tenaient en
// colonne à dix-neuf lignes, ce qui faisait de Health la plus haute boîte de sa
// rangée et rognait d'autant les courbes des trois autres.
//
// Elles sont rendues séparément de leur assemblage : c'est ce qui permet de les
// vérifier une par une, une fois collées les nœuds des deux arbres partagent une
// ligne.
func healthColumns(m Model) (left, right []string) {
	unscannedImages, imagesMeasured := m.unscannedImages()
	unscannedRepos, reposMeasured := m.unscannedRepositories()

	monitors := append([]string{theme.Bg("Monitors")},
		statusBranches(m.filterComponents(false), m.loadingServices, "")...)

	// L'échéance pend de Certs plutôt que de flotter au-dessus : c'est un fait
	// sur les certificats, et il n'a de sens que là. Le nom du certificat est
	// tombé avec le passage en deux colonnes — la demi-boîte ne le tient pas —
	// et c'est :status qui possède la liste nommée.
	//
	// Ce nœud de plus est aussi ce qui décale les deux colonnes : sans le
	// rattrapage ci-dessous, `Repositories` démarrerait une ligne au-dessus de
	// `Images` et les deux arbres du bas se liraient en escalier.
	certs := m.filterComponents(true)
	certTree := append([]string{theme.Bg("Certs")},
		statusBranches(certs, m.loadingServices, nearestExpiry(certs, m.loadingServices, false))...)

	top := max(len(monitors), len(certTree))
	left = append(padRuns(monitors, top), treeGap(), theme.Bg("Repositories"))
	left = append(left, postureBranches(m.posture, m.posture.Repositories, unscannedRepos, reposMeasured)...)

	right = append(padRuns(certTree, top), treeGap(), theme.Bg("Images"))
	right = append(right, postureBranches(m.posture, m.posture.Images, unscannedImages, imagesMeasured)...)

	return left, right
}

// statusBranches renders one node per state, plus an optional trailing node.
// Les trois états sont toujours là, y compris à zéro : une branche absente se
// lit comme un état qu'on ne surveille pas, et c'est l'inverse de ce qu'un zéro
// veut dire.
// `expiry` est vide pour les moniteurs, qui n'ont pas d'échéance ; quand il est
// là il devient le dernier nœud, et l'arbre gagne une ligne. Les deux colonnes
// n'ont donc pas la même hauteur, et c'est sideBySide qui l'absorbe.
func statusBranches(components []status.ComponentStatus, loading bool, expiry string) []string {
	nodes := func(up, down, errValue string) []string {
		out := []string{
			narrowBranch(false, "up", up),
			narrowBranch(false, "down", down),
			narrowBranch(expiry == "", "error", errValue),
		}
		if expiry != "" {
			out = append(out, narrowBranch(true, "expiry", expiry))
		}
		return out
	}

	switch {
	case loading:
		return nodes(unknownValue(), unknownValue(), unknownValue())
	case len(components) == 0:
		// Rien à surveiller : un seul nœud le dit, et les lignes vides gardent
		// la hauteur — une boîte qui rétrécit décale toute sa rangée.
		out := []string{narrowBranch(true, "configured", theme.DimStyle.Render("none"))}
		for range len(nodes("", "", "")) - 1 {
			out = append(out, theme.Bg(""))
		}
		return out
	default:
		ok, down, errCount := countStatuses(components)
		return nodes(
			countValue(ok)+theme.Bg("  ")+theme.StatusOKStyle.Render(theme.IconOK),
			failureCount(down, theme.IconError, theme.StatusDownStyle),
			failureCount(errCount, theme.IconWarning, theme.StatusErrorStyle),
		)
	}
}

// failureCount renders one failing state's tally. Le glyphe est celui de l'état
// — croix pour DOWN, alerte pour ERROR, le vocabulaire de :status — y compris à
// zéro : une coche sur la ligne « down » disait « tout va bien » à l'endroit
// même où l'on cherche combien sont tombés, et c'est l'état de la ligne qu'une
// icône nomme, pas son compte.
//
// C'est la couleur qui porte le compte : éteinte à zéro, parce qu'une croix
// rouge sur « 0 down » apprend la couleur au lecteur au lieu de l'alerter.
// L'icône reste à droite du chiffre, comme partout ailleurs.
func failureCount(n int, glyph string, style lipgloss.Style) string {
	if n == 0 {
		return countValue(0) + theme.Bg("  ") + theme.DimStyle.Render(glyph)
	}
	return severityCount(n) + theme.Bg("  ") + style.Render(glyph)
}

// postureBranches renders one family's tally, one figure per node.
//
// Chaque chiffre a sa branche plutôt que de partager une ligne : deux nombres
// côte à côte demandent de retenir lequel est lequel, et c'est précisément le
// chiffre qu'on lit en diagonale.
//
// `unscanned` a pris la place des HIGH parce qu'il se décide : il nomme les
// cibles sur lesquelles la boîte entière ne dit rien, et la réponse est de
// lancer un scan. Un HIGH de plus ne changeait aucune décision que la CRITICAL
// au-dessus n'avait déjà prise.
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
// Ce sont des cibles et non des secrets : deux dépôts sont deux décisions,
// quarante fuites dans le même n'en font qu'une, et c'est l'inventaire (:sec)
// qui détaille.
//
// `-` quand aucun verdict n'est connu, ce qui n'est pas une précaution
// théorique : `scan.enable_secret` coupée, un outil absent, ou des entrées
// écrites avant que le scan d'image ait une étape secrets — dans les trois cas
// un `0` dirait « aucune cible n'en porte » de cibles que personne n'a
// regardées. Un verdict connu sur une partie seulement suffit à afficher le
// compte : il est alors un plancher, et un plancher non nul se décide.
func secretsValue(side postureSide) string {
	if side.SecretsKnown == 0 {
		return unknownValue()
	}
	return severityCount(side.Secrets)
}

// unscannedValue renders a coverage gap. Il ne prend pas le rouge de
// severityCount : une cible jamais scannée n'a pas de CRITICAL, elle a une
// inconnue, et les deux ne se règlent pas de la même façon.
func unscannedValue(n int, measured bool) string {
	if !measured {
		return unknownValue()
	}
	return countValue(n)
}

// severityCount colours a finding count only when there is one to find. Un zéro
// en rouge apprend la couleur au lecteur au lieu de l'alerter.
func severityCount(n int) string {
	if n == 0 {
		return theme.DimStyle.Render("0")
	}
	return theme.StatusErrorStyle.Render(fmt.Sprintf("%d", n))
}

// scanAge renders how stale the oldest scan is. Un compteur de CRITICAL vieux
// de trois semaines compte sur du code qui n'existe plus.
// `never` plutôt que « nothing scanned » : sous un nœud appelé `oldest` la
// phrase longue répète le libellé, et une demi-boîte au palier `wide` le plus
// étroit ne tient que onze cellules de valeur.
func scanAge(side postureSide) string {
	if side.Targets == 0 {
		return theme.DimStyle.Render("never")
	}
	if side.Oldest.IsZero() {
		return unknownValue()
	}
	return theme.Bg(theme.TimeAgo(side.Oldest))
}

// nearestExpiry renders the certificate that runs out first — la seule des N
// dates qui demande une décision.
//
// `named` est faux dans une colonne partagée : une demi-boîte ne tient pas
// « 58 days  registry.example.com », et c'est le nombre de jours qui décide de
// quelque chose. La liste nommée appartient à :status, qui la possède déjà.
func nearestExpiry(certs []status.ComponentStatus, loading, named bool) string {
	if loading {
		return unknownValue()
	}
	if len(certs) == 0 {
		return theme.DimStyle.Render("none configured")
	}

	var soonest *status.ComponentStatus
	for i, c := range certs {
		if c.SSLDaysLeft == nil {
			continue
		}
		if soonest == nil || *c.SSLDaysLeft < *soonest.SSLDaysLeft {
			soonest = &certs[i]
		}
	}
	if soonest == nil {
		// Des certificats surveillés dont aucun n'a pu être lu : c'est une
		// absence de mesure, pas une échéance lointaine.
		return unknownValue()
	}

	days := *soonest.SSLDaysLeft
	value := theme.Bg(fmt.Sprintf("%d days", days))
	if days <= expirySoonDays {
		value = theme.StatusErrorStyle.Render(fmt.Sprintf("%d days", days))
	}
	if !named {
		return value
	}
	return value + theme.DimStyle.Render("  "+soonest.Name)
}

// expirySoonDays is where a certificate stops being a date and becomes a task.
const expirySoonDays = 30

// chartsPerBox is what a chart-bearing box holds — CPU and RAM, RX and TX. Les
// trois boîtes à graphes tiennent la même rangée, donc la hauteur libre se
// partage entre les deux courbes d'une seule d'entre elles.
const chartsPerBox = 2

// Bornes : en dessous de 3 lignes le braille n'a pas de quoi montrer sa
// résolution verticale, et au-delà de 12 un graphe de pourcentage n'apprend
// plus rien de la hauteur qu'il prend.
const (
	minBrailleHeight = 3
	maxChartHeight   = 12
)

// chartHeight is how many lines a chart gets.
//
// À `standard` elle vaut 1 : la grille y tient tout juste, donc un graphe prend
// la place d'une ligne vide au lieu de s'ajouter.
//
// À `wide` elle est **mesurée**, pas déduite de constantes : View() rend
// d'abord la grille sans aucune courbe, constate ce qui reste, et repasse le
// résultat par `chartLines`. Des constantes décrivant la hauteur des textes
// étaient justes le jour où elles ont été écrites et fausses dès qu'une boîte a
// gagné une ligne — ce qui est arrivé à Health le jour même.
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
	// La moyenne de charge n'est affichée nulle part : sur Windows elle
	// retourne {0,0,0} avec err=nil, donc une valeur indiscernable d'une
	// donnée, et un zéro se lit comme « au repos ».
	cpu, ram := unknownValue(), unknownValue()
	if m.host.OK {
		// Le pourcentage porte ce qui le rend lisible : 40 % sur quatre cœurs
		// et 40 % sur trente-deux ne décrivent pas la même machine, et 92 %
		// de mémoire ne dit pas s'il reste deux gigaoctets ou deux cents.
		cpu = percentValue(m.host.CPUPercent) + coreSuffix(m.host.Cores)
		ram = percentValue(m.host.MemPercent) +
			theme.DimStyle.Render("  "+humanBytes(m.host.MemUsed)+" of "+humanBytes(m.host.MemTotal))
	}

	// CPU puis sa courbe, RAM puis la sienne : les trois boîtes à graphes
	// suivent le même ordre, donc leurs courbes tombent sur les mêmes lignes
	// d'une colonne à l'autre. C'est ce qui les rend comparables d'un coup
	// d'œil, et une ligne de détail intercalée le défaisait.
	lines := []string{row("CPU", cpu)}
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.CPUPercent }, 100)...)
	lines = append(lines, row("RAM", ram))
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.MemPercent }, 100)...)

	// Il n'y a **pas** de ligne Disk ici : la place libre était le premier fait
	// de la boîte Storage, mot pour mot. Cette boîte mesure ce que le processeur
	// et la mémoire font *maintenant* ; le disque ne bouge pas à la seconde et
	// appartient à celle qui le détaille.
	return append(lines, toolsBlock(m)...)
}

// toolsBlock says whether this machine can do the work, and names what it
// cannot.
//
// Un compteur — « 4 of 5 available » — pose la question qu'il ne répond pas :
// lequel manque, et donc quoi installer. La liste complète, elle, coûte cinq
// lignes pour dire cinq fois « oui » sur une machine correctement outillée.
// D'où les deux formes : une ligne quand tout est là, un nœud par manquant
// sinon. C'est le seul bloc du dashboard dont la hauteur suit ses données, et
// il peut se le permettre — un outil installé ne se désinstalle pas entre deux
// rafraîchissements, là où un compte change à chaque tour.
//
// Les manquants se lisent contre knownTools et non contre ce qui a été
// détecté : un outil absent de la détection est absent tout court, et un
// dénominateur qui rétrécit avec elle rendrait « tout est là » d'une machine
// qui a perdu une sonde.
func toolsBlock(m Model) []string {
	if m.loadingTools {
		return []string{row("Tools", unknownValue())}
	}

	missing := missingTools(m.tools)
	if len(missing) == 0 {
		return []string{row("Tools", theme.Bg("all available  ")+theme.StatusOKStyle.Render(theme.IconOK))}
	}

	// Les noms gardent leur casse déclarée là où les autres nœuds sont en
	// minuscules : `running` et `images` sont des mots, `Gitleaks` est ce qu'il
	// faut taper pour l'installer.
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

// chartOf renders one host series at the palier's chart height. Les
// pourcentages sont tracés sur une échelle fixe de 0 à 100 : sans elle, une
// machine au repos rend un graphe aussi haut qu'une machine saturée, parce que
// l'échelle suivrait le maximum observé.
func chartOf(m Model, width int, t tier, pick func(metrics.HostSample) float64, maxValue float64) []string {
	return chartBlock(m, series(m.samples, pick), width, t, maxValue)
}

// chartBlock is a chart followed by one empty line. Le fond du graphe est plus
// clair que celui de la boîte, donc sans cette ligne la zone touche la valeur
// qui la suit et les deux se lisent comme un seul bloc.
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
			// Sans l'arbre, le total seul ne dit pas combien tournent.
			containers += theme.Bg("  ") + countValue(d.Running) + theme.Bg(" running")
		}
	}

	// Les **comptes** seulement : les tailles sont dans la boîte Storage, qui
	// répond à « combien de place » là où celle-ci répond à « combien il y en
	// a ». Elles étaient dans les deux, sous deux formes différentes.

	// CPU et RAM d'abord, dans le même ordre que Host et Network : les trois
	// boîtes à graphes se lisent alors sur les mêmes lignes, et les courbes se
	// comparent sans chercher laquelle est laquelle. Les comptes suivent.
	//
	// `docker stats` tourne sur son horloge propre (≈ 2 s par appel mesuré).
	// Les deux parts sont ramenées à ce que le daemon possède, donc elles vont
	// de 0 à 100 comme celles de Host — c'est ce que la mise en page promet en
	// les posant sur les mêmes lignes. Le suffixe dit contre quoi : `docker
	// info` compte les cœurs de la VM sous Windows et macOS, pas ceux de la
	// machine, donc ce n'est pas forcément le chiffre de la boîte Host.
	lines := []string{row("CPU", dockerPercent(m,
		func(a docker.Aggregate) float64 { return a.CPUPercent }, coreSuffix(m.dockerAgg.Cores)))}
	lines = append(lines, chartBlock(m, m.dockerSamples, width, t, 100)...)
	lines = append(lines, row("RAM", dockerPercent(m,
		func(a docker.Aggregate) float64 { return a.MemPercent }, "")))
	lines = append(lines, chartBlock(m, m.dockerMemSamples, width, t, 100)...)

	if t != tierWide {
		// Sans la seconde colonne, les deux arbres s'empilent : les conteneurs
		// se réduisent alors à leur ligne de tête, qui porte déjà le compte des
		// actifs, et l'inventaire garde sa racine — c'est elle qui dit que les
		// trois chiffres suivants parlent tous de la même chose.
		return append(append(lines, rowAt(narrowTreeValueColumn, "Containers", containers)),
			resourceTree(m)...)
	}

	// Sous les courbes, deux colonnes : l'arbre des conteneurs à gauche,
	// l'inventaire à droite. Ils font quatre lignes chacun, donc la boîte se
	// remplit sans qu'aucune moitié attende l'autre.
	left, right := dockerColumns(m, containers)
	return append(lines, sideBySide(left, right, theme.BoxContentWidth(width))...)
}

// dockerColumns builds the two runs the Docker box ends on. Un nœud par état :
// « 9 total  2 running » laisse le lecteur soustraire pour savoir combien
// dorment, et ne dit rien des conteneurs en pause.
func dockerColumns(m Model, containers string) (left, right []string) {
	left = []string{
		rowAt(narrowTreeValueColumn, "Containers", containers),
		narrowBranch(false, "running", dockerState(m, func(d shared.DockerStats) int { return d.Running })),
		narrowBranch(false, "stopped", dockerState(m, func(d shared.DockerStats) int { return d.Stopped })),
		narrowBranch(true, "paused", dockerState(m, func(d shared.DockerStats) int { return d.Paused })),
	}
	return left, resourceTree(m)
}

// resourceTree lists what the daemon holds besides its containers. Les trois
// comptes pendent d'une racine plutôt que de flotter côte à côte : ce sont des
// objets du même daemon, et les aligner sous un mot dit lequel, là où trois
// lignes de premier niveau se lisaient comme trois sujets.
//
// Networks y entre pour la même raison qu'images et volumes en font partie :
// c'est une ressource que :oci gère et que le dashboard ne comptait pas — la
// seule des trois que `docker system df` ignore, faute d'octets à déclarer.
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
	// net.IOCounters est cumulatif : le premier échantillon n'a rien à
	// soustraire, et une interface réinitialisée fait reculer le compteur. Dans
	// les deux cas il n'y a pas de débit — `-`, pas `0`.
	rx, tx := unknownValue(), unknownValue()
	if m.host.HasRate {
		rx = theme.Bg(humanBytes(uint64(m.host.NetRXPerSec))) + theme.DimStyle.Render("/s")
		tx = theme.Bg(humanBytes(uint64(m.host.NetTXPerSec))) + theme.DimStyle.Render("/s")
	}

	samples := unknownValue()
	if n := len(m.samples); n > 0 {
		samples = countValue(n) + theme.DimStyle.Render(" samples")
	}

	// Le débit n'a pas de plafond connu : l'échelle reste automatique, contre
	// une échelle fixe de 0 à 100 pour un pourcentage.
	lines := []string{row("RX", rx)}
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.NetRXPerSec }, 0)...)
	lines = append(lines, row("TX", tx))
	lines = append(lines, chartOf(m, width, t, func(s metrics.HostSample) float64 { return s.NetTXPerSec }, 0)...)
	return append(lines, row("History", samples))
}

// knownTools names the tools DevDesk detects, in the order detectTools builds
// them. C'est **elle** qui décide ce qui manque, jamais la liste détectée : un
// outil que la détection ne rend plus est absent, et le compter hors du
// dénominateur le ferait disparaître au lieu de le signaler.
//
// Elle doit rester en phase avec detectTools (model.go).
var knownTools = []string{"Docker", "Trivy", "Gitleaks", "Net Diag", "Git"}

// renderStorageSection answers one question — **où part la place** — in two
// trees: the volume the workspaces live on, and what Docker holds on it.
//
// Le chemin des workspaces n'y est plus : la boîte Code le porte déjà, sous
// l'arbre qui en parle. Répété ici il occupait la première ligne pour ne rien
// ajouter.
//
// Les tailles Docker viennent de `system df`, dont la vue ne lisait jusqu'ici
// que le récupérable — les trois autres colonnes étaient analysées et jetées.
// La boîte Docker (VM) garde les comptes, celle-ci prend les octets : c'était
// dans les deux, sous deux formes.
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

	// Les deux arbres s'**empilent**, à tous les paliers : ils font dix lignes
	// à eux deux, ce qui est la hauteur de leurs voisines de rangée, et les
	// mettre côte à côte laisserait la moitié basse de la boîte vide.
	return append(append(volume, treeGap()), dockerTree...)
}

// diskField renders one figure of the volume, `-` until it has been read.
func diskField(m Model, pick func(metrics.DiskUsage) string) string {
	if !m.wsDisk.OK {
		return unknownValue()
	}
	return theme.Bg(pick(m.wsDisk))
}

// diskUsedField carries the percentage next to the bytes: c'est le pourcentage
// qui dit s'il faut faire quelque chose, et les octets de combien. Les
// parenthèses disent lequel des deux est la mesure : `290 GB  86 %` se lit
// comme deux faits côte à côte, `290 GB (86 %)` comme un seul.
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

// reclaimable is the one figure of the box that names an action. Docker en
// place et rien à récupérer n'est pas Docker absent.
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

// Value states (§3.19). Trois états, pas deux : `-` n'est pas `0`, et une
// source indisponible garde ses libellés au lieu de les remplacer.
//
// Ce sont des fonctions, et pas des `var`, pour deux raisons dont la seconde
// touchait toute l'application :
//
//   - un `Render()` au niveau du paquet s'exécute à l'init, donc avant que
//     `ApplyTheme` n'ait chargé le thème du contexte : les deux chaînes
//     restaient figées sur les couleurs du thème par défaut. C'est le même
//     piège que celui déjà documenté sur `ColorChartBg` dans `ApplyTheme`.
//   - surtout, ce premier rendu déclenche le `sync.Once` par lequel lipgloss
//     mémorise le profil de couleur du terminal, **définitivement**. Il était
//     donc calculé pendant l'init des paquets, c'est-à-dire avant la ligne de
//     `main()` qui pose `COLORTERM=truecolor` quand WSL ne l'a pas propagé.
//     Tout le TUI retombait en ANSI256, où le fond de chaque thème est
//     quantifié sur la palette 256 : `#1e1e2e` (default, mocha) devient le
//     noir 232 et `#24273a` (macchiato) le bleu marine 17. Le fond ne
//     « respectait pas le thème » parce qu'il n'en recevait jamais la couleur
//     exacte.
//
// Rendre à la demande suffit à corriger les deux : le premier rendu a alors
// lieu dans `View()`, longtemps après `main()`.
func unknownValue() string     { return theme.DimStyle.Render("-") }
func unavailableValue() string { return theme.DimStyle.Render("n/a") }

// row renders one "label  value" line, aligned on labelWidth. Un libellé aussi
// long que la colonne garde quand même son espace : sans lui, "Docker root" et
// sa valeur se touchent et se lisent comme un seul mot.
func row(label, value string) string {
	return rowAt(labelWidth, label, value)
}

// rowAt is the same line on a chosen value column — ce dont une boîte à arbres
// a besoin : ses nœuds alignent leurs valeurs sur treeValueColumn, et une ligne
// de premier niveau qui garde labelWidth ouvrirait une seconde colonne de
// valeurs dans la même boîte.
func rowAt(column int, label, value string) string {
	width := max(column, len(label)+1)
	return theme.Bg(label+strings.Repeat(" ", width-len(label))) + value
}

// countValue renders a measured count: a zero informs no one, so it stays dim
// and the colour is spent on what is worth spotting (Rule 122's discipline).
func countValue(n int) string {
	if n > 0 {
		return theme.PrimaryColorStyle.Bold(true).Render(fmt.Sprintf("%d", n))
	}
	return theme.DimStyle.Render("0")
}

// percentValue renders a measured percentage. Il n'est jamais coloré par
// seuil : un seuil est un réglage, il vivrait dans la vue configuration, et une
// couleur inventée ici dirait « attention » sans que personne l'ait demandé.
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
	if down > 0 {
		out += theme.Bg(fmt.Sprintf("  %d ", down)) + theme.StatusDownStyle.Render(theme.IconError)
	}
	if errCount > 0 {
		out += theme.Bg(fmt.Sprintf("  %d ", errCount)) + theme.StatusErrorStyle.Render(theme.IconWarning)
	}
	return out
}

// truncatePath keeps a path's tail, which is the half that identifies it.
//
// Elle compte en **cellules et en runes**, pas en octets : `len(path)` mesure
// des octets, donc sur `C:\Users\José\dépôts` la coupe tombait au milieu d'un
// caractère accentué et rendait de l'UTF-8 invalide (`…\xa9\projet`). Un
// Windows français en produit à chaque `Téléchargements`.
func truncatePath(path string, width int) string {
	if width < 4 || lipgloss.Width(path) <= width {
		return path
	}

	runes := []rune(path)
	kept := countKept(runes, width-1) // l'ellipse occupe une cellule
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
