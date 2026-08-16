package dashboard

import "github.com/anthnel/devdesk/internal/ui/theme"

// tier is the dashboard's layout palier. Un terminal ne sait pas qu'il est 4K :
// il connaît des colonnes et des lignes, et deux tailles de police sur un même
// écran font deux terminaux. Le palier se calcule donc sur tea.WindowSizeMsg,
// et sur les deux dimensions séparément — la largeur décide du nombre de
// colonnes, la hauteur de ce qui tient.
type tier int

const (
	tierCompact tier = iota
	tierStandard
	tierWide
)

// Palier thresholds. Below `standard` the dashboard stacks; at `wide` it opens
// a third column, because two columns of a 240-cell terminal give 118-cell
// boxes to write "MRs 3 assigned" in.
//
// **Le nombre de colonnes ne dépend que de la largeur, et c'est un test qui
// l'a imposé.** Réduire les colonnes quand le terminal raccourcit est à
// l'envers : moins de colonnes veut dire plus de boîtes empilées, donc *plus*
// de hauteur nécessaire. À douze lignes de contenu, une grille 2×2 en perd six
// et une colonne unique en perd vingt-quatre. La hauteur ne gouverne donc que
// `wide`, dont les graphes braille sont réellement plus hauts.
//
// La hauteur est en lignes de *contenu*, pas en lignes de terminal : la vue
// reçoit ce que le routeur lui laisse — header 9, ligne de titre 1, footer 3.
const (
	standardMinWidth = 100
	wideMinWidth     = 180
	// La grille à trois colonnes empile trois rangées et ses boîtes portent des
	// arbres : son squelette — sans une seule courbe — fait une quarantaine de
	// lignes. En dessous, deux colonnes en perdent bien moins, ce que
	// TestTheChosenPalierLosesTheFewestLines vérifie à chaque hauteur.
	wideMinHeight = 42
)

// gridHeight is what a two-row grid of boxes needs, in content lines.
//
// **Le viewport du routeur ne défile pas** : rien ne lui transmet de touche, il
// coupe. Un layout trop haut ne fait donc pas apparaître une barre de
// défilement, il perd des lignes en silence — ce que §3.19 corrige par
// ailleurs. En dessous de gridHeight() aucun layout ne tient, et le palier ne
// choisit plus le meilleur mais le moins mauvais.
// Elle est **mesurée**, pas calculée : une constante décrivant la hauteur des
// boîtes se démode à la première ligne ajoutée, et c'est arrivé trois fois en
// deux jours — l'échéance de certificat, les arbres, la ligne vide sous chaque
// graphe.
func (m Model) gridHeight(t tier) int {
	columns := m.columnsFor(t)
	total := leadingBlank
	for _, height := range m.innerHeights(columns, t.columnWidth(m.width), t) {
		total += height + theme.BoxChrome
	}
	return total
}

// leadingBlank is the empty line between the title rule and the first row.
const leadingBlank = 1

// trailingBlank is the empty line every box keeps under its last fact.
//
// Elle est ajoutée à la hauteur de la **rangée**, pas au rendu de chaque
// section : padTo remplit ensuite jusqu'à cette hauteur, donc la boîte la plus
// haute de la rangée en reçoit exactement une et les autres davantage. Une
// ligne ajoutée par chaque render() aurait le même effet visuel sur la boîte la
// plus haute et une de trop partout ailleurs.
//
// Sans elle, la dernière valeur de la boîte la plus haute touche sa bordure
// basse — et c'est précisément la boîte que l'œil lit en premier.
const trailingBlank = 1

// Box geometry. Les boîtes empilées ne sont pas séparées par une ligne vide :
// leurs bordures s'en chargent, et à 30 lignes le budget est exactement de
// deux boîtes (2 × (6 + 2) = 16).
//
// nominalInnerHeight ne sert qu'à exprimer les seuils de palier : la hauteur
// réelle d'une boîte est *dérivée des sections à l'écran* (voir innerHeight
// dans view.go), donc une section qui grandit ne peut pas être tronquée par une
// constante que personne n'a pensé à suivre.
const (
	nominalInnerHeight = 6
	columnGap          = 1
	sidePadding        = 1
)

// layoutTier is the only thing that decides a palier. Aucun renderer ne calcule
// le sien : deux règles de palier, et les boîtes d'une même grille cessent de
// s'accorder sur leur hauteur.
func layoutTier(width, height int) tier {
	switch {
	case width >= wideMinWidth && height >= wideMinHeight:
		return tierWide
	case width >= standardMinWidth:
		return tierStandard
	default:
		return tierCompact
	}
}

// columns returns how many columns of boxes the palier lays out.
func (t tier) columns() int {
	switch t {
	case tierWide:
		return 3
	case tierStandard:
		return 2
	default:
		return 1
	}
}

// tabCountFor returns how many tabs the palier offers. À `wide`, la troisième
// colonne porte déjà les boîtes de Resources : proposer l'onglet en plus
// afficherait deux fois les mêmes trois boîtes, ce qui est précisément la
// duplication qu'un onglet est censé éviter.
//
// Il n'y a donc pas de barre d'onglets à `wide` : un seul onglet n'est pas un
// choix, et la barre dirait « vous êtes ici », ce que l'écran dit déjà.
//
// Ça demande que `App.resize` itère : il interroge la vue sur la hauteur de son
// footer avant de lui communiquer sa nouvelle taille, donc une barre qui
// apparaît avec le palier répondrait d'après la taille précédente. Deux passes
// suffisent, et la seconde interroge une vue qui connaît sa taille.
func tabCountFor(t tier) int {
	if t == tierWide {
		return 1
	}
	return int(tabCount)
}

// columnWidth returns the width of one box column for a total view width.
func (t tier) columnWidth(width int) int {
	n := t.columns()
	available := width - 2*sidePadding - columnGap*(n-1)
	return max(available/n, minColumnWidth)
}

// minColumnWidth is the narrowest column that still holds a label and a value.
const minColumnWidth = 24
