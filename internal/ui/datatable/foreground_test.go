package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// backgroundOnlyStyle declares a background and nothing else, ce qu'une colonne
// a le droit de faire.
func backgroundOnlyStyle(row) lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.ColorBackground)
}

// La couleur du **texte** d'une cellule, qui manquait.
//
// Ni `theme.DefaultTableStyles()` ni les styles de bubbles ne posent de
// foreground sur `Cell` : une colonne qui ne déclare pas de `Style` sortait donc
// dans la couleur par défaut du terminal, sur laquelle le thème n'a aucune
// prise. Quatre vues avaient réécrit `Foreground(theme.ColorText)` dans un
// `Style` à elles pour la récupérer, ce qui est la forme que prend un défaut
// manquant.

// Une colonne sans Style prend la couleur de texte du thème.
func TestACellWithNoStyleCarriesTheThemeForeground(t *testing.T) {
	withTrueColor(t)
	m := loaded(t) // aucune colonne ne déclare de Style

	unselected := rowLines(&m)[1]

	if !strings.Contains(unselected, foreground(theme.ColorText)) {
		t.Errorf("a row of a table with no styled column carries no theme foreground: %q", unselected)
	}
}

// Chaque segment **qui porte du texte** ouvre les deux couleurs.
//
// Le fond est exigé de tous les segments par le test voisin, parce qu'il se voit
// sur une espace ; le texte ne se voit que là où il y a un caractère, et lipgloss
// sort le padding d'une cellule en segments de sa seule couleur de fond. Exiger
// un foreground sur une espace serait exiger une séquence qui ne change rien à
// l'écran.
func TestEveryRunThatShowsTextCarriesTheThemeForeground(t *testing.T) {
	withTrueColor(t)
	m := loaded(t)

	unselected := rowLines(&m)[1]
	segments := strings.Split(unselected, "\x1b[0m")

	checked := 0
	for i, segment := range segments[:len(segments)-1] {
		if strings.TrimSpace(visible(segment)) == "" {
			continue
		}
		checked++
		if !strings.Contains(segment, foreground(theme.ColorText)) {
			t.Errorf("run %d (%q) carries no foreground", i, visible(segment))
		}
		if !strings.Contains(segment, background(theme.ColorBackground)) {
			t.Errorf("run %d (%q) carries no background", i, visible(segment))
		}
	}
	if checked == 0 {
		t.Fatal("no run carried any text, so this test asserted nothing")
	}
}

// Une colonne qui ne déclare qu'un fond reçoit le texte du thème — le symétrique
// exact de TestAColumnThatDeclaresNoBackgroundGetsTheAppOne.
func TestAColumnThatDeclaresNoForegroundGetsTheThemeOne(t *testing.T) {
	withTrueColor(t)

	cfg := testConfig()
	cfg.Columns[2].Style = backgroundOnlyStyle
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if got := rowLines(&m)[1]; !strings.Contains(got, foreground(theme.ColorText)) {
		t.Errorf("a column declaring only a background renders its text in the terminal's colour: %q", got)
	}
}

// **Et c'est ce qui interdit de poser la couleur sur `styles.Cell`.** Les
// cellules sont rendues, puis la ligne entière est passée à `styles.Selected` :
// une couleur de cellule y ouvrirait une séquence dont le reset referme le
// surlignage au milieu de la ligne. Le surlignage répond à « où suis-je », et
// aucune couleur de colonne ne vaut de le perdre.
func TestTheSelectedRowKeepsItsHighlightWhole(t *testing.T) {
	withTrueColor(t)
	m := colouredTable(t) // la colonne State est colorée, et la ligne 0 est sous le curseur

	selected := rowLines(&m)[0]

	if strings.Contains(selected, foreground(theme.ColorText)) {
		t.Error("the selected row carries a cell foreground, which would end the highlight at its reset")
	}
	if strings.Contains(selected, foreground(theme.ColorError)) || strings.Contains(selected, foreground(theme.ColorOK)) {
		t.Error("a column colour reached the selected row; styles.Selected must own it whole")
	}
	if !strings.Contains(selected, background(theme.ColorTableSelectedBg)) {
		t.Errorf("the selected row does not carry the selection background: %q", selected)
	}
	// Un seul reset, tout à la fin : c'est ce que « le surlignage entier » veut
	// dire, et ce qu'un foreground par cellule casserait.
	if n := strings.Count(selected, "\x1b[0m"); n != 1 {
		t.Errorf("the selected row closes %d times, want one reset at its end", n)
	}
}
